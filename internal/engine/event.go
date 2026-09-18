package engine

import "sigame/internal/packs"

// EvType enumerates the events the engine emits. Names match the eventual
// WebSocket message types 1:1 (see docs/protocol-engine.md).
type EvType string

const (
	EvStage             EvType = "STAGE"
	EvPlayers           EvType = "PLAYERS"
	EvOptions           EvType = "OPTIONS"
	EvRoundStart        EvType = "ROUND_START"
	EvRoundContent      EvType = "ROUND_CONTENT"
	EvTable             EvType = "TABLE"
	EvSums              EvType = "SUMS"
	EvSetChooser        EvType = "SET_CHOOSER"
	EvAskChoose         EvType = "ASK_CHOOSE"
	EvAskSelectPlayer   EvType = "ASK_SELECT_PLAYER"
	EvQuestionStart     EvType = "QUESTION_START"
	EvQuestionCaption   EvType = "QUESTION_CAPTION"
	EvShowmanHint       EvType = "SHOWMAN_HINT"
	EvContent           EvType = "CONTENT"
	EvContentState      EvType = "CONTENT_STATE"
	EvAnswerOptions     EvType = "ANSWER_OPTIONS"
	EvMediaWait         EvType = "MEDIA_WAIT"
	EvAskAnswer         EvType = "ASK_ANSWER"
	EvAnswerDraft       EvType = "ANSWER_DRAFT"
	EvPlayerAnswer      EvType = "PLAYER_ANSWER"
	EvAskValidate       EvType = "ASK_VALIDATE"
	EvValidation        EvType = "VALIDATION"
	EvAISuggestion      EvType = "AI_SUGGESTION"
	EvPersonScore       EvType = "PERSON_SCORE"
	EvPlayerState       EvType = "PLAYER_STATE"
	EvPass              EvType = "PASS"
	EvRightAnswer       EvType = "RIGHT_ANSWER"
	EvQuestionEnd       EvType = "QUESTION_END"
	EvAskStake          EvType = "ASK_STAKE"
	EvPersonStake       EvType = "PERSON_STAKE"
	EvAskDeleteTheme    EvType = "ASK_DELETE_THEME"
	EvThemeDeleted      EvType = "THEME_DELETED"
	EvFinalThink        EvType = "FINAL_THINK"
	EvTimerStart        EvType = "TIMER_START"
	EvTimerStop         EvType = "TIMER_STOP"
	EvTimerPause        EvType = "TIMER_PAUSE"
	EvTimerResume       EvType = "TIMER_RESUME"
	EvPause             EvType = "PAUSE"
	EvToggle            EvType = "TOGGLE"
	EvRoundEnd          EvType = "ROUND_END"
	EvWinner            EvType = "WINNER"
	EvGameEnd           EvType = "GAME_END"
	EvAppealStart       EvType = "APPEAL_START"
	EvAskAppealVote     EvType = "ASK_APPEAL_VOTE"
	EvAppealVote        EvType = "APPEAL_VOTE"
	EvAppealResult      EvType = "APPEAL_RESULT"
	EvUserError         EvType = "USER_ERROR"
	EvButtonArmRequest  EvType = "BUTTON_ARM_REQUEST"    // room only
	EvButtonDisarm      EvType = "BUTTON_DISARM_REQUEST" // room only
	EvValidationTimeout EvType = "VALIDATION_TIMEOUT"    // room only
)

// AudienceKind says who receives an event.
type AudienceKind string

const (
	AudienceAll    AudienceKind = "all"    // every connected client
	AudienceRole   AudienceKind = "role"   // every client with Audience.Role
	AudiencePerson AudienceKind = "person" // exactly Audience.PersonID
	AudienceExcept AudienceKind = "except" // everyone but Audience.PersonID
	AudienceRoom   AudienceKind = "room"   // the room actor only; never forwarded to clients
)

// Audience is the delivery target of an event.
type Audience struct {
	Kind     AudienceKind `json:"kind"`
	Role     Role         `json:"role,omitempty"`
	PersonID string       `json:"personId,omitempty"`
}

