package engine

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sort"

	"sigame/internal/packs"
)

// Sentinel errors. Apply wraps them with detail; use errors.Is to classify.
var (
	ErrNotAllowed  = errors.New("engine: not allowed")
	ErrBadState    = errors.New("engine: bad state")
	ErrBadArgument = errors.New("engine: bad argument")
)

// MaxPlayers mirrors SIGame's limit.
const MaxPlayers = 12

// Game is the pure, deterministic game state machine. It is not safe for
// concurrent use; the room actor owns it.
type Game struct {
	pack     *packs.Pack
	st       *State
	rng      *rand.Rand
	now      int64 // AtMs of the command being applied
	ev       []Event
	timerSeq int
}

// New creates a game in the lobby stage. players must contain 1..MaxPlayers
// unique IDs; showmanID must not be one of them. seed drives every random
// choice (timeouts, ties) so that a command stream replays identically.
func New(pack *packs.Pack, rules Rules, times TimeSettings, players []PlayerInit, showmanID string, seed int64) (*Game, error) {
	if pack == nil {
		return nil, fmt.Errorf("%w: nil pack", ErrBadArgument)
	}
	if len(pack.Rounds) == 0 {
		return nil, fmt.Errorf("%w: pack has no rounds", ErrBadArgument)
	}
	if len(players) == 0 || len(players) > MaxPlayers {
		return nil, fmt.Errorf("%w: players must be 1..%d, got %d", ErrBadArgument, MaxPlayers, len(players))
	}
	if showmanID == "" {
		return nil, fmt.Errorf("%w: empty showman id", ErrBadArgument)
	}
	if rules.Mode == "" {
		rules.Mode = ModeClassic
	}
	switch rules.Mode {
	case ModeClassic, ModeSimple, ModeQuiz, ModeTurnTaking:
	default:
		return nil, fmt.Errorf("%w: unknown mode %q", ErrBadArgument, rules.Mode)
	}
	if rules.ForYourselfFactor <= 0 {
		rules.ForYourselfFactor = 2
	}
	if rules.ButtonPenalty == "" {
		rules.ButtonPenalty = PenaltySubtract
	}
	if rules.ForYourselfPenalty == "" {
		rules.ForYourselfPenalty = PenaltyNone
	}
	if rules.ForAllPenalty == "" {
		rules.ForAllPenalty = PenaltySubtract
	}
	st := &State{
		Stage:      StageLobby,
		RoundIndex: -1,
		ShowmanID:  showmanID,
		Rules:      rules,
		Times:      times,
	}
	seen := map[string]bool{}
	for _, pi := range players {
		if pi.ID == "" || seen[pi.ID] || pi.ID == showmanID {
			return nil, fmt.Errorf("%w: invalid or duplicate player id %q", ErrBadArgument, pi.ID)
		}
		seen[pi.ID] = true
		st.Players = append(st.Players, &Player{ID: pi.ID, Name: pi.Name, Connected: true, State: PStateNone})
	}
	g := &Game{
		pack: pack,
		st:   st,
		rng:  rand.New(rand.NewPCG(uint64(seed), uint64(seed)^0x9E3779B97F4A7C15)),
	}
	return g, nil
}

// State returns the live state for read-only inspection. The pointer stays
// valid but its contents change on every Apply; callers must not mutate it.
func (g *Game) State() *State { return g.st }

// Apply feeds one command. On error the state is unchanged and no events are
// returned. Stale timeouts (unknown timer id) are ignored without error.
func (g *Game) Apply(cmd Command) ([]Event, error) {
	g.ev = nil
	g.now = cmd.AtMs
	if err := g.dispatch(cmd); err != nil {
		g.ev = nil
		return nil, err
	}
	out := g.ev
	g.ev = nil
	return out, nil
}

func (g *Game) dispatch(cmd Command) error {
	st := g.st
	if st.Ended && cmd.Type != CmdPlayerLeft && cmd.Type != CmdPlayerJoined {
		return fmt.Errorf("%w: game ended", ErrBadState)
	}
	switch cmd.Type {
	// --- room-originated ---
	case CmdTimeout:
		return g.onTimeout(cmd)
	case CmdButtonResult:
		return g.onButtonResult(cmd)
	case CmdMediaCompleted:
		return g.onMediaCompleted(cmd)
	case CmdMediaLoaded:
		return nil
	case CmdPlayerLeft:
		return g.onPlayerLeft(cmd)
	case CmdPlayerJoined:
		return g.onPlayerJoined(cmd)
	case CmdAISuggestion:
		if cmd.Actor.Role != RoleSystem {
			return fmt.Errorf("%w: AI suggestions come from the room", ErrNotAllowed)
		}
		f := cmd.Factor
		if f == 0 {
			f = 1
		}
		g.showman(EvAISuggestion, AISuggestionPayload{PersonID: cmd.PersonID, Right: cmd.Right, Factor: f, Reason: cmd.Reason})
		return nil

	// --- lobby ---
	case CmdStart:
		return g.onStart(cmd)
	case CmdPlayerReady:
		p, err := g.playerActor(cmd.Actor)
		if err != nil {
			return err
		}
		p.Ready = !p.Ready
		g.emitPlayers()
		return nil

	// --- showman / host controls (allowed while paused) ---
	case CmdShowmanPause:
		return g.onPause(cmd)
	case CmdMove:
		return g.onMove(cmd)
	case CmdToggle:
		return g.onToggle(cmd)
	case CmdChangeScore:
		return g.onChangeScore(cmd)
	case CmdSetChooser:
		return g.onSetChooser(cmd)
	case CmdSetOptions:
		return g.onSetOptions(cmd)
	case CmdKickPlayer:
		return g.onKick(cmd)
	case CmdNext:
		return g.onNext(cmd)
	}

	if st.Paused {
		return fmt.Errorf("%w: game is paused", ErrBadState)
	}
	switch cmd.Type {
	case CmdChooseQuestion:
		return g.onChooseQuestion(cmd)
	case CmdPass:
		return g.onPass(cmd)
	case CmdAnswer:
		return g.onAnswer(cmd)
	case CmdAnswerDraft:
		return g.onAnswerDraft(cmd)
	case CmdValidate:
		return g.onValidate(cmd)
	case CmdSelectPlayer:
		return g.onSelectPlayer(cmd)
	case CmdSetStake:
		return g.onSetStake(cmd)
	case CmdDeleteTheme:
		return g.onDeleteTheme(cmd)
	case CmdAppellate:
		return g.onAppellate(cmd)
	case CmdVoteAppeal:
		return g.onVoteAppeal(cmd)
	}
	return fmt.Errorf("%w: unknown command %q", ErrBadArgument, cmd.Type)
}

