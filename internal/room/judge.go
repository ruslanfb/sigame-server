package room

import (
	"context"
	"strings"
	"time"

	"sigame/internal/ai"
	"sigame/internal/engine"
	"sigame/internal/packs"
)

// Automatic validation.
//
//   - ai mode: every ASK_VALIDATE goes to the judge in a goroutine; the verdict
//     is applied as CmdValidate{Source:"ai"} when it arrives and the answer is
//     still pending. AI_VERDICT{applied:true} is broadcast.
//   - hybrid mode: the human showman keeps ASK_VALIDATE; the AI verdict is
//     forwarded as AI_SUGGESTION (engine) plus AI_VERDICT{applied:false} to the
//     staff, and applied automatically after HybridConfirmMs without a human
//     verdict.
//   - VALIDATION_TIMEOUT (any mode): the fuzzy judge decides with Source "auto".
//
// Uncertain verdicts are rejected answers: with FuzzyUncertainAsWrong they
// carry the normal penalty, otherwise factor 0 (no score change).

func (r *Room) judgesAutomatically() bool {
	return r.showman == ShowmanAI || r.showman == ShowmanHybrid
}

// validationPending reports whether the engine still waits for a verdict on
// (personID, answer). Hidden-answer previews are pending while the written
// phase runs and the player's answer is unchanged.
func (r *Room) validationPending(personID, answer string, preview bool) bool {
	if r.game == nil {
		return false
	}
	st := r.game.State()
	q := st.Question
	if q == nil || st.Stage != engine.StageQuestion {
		return false
	}
	if preview {
		if !q.Hidden || !(q.Sub == engine.SubHiddenAnswering || q.Sub == engine.SubStakes || q.Sub == engine.SubContent) {
			return false
		}
		gp := r.gamePlayer(personID)
		return gp != nil && gp.Answered && gp.Answer.Text == answer
	}
	return q.PendingAnswerID == personID && q.PendingAnswer == answer && (q.Sub == engine.SubValidating || q.Sub == engine.SubAnswering)
}

func (r *Room) packQuestion() *packs.Question {
	if r.game == nil {
		return nil
	}
	st := r.game.State()
	q := st.Question
	if q == nil || st.RoundIndex < 0 || st.RoundIndex >= len(r.pack.Rounds) {
		return nil
	}
	rd := &r.pack.Rounds[st.RoundIndex]
	if q.ThemeIndex < 0 || q.ThemeIndex >= len(rd.Themes) {
		return nil
	}
	th := &rd.Themes[q.ThemeIndex]
	if q.QuestionIndex < 0 || q.QuestionIndex >= len(th.Questions) {
		return nil
	}
	pq := &th.Questions[q.QuestionIndex]
	if pq.ID != "" && q.ID != "" && pq.ID != q.ID {
		return nil
	}
	return pq
}

// judgeRequest builds the ai.Request for the current question.
func (r *Room) judgeRequest(answer string, rights, wrongs []string) ai.Request {
	req := ai.Request{Right: rights, Wrong: wrongs, PlayerAnswer: answer, Language: r.language}
	if req.Language == "" {
		req.Language = r.pack.Language
	}
	if r.game == nil {
		return req
	}
	q := r.game.State().Question
	if q == nil {
		return req
	}
	req.QuestionID = q.ID
	req.Theme = q.Caption
	req.AnswerType = string(q.AnswerType)
	var texts []string
	for _, g := range q.Groups {
		for _, it := range g.Items {
			if it.Type == packs.ContentText && strings.TrimSpace(it.Text) != "" {
				texts = append(texts, strings.TrimSpace(it.Text))
			}
		}
	}
	req.QuestionText = strings.Join(texts, "\n")
	if pq := r.packQuestion(); pq != nil {
		req.Deviation = pq.Params.AnswerDeviation
		req.ShowmanComments = pq.Info.ShowmanComments
	}
	return req
}

// onAskValidate starts an asynchronous judge call for ai/hybrid rooms.
func (r *Room) onAskValidate(ask engine.AskValidatePayload) {
	if ask.Oral {
		return // oral game: the (human) showman decides by ear
	}
	req := r.judgeRequest(ask.Answer, ask.Rights, ask.Wrongs)
	judge := r.m.judge()
	timeout := r.m.deps.Cfg.AITimeout
	if timeout <= 0 {
		timeout = defaultAITimeout
	}
	r.m.wg.Add(1)
	go func() {
		defer r.m.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		v, err := judge.Judge(ctx, req)
		if err != nil {
			v, _ = ai.FuzzyJudge{}.Judge(ctx, req)
			v.Source = ai.SourceFallback
			v.Reason = "judge unavailable (" + err.Error() + "); " + v.Reason
		}
		r.post(func() { r.onAIVerdict(ask, v) })
	}()
}

