package engine

// GameMode selects the round strategy and default question types
// (docs/research/01-sigame-rules.md §4).
type GameMode string

const (
	ModeClassic    GameMode = "classic"    // table, chooser picks cells, default type simple; chooser = last correct button answerer
	ModeSimple     GameMode = "simple"     // sequential questions, default type simple, no chooser
	ModeQuiz       GameMode = "quiz"       // sequential questions, default type forAll, no chooser
	ModeTurnTaking GameMode = "turnTaking" // table, default type noRisk, chooser rotates after every question
)

// Penalty says what happens to the score after a wrong answer of a question class.
type Penalty string

const (
	PenaltySubtract Penalty = "subtract" // −price
	PenaltyNone     Penalty = "none"     // no score change ("без риска")
)

// Rules are the game rules (docs/research/01-sigame-rules.md §13). They are
// fixed at New; a subset may be changed mid-game with CmdSetOptions (§12).
type Rules struct {
	Mode                            GameMode `json:"mode" enum:"classic,simple,quiz,turnTaking" doc:"Game mode: classic (table + chooser), simple (sequential, button), quiz (sequential, forAll), turnTaking (table, noRisk, rotating chooser)"`
	FalseStart                      bool     `json:"falseStart" doc:"When true the button opens only after the whole question content was shown; when false the button is armed before content"`
	Oral                            bool     `json:"oral" doc:"Oral game: players answer by voice; the showman validates without a written answer and may act on behalf of players"`
	IgnoreWrong                     bool     `json:"ignoreWrong" doc:"Neutral game: never subtract points for wrong answers"`
	ButtonPenalty                   Penalty  `json:"buttonPenalty" enum:"subtract,none" doc:"Penalty for a wrong answer on button questions (simple)"`
	ForYourselfPenalty              Penalty  `json:"forYourselfPenalty" enum:"subtract,none" doc:"Penalty for a wrong answer on noRisk questions"`
	ForAllPenalty                   Penalty  `json:"forAllPenalty" enum:"subtract,none" doc:"Penalty for a wrong answer on forAll questions"`
	ForYourselfFactor               int      `json:"forYourselfFactor" doc:"Price multiplier for noRisk questions (default 2)"`
	ReadingSpeed                    int      `json:"readingSpeed" doc:"Text reading speed in characters per second (default 20); 0 = text has no automatic duration"`
	PartialText                     bool     `json:"partialText" doc:"Progressive text reveal (only meaningful when falseStart=false); the engine sends the whole text, clients animate"`
	HintShowman                     bool     `json:"hintShowman" doc:"Send right answers to the showman at question start"`
	Managed                         bool     `json:"managed" doc:"Managed game: automatic transitions wait for the showman's Next command instead of timers"`
	PlayAllQuestionsInFinal         bool     `json:"playAllQuestionsInFinal" doc:"Play every question of a final round instead of deleting themes"`
	AllowEveryoneToPlayHiddenStakes bool     `json:"allowEveryoneToPlayHiddenStakes" doc:"Players with a score ≤ 0 also take part in hidden-stake questions and the final round"`
	UseAppellations                 bool     `json:"useAppellations" doc:"Enable appeals (\"I am right\" / \"I disagree\")"`
	DisplayAnswerOptionsLabels      bool     `json:"displayAnswerOptionsLabels" doc:"Show option labels (A, B, C...) for select questions"`
	DisplayAnswerOptionsOneByOne    bool     `json:"displayAnswerOptionsOneByOne" doc:"Reveal select options one by one"`
	PrependThemeCommentsToQuestion  bool     `json:"prependThemeCommentsToQuestion" doc:"Show the theme comments as the first text item of every question of the theme"`
}

// DefaultRules returns the SIGame defaults (§13).
func DefaultRules() Rules {
	return Rules{
		Mode:                            ModeClassic,
		FalseStart:                      true,
		Oral:                            false,
		IgnoreWrong:                     false,
		ButtonPenalty:                   PenaltySubtract,
		ForYourselfPenalty:              PenaltyNone,
		ForAllPenalty:                   PenaltySubtract,
		ForYourselfFactor:               2,
		ReadingSpeed:                    20,
		PartialText:                     false,
		HintShowman:                     false,
		Managed:                         false,
		PlayAllQuestionsInFinal:         false,
		AllowEveryoneToPlayHiddenStakes: true,
		UseAppellations:                 true,
		DisplayAnswerOptionsLabels:      true,
		DisplayAnswerOptionsOneByOne:    true,
		PrependThemeCommentsToQuestion:  true,
	}
}