// ---- emit helpers ----------------------------------------------------------

func (g *Game) emit(t EvType, aud Audience, p any) {
	g.ev = append(g.ev, Event{Type: t, Audience: aud, Payload: p})
}
func (g *Game) all(t EvType, p any)           { g.emit(t, ToAll(), p) }
func (g *Game) to(id string, t EvType, p any) { g.emit(t, ToPerson(id), p) }
func (g *Game) showman(t EvType, p any)       { g.emit(t, ToShowman(), p) }
func (g *Game) room(t EvType, p any)          { g.emit(t, ToRoom(), p) }

func (g *Game) emitStage() {
	p := StagePayload{Stage: g.st.Stage, RoundIndex: g.st.RoundIndex}
	if g.st.Stage == StageQuestion && g.st.Question != nil {
		p.Sub = g.st.Question.Sub
	}
	g.all(EvStage, p)
}

func (g *Game) setSub(sub QSub) {
	g.st.Question.Sub = sub
	g.emitStage()
}

func (g *Game) emitSums() {
	g.all(EvSums, SumsPayload{Scores: g.scores()})
}

func (g *Game) scores() []ScoreEntry {
	out := make([]ScoreEntry, 0, len(g.st.Players))
	for _, p := range g.st.Players {
		if p.Kicked {
			continue
		}
		out = append(out, ScoreEntry{PersonID: p.ID, Score: p.Score})
	}
	return out
}

func (g *Game) playerInfos() []PlayerInfo {
	out := make([]PlayerInfo, 0, len(g.st.Players))
	for _, p := range g.st.Players {
		out = append(out, PlayerInfo{ID: p.ID, Name: p.Name, Score: p.Score, Connected: p.Connected, Ready: p.Ready,
			Kicked: p.Kicked, State: p.State, CanPress: p.CanPress, InGame: p.InGame})
	}
	return out
}

func (g *Game) emitPlayers() { g.all(EvPlayers, PlayersPayload{Players: g.playerInfos()}) }

func (g *Game) tablePayload() TablePayload {
	out := TablePayload{}
	for _, t := range g.st.Table {
		tt := TableTheme{Name: t.Name, Removed: t.Removed}
		for _, c := range t.Questions {
			tt.Questions = append(tt.Questions, TableCell{Price: c.Price, Played: c.Played, Removed: c.Removed})
		}
		out.Themes = append(out.Themes, tt)
	}
	return out
}

func (g *Game) emitTable() { g.all(EvTable, g.tablePayload()) }

func (g *Game) setPlayerState(p *Player, s PlayerState) {
	p.State = s
	g.all(EvPlayerState, PlayerStatePayload{PersonID: p.ID, State: s})
}

func (g *Game) addScore(p *Player, delta int, reason string) {
	if delta == 0 && reason != "correction" {
		return
	}
	p.Score += delta
	g.all(EvPersonScore, PersonScorePayload{PersonID: p.ID, Delta: delta, Score: p.Score, Reason: reason})
}

func (g *Game) setChooser(id, reason string) {
	g.st.ChooserID = id
	g.all(EvSetChooser, SetChooserPayload{PersonID: id, Reason: reason})
}

// ---- timers ----------------------------------------------------------------

func (g *Game) startTimer(kind string, dur int64, personID string) string {
	g.stopKind(kind)
	g.timerSeq++
	id := fmt.Sprintf("%s-%d", kind, g.timerSeq)
	t := &Timer{ID: id, Kind: kind, DurationMs: dur, StartedAtMs: g.now, PersonID: personID}
	g.st.Timers = append(g.st.Timers, t)
	g.all(EvTimerStart, TimerStartPayload{ID: id, Kind: kind, DurationMs: dur, PersonID: personID})
	if g.st.Paused {
		t.Paused = true
		t.RemainingMs = dur
		g.all(EvTimerPause, TimerPausePayload{ID: id, Kind: kind, RemainingMs: dur})
	}
	return id
}

