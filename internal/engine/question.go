package engine

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"sigame/internal/packs"
)

// ---- question start --------------------------------------------------------

func (g *Game) startQuestion(t, q int) {
	st := g.st
	pq := g.packQuestion(t, q)
	st.Table[t].Questions[q].Played = true
	qtype := pq.Type
	isDefault := false
	if qtype == "" {
		qtype = g.defaultQuestionType()
		isDefault = true
	}
	at := pq.Params.AnswerType
	if at == "" {
		at = packs.AnswerText
	}
	qs := &QuestionState{
		ThemeIndex: t, QuestionIndex: q, ID: pq.ID, Type: qtype, IsDefault: isDefault, Known: qtype.IsKnown(),
		AnswerType: at, Price: pq.Price, CurPriceRight: pq.Price, CurPriceWrong: pq.Price, Sub: SubAnnouncing,
		Caption: st.Table[t].Name, IsFinal: st.RoundType == packs.RoundFinal, Phase: "question",
		Rights: pq.Right, Wrongs: pq.Wrong, AnswerItems: pq.Params.Answer, Comments: pq.Info.Comments,
		Options: pq.Params.AnswerOptions, ExcludedOptions: map[string]bool{}, ValidationCache: map[string]validation{},
		PreVerdicts: map[string]validation{},
	}
	if qs.Price < 0 {
		qs.Price, qs.CurPriceRight, qs.CurPriceWrong = 0, 0, 0
	}
	if st.Rules.IgnoreWrong {
		qs.CurPriceWrong = 0
	}
	st.Question = qs
	st.Stage = StageQuestion
	st.Pending = nil
	for _, p := range st.Players {
		p.State = PStateNone
		p.CanPress = false
		p.InGame = false
		g.resetPlayerQuestion(p)
	}
	items := pq.Params.Question
	if st.Rules.PrependThemeCommentsToQuestion {
		if c := g.round().Themes[t].Info.Comments; c != "" {
			items = append([]packs.ContentItem{{Type: packs.ContentText, Text: c}}, items...)
		}
	}
	qs.Groups = g.buildGroups(items)
	g.emitStage()
	g.all(EvQuestionStart, QuestionStartPayload{QuestionID: pq.ID, ThemeIndex: t, QuestionIndex: q, Theme: st.Table[t].Name,
		Price: qs.Price, Type: qtype, IsDefault: isDefault, AnswerType: at})
	if st.Rules.HintShowman || pq.Info.ShowmanComments != "" {
		h := ShowmanHintPayload{ShowmanComments: pq.Info.ShowmanComments, Comments: pq.Info.Comments}
		if st.Rules.HintShowman {
			h.Rights, h.Wrongs = pq.Right, pq.Wrong
		}
		g.showman(EvShowmanHint, h)
	}
	g.emitTable()
	switch qtype {
	case packs.QSimple:
		g.simpleBegin()
	case packs.QStake:
		g.stakeBegin()
	case packs.QSecret:
		g.secretBegin(false, false)
	case packs.QSecretPublicPrice:
		g.secretBegin(true, false)
	case packs.QSecretNoQuestion:
		g.secretBegin(false, true)
	case packs.QNoRisk:
		g.noRiskBegin()
	case packs.QForAll:
		g.forAllBegin()
	case packs.QStakeAll:
		g.stakeAllBegin()
	default:
		g.customBegin()
	}
}

// wrongPrice applies a penalty class to a price.
func (g *Game) wrongPrice(class Penalty, price int) int {
	if g.st.Rules.IgnoreWrong || class == PenaltyNone {
		return 0
	}
	return price
}

// currentChooser returns the chooser, falling back to the lowest-score active
// player in modes without a chooser.
func (g *Game) currentChooser() string {
	if p := g.st.player(g.st.ChooserID); p != nil && p.Active() {
		return p.ID
	}
	if low := g.lowestScorePlayers(); len(low) > 0 {
		return low[0].ID
	}
	return ""
}

// ---- content ---------------------------------------------------------------

func viewItems(items []packs.ContentItem) []ContentItemView {
	out := make([]ContentItemView, 0, len(items))
	for _, it := range items {
		out = append(out, ContentItemView{Type: it.Type, Text: it.Text, MediaID: it.MediaID, URL: it.URL,
			Placement: it.EffectivePlacement(), DurationMs: it.DurationMs})
	}
	return out
}