// TimeSettings are timer durations in milliseconds (§8).
type TimeSettings struct {
	QuestionSelectionMs int64 `json:"questionSelectionMs" doc:"Chooser picks a cell; timeout → random active cell (30 s)"`
	ThemeSelectionMs    int64 `json:"themeSelectionMs" doc:"Final round theme deletion; timeout → random theme (30 s)"`
	PlayerSelectionMs   int64 `json:"playerSelectionMs" doc:"Secret question transfer; timeout → random other player (30 s)"`
	ButtonPressingMs    int64 `json:"buttonPressingMs" doc:"Thinking time while the button is open; timeout → nobody answered (5 s)"`
	AnsweringMs         int64 `json:"answeringMs" doc:"Typing the answer after winning the button; timeout → wrong (25 s)"`
	SoloAnsweringMs     int64 `json:"soloAnsweringMs" doc:"Direct answer on stake/secret/noRisk questions; timeout → wrong (25 s)"`
	HiddenAnsweringMs   int64 `json:"hiddenAnsweringMs" doc:"Written answers on forAll/stakeAll questions (45 s)"`
	StakeMakingMs       int64 `json:"stakeMakingMs" doc:"Each bid, secret price choice, hidden stake; timeout → pass/nominal/minimum (30 s)"`
	ShowmanDecisionMs   int64 `json:"showmanDecisionMs" doc:"Validation and tie selections; timeout → auto validation / random (30 s)"`
	RoundMs             int64 `json:"roundMs" doc:"Round duration; the round ends after the current question (3600 s)"`
	ButtonBlockingMs    int64 `json:"buttonBlockingMs" doc:"Button lockout after a misfire, used by the buzzer (3 s)"`
	ReflectionMs        int64 `json:"reflectionMs" doc:"Pause after text content and while the right answer is shown (2 s)"`
	ImageMs             int64 `json:"imageMs" doc:"Image/html display duration (5 s)"`
	PartialImageMs      int64 `json:"partialImageMs" doc:"Progressive image reveal duration (3 s)"`
	AppellationMs       int64 `json:"appellationMs" doc:"Appeal voting window (30 s)"`
}

// DefaultTimeSettings returns the SIGame defaults (§8).
func DefaultTimeSettings() TimeSettings {
	return TimeSettings{
		QuestionSelectionMs: 30_000,
		ThemeSelectionMs:    30_000,
		PlayerSelectionMs:   30_000,
		ButtonPressingMs:    5_000,
		AnsweringMs:         25_000,
		SoloAnsweringMs:     25_000,
		HiddenAnsweringMs:   45_000,
		StakeMakingMs:       30_000,
		ShowmanDecisionMs:   30_000,
		RoundMs:             3_600_000,
		ButtonBlockingMs:    3_000,
		ReflectionMs:        2_000,
		ImageMs:             5_000,
		PartialImageMs:      3_000,
		AppellationMs:       30_000,
	}
}

// RulesPatch is the subset of rules/times that may be changed mid-game
// (CmdSetOptions, §12). Nil fields are left untouched.
type RulesPatch struct {
	Oral                       *bool  `json:"oral,omitempty"`
	Managed                    *bool  `json:"managed,omitempty"`
	DisplayAnswerOptionsLabels *bool  `json:"displayAnswerOptionsLabels,omitempty"`
	FalseStart                 *bool  `json:"falseStart,omitempty"`
	ReadingSpeed               *int   `json:"readingSpeed,omitempty"`
	PartialText                *bool  `json:"partialText,omitempty"`
	UseAppellations            *bool  `json:"useAppellations,omitempty"`
	ButtonBlockingMs           *int64 `json:"buttonBlockingMs,omitempty"`
	PartialImageMs             *int64 `json:"partialImageMs,omitempty"`
}