func (g *Game) findTimer(id string) *Timer {
	for _, t := range g.st.Timers {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (g *Game) timerOfKind(kind string) *Timer {
	for _, t := range g.st.Timers {
		if t.Kind == kind {
			return t
		}
	}
	return nil
}

func (g *Game) removeTimer(id string) *Timer {
	for i, t := range g.st.Timers {
		if t.ID == id {
			g.st.Timers = append(g.st.Timers[:i], g.st.Timers[i+1:]...)
			return t
		}
	}
	return nil
}

func (g *Game) stopTimer(id string) {
	if t := g.removeTimer(id); t != nil {
		g.all(EvTimerStop, TimerStopPayload{ID: t.ID})
	}
}

func (g *Game) stopKind(kind string) bool {
	t := g.timerOfKind(kind)
	if t == nil {
		return false
	}
	g.stopTimer(t.ID)
	return true
}

// stopAllExcept stops every timer except the listed kinds (the round timer
// survives question transitions).
func (g *Game) stopAllExcept(keep ...string) {
	kept := map[string]bool{}
	for _, k := range keep {
		kept[k] = true
	}
	for _, t := range append([]*Timer(nil), g.st.Timers...) {
		if !kept[t.Kind] {
			g.stopTimer(t.ID)
		}
	}
}

func (g *Game) remaining(t *Timer) int64 {
	if t.Paused {
		return t.RemainingMs
	}
	r := t.DurationMs - (g.now - t.StartedAtMs)
	if r < 0 {
		r = 0
	}
	return r
}

// pauseTimer freezes a timer for game-logic reasons (button win, appeal);
// such timers survive a showman PAUSE/resume cycle untouched.
func (g *Game) pauseTimer(t *Timer) {
	if t.Paused {
		t.Frozen = true
		return
	}
	t.RemainingMs = g.remaining(t)
	t.Paused = true
	t.Frozen = true
	g.all(EvTimerPause, TimerPausePayload{ID: t.ID, Kind: t.Kind, RemainingMs: t.RemainingMs})
}

func (g *Game) resumeTimer(t *Timer) {
	if !t.Paused {
		return
	}
	t.Paused = false
	t.Frozen = false
	t.DurationMs = t.RemainingMs
	t.StartedAtMs = g.now
	g.all(EvTimerResume, TimerPausePayload{ID: t.ID, Kind: t.Kind, RemainingMs: t.RemainingMs})
}

// pauseAllTimers is the showman pause: every running timer is paused but not frozen.
func (g *Game) pauseAllTimers() {
	for _, t := range g.st.Timers {
		if t.Paused {
			continue
		}
		t.RemainingMs = g.remaining(t)
		t.Paused = true
		g.all(EvTimerPause, TimerPausePayload{ID: t.ID, Kind: t.Kind, RemainingMs: t.RemainingMs})
	}
}

// resumeAllTimers undoes pauseAllTimers; frozen timers stay paused.
func (g *Game) resumeAllTimers() {
	for _, t := range g.st.Timers {
		if !t.Frozen {
			g.resumeTimer(t)
		}
	}
}

// ---- permissions -----------------------------------------------------------

func isShowmanOrHost(a Actor) bool { return a.Role == RoleShowman || a.IsHost }

func (g *Game) requireShowman(a Actor) error {
	if a.Role != RoleShowman && !a.IsHost {
		return fmt.Errorf("%w: showman or host required", ErrNotAllowed)
	}
	return nil
}

func (g *Game) requireSystem(a Actor) error {
	if a.Role != RoleSystem {
		return fmt.Errorf("%w: room-originated command", ErrNotAllowed)
	}
	return nil
}

func (g *Game) playerActor(a Actor) (*Player, error) {
	if a.Role != RolePlayer {
		return nil, fmt.Errorf("%w: player required", ErrNotAllowed)
	}
	p := g.st.player(a.PersonID)
	if p == nil || p.Kicked {
		return nil, fmt.Errorf("%w: unknown player %q", ErrNotAllowed, a.PersonID)
	}
	return p, nil
}

// actingPlayer resolves the player a command acts for: the player itself, or
// (oral mode) the showman acting for cmd.PersonID / the expected player.
func (g *Game) actingPlayer(cmd Command, expected string) (*Player, error) {
	if cmd.Actor.Role == RoleShowman && g.st.Rules.Oral {
		id := cmd.PersonID
		if id == "" {
			id = expected
		}
		p := g.st.player(id)
		if p == nil {
			return nil, fmt.Errorf("%w: unknown player %q", ErrBadArgument, id)
		}
		return p, nil
	}
	return g.playerActor(cmd.Actor)
}

// ---- pack helpers ----------------------------------------------------------

func (g *Game) round() *packs.Round {
	if g.st.RoundIndex < 0 || g.st.RoundIndex >= len(g.pack.Rounds) {
		return nil
	}
	return &g.pack.Rounds[g.st.RoundIndex]
}

func (g *Game) packQuestion(t, q int) *packs.Question {
	r := g.round()
	if r == nil || t < 0 || t >= len(r.Themes) || q < 0 || q >= len(r.Themes[t].Questions) {
		return nil
	}
	return &r.Themes[t].Questions[q]
}

// roundMinMax returns the minimum and maximum positive price of the round.
func (g *Game) roundMinMax() (int, int) {
	minP, maxP := 0, 0
	for _, t := range g.st.Table {
		for _, c := range t.Questions {
			if c.Price <= 0 {
				continue
			}
			if minP == 0 || c.Price < minP {
				minP = c.Price
			}
			if c.Price > maxP {
				maxP = c.Price
			}
		}
	}
	return minP, maxP
}

// stakeStep is the largest power of 10 ≤ the minimum positive price of the round.
func (g *Game) stakeStep() int {
	minP, _ := g.roundMinMax()
	if minP < 10 {
		return 1
	}
	step := 1
	for step*10 <= minP {
		step *= 10
	}
	return step
}

func (g *Game) defaultQuestionType() packs.QuestionType {
	if g.st.RoundType == packs.RoundFinal {
		return packs.QStakeAll
	}
	switch g.st.Rules.Mode {
	case ModeQuiz:
		return packs.QForAll
	case ModeTurnTaking:
		return packs.QNoRisk
	}
	return packs.QSimple
}

func (g *Game) sequential() bool {
	return g.st.Rules.Mode == ModeSimple || g.st.Rules.Mode == ModeQuiz
}

func (g *Game) randomIndex(n int) int {
	if n <= 1 {
		return 0
	}
	return g.rng.IntN(n)
}

func scaled(price int, factor float64) int {
	return int(math.Round(float64(price) * factor))
}

// lowestScorePlayers returns active players with the minimum score, seat order.
func (g *Game) lowestScorePlayers() []*Player {
	var out []*Player
	minS := 0
	for _, p := range g.st.activePlayers() {
		if out == nil || p.Score < minS {
			out = []*Player{p}
			minS = p.Score
		} else if p.Score == minS {
			out = append(out, p)
		}
	}
	return out
}

func ids(ps []*Player) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}

// ---- lobby / game flow -----------------------------------------------------

func (g *Game) onStart(cmd Command) error {
	if err := g.requireShowman(cmd.Actor); err != nil {
		return err
	}
	if g.st.Stage != StageLobby {
		return fmt.Errorf("%w: game already started", ErrBadState)
	}
	if len(g.st.activePlayers()) == 0 {
		return fmt.Errorf("%w: no connected players", ErrBadState)
	}
	g.all(EvOptions, OptionsPayload{Rules: g.st.Rules, Times: g.st.Times})
	g.emitPlayers()
	g.startRound(0)
	return nil
}

func (g *Game) startRound(idx int) {
	st := g.st
	g.stopAllExcept()
	st.Question = nil
	st.Pending = nil
	st.Appeal = nil
	st.RoundTimedOut = false
	st.WaitNext = false
	st.FinalGroups = nil
	st.FinalOffset = 0
	st.DeleteStep = 0
	st.DeleterID = ""
	st.DeleterUsed = nil
	if idx >= len(g.pack.Rounds) {
		g.endGame()
		return
	}
	if idx < 0 {
		idx = 0
	}
	r := &g.pack.Rounds[idx]
	st.RoundIndex = idx
	st.RoundName = r.Name
	st.RoundType = r.Type
	if st.RoundType == "" {
		st.RoundType = packs.RoundStandard
	}
	st.Table = nil
	var themeNames []string
	for _, t := range r.Themes {
		ts := ThemeState{Name: t.Name}
		for _, q := range t.Questions {
			ts.Questions = append(ts.Questions, CellState{Price: q.Price, Removed: q.IsEmpty()})
		}
		st.Table = append(st.Table, ts)
		themeNames = append(themeNames, t.Name)
	}
	for _, p := range st.Players {
		p.State = PStateNone
		p.CanPress = false
		p.InGame = false
		g.resetPlayerQuestion(p)
	}
	st.Stage = StageRoundIntro
	g.emitStage()
	g.all(EvRoundStart, RoundStartPayload{Index: idx, Name: r.Name, Type: st.RoundType, Themes: themeNames})
	g.all(EvRoundContent, g.roundContent(r))
	g.emitTable()
	g.emitSums()
	if st.Times.RoundMs > 0 {
		g.startTimer(TimerRound, st.Times.RoundMs, "")
	}
	if st.Rules.Managed {
		st.WaitNext = true
		return
	}
	g.enterRound()
}

func (g *Game) roundContent(r *packs.Round) RoundContentPayload {
	out := RoundContentPayload{}
	seen := map[string]bool{}
	add := func(items []packs.ContentItem) {
		for _, it := range items {
			if it.MediaID != "" && !seen["m"+it.MediaID] {
				seen["m"+it.MediaID] = true
				out.MediaIDs = append(out.MediaIDs, it.MediaID)
			}
			if it.URL != "" && !seen["u"+it.URL] {
				seen["u"+it.URL] = true
				out.URLs = append(out.URLs, it.URL)
			}
		}
	}
	for _, t := range r.Themes {
		for _, q := range t.Questions {
			add(q.Params.Question)
			for _, o := range q.Params.AnswerOptions {
				add(o.Content)
			}
		}
	}
	if out.MediaIDs == nil {
		out.MediaIDs = []string{}
	}
	return out
}

// enterRound leaves roundIntro for the round's first step.
func (g *Game) enterRound() {
	g.st.WaitNext = false
	if g.st.RoundType == packs.RoundFinal {
		g.startFinalRound()
		return
	}
	g.beginSelection(true)
}

// hasPlayableCells reports whether any cell is still available.
func (g *Game) hasPlayableCells() bool {
	for _, t := range g.st.Table {
		if t.Removed {
			continue
		}
		for _, c := range t.Questions {
			if !c.Played && !c.Removed {
				return true
			}
		}
	}
	return false
}

// beginSelection starts the cell selection step. roundStart selects the
// initial chooser (lowest score, ties → showman).
func (g *Game) beginSelection(roundStart bool) {
	st := g.st
	if st.RoundTimedOut {
		g.endRound("timeout")
		return
	}
	if !g.hasPlayableCells() {
		g.endRound("empty")
		return
	}
	st.Stage = StageSelecting
	st.Pending = nil
	g.emitStage()
	if g.sequential() {
		t, q, ok := g.nextSequentialCell()
		if !ok {
			g.endRound("empty")
			return
		}
		g.startQuestion(t, q)
		return
	}
	if roundStart || st.player(st.ChooserID) == nil || !st.player(st.ChooserID).Active() {
		low := g.lowestScorePlayers()
		if len(low) == 0 {
			g.endRound("noPlayers")
			return
		}
		if len(low) > 1 {
			g.askSelectPlayer(PendingChooser, st.ShowmanID, ids(low))
			return
		}
		g.setChooser(low[0].ID, "roundStart")
	}
	g.askChoose()
}

func (g *Game) nextSequentialCell() (int, int, bool) {
	st := g.st
	for t := 0; t < len(st.Table); t++ {
		if st.Table[t].Removed {
			continue
		}
		for q := 0; q < len(st.Table[t].Questions); q++ {
			c := st.Table[t].Questions[q]
			if !c.Played && !c.Removed {
				return t, q, true
			}
		}
	}
	return 0, 0, false
}

func (g *Game) askChoose() {
	st := g.st
	dur := st.Times.QuestionSelectionMs
	p := AskChoosePayload{PersonID: st.ChooserID, DurationMs: dur}
	g.to(st.ChooserID, EvAskChoose, p)
	if st.Rules.Oral {
		g.showman(EvAskChoose, p)
	}
	g.startTimer(TimerQuestionSelection, dur, st.ChooserID)
}

func (g *Game) askSelectPlayer(kind PendingKind, decider string, candidates []string) {
	st := g.st
	st.Pending = &Pending{Kind: kind, DeciderID: decider, Candidates: candidates}
	var dur int64
	var timerKind string
	if kind == PendingSecretTransfer {
		dur, timerKind = st.Times.PlayerSelectionMs, TimerPlayerSelection
	} else {
		dur, timerKind = st.Times.ShowmanDecisionMs, TimerShowmanDecision
	}
	p := AskSelectPlayerPayload{Reason: string(kind), Candidates: candidates, DeciderID: decider, DurationMs: dur}
	g.to(decider, EvAskSelectPlayer, p)
	if kind == PendingSecretTransfer && st.Rules.Oral {
		g.showman(EvAskSelectPlayer, p)
	}
	g.startTimer(timerKind, dur, decider)
}

func (g *Game) onSelectPlayer(cmd Command) error {
	st := g.st
	pd := st.Pending
	if pd == nil {
		return fmt.Errorf("%w: no player selection pending", ErrBadState)
	}
	if pd.Kind == PendingSecretTransfer {
		if !(cmd.Actor.PersonID == pd.DeciderID || (cmd.Actor.Role == RoleShowman && st.Rules.Oral)) {
			return fmt.Errorf("%w: only the chooser selects the recipient", ErrNotAllowed)
		}
	} else if cmd.Actor.Role != RoleShowman {
		return fmt.Errorf("%w: showman decides ties", ErrNotAllowed)
	}
	found := false
	for _, c := range pd.Candidates {
		if c == cmd.PersonID {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("%w: %q is not a candidate", ErrBadArgument, cmd.PersonID)
	}
	g.resolvePending(cmd.PersonID)
	return nil
}

func (g *Game) resolvePending(id string) {
	st := g.st
	pd := st.Pending
	st.Pending = nil
	g.stopKind(TimerShowmanDecision)
	g.stopKind(TimerPlayerSelection)
	switch pd.Kind {
	case PendingChooser:
		g.setChooser(id, "roundStart")
		g.askChoose()
	case PendingStaker:
		g.stakeAskBid(id)
	case PendingDeleter:
		g.finalAskDelete(id)
	case PendingSecretTransfer:
		g.secretRecipientChosen(id)
	}
}

func (g *Game) onChooseQuestion(cmd Command) error {
	st := g.st
	if st.Stage != StageSelecting || st.Pending != nil {
		return fmt.Errorf("%w: not selecting", ErrBadState)
	}
	if g.sequential() {
		return fmt.Errorf("%w: sequential round", ErrBadState)
	}
	if !(cmd.Actor.PersonID == st.ChooserID && cmd.Actor.Role == RolePlayer) && !(cmd.Actor.Role == RoleShowman && st.Rules.Oral) {
		return fmt.Errorf("%w: only the chooser selects", ErrNotAllowed)
	}
	if cmd.Theme < 0 || cmd.Theme >= len(st.Table) || cmd.Q < 0 || cmd.Q >= len(st.Table[cmd.Theme].Questions) {
		return fmt.Errorf("%w: cell %d/%d out of range", ErrBadArgument, cmd.Theme, cmd.Q)
	}
	c := st.Table[cmd.Theme].Questions[cmd.Q]
	if c.Played || c.Removed || st.Table[cmd.Theme].Removed {
		return fmt.Errorf("%w: cell %d/%d is not available", ErrBadArgument, cmd.Theme, cmd.Q)
	}
	g.stopKind(TimerQuestionSelection)
	g.startQuestion(cmd.Theme, cmd.Q)
	return nil
}

func (g *Game) randomCell() (int, int) {
	var cells [][2]int
	for t, th := range g.st.Table {
		if th.Removed {
			continue
		}
		for q, c := range th.Questions {
			if !c.Played && !c.Removed {
				cells = append(cells, [2]int{t, q})
			}
		}
	}
	c := cells[g.randomIndex(len(cells))]
	return c[0], c[1]
}

// endRound closes the round and moves on.
func (g *Game) endRound(reason string) {
	st := g.st
	g.stopAllExcept()
	st.Pending = nil
	st.Stage = StageRoundEnd
	g.emitStage()
	g.all(EvRoundEnd, RoundEndPayload{Index: st.RoundIndex, Reason: reason})
	g.emitSums()
	if st.Rules.Managed {
		st.WaitNext = true
		return
	}
	g.startRound(st.RoundIndex + 1)
}

func (g *Game) endGame() {
	st := g.st
	g.stopAllExcept()
	st.Stage = StageGameEnd
	st.Ended = true
	g.emitStage()
	winner := ""
	best := 0
	first := true
	tie := false
	for _, p := range st.Players {
		if p.Kicked {
			continue
		}
		if first || p.Score > best {
			best, winner, first, tie = p.Score, p.ID, false, false
		} else if p.Score == best {
			tie = true
		}
	}
	if tie {
		winner = ""
	}
	st.Winner = winner
	g.all(EvWinner, WinnerPayload{PersonID: winner})
	g.all(EvGameEnd, GameEndPayload{Scores: g.scores(), WinnerID: winner, Statistics: g.statistics()})
}

func (g *Game) statistics() Statistics {
	s := Statistics{QuestionsPlayed: g.st.QuestionsPlayed, Players: []PlayerStats{}}
	for _, p := range g.st.Players {
		s.Players = append(s.Players, PlayerStats{PersonID: p.ID, RightAnswers: p.Right, WrongAnswers: p.Wrong,
			AcceptedAnswers: p.AcceptedAnswers, RejectedAnswers: p.RejectedAnswers, Appeals: p.Appeals})
	}
	return s
}

// ---- timeouts --------------------------------------------------------------

func (g *Game) onTimeout(cmd Command) error {
	if err := g.requireSystem(cmd.Actor); err != nil {
		return err
	}
	t := g.findTimer(cmd.TimerID)
	if t == nil || t.Paused || g.st.Paused {
		return nil // stale or frozen
	}
	g.removeTimer(t.ID)
	st := g.st
	switch t.Kind {
	case TimerRound:
		st.RoundTimedOut = true
		if st.Stage == StageSelecting && st.Pending == nil {
			g.endRound("timeout")
		}
	case TimerQuestionSelection:
		if st.Stage == StageSelecting {
			th, q := g.randomCell()
			g.startQuestion(th, q)
		}
	case TimerShowmanDecision:
		g.onShowmanDecisionTimeout()
	case TimerPlayerSelection:
		if pd := st.Pending; pd != nil && pd.Kind == PendingSecretTransfer {
			cands := pd.Candidates
			var others []string
			for _, c := range cands {
				if c != st.ChooserID {
					others = append(others, c)
				}
			}
			if len(others) > 0 {
				cands = others
			}
			g.resolvePending(cands[g.randomIndex(len(cands))])
		}
	case TimerThemeSelection:
		g.finalDeleteTimeout()
	case TimerContent, TimerMediaFallback:
		g.contentGroupDone()
	case TimerButtonPressing:
		g.nobodyAnswered()
	case TimerAnswering, TimerSoloAnswering:
		g.answerTimeout()
	case TimerHiddenAnswering:
		g.hiddenAnsweringDone()
	case TimerStakeMaking:
		g.stakeTimeout(t)
	case TimerReflection:
		g.endQuestion()
	case TimerAppellation:
		g.appealFinish()
	}
	return nil
}

func (g *Game) onShowmanDecisionTimeout() {
	st := g.st
	if pd := st.Pending; pd != nil && pd.Kind != PendingSecretTransfer {
		g.resolvePending(pd.Candidates[g.randomIndex(len(pd.Candidates))])
		return
	}
	q := st.Question
	if q != nil && q.Sub == SubValidating && q.PendingAnswerID != "" {
		g.room(EvValidationTimeout, ValidationTimeoutPayload{PersonID: q.PendingAnswerID, Answer: q.PendingAnswer, Rights: q.Rights, Wrongs: q.Wrongs})
	}
}

// ---- showman / host controls ----------------------------------------------

func (g *Game) onPause(cmd Command) error {
	if err := g.requireShowman(cmd.Actor); err != nil {
		return err
	}
	st := g.st
	if st.Stage == StageLobby || st.Stage == StageGameEnd {
		return fmt.Errorf("%w: nothing to pause", ErrBadState)
	}
	if cmd.On == st.Paused {
		return nil
	}
	st.Paused = cmd.On
	g.all(EvPause, PausePayload{On: cmd.On})
	if cmd.On {
		g.pauseAllTimers()
	} else {
		g.resumeAllTimers()
	}
	return nil
}

func (g *Game) onNext(cmd Command) error {
	if err := g.requireShowman(cmd.Actor); err != nil {
		return err
	}
	st := g.st
	switch st.Stage {
	case StageRoundIntro:
		g.enterRound()
		return nil
	case StageRoundEnd:
		st.WaitNext = false
		g.startRound(st.RoundIndex + 1)
		return nil
	case StageQuestion:
		q := st.Question
		switch q.Sub {
		case SubContent:
			if q.WaitNext || g.timerOfKind(TimerContent) != nil || g.timerOfKind(TimerMediaFallback) != nil || q.AwaitingMediaIDs {
				g.stopKind(TimerContent)
				g.stopKind(TimerMediaFallback)
				q.AwaitingMediaIDs = false
				g.contentGroupDone()
				return nil
			}
		case SubReveal:
			g.stopKind(TimerReflection)
			g.endQuestion()
			return nil
		case SubAnswering:
			if !q.Known { // custom question: the showman ends it
				g.showAnswer()
				return nil
			}
		case SubEnd:
			if q.WaitNext {
				q.WaitNext = false
				g.afterQuestion()
				return nil
			}
		}
	}
	return fmt.Errorf("%w: nothing to advance", ErrBadState)
}

func (g *Game) onMove(cmd Command) error {
	if err := g.requireShowman(cmd.Actor); err != nil {
		return err
	}
	st := g.st
	if st.Stage == StageLobby {
		return fmt.Errorf("%w: game not started", ErrBadState)
	}
	switch cmd.Dir {
	case 1:
		switch st.Stage {
		case StageRoundIntro:
			g.enterRound()
		case StageRoundEnd:
			st.WaitNext = false
			g.startRound(st.RoundIndex + 1)
		case StageQuestion:
			q := st.Question
			switch q.Sub {
			case SubReveal:
				g.stopKind(TimerReflection)
				g.endQuestion()
			case SubEnd:
				q.WaitNext = false
				g.afterQuestion()
			default:
				g.abortQuestionToAnswer()
			}
		default:
			return fmt.Errorf("%w: cannot move forward now", ErrBadState)
		}
	case -1:
		if st.Stage != StageQuestion || st.Question.Sub == SubEnd {
			return fmt.Errorf("%w: no question to return", ErrBadState)
		}
		g.returnQuestionToTable()
	case 2:
		g.endRound("manual")
	case -2:
		g.stopAllExcept()
		g.startRound(st.RoundIndex - 1)
	case 3:
		if cmd.Round < 0 || cmd.Round >= len(g.pack.Rounds) {
			return fmt.Errorf("%w: round %d out of range", ErrBadArgument, cmd.Round)
		}
		g.stopAllExcept()
		g.startRound(cmd.Round)
	default:
		return fmt.Errorf("%w: bad move dir %d", ErrBadArgument, cmd.Dir)
	}
	return nil
}

// abortQuestionToAnswer skips the rest of a question and reveals the answer.
func (g *Game) abortQuestionToAnswer() {
	q := g.st.Question
	g.stopAllExcept(TimerRound)
	g.st.Pending = nil
	if q.Armed {
		q.Armed = false
		g.room(EvButtonDisarm, ButtonDisarmRequestPayload{QuestionID: q.ID})
	}
	g.showAnswer()
}

// returnQuestionToTable cancels the current question and makes it available again.
func (g *Game) returnQuestionToTable() {
	st := g.st
	q := st.Question
	g.stopAllExcept(TimerRound)
	st.Pending = nil
	if q.Armed {
		q.Armed = false
		g.room(EvButtonDisarm, ButtonDisarmRequestPayload{QuestionID: q.ID})
	}
	// Undo score changes of this question.
	for i := len(q.History) - 1; i >= 0; i-- {
		o := q.History[i]
		if o.Reverted {
			continue
		}
		if p := st.player(o.PersonID); p != nil && o.Delta != 0 {
			g.addScore(p, -o.Delta, "correction")
		}
	}
	st.Table[q.ThemeIndex].Questions[q.QuestionIndex].Played = false
	g.all(EvQuestionEnd, QuestionEndPayload{ThemeIndex: q.ThemeIndex, QuestionIndex: q.QuestionIndex})
	st.Question = nil
	for _, p := range st.Players {
		p.CanPress = false
		p.State = PStateNone
		g.resetPlayerQuestion(p)
	}
	g.emitTable()
	g.emitSums()
	if st.RoundType == packs.RoundFinal {
		g.finalContinue()
		return
	}
	g.beginSelection(false)
}

func (g *Game) onToggle(cmd Command) error {
	if err := g.requireShowman(cmd.Actor); err != nil {
		return err
	}
	st := g.st
	if st.Stage == StageLobby || st.Stage == StageGameEnd {
		return fmt.Errorf("%w: no table", ErrBadState)
	}
	if cmd.Theme < 0 || cmd.Theme >= len(st.Table) || cmd.Q < 0 || cmd.Q >= len(st.Table[cmd.Theme].Questions) {
		return fmt.Errorf("%w: cell out of range", ErrBadArgument)
	}
	c := &st.Table[cmd.Theme].Questions[cmd.Q]
	if c.Price < 0 {
		return fmt.Errorf("%w: empty slot", ErrBadArgument)
	}
	if st.Question != nil && st.Stage == StageQuestion && st.Question.ThemeIndex == cmd.Theme && st.Question.QuestionIndex == cmd.Q {
		return fmt.Errorf("%w: cell is being played", ErrBadState)
	}
	if c.Removed || c.Played {
		c.Removed, c.Played = false, false
	} else {
		c.Removed = true
	}
	g.all(EvToggle, TogglePayload{ThemeIndex: cmd.Theme, QuestionIndex: cmd.Q, Active: !c.Removed})
	g.emitTable()
	if st.Stage == StageSelecting && st.Pending == nil && !g.hasPlayableCells() {
		g.endRound("empty")
	}
	return nil
}

func (g *Game) onChangeScore(cmd Command) error {
	if err := g.requireShowman(cmd.Actor); err != nil {
		return err
	}
	p := g.st.player(cmd.PersonID)
	if p == nil {
		return fmt.Errorf("%w: unknown player %q", ErrBadArgument, cmd.PersonID)
	}
	g.addScore(p, cmd.NewSum-p.Score, "correction")
	g.emitSums()
	return nil
}

func (g *Game) onSetChooser(cmd Command) error {
	if err := g.requireShowman(cmd.Actor); err != nil {
		return err
	}
	st := g.st
	p := st.player(cmd.PersonID)
	if p == nil || !p.Active() {
		return fmt.Errorf("%w: unknown or inactive player %q", ErrBadArgument, cmd.PersonID)
	}
	if st.Stage == StageLobby || st.Stage == StageGameEnd {
		return fmt.Errorf("%w: no chooser now", ErrBadState)
	}
	g.setChooser(p.ID, "showman")
	if st.Stage == StageSelecting && st.Pending != nil && st.Pending.Kind == PendingChooser {
		st.Pending = nil
		g.stopKind(TimerShowmanDecision)
		g.askChoose()
	} else if st.Stage == StageSelecting && st.Pending == nil && !g.sequential() {
		g.stopKind(TimerQuestionSelection)
		g.askChoose()
	}
	return nil
}

func (g *Game) onSetOptions(cmd Command) error {
	if err := g.requireShowman(cmd.Actor); err != nil {
		return err
	}
	o := cmd.Options
	if o == nil {
		return fmt.Errorf("%w: no options", ErrBadArgument)
	}
	r := &g.st.Rules
	t := &g.st.Times
	if o.Oral != nil {
		r.Oral = *o.Oral
	}
	if o.Managed != nil {
		r.Managed = *o.Managed
	}
	if o.DisplayAnswerOptionsLabels != nil {
		r.DisplayAnswerOptionsLabels = *o.DisplayAnswerOptionsLabels
	}
	if o.FalseStart != nil {
		r.FalseStart = *o.FalseStart
	}
	if o.ReadingSpeed != nil {
		if *o.ReadingSpeed < 0 {
			return fmt.Errorf("%w: negative reading speed", ErrBadArgument)
		}
		r.ReadingSpeed = *o.ReadingSpeed
	}
	if o.PartialText != nil {
		r.PartialText = *o.PartialText
	}
	if o.UseAppellations != nil {
		r.UseAppellations = *o.UseAppellations
	}
	if o.ButtonBlockingMs != nil {
		t.ButtonBlockingMs = *o.ButtonBlockingMs
	}
	if o.PartialImageMs != nil {
		t.PartialImageMs = *o.PartialImageMs
	}
	g.all(EvOptions, OptionsPayload{Rules: *r, Times: *t})
	return nil
}

func (g *Game) onKick(cmd Command) error {
	if !cmd.Actor.IsHost && cmd.Actor.Role != RoleSystem {
		return fmt.Errorf("%w: host required", ErrNotAllowed)
	}
	p := g.st.player(cmd.PersonID)
	if p == nil || p.Kicked {
		return fmt.Errorf("%w: unknown player %q", ErrBadArgument, cmd.PersonID)
	}
	if cmd.PersonID == cmd.Actor.PersonID {
		return fmt.Errorf("%w: cannot kick yourself", ErrNotAllowed)
	}
	p.Kicked = true
	p.Connected = false
	g.emitPlayers()
	g.playerGone(p)
	return nil
}

func (g *Game) onPlayerLeft(cmd Command) error {
	if err := g.requireSystem(cmd.Actor); err != nil {
		return err
	}
	p := g.st.player(cmd.PersonID)
	if p == nil {
		return fmt.Errorf("%w: unknown player %q", ErrBadArgument, cmd.PersonID)
	}
	if !p.Connected {
		return nil
	}
	p.Connected = false
	g.emitPlayers()
	g.playerGone(p)
	return nil
}

func (g *Game) onPlayerJoined(cmd Command) error {
	if err := g.requireSystem(cmd.Actor); err != nil {
		return err
	}
	st := g.st
	if p := st.player(cmd.PersonID); p != nil {
		if p.Kicked {
			return fmt.Errorf("%w: player %q was kicked", ErrNotAllowed, cmd.PersonID)
		}
		p.Connected = true
		if cmd.Name != "" {
			p.Name = cmd.Name
		}
		g.emitPlayers()
		return nil
	}
	if cmd.PersonID == "" || cmd.PersonID == st.ShowmanID {
		return fmt.Errorf("%w: invalid player id", ErrBadArgument)
	}
	n := 0
	for _, p := range st.Players {
		if !p.Kicked {
			n++
		}
	}
	if n >= MaxPlayers {
		return fmt.Errorf("%w: table is full", ErrBadState)
	}
	st.Players = append(st.Players, &Player{ID: cmd.PersonID, Name: cmd.Name, Connected: true, State: PStateNone})
	g.emitPlayers()
	g.emitSums()
	return nil
}

// playerGone handles a disconnected/kicked player that the game waits for.
// Timers keep running so that a reconnecting player can still act; the usual
// timeout behaviour applies. Hidden phases that only wait for this player
// complete immediately.
func (g *Game) playerGone(p *Player) {
	st := g.st
	q := st.Question
	if st.Stage != StageQuestion || q == nil {
		return
	}
	switch q.Sub {
	case SubHiddenAnswering:
		if g.allHiddenAnswered() {
			g.stopKind(TimerHiddenAnswering)
			g.hiddenAnsweringDone()
		}
	case SubStakes:
		if q.HiddenAll {
			if g.allHiddenStakesMade() {
				g.stopKind(TimerStakeMaking)
				g.hiddenStakesDone()
			}
		} else if q.Stakes != nil && q.Stakes.Active[p.ID] && q.Stakes.Leader != p.ID {
			g.stakeSetPass(p, false)
		}
	case SubContent:
		if q.AwaitingMediaIDs && g.allMediaDone() {
			g.stopKind(TimerMediaFallback)
			q.AwaitingMediaIDs = false
			g.contentGroupDone()
		}
	}
}

func (g *Game) resetPlayerQuestion(p *Player) {
	p.StakeMade, p.Stake, p.StakeAllIn, p.StakePassed = false, 0, false, false
	p.Answered, p.Answer, p.MediaDone, p.AppealUsed = false, AnswerView{}, false, false
}

// sortedByScore returns active players sorted by score ascending (stable by seat).
func (g *Game) sortedByScore() []*Player {
	ps := g.st.activePlayers()
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Score < ps[j].Score })
	return ps
}