// Audience constructors.
func ToAll() Audience               { return Audience{Kind: AudienceAll} }
func ToRole(r Role) Audience        { return Audience{Kind: AudienceRole, Role: r} }
func ToPerson(id string) Audience   { return Audience{Kind: AudiencePerson, PersonID: id} }
func ToExcept(id string) Audience   { return Audience{Kind: AudienceExcept, PersonID: id} }
func ToShowman() Audience           { return ToRole(RoleShowman) }
func ToRoom() Audience              { return Audience{Kind: AudienceRoom} }
func (a Audience) IsRoomOnly() bool { return a.Kind == AudienceRoom }

// Event is one output of Apply. Payload is one of the *Payload structs below
// (exported, camelCase JSON) and is meant to be marshalled 1:1 as the WS
// message payload.
type Event struct {
	Type     EvType   `json:"type"`
	Audience Audience `json:"audience"`
	Payload  any      `json:"payload,omitempty"`
}

// IsInternal reports whether the event is addressed to the room actor and
// must not be forwarded to clients (BUTTON_ARM_REQUEST, BUTTON_DISARM_REQUEST,
// VALIDATION_TIMEOUT). Timer events are NOT internal: the room schedules
// them and forwards them to clients for progress bars.
func (e Event) IsInternal() bool { return e.Audience.Kind == AudienceRoom }

// IsTimer reports whether the event is a timer request the room must act on
// (schedule on TIMER_START/TIMER_RESUME, cancel on TIMER_STOP/TIMER_PAUSE).
func (e Event) IsTimer() bool {
	switch e.Type {
	case EvTimerStart, EvTimerStop, EvTimerPause, EvTimerResume:
		return true
	}
	return false
}

// ---- payloads -------------------------------------------------------------

// StagePayload announces a stage change.
type StagePayload struct {
	Stage      Stage `json:"stage"`
	Sub        QSub  `json:"sub,omitempty"`
	RoundIndex int   `json:"roundIndex"`
}

// PlayerInfo is a player as seen by everyone.
type PlayerInfo struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Score     int         `json:"score"`
	Connected bool        `json:"connected"`
	Ready     bool        `json:"ready"`
	Kicked    bool        `json:"kicked,omitempty"`
	State     PlayerState `json:"state"`
	CanPress  bool        `json:"canPress"`
	InGame    bool        `json:"inGame"`
}

// PlayersPayload lists the players after a join/leave/kick/ready change.
type PlayersPayload struct {
	Players []PlayerInfo `json:"players"`
}

// OptionsPayload carries the effective rules and timings.
type OptionsPayload struct {
	Rules Rules        `json:"rules"`
	Times TimeSettings `json:"times"`
}

// RoundStartPayload announces a round.
type RoundStartPayload struct {
	Index  int             `json:"index"`
	Name   string          `json:"name"`
	Type   packs.RoundType `json:"type"`
	Themes []string        `json:"themes"`
}

// RoundContentPayload lists media to preload (question content and select
// options of the round; answer media excluded).
type RoundContentPayload struct {
	MediaIDs []string `json:"mediaIds"`
	URLs     []string `json:"urls,omitempty"`
}

// TableCell is one cell of the table as seen by everyone.
type TableCell struct {
	Price   int  `json:"price"`
	Played  bool `json:"played"`
	Removed bool `json:"removed"`
}

// TableTheme is one theme of the table.
type TableTheme struct {
	Name      string      `json:"name"`
	Removed   bool        `json:"removed"`
	Questions []TableCell `json:"questions"`
}

// TablePayload is the whole table of the current round.
type TablePayload struct {
	Themes []TableTheme `json:"themes"`
}

// ScoreEntry is one row of SUMS.
type ScoreEntry struct {
	PersonID string `json:"personId"`
	Score    int    `json:"score"`
}

// SumsPayload carries all scores.
type SumsPayload struct {
	Scores []ScoreEntry `json:"scores"`
}

