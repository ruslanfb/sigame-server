package engine

import "fmt"

// Stake (auction) questions, docs/research/01-sigame-rules.md §5.2.

func (g *Game) stakeBegin() {
	st := g.st
	q := st.Question
	q.Direct = true
	chooser := g.currentChooser()
	ss := &StakeState{Step: g.stakeStep(), Nominal: q.Price, Active: map[string]bool{}, Bids: map[string]int{},
		HasBid: map[string]bool{}, AllIn: map[string]bool{}}
	for _, p := range st.activePlayers() {
		if p.ID == chooser || p.Score > q.Price {
			ss.Participants = append(ss.Participants, p.ID)
			ss.Active[p.ID] = true
			continue
		}
		g.all(EvPersonStake, PersonStakePayload{PersonID: p.ID, Mode: StakePass})
		g.setPlayerState(p, PStatePass)
	}
	q.Stakes = ss
	g.setSub(SubStakes)
	if chooser == "" {
		g.showAnswer()
		return
	}
	g.stakeAskBid(chooser)
}

func roundUp(v, step int) int {
	if step <= 1 || v%step == 0 {
		return v
	}
	return (v/step + 1) * step
}

// stakeAskBid gives the turn to a bidder, forcing nominal / auto-pass where
// the rules leave no choice.
func (g *Game) stakeAskBid(id string) {
	st := g.st
	q := st.Question
	ss := q.Stakes
	p := st.player(id)
	st.Pending = nil
	ss.Current = id
	if !ss.HasBid[id] {
		seen := false
		for _, o := range ss.Order {
			if o == id {
				seen = true
			}
		}
		if !seen {
			ss.Order = append(ss.Order, id)
		}
	}
	if ss.Stake > 0 && p.Score <= ss.Stake {
		g.stakeSetPass(p, false)
		return
	}
	var modes []StakeMode
	var minB, maxB int
	if ss.Stake == 0 {
		if p.Score < ss.Nominal || len(ss.Participants) == 1 {
			g.stakeApply(p, StakeNominal, ss.Nominal)
			return
		}
		minB, maxB = ss.Nominal, p.Score
		modes = []StakeMode{StakeNominal}
		if p.Score > ss.Nominal {
			modes = append(modes, StakeStake)
		}
		modes = append(modes, StakeAllIn)
	} else {
		minB, maxB = roundUp(ss.Stake+ss.Step, ss.Step), p.Score
		modes = []StakeMode{StakePass}
		if !ss.AnyAllIn && minB <= p.Score {
			modes = append(modes, StakeStake)
		}
		modes = append(modes, StakeAllIn)
	}
	ss.AllowedModes, ss.Min, ss.Max = modes, minB, maxB
	dur := st.Times.StakeMakingMs
	pl := AskStakePayload{PersonID: id, Modes: modes, Min: minB, Max: maxB, Step: ss.Step, Reason: "stake", DurationMs: dur}
	g.to(id, EvAskStake, pl)
	if st.Rules.Oral {
		g.showman(EvAskStake, pl)
	}
	g.startTimer(TimerStakeMaking, dur, id)
}

func stakeModeAllowed(ss *StakeState, mode StakeMode) bool {
	for _, m := range ss.AllowedModes {
		if m == mode {
			return true
		}
	}
	return false
}

// stakeApply records a bid of the current bidder.
func (g *Game) stakeApply(p *Player, mode StakeMode, amount int) {
	st := g.st
	ss := st.Question.Stakes
	g.stopKind(TimerStakeMaking)
	ss.HasBid[p.ID] = true
	switch mode {
	case StakeNominal:
		amount = ss.Nominal
	case StakeAllIn:
		amount = p.Score
		ss.AllIn[p.ID] = true
		ss.AnyAllIn = true
	}
	ss.Bids[p.ID] = amount
	ss.Stake = amount
	ss.Leader = p.ID
	g.all(EvPersonStake, PersonStakePayload{PersonID: p.ID, Mode: mode, Amount: amount})
	for _, id := range ss.Participants {
		if id == p.ID || !ss.Active[id] {
			continue
		}
		if o := st.player(id); o.Score <= amount {
			g.stakeMarkPass(o)
		}
	}
	g.stakeAdvance()
}

func (g *Game) stakeMarkPass(p *Player) {
	ss := g.st.Question.Stakes
	ss.Active[p.ID] = false
	ss.HasBid[p.ID] = true
	g.all(EvPersonStake, PersonStakePayload{PersonID: p.ID, Mode: StakePass})
	g.setPlayerState(p, PStatePass)
}

// stakeSetPass handles a pass by the current bidder (turn, timeout,
// disconnect) or an early pass by another bidder.
func (g *Game) stakeSetPass(p *Player, voluntary bool) {
	ss := g.st.Question.Stakes
	if !ss.Active[p.ID] {
		return
	}
	g.stakeMarkPass(p)
	if ss.Current == p.ID {
		g.stopKind(TimerStakeMaking)
		g.stakeAdvance()
		return
	}
	if active := g.stakeActiveIDs(); len(active) == 1 && active[0] == ss.Leader {
		g.stakeWinner(ss.Leader)
	}
}