func (g *Game) itemDuration(it packs.ContentItem) int64 {
	if it.DurationMs > 0 {
		return it.DurationMs
	}
	switch it.Type {
	case packs.ContentText:
		if g.st.Rules.ReadingSpeed <= 0 {
			return 0
		}
		n := utf8.RuneCountInString(it.Text)
		ms := int64(math.Ceil(float64(n) * 1000 / float64(g.st.Rules.ReadingSpeed)))
		return ms + g.st.Times.ReflectionMs
	case packs.ContentImage, packs.ContentHTML:
		return g.st.Times.ImageMs
	default: // audio / video with unknown duration
		return -1
	}
}

// buildGroups chains NoWait items with the following item into one group and
// computes the planned wait of every group.
func (g *Game) buildGroups(items []packs.ContentItem) []ContentGroup {
	var groups []ContentGroup
	open := false
	for _, it := range items {
		if !open {
			groups = append(groups, ContentGroup{})
			open = true
		}
		cur := &groups[len(groups)-1]
		cur.Items = append(cur.Items, it)
		if !it.NoWait {
			open = false
		}
	}
	for i := range groups {
		grp := &groups[i]
		for _, it := range grp.Items {
			d := g.itemDuration(it)
			if d < 0 {
				grp.MediaWait = true
				grp.MediaID, grp.MediaURL = it.MediaID, it.URL
				continue
			}
			if d > grp.WaitMs {
				grp.WaitMs = d
			}
		}
	}
	return groups
}

func (g *Game) playContent() {
	q := g.st.Question
	q.Cursor = 0
	q.ContentDone = false
	g.setSub(SubContent)
	g.showGroup()
}

func (g *Game) showGroup() {
	st := g.st
	q := st.Question
	if q.Cursor >= len(q.Groups) {
		g.contentFinished()
		return
	}
	grp := q.Groups[q.Cursor]
	g.all(EvContent, ContentPayload{Phase: q.Phase, Index: q.Cursor, Items: viewItems(grp.Items), WaitMs: grp.WaitMs})
	if st.Rules.Managed {
		q.WaitNext = true
		return
	}
	if grp.MediaWait {
		q.AwaitingMediaIDs = true
		for _, p := range st.Players {
			p.MediaDone = false
		}
		g.all(EvMediaWait, MediaWaitPayload{MediaID: grp.MediaID, URL: grp.MediaURL})
		g.startTimer(TimerMediaFallback, grp.WaitMs, "")
		return
	}
	if grp.WaitMs <= 0 {
		q.WaitNext = true
		return
	}
	g.startTimer(TimerContent, grp.WaitMs, "")
}

// contentGroupDone advances to the next content group.
func (g *Game) contentGroupDone() {
	q := g.st.Question
	if q == nil || q.Sub != SubContent {
		return
	}
	q.WaitNext = false
	q.AwaitingMediaIDs = false
	q.Cursor++
	g.showGroup()
}

func (g *Game) allMediaDone() bool {
	for _, p := range g.st.activePlayers() {
		if !p.MediaDone {
			return false
		}
	}
	return true
}

func (g *Game) onMediaCompleted(cmd Command) error {
	st := g.st
	q := st.Question
	if q == nil || st.Stage != StageQuestion || q.Sub != SubContent || !q.AwaitingMediaIDs || st.Paused {
		return nil
	}
	if cmd.Actor.Role == RoleSystem && cmd.PersonID == "" {
		g.stopKind(TimerMediaFallback)
		g.contentGroupDone()
		return nil
	}
	id := cmd.PersonID
	if id == "" {
		id = cmd.Actor.PersonID
	}
	p := st.player(id)
	if p == nil {
		return nil
	}
	p.MediaDone = true
	if g.allMediaDone() {
		g.stopKind(TimerMediaFallback)
		g.contentGroupDone()
	}
	return nil
}

// contentFinished runs after the last question content group.
func (g *Game) contentFinished() {
	st := g.st
	q := st.Question
	q.ContentDone = true
	if q.AnswerType == packs.AnswerSelect && !q.OptionsShown {
		q.OptionsShown = true
		opts := make([]AnswerOptionView, 0, len(q.Options))
		for _, o := range q.Options {
			opts = append(opts, AnswerOptionView{Label: o.Label, Content: viewItems(o.Content)})
		}
		g.all(EvAnswerOptions, AnswerOptionsPayload{Options: opts, ShowLabels: st.Rules.DisplayAnswerOptionsLabels, OneByOne: st.Rules.DisplayAnswerOptionsOneByOne})
	}
	switch {
	case q.Button:
		if !g.anyCanPress() {
			g.showAnswer()
			return
		}
		if q.Armed { // false starts disabled: buttons were armed before content
			g.setSub(SubButtonWait)
			g.startTimer(TimerButtonPressing, q.ThinkingRemain, "")
			return
		}
		g.armButton(false)
	case q.Hidden:
		g.hiddenAnsweringBegin()
	case q.Direct:
		g.askDirectAnswer()
	default: // custom: the showman scores manually and ends with Next
		g.setSub(SubAnswering)
	}
}

