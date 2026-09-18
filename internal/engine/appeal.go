package engine

import "fmt"

// Appeals, docs/research/01-sigame-rules.md §11.

func (g *Game) onAppellate(cmd Command) error {
	st := g.st
	p, err := g.playerActor(cmd.Actor)
	if err != nil {
		return err
	}
	if !st.Rules.UseAppellations {
		return fmt.Errorf("%w: appeals are disabled", ErrNotAllowed)
	}
	q := st.Question
	if q == nil || len(q.History) == 0 || st.Stage == StageLobby || st.Stage == StageGameEnd {
		return fmt.Errorf("%w: nothing to appeal", ErrBadState)
	}
	var req AppealRequest
	if cmd.For {
		idx := -1
		for i := len(q.History) - 1; i >= 0; i-- {
			o := q.History[i]
			if o.PersonID == p.ID && !o.Right && !o.Pass && !o.Reverted {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: no rejected answer to appeal", ErrNotAllowed)
		}
		if p.AppealUsed {
			return fmt.Errorf("%w: already appealed this question", ErrNotAllowed)
		}
		p.AppealUsed = true
		req = AppealRequest{Kind: AppealFor, AppellantID: p.ID, AnswererID: p.ID, OutcomeIdx: idx}
	} else {
		if len(st.activePlayers()) <= 3 {
			return fmt.Errorf("%w: AppellationFailedTooFewPlayers: at least 4 connected players are required", ErrNotAllowed)
		}
		if q.Hidden {
			return fmt.Errorf("%w: only single-answerer questions can be contested", ErrNotAllowed)
		}
		if q.AgainstUsed {
			return fmt.Errorf("%w: this answer was already contested", ErrNotAllowed)
		}
		idx := -1
		for i := len(q.History) - 1; i >= 0; i-- {
			if o := q.History[i]; o.Right && !o.Reverted {
				idx = i
				break
			}
		}
		if idx < 0 {
			return fmt.Errorf("%w: no accepted answer to contest", ErrNotAllowed)
		}
		if q.History[idx].PersonID == p.ID {
			return fmt.Errorf("%w: cannot contest your own answer", ErrNotAllowed)
		}
		q.AgainstUsed = true
		req = AppealRequest{Kind: AppealAgainst, AppellantID: p.ID, AnswererID: q.History[idx].PersonID, OutcomeIdx: idx}
	}
	p.Appeals++
	q.AppealQueue = append(q.AppealQueue, req)
	if q.Sub == SubEnd && st.Appeal == nil {
		g.processAppeals()
	}
	return nil
}

// processAppeals starts the next queued appeal once the question is over.
func (g *Game) processAppeals() {
	st := g.st
	q := st.Question
	if st.Appeal != nil || q == nil || len(q.AppealQueue) == 0 {
		return
	}
	req := q.AppealQueue[0]
	q.AppealQueue = q.AppealQueue[1:]
	if q.History[req.OutcomeIdx].Reverted {
		g.processAppeals()
		return
	}
	g.appealStart(req)
}

func (g *Game) appealStart(req AppealRequest) {
	st := g.st
	q := st.Question
	a := &Appeal{AppealRequest: req, Votes: map[string]bool{}}
	for _, p := range st.activePlayers() {
		a.Voters = append(a.Voters, p.ID)
	}
	a.Voters = append(a.Voters, st.ShowmanID)
	a.Total = len(a.Voters)
	if req.Kind == AppealFor {
		a.Votes[req.AppellantID] = true
		a.Votes[st.ShowmanID] = false
	} else {
		a.Votes[st.ShowmanID] = true
		a.Votes[req.AnswererID] = true
		a.Votes[req.AppellantID] = false
	}
	for _, v := range a.Voters {
		if _, ok := a.Votes[v]; !ok {
			a.Pending = append(a.Pending, v)
		}
	}
	st.Appeal = a
	for _, t := range st.Timers {
		g.pauseTimer(t)
	}
	answer := q.History[req.OutcomeIdx].Answer
	dur := st.Times.AppellationMs
	g.all(EvAppealStart, AppealStartPayload{Kind: string(req.Kind), AppellantID: req.AppellantID, AnswererID: req.AnswererID, Answer: answer, Voters: a.Voters, DurationMs: dur})
	for _, v := range a.Pending {
		g.to(v, EvAskAppealVote, AskAppealVotePayload{Kind: string(req.Kind), AnswererID: req.AnswererID, Answer: answer, Rights: q.Rights})
	}
	if g.appealDecided() {
		g.appealFinish()
		return
	}
	g.startTimer(TimerAppellation, dur, "")
	if st.Paused { // started paused; the vote waits for the showman to resume
		return
	}
	if t := g.timerOfKind(TimerAppellation); t != nil && t.Paused {
		g.resumeTimer(t)
	}
}

func (g *Game) appealCounts() (int, int) {
	f, ag := 0, 0
	for _, v := range g.st.Appeal.Votes {
		if v {
			f++
		} else {
			ag++
		}
	}
	return f, ag
}

func (g *Game) appealDecided() bool {
	a := g.st.Appeal
	f, ag := g.appealCounts()
	return len(a.Pending) == 0 || f*2 > a.Total || ag*2 > a.Total
}

func (g *Game) onVoteAppeal(cmd Command) error {
	st := g.st
	a := st.Appeal
	if a == nil {
		return fmt.Errorf("%w: no appeal in progress", ErrBadState)
	}
	p, err := g.playerActor(cmd.Actor)
	if err != nil {
		return err
	}
	idx := -1
	for i, v := range a.Pending {
		if v == p.ID {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("%w: not a pending voter", ErrNotAllowed)
	}
	a.Pending = append(a.Pending[:idx], a.Pending[idx+1:]...)
	a.Votes[p.ID] = cmd.Right
	g.all(EvAppealVote, AppealVotePayload{PersonID: p.ID, Right: cmd.Right})
	if g.appealDecided() {
		g.appealFinish()
	}
	return nil
}

// appealFinish tallies the votes, applies the result and resumes the game.
func (g *Game) appealFinish() {
	st := g.st
	a := st.Appeal
	if a == nil {
		return
	}
	q := st.Question
	g.stopKind(TimerAppellation)
	f, ag := g.appealCounts()
	accepted := false
	chooserChanged := false
	if a.Kind == AppealFor {
		accepted = f > ag
	} else {
		accepted = ag > f
	}
	if accepted {
		chooserChanged = g.appealApply(a)
	}
	g.all(EvAppealResult, AppealResultPayload{Kind: string(a.Kind), AnswererID: a.AnswererID, Accepted: accepted, For: f, Against: ag})
	st.Appeal = nil
	g.emitSums()
	if len(q.AppealQueue) > 0 {
		g.processAppeals()
		return
	}
	for _, t := range st.Timers {
		if t.Frozen && !st.Paused {
			g.resumeTimer(t)
		} else if t.Frozen {
			t.Frozen = false // stays paused until the showman resumes
		}
	}
	if st.Stage == StageQuestion && q.Sub == SubEnd {
		g.afterQuestion()
		return
	}
	if chooserChanged && st.Stage == StageSelecting && st.Pending == nil && !g.sequential() {
		g.stopKind(TimerQuestionSelection)
		g.askChoose()
	}
}

// appealApply flips the contested outcome. Returns true if the chooser changed.
func (g *Game) appealApply(a *Appeal) bool {
	st := g.st
	q := st.Question
	o := &q.History[a.OutcomeIdx]
	p := st.player(o.PersonID)
	if p == nil {
		return false
	}
	o.Reverted = true
	g.addScore(p, -o.Delta, "appeal")
	var n Outcome
	if a.Kind == AppealFor {
		priceR := q.CurPriceRight
		if q.HiddenAll {
			priceR = p.Stake
		}
		n = Outcome{PersonID: p.ID, Answer: o.Answer, Right: true, Factor: o.Factor, Delta: scaled(priceR, o.Factor)}
		p.Wrong--
		p.Right++
		p.AcceptedAnswers = append(p.AcceptedAnswers, o.Answer)
		g.all(EvValidation, ValidationPayload{PersonID: p.ID, Answer: o.Answer, Right: true, Factor: o.Factor, Source: "appeal"})
		g.addScore(p, n.Delta, "appeal")
		g.setPlayerState(p, PStateRight)
		if !q.Hidden {
			for i := a.OutcomeIdx + 1; i < len(q.History); i++ {
				l := &q.History[i]
				if l.Reverted {
					continue
				}
				l.Reverted = true
				if lp := st.player(l.PersonID); lp != nil {
					g.addScore(lp, -l.Delta, "appeal")
					if l.Right {
						lp.Right--
					} else if !l.Pass {
						lp.Wrong--
					}
					g.setPlayerState(lp, PStateNone)
				}
			}
		}
	} else {
		n = Outcome{PersonID: p.ID, Answer: o.Answer, Right: false, Factor: o.Factor, Delta: -scaled(q.CurPriceWrong, o.Factor)}
		p.Right--
		p.Wrong++
		p.RejectedAnswers = append(p.RejectedAnswers, o.Answer)
		g.all(EvValidation, ValidationPayload{PersonID: p.ID, Answer: o.Answer, Right: false, Factor: o.Factor, Source: "appeal"})
		g.addScore(p, n.Delta, "appeal")
		g.setPlayerState(p, PStateWrong)
	}
	q.History = append(q.History, n)
	if a.Kind == AppealFor && st.Rules.Mode == ModeClassic && !q.Hidden && st.ChooserID != p.ID {
		g.setChooser(p.ID, "appeal")
		return true
	}
	return false
}