func (g *Game) stakeActiveIDs() []string {
	ss := g.st.Question.Stakes
	var out []string
	for _, id := range ss.Participants {
		if ss.Active[id] {
			out = append(out, id)
		}
	}
	return out
}

// stakeAdvance picks the next bidder or the winner.
func (g *Game) stakeAdvance() {
	st := g.st
	ss := st.Question.Stakes
	active := g.stakeActiveIDs()
	if len(active) == 0 {
		if ss.Leader == "" {
			g.showAnswer()
			return
		}
		g.stakeWinner(ss.Leader)
		return
	}
	if len(active) == 1 && active[0] == ss.Leader {
		g.stakeWinner(ss.Leader)
		return
	}
	var unbid []*Player
	for _, id := range active {
		if !ss.HasBid[id] {
			unbid = append(unbid, st.player(id))
		}
	}
	if len(unbid) > 0 {
		var low []*Player
		for _, p := range unbid {
			if low == nil || p.Score < low[0].Score {
				low = []*Player{p}
			} else if p.Score == low[0].Score {
				low = append(low, p)
			}
		}
		if len(low) == 1 {
			g.stakeAskBid(low[0].ID)
			return
		}
		g.askSelectPlayer(PendingStaker, st.ShowmanID, ids(low))
		return
	}
	n := len(ss.Order)
	start := 0
	for i, id := range ss.Order {
		if id == ss.Current {
			start = i
		}
	}
	for i := 1; i <= n; i++ {
		id := ss.Order[(start+i)%n]
		if ss.Active[id] && id != ss.Leader {
			g.stakeAskBid(id)
			return
		}
	}
	g.stakeWinner(ss.Leader)
}

func (g *Game) stakeWinner(id string) {
	st := g.st
	q := st.Question
	ss := q.Stakes
	g.stopKind(TimerStakeMaking)
	st.Pending = nil
	q.AnswererID = id
	q.CurPriceRight = ss.Stake
	q.CurPriceWrong = g.wrongPrice(PenaltySubtract, ss.Stake)
	g.setChooser(id, "stakeWinner")
	g.all(EvQuestionCaption, QuestionCaptionPayload{Theme: q.Caption, Price: ss.Stake})
	g.playContent()
}

func (g *Game) onSetStake(cmd Command) error {
	st := g.st
	q := st.Question
	if q == nil || st.Stage != StageQuestion {
		return fmt.Errorf("%w: no question", ErrBadState)
	}
	switch q.Sub {
	case SubPriceSelect:
		p, err := g.actingPlayer(cmd, q.AnswererID)
		if err != nil {
			return err
		}
		if p.ID != q.AnswererID {
			return fmt.Errorf("%w: only the recipient picks the price", ErrNotAllowed)
		}
		amount := cmd.Amount
		switch cmd.StakeMode {
		case StakeNominal:
			amount = q.PriceChoices[0]
		case StakeAllIn:
			amount = q.PriceChoices[len(q.PriceChoices)-1]
		case StakeStake, "":
		default:
			return fmt.Errorf("%w: mode %q not allowed", ErrBadArgument, cmd.StakeMode)
		}
		ok := false
		for _, c := range q.PriceChoices {
			if c == amount {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("%w: price %d is not one of %v", ErrBadArgument, amount, q.PriceChoices)
		}
		g.stopKind(TimerStakeMaking)
		g.secretAfterPrice(amount)
		return nil
	case SubStakes:
		if q.HiddenAll {
			return g.hiddenSetStake(cmd)
		}
		ss := q.Stakes
		p, err := g.actingPlayer(cmd, ss.Current)
		if err != nil {
			return err
		}
		if p.ID != ss.Current {
			return fmt.Errorf("%w: not your turn to bid", ErrNotAllowed)
		}
		mode := cmd.StakeMode
		if !stakeModeAllowed(ss, mode) {
			return fmt.Errorf("%w: mode %q not allowed (allowed %v)", ErrBadArgument, mode, ss.AllowedModes)
		}
		if mode == StakePass {
			g.stakeSetPass(p, true)
			return nil
		}
		if mode == StakeStake {
			a := cmd.Amount
			if a < ss.Min || a > ss.Max || ((a-ss.Min)%ss.Step != 0 && a != ss.Max) {
				return fmt.Errorf("%w: stake %d must be in [%d,%d] step %d", ErrBadArgument, a, ss.Min, ss.Max, ss.Step)
			}
		}
		g.stakeApply(p, mode, cmd.Amount)
		return nil
	}
	return fmt.Errorf("%w: no stake expected", ErrBadState)
}

func (g *Game) stakeTimeout(t *Timer) {
	st := g.st
	q := st.Question
	if q == nil || st.Stage != StageQuestion {
		return
	}
	switch q.Sub {
	case SubPriceSelect:
		g.secretAfterPrice(q.PriceChoices[0])
	case SubStakes:
		if q.HiddenAll {
			g.hiddenStakesTimeout()
			return
		}
		ss := q.Stakes
		p := st.player(ss.Current)
		if p == nil {
			return
		}
		if stakeModeAllowed(ss, StakePass) {
			g.stakeSetPass(p, false)
			return
		}
		g.stakeApply(p, StakeNominal, ss.Nominal)
	}
}