// ---- simple (button) -------------------------------------------------------

func (g *Game) simpleBegin() {
	st := g.st
	q := st.Question
	q.Button = true
	q.CurPriceWrong = g.wrongPrice(st.Rules.ButtonPenalty, q.Price)
	q.ThinkingRemain = st.Times.ButtonPressingMs
	for _, p := range st.activePlayers() {
		p.CanPress = true
	}
	if !st.Rules.FalseStart {
		q.Armed = true
		g.room(EvButtonArmRequest, ButtonArmRequestPayload{QuestionID: q.ID, EligiblePlayerIDs: g.eligibleIDs(), ThinkingRemainingMs: -1})
	}
	g.playContent()
}

func (g *Game) eligibleIDs() []string {
	var out []string
	for _, p := range g.st.activePlayers() {
		if p.CanPress {
			out = append(out, p.ID)
		}
	}
	return out
}

func (g *Game) anyCanPress() bool { return len(g.eligibleIDs()) > 0 }

// armButton opens the button after content (or re-arms it after a wrong answer).
func (g *Game) armButton(rearm bool) {
	q := g.st.Question
	q.Armed = true
	g.setSub(SubButtonWait)
	g.room(EvButtonArmRequest, ButtonArmRequestPayload{QuestionID: q.ID, EligiblePlayerIDs: g.eligibleIDs(), ThinkingRemainingMs: q.ThinkingRemain, Rearm: rearm})
	if t := g.timerOfKind(TimerButtonPressing); t != nil && t.Paused {
		g.resumeTimer(t)
		return
	}
	g.startTimer(TimerButtonPressing, q.ThinkingRemain, "")
}

func (g *Game) onButtonResult(cmd Command) error {
	if err := g.requireSystem(cmd.Actor); err != nil {
		return err
	}
	st := g.st
	q := st.Question
	if q == nil || st.Stage != StageQuestion || !q.Button || !q.Armed || st.Paused {
		return nil // stale decision
	}
	if cmd.Nobody {
		if q.Sub == SubButtonWait {
			g.stopKind(TimerButtonPressing)
			g.nobodyAnswered()
		}
		return nil
	}
	p := st.player(cmd.WinnerID)
	if p == nil || !p.Active() || !p.CanPress {
		return fmt.Errorf("%w: %q cannot press", ErrBadArgument, cmd.WinnerID)
	}
	q.Armed = false
	p.CanPress = false
	switch q.Sub {
	case SubContent:
		for _, k := range []string{TimerContent, TimerMediaFallback} {
			if t := g.timerOfKind(k); t != nil {
				g.pauseTimer(t)
			}
		}
		g.all(EvContentState, ContentStatePayload{State: "paused"})
	case SubButtonWait:
		if t := g.timerOfKind(TimerButtonPressing); t != nil {
			g.pauseTimer(t)
			q.ThinkingRemain = t.RemainingMs
		}
	}
	g.askAnswer(p, TimerAnswering, st.Times.AnsweringMs)
	return nil
}

func (g *Game) nobodyAnswered() {
	q := g.st.Question
	if q.Armed {
		q.Armed = false
		g.room(EvButtonDisarm, ButtonDisarmRequestPayload{QuestionID: q.ID})
	}
	g.showAnswer()
}

func (g *Game) onPass(cmd Command) error {
	st := g.st
	q := st.Question
	if q == nil || st.Stage != StageQuestion {
		return fmt.Errorf("%w: no question", ErrBadState)
	}
	if q.Sub == SubStakes && q.Stakes != nil {
		p, err := g.actingPlayer(cmd, q.Stakes.Current)
		if err != nil {
			return err
		}
		if !q.Stakes.Active[p.ID] {
			return fmt.Errorf("%w: not bidding", ErrBadState)
		}
		if q.Stakes.Leader == p.ID {
			return fmt.Errorf("%w: the highest bidder cannot pass", ErrNotAllowed)
		}
		if q.Stakes.Current == p.ID && q.Stakes.Stake == 0 {
			return fmt.Errorf("%w: the opener cannot pass", ErrNotAllowed)
		}
		g.stakeSetPass(p, true)
		return nil
	}
	p, err := g.playerActor(cmd.Actor)
	if err != nil {
		return err
	}
	if !q.Button || !(q.Sub == SubContent || q.Sub == SubButtonWait) || !p.CanPress {
		return fmt.Errorf("%w: cannot pass now", ErrBadState)
	}
	p.CanPress = false
	g.all(EvPass, PassPayload{PersonID: p.ID})
	g.setPlayerState(p, PStatePass)
	if !g.anyCanPress() {
		if q.Sub == SubButtonWait {
			g.stopKind(TimerButtonPressing)
			g.nobodyAnswered()
		} else if q.Armed {
			q.Armed = false
			g.room(EvButtonDisarm, ButtonDisarmRequestPayload{QuestionID: q.ID})
		}
		return nil
	}
	if q.Armed {
		g.room(EvButtonArmRequest, ButtonArmRequestPayload{QuestionID: q.ID, EligiblePlayerIDs: g.eligibleIDs(), ThinkingRemainingMs: q.ThinkingRemain, Rearm: true})
	}
	return nil
}