// SetChooserPayload announces the chooser. Reason: roundStart, rightAnswer,
// stakeWinner, secretRecipient, rotation, appeal, showman.
type SetChooserPayload struct {
	PersonID string `json:"personId"`
	Reason   string `json:"reason"`
}

// AskChoosePayload asks the chooser to pick a cell.
type AskChoosePayload struct {
	PersonID   string `json:"personId"`
	DurationMs int64  `json:"durationMs"`
}

// AskSelectPlayerPayload asks someone to pick a player. Reason: chooser,
// staker, deleter (showman decides), secretTransfer (chooser decides).
type AskSelectPlayerPayload struct {
	Reason     string   `json:"reason"`
	Candidates []string `json:"candidates"`
	DeciderID  string   `json:"deciderId"`
	DurationMs int64    `json:"durationMs"`
}

// QuestionStartPayload announces the selected question.
type QuestionStartPayload struct {
	QuestionID    string             `json:"questionId"`
	ThemeIndex    int                `json:"themeIndex"`
	QuestionIndex int                `json:"questionIndex"`
	Theme         string             `json:"theme"`
	Price         int                `json:"price"`
	Type          packs.QuestionType `json:"type"`
	IsDefault     bool               `json:"isDefault"`
	AnswerType    packs.AnswerType   `json:"answerType"`
}

// QuestionCaptionPayload replaces the theme/price on screen (secret questions,
// final theme, stake winner).
type QuestionCaptionPayload struct {
	Theme string `json:"theme"`
	Price int    `json:"price"`
}

// ShowmanHintPayload is sent to the showman only.
type ShowmanHintPayload struct {
	Rights          []string `json:"rights,omitempty"`
	Wrongs          []string `json:"wrongs,omitempty"`
	ShowmanComments string   `json:"showmanComments,omitempty"`
	Comments        string   `json:"comments,omitempty"`
}

// ContentItemView is a content item projected for clients (never answer
// content before the reveal).
type ContentItemView struct {
	Type       packs.ContentType `json:"type"`
	Text       string            `json:"text,omitempty"`
	MediaID    string            `json:"mediaId,omitempty"`
	URL        string            `json:"url,omitempty"`
	Placement  packs.Placement   `json:"placement"`
	DurationMs int64             `json:"durationMs,omitempty"`
}

// ContentPayload shows a group of content items together. Phase: question | answer.
type ContentPayload struct {
	Phase  string            `json:"phase"`
	Index  int               `json:"index"`
	Items  []ContentItemView `json:"items"`
	WaitMs int64             `json:"waitMs"` // planned display time; 0 = waits for media completion / Next
}

// ContentStatePayload reports media pause/resume around button wins.
type ContentStatePayload struct {
	State string `json:"state"` // paused | resumed
}

// AnswerOptionView is a select option.
type AnswerOptionView struct {
	Label   string            `json:"label"`
	Content []ContentItemView `json:"content"`
}

// AnswerOptionsPayload lists the select options.
type AnswerOptionsPayload struct {
	Options    []AnswerOptionView `json:"options"`
	ShowLabels bool               `json:"showLabels"`
	OneByOne   bool               `json:"oneByOne"`
}

// MediaWaitPayload says the engine waits for MEDIA_COMPLETED for this media.
type MediaWaitPayload struct {
	MediaID string `json:"mediaId,omitempty"`
	URL     string `json:"url,omitempty"`
}

// AskAnswerPayload asks a player to answer.
type AskAnswerPayload struct {
	PersonID   string           `json:"personId"`
	AnswerType packs.AnswerType `json:"answerType"`
	DurationMs int64            `json:"durationMs"`
	Hidden     bool             `json:"hidden"` // written answer, revealed later
	Oral       bool             `json:"oral"`   // answer by voice; the showman validates
}

// AnswerDraftPayload forwards live typing to the showman.
type AnswerDraftPayload struct {
	PersonID string `json:"personId"`
	Text     string `json:"text"`
}

