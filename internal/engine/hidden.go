package engine

import (
	"fmt"

	"sigame/internal/packs"
)

// forAll (§5.5) and stakeAll (§5.6): written answers from several players,
// revealed and validated one by one.

func (g *Game) forAllBegin() {
	st := g.st
	q := st.Question
	q.Hidden = true
	q.CurPriceWrong = g.wrongPrice(st.Rules.ForAllPenalty, q.Price)
	for _, p := range st.activePlayers() {
		p.InGame = true
		q.Answerers = append(q.Answerers, p.ID)
	}
	g.playContent()
}

// stakeAllParticipants: final round → players admitted at the round start;
// otherwise players with a positive score, or everyone when allowed.
func (g *Game) stakeAllParticipants() []*Player {
	st := g.st
	var out []*Player
	for _, p := range st.activePlayers() {
		if st.RoundType == packs.RoundFinal {
			if p.FinalInGame {
				out = append(out, p)
			}
			continue
		}
		if p.Score > 0 || st.Rules.AllowEveryoneToPlayHiddenStakes {
			out = append(out, p)
		}
	}
	return out
}

func (g *Game) stakeAllBegin() {
	st := g.st
	q := st.Question
	q.Hidden, q.HiddenAll = true, true
	parts := g.stakeAllParticipants()
	if len(parts) == 0 {
		g.showAnswer()
		return
	}
	in := map[string]bool{}
	for _, p := range parts {
		p.InGame = true
		in[p.ID] = true
		q.Answerers = append(q.Answerers, p.ID)
	}
	for _, p := range st.activePlayers() {
		if !in[p.ID] {
			g.setPlayerState(p, PStatePass)
		}
	}
	g.setSub(SubStakes)
	dur := st.Times.StakeMakingMs
	missing := 0
	for _, p := range parts {
		if p.Score <= 1 {
			g.hiddenStakeSet(p, 1)
			continue
		}
		missing++
		pl := AskStakePayload{PersonID: p.ID, Modes: []StakeMode{StakeStake, StakeAllIn}, Min: 1, Max: p.Score, Step: 1, Reason: "hiddenStake", DurationMs: dur}
		g.to(p.ID, EvAskStake, pl)
		if st.Rules.Oral {
			g.showman(EvAskStake, pl)
		}
	}
	if missing == 0 {
		g.hiddenStakesDone()
		return
	}
	g.startTimer(TimerStakeMaking, dur, "")
}

func (g *Game) hiddenStakeSet(p *Player, amount int) {
	p.StakeMade, p.Stake = true, amount
	p.StakeAllIn = amount == p.Score && p.Score > 0
	g.emit(EvPersonStake, ToExcept(g.st.ShowmanID), PersonStakePayload{PersonID: p.ID, Mode: StakeStake, Hidden: true})
	g.showman(EvPersonStake, PersonStakePayload{PersonID: p.ID, Mode: StakeStake, Amount: amount, Hidden: true})
}

func (g *Game) hiddenSetStake(cmd Command) error {
	p, err := g.actingPlayer(cmd, "")
	if err != nil {
		return err
	}
	if !p.InGame || !g.isAnswerer(p.ID) {
		return fmt.Errorf("%w: not taking part", ErrNotAllowed)
	}
	if p.StakeMade {
		return fmt.Errorf("%w: stake already made", ErrBadState)
	}
	amount := cmd.Amount
	switch cmd.StakeMode {
	case StakeAllIn:
		amount = p.Score
	case StakeStake, "":
	default:
		return fmt.Errorf("%w: mode %q not allowed", ErrBadArgument, cmd.StakeMode)
	}
	maxS := p.Score
	if maxS < 1 {
		maxS = 1
	}
	if amount < 1 || amount > maxS {
		return fmt.Errorf("%w: stake %d must be in [1,%d]", ErrBadArgument, amount, maxS)
	}
	g.hiddenStakeSet(p, amount)
	if g.allHiddenStakesMade() {
		g.stopKind(TimerStakeMaking)
		g.hiddenStakesDone()
	}
	return nil
}

