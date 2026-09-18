package engine

import "sigame/internal/packs"

// Snapshot is a role-projected view of the game for (re)connecting clients.
// Timer deadlines are the room's business: the snapshot carries every timer's
// kind, total duration, start (room clock) and pause state.
type Snapshot struct {
	Role     Role   `json:"role"`
	PersonID string `json:"personId"`

	Stage      Stage           `json:"stage"`
	Sub        QSub            `json:"sub,omitempty"`
	RoundIndex int             `json:"roundIndex"`
	RoundName  string          `json:"roundName,omitempty"`
	RoundType  packs.RoundType `json:"roundType,omitempty"`
	Table      TablePayload    `json:"table"`
	ChooserID  string          `json:"chooserId,omitempty"`
	Players    []PlayerInfo    `json:"players"`
	Scores     []ScoreEntry    `json:"scores"`
	Rules      Rules           `json:"rules"`
	Times      TimeSettings    `json:"times"`
	Paused     bool            `json:"paused"`
	WaitNext   bool            `json:"waitNext"`
	Timers     []TimerView     `json:"timers"`
	Winner     string          `json:"winner,omitempty"`
	Ended      bool            `json:"ended"`

	Question *QuestionView `json:"question,omitempty"`
	Pending  *PendingView  `json:"pending,omitempty"`
	Appeal   *AppealView   `json:"appeal,omitempty"`

	// Prompts outstanding for this person (nil when none).
	AskChoose      *AskChoosePayload      `json:"askChoose,omitempty"`
	AskAnswer      *AskAnswerPayload      `json:"askAnswer,omitempty"`
	AskStake       *AskStakePayload       `json:"askStake,omitempty"`
	AskValidate    *AskValidatePayload    `json:"askValidate,omitempty"`
	AskDeleteTheme *AskDeleteThemePayload `json:"askDeleteTheme,omitempty"`
	AskAppealVote  *AskAppealVotePayload  `json:"askAppealVote,omitempty"`
}

// TimerView describes a timer for resume.
type TimerView struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DurationMs  int64  `json:"durationMs"`
	StartedAtMs int64  `json:"startedAtMs"`
	Paused      bool   `json:"paused"`
	RemainingMs int64  `json:"remainingMs"` // valid when paused
	PersonID    string `json:"personId,omitempty"`
}

// PendingView is an outstanding SELECT_PLAYER decision.
type PendingView struct {
	Kind       PendingKind `json:"kind"`
	DeciderID  string      `json:"deciderId"`
	Candidates []string    `json:"candidates"`
}

// AppealView is the running appeal.
type AppealView struct {
	Kind        string   `json:"kind"`
	AppellantID string   `json:"appellantId"`
	AnswererID  string   `json:"answererId"`
	Answer      string   `json:"answer"`
	For         int      `json:"for"`
	Against     int      `json:"against"`
	Pending     []string `json:"pending"`
}

// OutcomeView is a scored answer.
type OutcomeView struct {
	PersonID string  `json:"personId"`
	Answer   string  `json:"answer,omitempty"`
	Right    bool    `json:"right"`
	Factor   float64 `json:"factor"`
	Delta    int     `json:"delta"`
	Reverted bool    `json:"reverted,omitempty"`
}

// StakesView is the visible bidding state.
type StakesView struct {
	Nominal int            `json:"nominal"`
	Step    int            `json:"step"`
	Stake   int            `json:"stake"`
	Leader  string         `json:"leader,omitempty"`
	Current string         `json:"current,omitempty"`
	Bids    map[string]int `json:"bids"`
	Passed  []string       `json:"passed"`
	AllIn   []string       `json:"allIn"`
}

// QuestionView is the projected question play state.
type QuestionView struct {
	QuestionID    string                `json:"questionId"`
	ThemeIndex    int                   `json:"themeIndex"`
	QuestionIndex int                   `json:"questionIndex"`
	Theme         string                `json:"theme"`
	Price         int                   `json:"price"`
	CurPrice      int                   `json:"curPrice"`
	Type          packs.QuestionType    `json:"type"`
	IsDefault     bool                  `json:"isDefault"`
	AnswerType    packs.AnswerType      `json:"answerType"`
	Sub           QSub                  `json:"sub"`
	AnswererID    string                `json:"answererId,omitempty"`
	Answerers     []string              `json:"answerers,omitempty"`
	Content       []ContentPayload      `json:"content"`
	Options       *AnswerOptionsPayload `json:"options,omitempty"`
	Excluded      []string              `json:"excludedOptions,omitempty"`
	RightAnswer   *RightAnswerPayload   `json:"rightAnswer,omitempty"`
	Stakes        *StakesView           `json:"stakes,omitempty"`
	HiddenStakes  map[string]int        `json:"hiddenStakes,omitempty"` // showman: all; player: own
	HiddenAnswers map[string]AnswerView `json:"hiddenAnswers,omitempty"`
	Rights        []string              `json:"rights,omitempty"` // showman only
	Wrongs        []string              `json:"wrongs,omitempty"` // showman only
	History       []OutcomeView         `json:"history"`
	Armed         bool                  `json:"armed"`
	ThinkingMs    int64                 `json:"thinkingRemainingMs"`
	Eligible      []string              `json:"eligiblePlayerIds,omitempty"`
}