// rearm lets the remaining players press again after a wrong answer.
func (g *Game) rearm() {
	q := g.st.Question
	if !q.ContentDone {
		q.Armed = true
		g.setSub(SubContent)
		g.all(EvContentState, ContentStatePayload{State: "resumed"})
		for _, k := range []string{TimerContent, TimerMediaFallback} {
			if t := g.timerOfKind(k); t != nil {
				g.resumeTimer(t)
			}
		}
		g.room(EvButtonArmRequest, ButtonArmRequestPayload{QuestionID: q.ID, EligiblePlayerIDs: g.eligibleIDs(), ThinkingRemainingMs: -1, Rearm: true})
		if q.WaitNext || q.AwaitingMediaIDs {
			return
		}
		if g.timerOfKind(TimerContent) == nil && g.timerOfKind(TimerMediaFallback) == nil {
			g.contentGroupDone()
		}
		return
	}
	g.armButton(true)
}

// ---- secret ("кот в мешке") ------------------------------------------------

func (g *Game) secretBegin(public, noQuestion bool) {
	st := g.st
	q := st.Question
	pq := g.packQuestion(q.ThemeIndex, q.QuestionIndex)
	q.Direct = true
	q.SecretPublic = public
	q.NoQuestion = noQuestion
	if pq.Params.Theme != "" {
		q.Caption = pq.Params.Theme
	}
	if public {
		g.announceSecretPrice()
	}
	g.secretTransfer(pq.Params.SelectionMode)
}

func (g *Game) announceSecretPrice() {
	q := g.st.Question
	pq := g.packQuestion(q.ThemeIndex, q.QuestionIndex)
	price := 0
	if ch := g.secretPriceChoices(pq.Params.Price); len(ch) == 1 {
		price = ch[0]
	}
	g.all(EvQuestionCaption, QuestionCaptionPayload{Theme: q.Caption, Price: price})
}

func (g *Game) secretTransfer(mode packs.SelectionMode) {
	st := g.st
	chooser := g.currentChooser()
	var cands []string
	for _, p := range st.activePlayers() {
		if mode == packs.SelectExceptCurrent && p.ID == chooser {
			continue
		}
		cands = append(cands, p.ID)
	}
	if len(cands) == 0 {
		cands = []string{chooser}
	}
	if len(cands) == 1 {
		g.secretRecipientChosen(cands[0])
		return
	}
	g.setSub(SubSecretTransfer)
	g.askSelectPlayer(PendingSecretTransfer, chooser, cands)
}

func (g *Game) secretRecipientChosen(id string) {
	q := g.st.Question
	q.AnswererID = id
	g.setChooser(id, "secretRecipient")
	if !q.SecretPublic {
		g.announceSecretPrice()
	}
	g.secretPriceSelect()
}

// secretPriceChoices expands a numberSet (nil = nominal price).
func (g *Game) secretPriceChoices(ns *packs.NumberSet) []int {
	q := g.st.Question
	if ns == nil {
		return []int{q.Price}
	}
	if ns.Min == ns.Max {
		if ns.Min == 0 {
			lo, hi := g.roundMinMax()
			if lo == hi {
				return []int{lo}
			}
			return []int{lo, hi}
		}
		return []int{ns.Min}
	}
	lo, hi := ns.Min, ns.Max
	if lo > hi {
		lo, hi = hi, lo
	}
	if ns.Step <= 0 || ns.Step >= hi-lo {
		return []int{lo, hi}
	}
	var out []int
	for v := lo; v <= hi; v += ns.Step {
		out = append(out, v)
	}
	return out
}