// normaliseVerdict maps a judge verdict to (right, factor).
func (r *Room) normaliseVerdict(v ai.Verdict) (right bool, factor float64) {
	if v.Uncertain {
		if r.m.deps.Cfg.FuzzyUncertainAsWrong {
			return false, 1
		}
		return false, 0
	}
	if v.Right {
		if v.Factor <= 0 || v.Factor > 1 {
			return true, 1
		}
		return true, v.Factor
	}
	return false, 1
}

func (r *Room) validateCmd(personID string, right bool, factor float64, source string) engine.Command {
	return engine.Command{Type: engine.CmdValidate, Actor: systemActor, PersonID: personID, Right: right, Factor: factor, FactorZero: !right && factor == 0, Source: source}
}

func (r *Room) onAIVerdict(ask engine.AskValidatePayload, v ai.Verdict) {
	if r.closing || !r.validationPending(ask.PersonID, ask.Answer, ask.Preview) {
		return
	}
	right, factor := r.normaliseVerdict(v)
	human := r.humanShowman()
	if r.showman == ShowmanHybrid && human != nil {
		r.submit(engine.Command{Type: engine.CmdAISuggestion, Actor: systemActor, PersonID: ask.PersonID, Right: right, Factor: factor, Reason: v.Reason})
		r.announceVerdict(ask.PersonID, right, factor, v, false, ask.Preview)
		if ask.Preview {
			return
		}
		key := r.validationKey(ask.PersonID, ask.Answer)
		r.cancelHybrid()
		r.hybridKey = key
		wait := r.hybridConfirmMs
		if wait <= 0 {
			wait = defaultHybridConfirmMs
		}
		r.hybridTimer = r.after(time.Duration(wait)*time.Millisecond, func() { r.applyHybrid(key, ask, right, factor, v) })
		return
	}
	r.submit(r.validateCmd(ask.PersonID, right, factor, "ai"))
	r.announceVerdict(ask.PersonID, right, factor, v, true, ask.Preview)
}

// announceVerdict tells the showman everything (verdict, factor, reason —
// the reason may quote the accepted answers) and everyone else only that an
// automatic judgement was applied. Nothing about a hidden (preview) answer
// leaves the showman audience before the reveal.
func (r *Room) announceVerdict(personID string, right bool, factor float64, v ai.Verdict, applied, preview bool) {
	full := marshal(AIVerdictPayload{PersonID: personID, Right: right, Factor: factor, Uncertain: v.Uncertain, Reason: v.Reason, Source: v.Source, Applied: applied})
	r.broadcastRaw(MsgAIVerdict, full, isShowmanRole)
	if !applied || preview {
		return
	}
	pub := marshal(AIVerdictPublic{PersonID: personID, Source: v.Source, Applied: true})
	r.broadcastRaw(MsgAIVerdict, pub, func(p *person) bool { return !isShowmanRole(p) })
}

func (r *Room) validationKey(personID, answer string) string {
	qid := ""
	if r.game != nil {
		if q := r.game.State().Question; q != nil {
			qid = q.ID
		}
	}
	return qid + "\x00" + personID + "\x00" + answer
}

func (r *Room) applyHybrid(key string, ask engine.AskValidatePayload, right bool, factor float64, v ai.Verdict) {
	if r.hybridKey != key {
		return
	}
	r.hybridTimer, r.hybridKey = nil, ""
	if r.closing || !r.validationPending(ask.PersonID, ask.Answer, false) {
		return
	}
	r.submit(r.validateCmd(ask.PersonID, right, factor, "ai"))
	r.announceVerdict(ask.PersonID, right, factor, v, true, false)
}

func (r *Room) cancelHybrid() {
	if r.hybridTimer != nil {
		r.hybridTimer.Stop()
		r.hybridTimer = nil
	}
	r.hybridKey = ""
}

// onValidationTimeout applies the fuzzy judge (Source "auto").
func (r *Room) onValidationTimeout(p engine.ValidationTimeoutPayload) {
	r.cancelHybrid()
	req := r.judgeRequest(p.Answer, p.Rights, p.Wrongs)
	v, err := ai.FuzzyJudge{}.Judge(context.Background(), req)
	if err != nil {
		v = ai.Verdict{Uncertain: true, Source: ai.SourceFuzzy, Reason: err.Error()}
	}
	right, factor := r.normaliseVerdict(v)
	r.enqueue(r.validateCmd(p.PersonID, right, factor, "auto"))
	r.announceVerdict(p.PersonID, right, factor, v, true, false)
}
