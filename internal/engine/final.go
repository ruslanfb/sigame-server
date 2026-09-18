package engine

import (
	"fmt"
	"sort"
)

// Final (theme-list) round, docs/research/01-sigame-rules.md §10.

func (g *Game) startFinalRound() {
	st := g.st
	active := st.activePlayers()
	n := 0
	for _, p := range active {
		p.FinalInGame = p.Score > 0
		if p.FinalInGame {
			n++
		}
	}
	if n == 0 && st.Rules.AllowEveryoneToPlayHiddenStakes {
		for _, p := range active {
			p.FinalInGame = true
		}
		n = len(active)
	}
	if n == 0 {
		g.endRound("noPlayers")
		return
	}
	for _, p := range active {
		if !p.FinalInGame {
			g.setPlayerState(p, PStatePass)
		}
	}
	g.emitPlayers()
	if st.Rules.PlayAllQuestionsInFinal {
		g.finalContinue()
		return
	}
	st.Stage = StageFinalThemes
	g.emitStage()
	g.finalBuildOrder()
	g.finalNextDeleter()
}

// finalRemainingThemes lists themes that are not deleted and still have a cell.
func (g *Game) finalRemainingThemes() []int {
	var out []int
	for t, th := range g.st.Table {
		if th.Removed {
			continue
		}
		for _, c := range th.Questions {
			if !c.Played && !c.Removed {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// finalBuildOrder groups participants by score ascending and rotates the
// sequence so that the highest-score group performs the last deletion.
func (g *Game) finalBuildOrder() {
	st := g.st
	var ps []*Player
	for _, p := range st.activePlayers() {
		if p.FinalInGame {
			ps = append(ps, p)
		}
	}
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Score < ps[j].Score })
	st.FinalGroups = nil
	for i, p := range ps {
		if i > 0 && ps[i-1].Score == p.Score {
			st.FinalGroups[len(st.FinalGroups)-1] = append(st.FinalGroups[len(st.FinalGroups)-1], p.ID)
			continue
		}
		st.FinalGroups = append(st.FinalGroups, []string{p.ID})
	}
	n := len(ps)
	d := len(g.finalRemainingThemes()) - 1
	st.FinalOffset = 0
	if n > 0 && d > 0 {
		st.FinalOffset = (n - d%n) % n
	}
	st.DeleteStep = 0
	st.DeleterUsed = map[string]bool{}
}

func (g *Game) finalGroupOf(id string) int {
	for gi, grp := range g.st.FinalGroups {
		for _, m := range grp {
			if m == id {
				return gi
			}
		}
	}
	return -1
}

// finalNextDeleter asks the next deleter (or the showman on ties).
func (g *Game) finalNextDeleter() {
	st := g.st
	if len(g.finalRemainingThemes()) <= 1 {
		g.finalPlayLastTheme()
		return
	}
	var slots []int
	for gi, grp := range st.FinalGroups {
		for range grp {
			slots = append(slots, gi)
		}
	}
	n := len(slots)
	for tries := 0; tries < n; tries++ {
		gi := slots[(st.DeleteStep+st.FinalOffset)%n]
		var members, cands []string
		for _, id := range st.FinalGroups[gi] {
			if p := st.player(id); p != nil && p.Active() {
				members = append(members, id)
				if !st.DeleterUsed[fmt.Sprintf("%d:%s", gi, id)] {
					cands = append(cands, id)
				}
			}
		}
		if len(members) == 0 {
			st.DeleteStep++
			continue
		}
		if len(cands) == 0 {
			for _, id := range members {
				delete(st.DeleterUsed, fmt.Sprintf("%d:%s", gi, id))
			}
			cands = members
		}
		if len(cands) == 1 {
			g.finalAskDelete(cands[0])
			return
		}
		g.askSelectPlayer(PendingDeleter, st.ShowmanID, cands)
		return
	}
	g.finalDeleteTimeout() // nobody connected: the engine deletes at random
}

func (g *Game) finalAskDelete(id string) {
	st := g.st
	st.Pending = nil
	if gi := g.finalGroupOf(id); gi >= 0 {
		st.DeleterUsed[fmt.Sprintf("%d:%s", gi, id)] = true
	}
	st.DeleteStep++
	st.DeleterID = id
	dur := st.Times.ThemeSelectionMs
	pl := AskDeleteThemePayload{PersonID: id, Themes: g.finalRemainingThemes(), DurationMs: dur}
	g.to(id, EvAskDeleteTheme, pl)
	if st.Rules.Oral {
		g.showman(EvAskDeleteTheme, pl)
	}
	g.startTimer(TimerThemeSelection, dur, id)
}

func (g *Game) onDeleteTheme(cmd Command) error {
	st := g.st
	if st.Stage != StageFinalThemes || st.DeleterID == "" || st.Pending != nil {
		return fmt.Errorf("%w: no theme deletion pending", ErrBadState)
	}
	p, err := g.actingPlayer(cmd, st.DeleterID)
	if err != nil {
		return err
	}
	if p.ID != st.DeleterID {
		return fmt.Errorf("%w: it is %q's turn to delete", ErrNotAllowed, st.DeleterID)
	}
	ok := false
	for _, t := range g.finalRemainingThemes() {
		if t == cmd.Theme {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("%w: theme %d cannot be deleted", ErrBadArgument, cmd.Theme)
	}
	g.stopKind(TimerThemeSelection)
	g.finalDelete(cmd.Theme, p.ID)
	return nil
}

func (g *Game) finalDelete(t int, by string) {
	st := g.st
	st.Table[t].Removed = true
	st.DeleterID = ""
	g.all(EvThemeDeleted, ThemeDeletedPayload{ThemeIndex: t, PersonID: by})
	g.emitTable()
	g.finalNextDeleter()
}

func (g *Game) finalDeleteTimeout() {
	st := g.st
	rem := g.finalRemainingThemes()
	if len(rem) <= 1 {
		g.finalPlayLastTheme()
		return
	}
	g.finalDelete(rem[g.randomIndex(len(rem))], st.DeleterID)
}

// finalPlayLastTheme plays the first available question of the remaining
// theme; its other cells are removed (only the first question is played).
func (g *Game) finalPlayLastTheme() {
	st := g.st
	rem := g.finalRemainingThemes()
	if len(rem) == 0 {
		g.endRound("empty")
		return
	}
	t := rem[0]
	first := -1
	for q, c := range st.Table[t].Questions {
		if !c.Played && !c.Removed {
			if first < 0 {
				first = q
			} else {
				st.Table[t].Questions[q].Removed = true
			}
		}
	}
	if first < 0 {
		g.endRound("empty")
		return
	}
	g.all(EvQuestionCaption, QuestionCaptionPayload{Theme: st.Table[t].Name})
	g.startQuestion(t, first)
}

// finalContinue runs after a final-round question ended or was returned.
func (g *Game) finalContinue() {
	st := g.st
	if st.RoundTimedOut {
		g.endRound("timeout")
		return
	}
	if st.Rules.PlayAllQuestionsInFinal {
		t, q, ok := g.nextSequentialCell()
		if !ok {
			g.endRound("empty")
			return
		}
		g.all(EvQuestionCaption, QuestionCaptionPayload{Theme: st.Table[t].Name})
		g.startQuestion(t, q)
		return
	}
	if len(g.finalRemainingThemes()) == 1 {
		g.finalPlayLastTheme()
		return
	}
	g.endRound("empty")
}