func (g *Game) secretPriceSelect() {
	st := g.st
	q := st.Question
	pq := g.packQuestion(q.ThemeIndex, q.QuestionIndex)
	choices := g.secretPriceChoices(pq.Params.Price)
	if len(choices) == 1 {
		g.secretAfterPrice(choices[0])
		return
	}
	q.PriceChoices = choices
	g.setSub(SubPriceSelect)
	step := choices[1] - choices[0]
	dur := st.Times.StakeMakingMs
	p := AskStakePayload{PersonID: q.AnswererID, Modes: []StakeMode{StakeStake}, Min: choices[0], Max: choices[len(choices)-1], Step: step, Reason: "secretPrice", DurationMs: dur}
	g.to(q.AnswererID, EvAskStake, p)
	if st.Rules.Oral {
		g.showman(EvAskStake, p)
	}
	g.startTimer(TimerStakeMaking, dur, q.AnswererID)
}

func (g *Game) secretAfterPrice(price int) {
	st := g.st
	q := st.Question
	q.PriceChoices = nil
	q.CurPriceRight = price
	q.CurPriceWrong = g.wrongPrice(PenaltySubtract, price)
	g.all(EvQuestionCaption, QuestionCaptionPayload{Theme: q.Caption, Price: price})
	if q.NoQuestion {
		p := st.player(q.AnswererID)
		g.addScore(p, price, "secret")
		g.setPlayerState(p, PStateRight)
		q.History = append(q.History, Outcome{PersonID: p.ID, Right: true, Factor: 1, Delta: price})
		g.setSub(SubReveal)
		g.endQuestion()
		return
	}
	g.playContent()
}

// ---- noRisk / custom -------------------------------------------------------

func (g *Game) noRiskBegin() {
	st := g.st
	q := st.Question
	q.Direct = true
	q.AnswererID = g.currentChooser()
	q.CurPriceRight = q.Price * st.Rules.ForYourselfFactor
	q.CurPriceWrong = g.wrongPrice(st.Rules.ForYourselfPenalty, q.Price*st.Rules.ForYourselfFactor)
	g.all(EvQuestionCaption, QuestionCaptionPayload{Theme: q.Caption, Price: q.CurPriceRight})
	g.playContent()
}

func (g *Game) customBegin() {
	g.playContent()
}

// ---- answering -------------------------------------------------------------

func (g *Game) answerDuration(def int64) int64 {
	q := g.st.Question
	if pq := g.packQuestion(q.ThemeIndex, q.QuestionIndex); pq != nil && pq.Params.AnswerDurationMs > 0 {
		return pq.Params.AnswerDurationMs
	}
	return def
}

func (g *Game) askDirectAnswer() {
	q := g.st.Question
	p := g.st.player(q.AnswererID)
	if p == nil {
		g.showAnswer()
		return
	}
	g.askAnswer(p, TimerSoloAnswering, g.st.Times.SoloAnsweringMs)
}

// askAnswer asks a single player to answer (button or direct).
func (g *Game) askAnswer(p *Player, timerKind string, def int64) {
	st := g.st
	q := st.Question
	q.AnswererID = p.ID
	g.setSub(SubAnswering)
	g.setPlayerState(p, PStateAnswering)
	dur := g.answerDuration(def)
	oral := st.Rules.Oral && q.AnswerType == packs.AnswerText
	g.to(p.ID, EvAskAnswer, AskAnswerPayload{PersonID: p.ID, AnswerType: q.AnswerType, DurationMs: dur, Oral: oral})
	if oral {
		q.PendingAnswerID, q.PendingAnswer = p.ID, ""
		g.showman(EvAskValidate, AskValidatePayload{PersonID: p.ID, Answer: "", Rights: q.Rights, Wrongs: q.Wrongs, AllowFactor: true, Oral: true})
	}
	g.startTimer(timerKind, dur, p.ID)
}

func answerText(av AnswerView) string {
	switch {
	case av.OptionLabel != "":
		return av.OptionLabel
	case av.Number != nil:
		return strconv.FormatFloat(*av.Number, 'f', -1, 64)
	case av.Point != nil:
		return fmt.Sprintf("%.3f,%.3f", av.Point.X, av.Point.Y)
	}
	return av.Text
}

func answerFromCmd(cmd Command) AnswerView {
	av := AnswerView{Text: cmd.Text, OptionLabel: cmd.OptionLabel, Number: cmd.Number, Point: cmd.Point}
	if cmd.RightSet {
		r := cmd.Right
		av.ClientRight = &r
	}
	return av
}