func (g *Game) allHiddenStakesMade() bool {
	for _, id := range g.st.Question.Answerers {
		if p := g.st.player(id); p.Active() && p.InGame && !p.StakeMade {
			return false
		}
	}
	return true
}

func (g *Game) hiddenStakesTimeout() {
	for _, id := range g.st.Question.Answerers {
		if p := g.st.player(id); p.InGame && !p.StakeMade {
			g.hiddenStakeSet(p, 1)
		}
	}
	g.hiddenStakesDone()
}

func (g *Game) hiddenStakesDone() {
	q := g.st.Question
	if q == nil || q.Sub != SubStakes {
		return
	}
	for _, id := range q.Answerers {
		if p := g.st.player(id); !p.StakeMade {
			g.hiddenStakeSet(p, 1)
		}
	}
	g.playContent()
}

func (g *Game) hiddenAnsweringBegin() {
	st := g.st
	q := st.Question
	dur := g.answerDuration(st.Times.HiddenAnsweringMs)
	asked := 0
	for _, id := range q.Answerers {
		p := st.player(id)
		if !p.Active() {
			continue
		}
		asked++
		g.setPlayerState(p, PStateAnswering)
		g.to(p.ID, EvAskAnswer, AskAnswerPayload{PersonID: p.ID, AnswerType: q.AnswerType, DurationMs: dur, Hidden: true})
	}
	if asked == 0 {
		g.showAnswer()
		return
	}
	g.setSub(SubHiddenAnswering)
	g.all(EvFinalThink, FinalThinkPayload{DurationMs: dur})
	g.startTimer(TimerHiddenAnswering, dur, "")
}

func (g *Game) allHiddenAnswered() bool {
	for _, id := range g.st.Question.Answerers {
		if p := g.st.player(id); p.Active() && p.InGame && !p.Answered {
			return false
		}
	}
	return true
}

func (g *Game) hiddenAnsweringDone() {
	q := g.st.Question
	if q == nil || q.Sub != SubHiddenAnswering {
		return
	}
	q.RevealIdx = 0
	g.hiddenRevealNext()
}

// hiddenRevealNext reveals answers in seat order until one needs the showman.
func (g *Game) hiddenRevealNext() {
	st := g.st
	q := st.Question
	for q.RevealIdx < len(q.Answerers) {
		p := st.player(q.Answerers[q.RevealIdx])
		q.RevealIdx++
		if !p.Answered && !p.Active() {
			continue // left without answering: skipped
		}
		av := p.Answer
		if !p.Answered {
			av = AnswerView{Text: "-"}
		}
		text := answerText(av)
		g.all(EvPlayerAnswer, PlayerAnswerPayload{PersonID: p.ID, Answer: av})
		if !p.Answered {
			g.hiddenScore(p, "-", false, 1, "auto")
			continue
		}
		if right, auto := g.autoCheck(av); auto {
			g.hiddenScore(p, text, right, 1, "auto")
			continue
		}
		if v, ok := q.PreVerdicts[p.ID]; ok {
			g.hiddenScore(p, text, v.right, v.factor, "showman")
			continue
		}
		if v, ok := q.ValidationCache[normAnswer(text)]; ok {
			g.hiddenScore(p, text, v.right, v.factor, "auto")
			continue
		}
		g.askValidate(p.ID, text)
		return
	}
	g.showAnswer()
}

// hiddenScore applies one revealed verdict (±price or ±stake).
func (g *Game) hiddenScore(p *Player, answer string, right bool, factor float64, source string) {
	q := g.st.Question
	q.PendingAnswerID, q.PendingAnswer = "", ""
	priceR, priceW := q.CurPriceRight, q.CurPriceWrong
	if q.HiddenAll {
		priceR, priceW = p.Stake, g.wrongPrice(PenaltySubtract, p.Stake)
		g.all(EvPersonStake, PersonStakePayload{PersonID: p.ID, Mode: StakeStake, Amount: p.Stake})
	}
	g.applyOutcome(p, answer, right, factor, source, priceR, priceW)
}

func (g *Game) hiddenApplyVerdict(p *Player, answer string, right bool, factor float64, source string) {
	g.hiddenScore(p, answer, right, factor, source)
	g.hiddenRevealNext()
}
