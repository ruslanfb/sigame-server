package engine

import "sigame/internal/packs"

// Stage is the top-level game stage.
type Stage string

const (
	StageLobby       Stage = "lobby"
	StageRoundIntro  Stage = "roundIntro"  // round announced; waits for Next in Managed mode
	StageSelecting   Stage = "selecting"   // chooser picks a cell (or a chooser tie is being resolved)
	StageQuestion    Stage = "question"    // a question is being played (see QSub)
	StageRoundEnd    Stage = "roundEnd"    // round finished; waits for Next in Managed mode
	StageFinalThemes Stage = "finalThemes" // final round: themes are being deleted
	StageGameEnd     Stage = "gameEnd"
)

// QSub is the sub-state of a question.
type QSub string

const (
	SubAnnouncing      QSub = "announcing"      // question announced, nothing asked yet (transient)
	SubSecretTransfer  QSub = "secretTransfer"  // chooser picks the recipient
	SubPriceSelect     QSub = "priceSelect"     // recipient picks the secret price
	SubStakes          QSub = "stakes"          // bidding (stake) or hidden stakes (stakeAll)
	SubContent         QSub = "content"         // question content is being shown
	SubButtonWait      QSub = "buttonWait"      // button open, waiting for the buzzer result
	SubAnswering       QSub = "answering"       // one player answers directly / after the button
	SubValidating      QSub = "validating"      // waiting for the showman verdict
	SubHiddenAnswering QSub = "hiddenAnswering" // written answers (forAll / stakeAll)
	SubReveal          QSub = "reveal"          // right answer shown (reflection timer / Next)
	SubEnd             QSub = "end"             // question finished; appeals may still run
)

// PlayerState mirrors SIGame PLAYER_STATE values.
type PlayerState string

const (
	PStateNone        PlayerState = "none"
	PStateAnswering   PlayerState = "answering"
	PStateLost        PlayerState = "lost"
	PStateRight       PlayerState = "right"
	PStateWrong       PlayerState = "wrong"
	PStateHasAnswered PlayerState = "hasAnswered"
	PStatePass        PlayerState = "pass"
)

// PlayerInit describes a player seat at New.
type PlayerInit struct {
	ID   string
	Name string
}

// Player is a seated player.
type Player struct {
	ID        string
	Name      string
	Score     int
	Connected bool
	Kicked    bool
	Ready     bool
	CanPress  bool
	State     PlayerState
	InGame    bool // participates in the current hidden-answer question
	// FinalInGame: admitted to the final round (score > 0, or everyone when allowed).
	FinalInGame bool

	// Per-question, hidden until reveal.
	StakeMade   bool
	Stake       int
	StakeAllIn  bool
	StakePassed bool
	Answered    bool
	Answer      AnswerView
	MediaDone   bool

	// Appeal bookkeeping.
	AppealUsed bool

	// Statistics.
	Right           int
	Wrong           int
	AcceptedAnswers []string
	RejectedAnswers []string
	Appeals         int
}

// Active reports whether the player is seated and connected.
func (p *Player) Active() bool { return p.Connected && !p.Kicked }

// CellState is a table cell.
type CellState struct {
	Price   int
	Played  bool
	Removed bool // empty slot or removed by TOGGLE
}

// ThemeState is a table theme.
type ThemeState struct {
	Name      string
	Removed   bool // final round: deleted
	Questions []CellState
}

// Timer is a running or paused timer. StartedAtMs is the room clock (AtMs of
// the command that started/resumed it).
type Timer struct {
	ID          string
	Kind        string
	DurationMs  int64
	StartedAtMs int64
	Paused      bool
	Frozen      bool  // paused by game logic (button win, appeal); not resumed by PAUSE off
	RemainingMs int64 // valid when Paused
	PersonID    string
}

// Timer kinds.
const (
	TimerQuestionSelection = "questionSelection"
	TimerThemeSelection    = "themeSelection"
	TimerPlayerSelection   = "playerSelection"
	TimerButtonPressing    = "buttonPressing"
	TimerAnswering         = "answering"
	TimerSoloAnswering     = "soloAnswering"
	TimerHiddenAnswering   = "hiddenAnswering"
	TimerStakeMaking       = "stakeMaking"
	TimerShowmanDecision   = "showmanDecision"
	TimerRound             = "round"
	TimerReflection        = "reflection"
	TimerContent           = "content"
	TimerMediaFallback     = "mediaFallback"
	TimerAppellation       = "appellation"
)

// Outcome is one scored answer within a question (appeals revert them).
type Outcome struct {
	PersonID string
	Answer   string
	Right    bool
	Factor   float64
	Delta    int
	Pass     bool // factor 0: no score change
	Reverted bool
}

// PendingKind is what the engine currently waits for from a decider.
type PendingKind string

const (
	PendingChooser        PendingKind = "chooser"        // showman picks the round-start chooser among tied players
	PendingStaker         PendingKind = "staker"         // showman picks the next bidder among tied players
	PendingDeleter        PendingKind = "deleter"        // showman picks the next theme deleter among tied players
	PendingSecretTransfer PendingKind = "secretTransfer" // chooser picks the secret recipient
)