func (g *Game) onAnswerDraft(cmd Command) error {
	p, err := g.playerActor(cmd.Actor)
	if err != nil {
		return err
	}
	q := g.st.Question
	if q == nil || g.st.Stage != StageQuestion || !(q.Sub == SubAnswering || q.Sub == SubHiddenAnswering) {
		return fmt.Errorf("%w: not answering", ErrBadState)
	}
	g.showman(EvAnswerDraft, AnswerDraftPayload{PersonID: p.ID, Text: cmd.Text})
	return nil
}

func (g *Game) onAnswer(cmd Command) error {
	st := g.st
	q := st.Question
	if q == nil || st.Stage != StageQuestion {
		return fmt.Errorf("%w: no question", ErrBadState)
	}
	av := answerFromCmd(cmd)
	switch q.Sub {
	case SubAnswering:
		if !q.Known {
			return fmt.Errorf("%w: custom question is scored by the showman", ErrBadState)
		}
		p, err := g.actingPlayer(cmd, q.AnswererID)
		if err != nil {
			return err
		}
		if p.ID != q.AnswererID {
			return fmt.Errorf("%w: not the answerer", ErrNotAllowed)
		}
		if q.PendingAnswerID != "" { // oral: the showman already validates; record the text only
			p.Answer = av
			g.all(EvPlayerAnswer, PlayerAnswerPayload{PersonID: p.ID, Answer: av})
			return nil
		}
		g.stopKind(TimerAnswering)
		g.stopKind(TimerSoloAnswering)
		g.processSingleAnswer(p, av)
		return nil
	case SubHiddenAnswering:
		p, err := g.actingPlayer(cmd, "")
		if err != nil {
			return err
		}
		if !g.isAnswerer(p.ID) || !p.InGame {
			return fmt.Errorf("%w: not answering this question", ErrNotAllowed)
		}
		if p.Answered {
			return fmt.Errorf("%w: already answered", ErrBadState)
		}
		p.Answered = true
		p.Answer = av
		g.setPlayerState(p, PStateHasAnswered)
		g.showman(EvPlayerAnswer, PlayerAnswerPayload{PersonID: p.ID, Answer: av})
		if q.AnswerType == packs.AnswerText {
			g.showman(EvAskValidate, AskValidatePayload{PersonID: p.ID, Answer: answerText(av), Rights: q.Rights, Wrongs: q.Wrongs, AllowFactor: true, Preview: true})
		}
		if g.allHiddenAnswered() {
			g.stopKind(TimerHiddenAnswering)
			g.hiddenAnsweringDone()
		}
		return nil
	}
	return fmt.Errorf("%w: not answering", ErrBadState)
}

func (g *Game) isAnswerer(id string) bool {
	for _, a := range g.st.Question.Answerers {
		if a == id {
			return true
		}
	}
	return false
}

// autoCheck validates non-text answer types.
func (g *Game) autoCheck(av AnswerView) (bool, bool) {
	q := g.st.Question
	switch q.AnswerType {
	case packs.AnswerSelect:
		return len(q.Rights) > 0 && strings.EqualFold(strings.TrimSpace(av.OptionLabel), strings.TrimSpace(q.Rights[0])), true
	case packs.AnswerNumber:
		if av.Number == nil || len(q.Rights) == 0 {
			return false, true
		}
		want, err := strconv.ParseFloat(strings.TrimSpace(q.Rights[0]), 64)
		if err != nil {
			return false, true
		}
		dev := g.packQuestion(q.ThemeIndex, q.QuestionIndex).Params.AnswerDeviation
		return math.Abs(*av.Number-want) <= dev, true
	case packs.AnswerPoint:
		if av.Point == nil || len(q.Rights) == 0 {
			return false, true
		}
		parts := strings.Split(strings.TrimSpace(q.Rights[0]), ",")
		if len(parts) < 2 {
			return false, true
		}
		x, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
		y, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if e1 != nil || e2 != nil {
			return false, true
		}
		tol := math.Max(g.packQuestion(q.ThemeIndex, q.QuestionIndex).Params.AnswerDeviation, 0.02)
		if len(parts) > 2 {
			if r, err := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64); err == nil && r > tol {
				tol = r
			}
		}
		return math.Hypot(av.Point.X-x, av.Point.Y-y) <= tol, true
	case packs.AnswerClient:
		return av.ClientRight != nil && *av.ClientRight, true
	}
	return false, false
}