// AnswerView is a submitted answer.
type AnswerView struct {
	Text        string   `json:"text,omitempty"`
	OptionLabel string   `json:"optionLabel,omitempty"`
	Number      *float64 `json:"number,omitempty"`
	Point       *Point   `json:"point,omitempty"`
	ClientRight *bool    `json:"clientRight,omitempty"` // answerType=client: the client's own verdict
}

// PlayerAnswerPayload publishes a player's answer.
type PlayerAnswerPayload struct {
	PersonID string     `json:"personId"`
	Answer   AnswerView `json:"answer"`
}

// AskValidatePayload asks the showman to validate (showman only).
type AskValidatePayload struct {
	PersonID    string   `json:"personId"`
	Answer      string   `json:"answer"`
	Rights      []string `json:"rights"`
	Wrongs      []string `json:"wrongs,omitempty"`
	AllowFactor bool     `json:"allowFactor"`
	Oral        bool     `json:"oral"`
	AutoAfterMs int64    `json:"autoAfterMs"` // 0 = no automatic fallback
	Preview     bool     `json:"preview"`     // hidden-answer preview: a verdict is cached until the reveal
}

// ValidationPayload publishes a verdict.
type ValidationPayload struct {
	PersonID       string  `json:"personId"`
	Answer         string  `json:"answer"`
	Right          bool    `json:"right"`
	Factor         float64 `json:"factor"`
	Source         string  `json:"source"`
	ExcludedOption string  `json:"excludedOption,omitempty"`
}

// ValidationTimeoutPayload (room only): the showman did not decide in time;
// the room must answer with CmdValidate{Source: "auto"|"ai"}.
type ValidationTimeoutPayload struct {
	PersonID string   `json:"personId"`
	Answer   string   `json:"answer"`
	Rights   []string `json:"rights"`
	Wrongs   []string `json:"wrongs,omitempty"`
}

// AISuggestionPayload forwards a hybrid-mode AI verdict to the showman.
type AISuggestionPayload struct {
	PersonID string  `json:"personId"`
	Right    bool    `json:"right"`
	Factor   float64 `json:"factor"`
	Reason   string  `json:"reason,omitempty"`
}

// PersonScorePayload reports a score change. Reason: answer, penalty,
// appeal, correction, secret, bonus.
type PersonScorePayload struct {
	PersonID string `json:"personId"`
	Delta    int    `json:"delta"`
	Score    int    `json:"score"`
	Reason   string `json:"reason"`
}

// PlayerStatePayload reports a player's question state.
type PlayerStatePayload struct {
	PersonID string      `json:"personId"`
	State    PlayerState `json:"state"`
}

// PassPayload reports a voluntary pass.
type PassPayload struct {
	PersonID string `json:"personId"`
}

// RightAnswerPayload reveals the right answer.
type RightAnswerPayload struct {
	Text     string            `json:"text"`
	Items    []ContentItemView `json:"items,omitempty"`
	Comments string            `json:"comments,omitempty"`
}

// QuestionEndPayload closes a question.
type QuestionEndPayload struct {
	ThemeIndex    int `json:"themeIndex"`
	QuestionIndex int `json:"questionIndex"`
}

// AskStakePayload asks for a stake. Reason: stake, secretPrice, hiddenStake.
type AskStakePayload struct {
	PersonID   string      `json:"personId"`
	Modes      []StakeMode `json:"modes"`
	Min        int         `json:"min"`
	Max        int         `json:"max"`
	Step       int         `json:"step"`
	Reason     string      `json:"reason"`
	DurationMs int64       `json:"durationMs"`
}

// PersonStakePayload announces a stake decision. Hidden stakes carry Amount 0
// for everyone but the showman/owner until the reveal.
type PersonStakePayload struct {
	PersonID string    `json:"personId"`
	Mode     StakeMode `json:"mode"`
	Amount   int       `json:"amount"`
	Hidden   bool      `json:"hidden"`
}

// AskDeleteThemePayload asks the deleter to remove a theme.
type AskDeleteThemePayload struct {
	PersonID   string `json:"personId"`
	Themes     []int  `json:"themes"` // remaining theme indexes
	DurationMs int64  `json:"durationMs"`
}