// Snapshot builds the projection for role/personID. Players and viewers never
// see right answers, showman comments, other players' hidden stakes or
// unrevealed written answers.
func (g *Game) Snapshot(role Role, personID string) Snapshot {
	st := g.st
	s := Snapshot{Role: role, PersonID: personID, Stage: st.Stage, RoundIndex: st.RoundIndex, RoundName: st.RoundName,
		RoundType: st.RoundType, Table: g.tablePayload(), ChooserID: st.ChooserID, Players: g.playerInfos(), Scores: g.scores(),
		Rules: st.Rules, Times: st.Times, Paused: st.Paused, WaitNext: st.WaitNext, Winner: st.Winner, Ended: st.Ended, Timers: []TimerView{}}
	for _, t := range st.Timers {
		s.Timers = append(s.Timers, TimerView{ID: t.ID, Kind: t.Kind, DurationMs: t.DurationMs, StartedAtMs: t.StartedAtMs, Paused: t.Paused, RemainingMs: t.RemainingMs, PersonID: t.PersonID})
	}
	isShowman := role == RoleShowman
	if pd := st.Pending; pd != nil {
		s.Pending = &PendingView{Kind: pd.Kind, DeciderID: pd.DeciderID, Candidates: pd.Candidates}
	}
	if a := st.Appeal; a != nil {
		f, ag := g.appealCounts()
		answer := st.Question.History[a.OutcomeIdx].Answer
		s.Appeal = &AppealView{Kind: string(a.Kind), AppellantID: a.AppellantID, AnswererID: a.AnswererID, Answer: answer, For: f, Against: ag, Pending: append([]string{}, a.Pending...)}
		for _, v := range a.Pending {
			if v == personID {
				s.AskAppealVote = &AskAppealVotePayload{Kind: string(a.Kind), AnswererID: a.AnswererID, Answer: answer, Rights: st.Question.Rights}
			}
		}
	}
	if st.Stage == StageSelecting && st.Pending == nil && personID == st.ChooserID && !g.sequential() {
		s.AskChoose = &AskChoosePayload{PersonID: st.ChooserID, DurationMs: st.Times.QuestionSelectionMs}
	}
	if st.Stage == StageFinalThemes && st.DeleterID == personID {
		s.AskDeleteTheme = &AskDeleteThemePayload{PersonID: personID, Themes: g.finalRemainingThemes(), DurationMs: st.Times.ThemeSelectionMs}
	}
	q := st.Question
	if q == nil || st.Stage != StageQuestion {
		return s
	}
	s.Sub = q.Sub
	qv := &QuestionView{QuestionID: q.ID, ThemeIndex: q.ThemeIndex, QuestionIndex: q.QuestionIndex, Theme: q.Caption, Price: q.Price,
		CurPrice: q.CurPriceRight, Type: q.Type, IsDefault: q.IsDefault, AnswerType: q.AnswerType, Sub: q.Sub, AnswererID: q.AnswererID,
		Answerers: q.Answerers, Content: []ContentPayload{}, History: []OutcomeView{}, Armed: q.Armed, ThinkingMs: q.ThinkingRemain}
	if q.Armed {
		qv.Eligible = g.eligibleIDs()
	}
	if q.Sub != SubAnnouncing && q.Sub != SubSecretTransfer && q.Sub != SubPriceSelect && q.Sub != SubStakes {
		for i := 0; i < len(q.Groups) && i <= q.Cursor; i++ {
			qv.Content = append(qv.Content, ContentPayload{Phase: "question", Index: i, Items: viewItems(q.Groups[i].Items), WaitMs: q.Groups[i].WaitMs})
		}
	}
	if q.OptionsShown {
		opts := make([]AnswerOptionView, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, AnswerOptionView{Label: o.Label, Content: viewItems(o.Content)})
		}
		qv.Options = &AnswerOptionsPayload{Options: opts, ShowLabels: st.Rules.DisplayAnswerOptionsLabels, OneByOne: st.Rules.DisplayAnswerOptionsOneByOne}
		for l := range q.ExcludedOptions {
			qv.Excluded = append(qv.Excluded, l)
		}
	}
	if q.Sub == SubReveal || q.Sub == SubEnd {
		text := ""
		if len(q.Rights) > 0 {
			text = q.Rights[0]
		}
		qv.RightAnswer = &RightAnswerPayload{Text: text, Items: viewItems(q.AnswerItems), Comments: q.Comments}
	}
	if isShowman {
		qv.Rights, qv.Wrongs = q.Rights, q.Wrongs
	}
	for _, o := range q.History {
		ov := OutcomeView{PersonID: o.PersonID, Right: o.Right, Factor: o.Factor, Delta: o.Delta, Reverted: o.Reverted}
		if isShowman || !q.Hidden || o.PersonID == personID {
			ov.Answer = o.Answer
		}
		qv.History = append(qv.History, ov)
	}
	if ss := q.Stakes; ss != nil {
		sv := &StakesView{Nominal: ss.Nominal, Step: ss.Step, Stake: ss.Stake, Leader: ss.Leader, Current: ss.Current, Bids: map[string]int{}, Passed: []string{}, AllIn: []string{}}
		for _, id := range ss.Participants {
			if b, ok := ss.Bids[id]; ok {
				sv.Bids[id] = b
			}
			if !ss.Active[id] {
				sv.Passed = append(sv.Passed, id)
			}
			if ss.AllIn[id] {
				sv.AllIn = append(sv.AllIn, id)
			}
		}
		qv.Stakes = sv
		if q.Sub == SubStakes && ss.Current == personID {
			s.AskStake = &AskStakePayload{PersonID: personID, Modes: ss.AllowedModes, Min: ss.Min, Max: ss.Max, Step: ss.Step, Reason: "stake", DurationMs: st.Times.StakeMakingMs}
		}
	}
	if q.Hidden {
		revealed := map[string]bool{}
		for i := 0; i < q.RevealIdx && i < len(q.Answerers); i++ {
			revealed[q.Answerers[i]] = true
		}
		for _, id := range q.Answerers {
			p := st.player(id)
			if p == nil {
				continue
			}
			visible := isShowman || id == personID || revealed[id]
			if q.HiddenAll && p.StakeMade && visible {
				if qv.HiddenStakes == nil {
					qv.HiddenStakes = map[string]int{}
				}
				qv.HiddenStakes[id] = p.Stake
			}
			if p.Answered && visible {
				if qv.HiddenAnswers == nil {
					qv.HiddenAnswers = map[string]AnswerView{}
				}
				qv.HiddenAnswers[id] = p.Answer
			}
		}
		if p := st.player(personID); p != nil && p.InGame && g.isAnswerer(personID) {
			if q.Sub == SubStakes && q.HiddenAll && !p.StakeMade {
				s.AskStake = &AskStakePayload{PersonID: personID, Modes: []StakeMode{StakeStake, StakeAllIn}, Min: 1, Max: p.Score, Step: 1, Reason: "hiddenStake", DurationMs: st.Times.StakeMakingMs}
			}
			if q.Sub == SubHiddenAnswering && !p.Answered && p.Active() {
				s.AskAnswer = &AskAnswerPayload{PersonID: personID, AnswerType: q.AnswerType, DurationMs: g.answerDuration(st.Times.HiddenAnsweringMs), Hidden: true}
			}
		}
	}
	if q.Sub == SubPriceSelect && q.AnswererID == personID && len(q.PriceChoices) > 1 {
		s.AskStake = &AskStakePayload{PersonID: personID, Modes: []StakeMode{StakeStake}, Min: q.PriceChoices[0], Max: q.PriceChoices[len(q.PriceChoices)-1], Step: q.PriceChoices[1] - q.PriceChoices[0], Reason: "secretPrice", DurationMs: st.Times.StakeMakingMs}
	}
	if q.Sub == SubAnswering && q.AnswererID == personID && q.Known {
		def := st.Times.SoloAnsweringMs
		if q.Button {
			def = st.Times.AnsweringMs
		}
		s.AskAnswer = &AskAnswerPayload{PersonID: personID, AnswerType: q.AnswerType, DurationMs: g.answerDuration(def), Oral: q.PendingAnswerID != ""}
	}
	if isShowman && q.PendingAnswerID != "" && (q.Sub == SubValidating || q.Sub == SubAnswering) {
		s.AskValidate = &AskValidatePayload{PersonID: q.PendingAnswerID, Answer: q.PendingAnswer, Rights: q.Rights, Wrongs: q.Wrongs, AllowFactor: true, Oral: q.Sub == SubAnswering, AutoAfterMs: st.Times.ShowmanDecisionMs}
	}
	s.Question = qv
	return s
}