func normAnswer(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// processSingleAnswer handles the answer of the sole answerer.
func (g *Game) processSingleAnswer(p *Player, av AnswerView) {
	st := g.st
	q := st.Question
	text := answerText(av)
	p.Answer = av
	g.all(EvPlayerAnswer, PlayerAnswerPayload{PersonID: p.ID, Answer: av})
	if right, auto := g.autoCheck(av); auto {
		g.applyVerdict(p, text, right, 1, "auto")
		return
	}
	if v, ok := q.ValidationCache[normAnswer(text)]; ok {
		g.applyVerdict(p, text, v.right, v.factor, "auto")
		return
	}
	g.askValidate(p.ID, text)
}

func (g *Game) askValidate(id, text string) {
	st := g.st
	q := st.Question
	q.PendingAnswerID, q.PendingAnswer = id, text
	g.setSub(SubValidating)
	g.showman(EvAskValidate, AskValidatePayload{PersonID: id, Answer: text, Rights: q.Rights, Wrongs: q.Wrongs, AllowFactor: true, AutoAfterMs: st.Times.ShowmanDecisionMs})
	g.startTimer(TimerShowmanDecision, st.Times.ShowmanDecisionMs, st.ShowmanID)
}

func (g *Game) answerTimeout() {
	st := g.st
	q := st.Question
	if q == nil || q.Sub != SubAnswering {
		return
	}
	if q.PendingAnswerID != "" { // oral: the showman decides
		return
	}
	p := st.player(q.AnswererID)
	if p == nil {
		g.showAnswer()
		return
	}
	g.all(EvPlayerAnswer, PlayerAnswerPayload{PersonID: p.ID, Answer: AnswerView{Text: "-"}})
	g.applyVerdict(p, "-", false, 1, "auto")
}

func (g *Game) onValidate(cmd Command) error {
	st := g.st
	q := st.Question
	if cmd.Actor.Role != RoleShowman && cmd.Actor.Role != RoleSystem {
		return fmt.Errorf("%w: showman or room required", ErrNotAllowed)
	}
	if q == nil || st.Stage != StageQuestion {
		return fmt.Errorf("%w: no question", ErrBadState)
	}
	factor := cmd.Factor
	if cmd.FactorZero {
		factor = 0
	} else if factor == 0 {
		factor = 1
	}
	if factor < 0 {
		return fmt.Errorf("%w: negative factor", ErrBadArgument)
	}
	source := cmd.Source
	if source == "" {
		source = "showman"
	}
	id := cmd.PersonID
	if !q.Known && q.Sub == SubAnswering { // custom question: manual scoring
		p := st.player(id)
		if p == nil {
			return fmt.Errorf("%w: unknown player %q", ErrBadArgument, id)
		}
		g.applyOutcome(p, cmd.Text, cmd.Right, factor, source, q.CurPriceRight, q.CurPriceWrong)
		return nil
	}
	if q.Hidden {
		if q.Sub == SubHiddenAnswering || q.Sub == SubStakes || q.Sub == SubContent {
			p := st.player(id)
			if p == nil || !g.isAnswerer(id) {
				return fmt.Errorf("%w: %q is not answering", ErrBadArgument, id)
			}
			q.PreVerdicts[id] = validation{right: cmd.Right, factor: factor}
			return nil
		}
		if q.Sub != SubValidating || (id != "" && id != q.PendingAnswerID) {
			return fmt.Errorf("%w: no validation pending for %q", ErrBadState, id)
		}
		g.stopKind(TimerShowmanDecision)
		g.hiddenApplyVerdict(st.player(q.PendingAnswerID), q.PendingAnswer, cmd.Right, factor, source)
		return nil
	}
	if q.PendingAnswerID == "" || !(q.Sub == SubValidating || q.Sub == SubAnswering) {
		return fmt.Errorf("%w: no validation pending", ErrBadState)
	}
	if id != "" && id != q.PendingAnswerID {
		return fmt.Errorf("%w: pending validation is for %q", ErrBadArgument, q.PendingAnswerID)
	}
	p := st.player(q.PendingAnswerID)
	g.stopKind(TimerShowmanDecision)
	g.stopKind(TimerAnswering)
	g.stopKind(TimerSoloAnswering)
	g.applyVerdict(p, q.PendingAnswer, cmd.Right, factor, source)
	return nil
}

// applyOutcome scores one answer and records it in the question history.
func (g *Game) applyOutcome(p *Player, answer string, right bool, factor float64, source string, priceRight, priceWrong int) *Outcome {
	q := g.st.Question
	if q.AnswerType == packs.AnswerText && answer != "" && answer != "-" {
		q.ValidationCache[normAnswer(answer)] = validation{right: right, factor: factor}
	}
	o := Outcome{PersonID: p.ID, Answer: answer, Right: right, Factor: factor}
	ex := ""
	switch {
	case right:
		o.Delta = scaled(priceRight, factor)
		g.all(EvValidation, ValidationPayload{PersonID: p.ID, Answer: answer, Right: true, Factor: factor, Source: source})
		g.addScore(p, o.Delta, "answer")
		g.setPlayerState(p, PStateRight)
		p.Right++
		p.AcceptedAnswers = append(p.AcceptedAnswers, answer)
	case factor == 0:
		o.Pass = true
		g.all(EvValidation, ValidationPayload{PersonID: p.ID, Answer: answer, Right: false, Factor: 0, Source: source})
		g.setPlayerState(p, PStatePass)
	default:
		o.Delta = -scaled(priceWrong, factor)
		if q.AnswerType == packs.AnswerSelect && answer != "" {
			q.ExcludedOptions[answer] = true
			ex = answer
		}
		g.all(EvValidation, ValidationPayload{PersonID: p.ID, Answer: answer, Right: false, Factor: factor, Source: source, ExcludedOption: ex})
		g.addScore(p, o.Delta, "penalty")
		g.setPlayerState(p, PStateWrong)
		p.Wrong++
		p.RejectedAnswers = append(p.RejectedAnswers, answer)
	}
	q.History = append(q.History, o)
	return &q.History[len(q.History)-1]
}

// applyVerdict finishes the answer of the sole answerer.
func (g *Game) applyVerdict(p *Player, answer string, right bool, factor float64, source string) {
	st := g.st
	q := st.Question
	q.PendingAnswerID, q.PendingAnswer = "", ""
	o := g.applyOutcome(p, answer, right, factor, source, q.CurPriceRight, q.CurPriceWrong)
	if right {
		if st.Rules.Mode == ModeClassic && q.Button {
			g.setChooser(p.ID, "rightAnswer")
		}
		g.showAnswer()
		return
	}
	_ = o
	if q.AnswerType == packs.AnswerSelect && len(q.Options)-len(q.ExcludedOptions) <= 1 {
		g.showAnswer()
		return
	}
	if q.Button {
		p.CanPress = false
		if g.anyCanPress() {
			g.rearm()
			return
		}
	}
	g.showAnswer()
}

// ---- reveal / end ----------------------------------------------------------

func (g *Game) showAnswer() {
	st := g.st
	q := st.Question
	g.stopAllExcept(TimerRound)
	st.Pending = nil
	if q.Armed {
		q.Armed = false
		g.room(EvButtonDisarm, ButtonDisarmRequestPayload{QuestionID: q.ID})
	}
	q.PendingAnswerID, q.PendingAnswer = "", ""
	g.setSub(SubReveal)
	text := ""
	if len(q.Rights) > 0 {
		text = q.Rights[0]
	}
	g.all(EvRightAnswer, RightAnswerPayload{Text: text, Items: viewItems(q.AnswerItems), Comments: q.Comments})
	if st.Rules.Managed {
		q.WaitNext = true
		return
	}
	wait := st.Times.ReflectionMs
	for _, it := range q.AnswerItems {
		if d := g.itemDuration(it); d > wait {
			wait = d
		}
	}
	if wait <= 0 {
		g.endQuestion()
		return
	}
	g.startTimer(TimerReflection, wait, "")
}

func (g *Game) endQuestion() {
	st := g.st
	q := st.Question
	if q == nil || q.Sub == SubEnd {
		return
	}
	q.WaitNext = false
	g.stopAllExcept(TimerRound)
	g.setSub(SubEnd)
	g.all(EvQuestionEnd, QuestionEndPayload{ThemeIndex: q.ThemeIndex, QuestionIndex: q.QuestionIndex})
	st.QuestionsPlayed++
	g.emitSums()
	if len(q.AppealQueue) > 0 {
		g.processAppeals()
		return
	}
	g.afterQuestion()
}

// afterQuestion moves on after a question (and its appeals) finished.
func (g *Game) afterQuestion() {
	st := g.st
	if st.Appeal != nil {
		return
	}
	if st.Rules.Mode == ModeTurnTaking {
		ps := st.activePlayers()
		if len(ps) > 0 {
			idx := -1
			for i, p := range ps {
				if p.ID == st.ChooserID {
					idx = i
				}
			}
			g.setChooser(ps[(idx+1)%len(ps)].ID, "rotation")
		}
	}
	for _, p := range st.Players {
		p.CanPress = false
	}
	if st.RoundType == packs.RoundFinal {
		g.finalContinue()
		return
	}
	g.beginSelection(false)
}