// ThemeDeletedPayload reports a deleted theme.
type ThemeDeletedPayload struct {
	ThemeIndex int    `json:"themeIndex"`
	PersonID   string `json:"personId"`
}

// FinalThinkPayload starts written answering.
type FinalThinkPayload struct {
	DurationMs int64 `json:"durationMs"`
}

// TimerStartPayload requests/announces a timer.
type TimerStartPayload struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	DurationMs int64  `json:"durationMs"` // 0 with kind=mediaFallback: the room picks the duration
	PersonID   string `json:"personId,omitempty"`
}

// TimerStopPayload cancels a timer.
type TimerStopPayload struct {
	ID string `json:"id"`
}

// TimerPausePayload freezes a timer; TimerResume restarts it with RemainingMs.
type TimerPausePayload struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	RemainingMs int64  `json:"remainingMs"`
}

// PausePayload announces a game pause state.
type PausePayload struct {
	On bool `json:"on"`
}

// TogglePayload reports a removed/restored cell.
type TogglePayload struct {
	ThemeIndex    int  `json:"themeIndex"`
	QuestionIndex int  `json:"questionIndex"`
	Active        bool `json:"active"`
}

// RoundEndPayload closes a round. Reason: empty, timeout, manual, noPlayers.
type RoundEndPayload struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// WinnerPayload names the winner; empty PersonID = no single winner (tie).
type WinnerPayload struct {
	PersonID string `json:"personId"`
}

// PlayerStats are per-player statistics.
type PlayerStats struct {
	PersonID        string   `json:"personId"`
	RightAnswers    int      `json:"rightAnswers"`
	WrongAnswers    int      `json:"wrongAnswers"`
	AcceptedAnswers []string `json:"acceptedAnswers,omitempty"`
	RejectedAnswers []string `json:"rejectedAnswers,omitempty"`
	Appeals         int      `json:"appeals"`
}

// Statistics summarise the game.
type Statistics struct {
	QuestionsPlayed int           `json:"questionsPlayed"`
	Players         []PlayerStats `json:"players"`
}

// GameEndPayload closes the game.
type GameEndPayload struct {
	Scores     []ScoreEntry `json:"scores"`
	WinnerID   string       `json:"winnerId"`
	Statistics Statistics   `json:"statistics"`
}

// AppealStartPayload opens an appeal. Kind: for ("I am right") | against ("I disagree").
type AppealStartPayload struct {
	Kind        string   `json:"kind"`
	AppellantID string   `json:"appellantId"`
	AnswererID  string   `json:"answererId"`
	Answer      string   `json:"answer"`
	Voters      []string `json:"voters"`
	DurationMs  int64    `json:"durationMs"`
}

// AskAppealVotePayload asks a voter to vote.
type AskAppealVotePayload struct {
	Kind       string   `json:"kind"`
	AnswererID string   `json:"answererId"`
	Answer     string   `json:"answer"`
	Rights     []string `json:"rights"`
}

// AppealVotePayload reports a vote.
type AppealVotePayload struct {
	PersonID string `json:"personId"`
	Right    bool   `json:"right"`
}

// AppealResultPayload closes an appeal.
type AppealResultPayload struct {
	Kind       string `json:"kind"`
	AnswererID string `json:"answererId"`
	Accepted   bool   `json:"accepted"`
	For        int    `json:"for"`
	Against    int    `json:"against"`
}

// UserErrorPayload is a soft error addressed to one person.
type UserErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ButtonArmRequestPayload (room only) asks the buzzer to open the button.
type ButtonArmRequestPayload struct {
	QuestionID          string   `json:"questionId"`
	EligiblePlayerIDs   []string `json:"eligiblePlayerIds"`
	ThinkingRemainingMs int64    `json:"thinkingRemainingMs"` // -1 = content still playing (no thinking timer yet)
	Rearm               bool     `json:"rearm"`
}

// ButtonDisarmRequestPayload (room only) closes the button.
type ButtonDisarmRequestPayload struct {
	QuestionID string `json:"questionId"`
}