// Pending is an outstanding SELECT_PLAYER decision.
type Pending struct {
	Kind       PendingKind
	DeciderID  string
	Candidates []string
}

// StakeState is the bidding state of a stake question.
type StakeState struct {
	Step         int
	Nominal      int
	Participants []string        // in seat order
	Active       map[string]bool // still bidding
	Bids         map[string]int
	HasBid       map[string]bool
	AllIn        map[string]bool
	Order        []string // order in which players bid for the first time
	Current      string   // whose turn it is
	Stake        int      // current highest bid (0 = none)
	Leader       string
	AnyAllIn     bool
	AllowedModes []StakeMode
	Min, Max     int
}

// ContentGroup is a set of items displayed together (NoWait chaining).
type ContentGroup struct {
	Items      []packs.ContentItem
	WaitMs     int64 // planned duration; 0 = wait for media completion
	MediaWait  bool
	MediaID    string
	MediaURL   string
	PartialImg bool
}

// AppealKind is for ("I am right") or against ("I disagree").
type AppealKind string

const (
	AppealFor     AppealKind = "for"
	AppealAgainst AppealKind = "against"
)

// AppealRequest is a queued appeal.
type AppealRequest struct {
	Kind        AppealKind
	AppellantID string
	AnswererID  string // whose outcome is contested
	OutcomeIdx  int
}

// Appeal is the running vote.
type Appeal struct {
	AppealRequest
	Voters  []string        // all eligible voters (players + showman)
	Votes   map[string]bool // personID → for(true)/against(false)
	Total   int
	Pending []string // voters still to vote
}

// QuestionState is the play state of the current (or last) question.
type QuestionState struct {
	ThemeIndex    int
	QuestionIndex int
	ID            string
	Type          packs.QuestionType
	IsDefault     bool
	Known         bool // engine has a script for the type; false = custom/manual
	AnswerType    packs.AnswerType
	Price         int // nominal
	CurPriceRight int
	CurPriceWrong int
	Sub           QSub
	Caption       string // theme shown on screen
	IsFinal       bool   // played in a theme-list round

	Button       bool // button question
	Armed        bool // buttons currently armed
	Direct       bool // single direct answerer
	Hidden       bool // written answers from several players
	HiddenAll    bool // stakeAll: ±stake per player
	SecretPublic bool // secretPublicPrice: theme/price announced before the transfer
	NoQuestion   bool // secretNoQuestion: the recipient just receives the price
	OptionsShown bool // select options were sent

	AnswererID string
	Answerers  []string // hidden mode participants in reveal order
	RevealIdx  int      // next answerer to reveal (hidden mode)

	Groups         []ContentGroup
	Cursor         int    // index of the group currently shown
	Phase          string // question | answer
	ContentDone    bool
	WaitNext       bool // Managed mode / explicit wait for CmdNext
	ThinkingRemain int64

	Stakes *StakeState

	Options          []packs.AnswerOption
	ExcludedOptions  map[string]bool
	ValidationCache  map[string]validation
	PendingAnswerID  string // whose answer awaits a verdict
	PendingAnswer    string
	PreVerdicts      map[string]validation // hidden mode: showman verdicts given before the reveal
	History          []Outcome
	AppealQueue      []AppealRequest
	AgainstUsed      bool
	Rights           []string
	Wrongs           []string
	AnswerItems      []packs.ContentItem
	Comments         string
	PriceChoices     []int // secret price selection
	StakeAllMissing  int   // hidden stakes still awaited
	AwaitingMediaIDs bool
}

type validation struct {
	right  bool
	factor float64
}

// State is the complete engine state. It is exposed read-only by Game.State;
// callers must not mutate it and must not retain it across Apply calls.
type State struct {
	Stage      Stage
	RoundIndex int // -1 in the lobby
	RoundName  string
	RoundType  packs.RoundType
	Table      []ThemeState
	ChooserID  string
	Players    []*Player
	ShowmanID  string
	Rules      Rules
	Times      TimeSettings

	Question *QuestionState // current or last played question (nil before the first)
	Pending  *Pending
	Timers   []*Timer
	Paused   bool
	Appeal   *Appeal

	RoundTimedOut   bool
	WaitNext        bool       // roundIntro / roundEnd: waiting for CmdNext (Managed mode)
	FinalGroups     [][]string // final round: participants grouped by equal score, ascending
	FinalOffset     int        // rotation so that the highest group deletes last
	DeleteStep      int        // deletions performed so far
	DeleterID       string     // player currently asked to delete a theme
	DeleterUsed     map[string]bool
	Winner          string
	QuestionsPlayed int
	Ended           bool
}

func (s *State) player(id string) *Player {
	for _, p := range s.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func (s *State) activePlayers() []*Player {
	out := make([]*Player, 0, len(s.Players))
	for _, p := range s.Players {
		if p.Active() {
			out = append(out, p)
		}
	}
	return out
}

func (s *State) activeIDs() []string {
	ps := s.activePlayers()
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}
