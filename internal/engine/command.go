package engine

// Role of the person issuing a command. RoleSystem marks commands that
// originate from the room actor itself (timer expiries, buzzer decisions,
// media bookkeeping, AI verdicts, connection changes).
type Role string

const (
	RoleShowman Role = "showman"
	RolePlayer  Role = "player"
	RoleViewer  Role = "viewer"
	RoleSystem  Role = "system"
)

// Actor identifies who issued a command. IsHost grants the room-owner
// powers (start, kick, options, pause, move) in addition to the role.
type Actor struct {
	PersonID string `json:"personId"`
	Role     Role   `json:"role" enum:"showman,player,viewer,system"`
	IsHost   bool   `json:"isHost,omitempty"`
}

// CmdType enumerates the commands the room feeds into the engine.
type CmdType string

const (
	CmdStart          CmdType = "START"           // host/showman: start the game (lobby only)
	CmdPlayerReady    CmdType = "PLAYER_READY"    // player: toggle ready flag (informational)
	CmdChooseQuestion CmdType = "CHOOSE_QUESTION" // chooser (showman in oral mode): Theme, Q
	CmdButtonResult   CmdType = "BUTTON_RESULT"   // system (buzzer): WinnerID or Nobody
	CmdPass           CmdType = "PASS"            // player: give up the button / pass in stakes
	CmdAnswer         CmdType = "ANSWER"          // answerer: Text | OptionLabel | Number | Point | Right(client)
	CmdAnswerDraft    CmdType = "ANSWER_DRAFT"    // answerer: live typing preview (forwarded to the showman)
	CmdValidate       CmdType = "VALIDATE"        // showman/system: PersonID, Right, Factor, Source
	CmdSelectPlayer   CmdType = "SELECT_PLAYER"   // showman (ties) or chooser (secret transfer): PersonID
	CmdSetStake       CmdType = "SET_STAKE"       // staker: StakeMode, Amount
	CmdDeleteTheme    CmdType = "DELETE_THEME"    // deleter: Theme
	CmdAppellate      CmdType = "APPELLATE"       // player: For (true = "I am right", false = "I disagree")
	CmdVoteAppeal     CmdType = "VOTE_APPEAL"     // player: Right
	CmdTimeout        CmdType = "TIMEOUT"         // system: TimerID expired
	CmdMediaCompleted CmdType = "MEDIA_COMPLETED" // system/player: PersonID ("" = all players finished)
	CmdMediaLoaded    CmdType = "MEDIA_LOADED"    // player: informational
	CmdShowmanPause   CmdType = "PAUSE"           // showman/host: On
	CmdMove           CmdType = "MOVE"            // showman/host: Dir (-2,-1,1,2,3), Round
	CmdToggle         CmdType = "TOGGLE"          // showman: Theme, Q
	CmdChangeScore    CmdType = "CHANGE_SCORE"    // showman: PersonID, NewSum
	CmdSetChooser     CmdType = "SET_CHOOSER"     // showman: PersonID
	CmdPlayerLeft     CmdType = "PLAYER_LEFT"     // system: PersonID disconnected
	CmdPlayerJoined   CmdType = "PLAYER_JOINED"   // system: PersonID (+Name) connected or reconnected
	CmdKickPlayer     CmdType = "KICK_PLAYER"     // host: PersonID
	CmdSetOptions     CmdType = "SET_OPTIONS"     // host/showman: Options
	CmdNext           CmdType = "NEXT"            // showman/host: advance a waiting point (Managed mode "Дальше")
	CmdAISuggestion   CmdType = "AI_SUGGESTION"   // system: PersonID, Right, Factor, Reason → forwarded to the showman
)

// StakeMode is the kind of a stake decision.
type StakeMode string

const (
	StakeNominal StakeMode = "nominal" // bid the nominal price
	StakeStake   StakeMode = "stake"   // bid Amount
	StakeAllIn   StakeMode = "allIn"   // bid the whole score
	StakePass    StakeMode = "pass"    // give up
)

// Point is a normalised (0..1) point on the last shown image.
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Command is the single input type of the engine. Only the fields relevant
// to Type are read; see docs/protocol-engine.md for the per-command table.
type Command struct {
	Type  CmdType `json:"type"`
	Actor Actor   `json:"actor"`
	// AtMs is the room's monotonic clock at delivery time. The engine uses it
	// only to compute the remaining time of running timers (pause, button
	// re-arm); it never compares it with wall-clock time.
	AtMs int64 `json:"atMs"`

	Theme int `json:"theme,omitempty"` // theme index (ChooseQuestion, Toggle, DeleteTheme)
	Q     int `json:"q,omitempty"`     // question index within the theme (ChooseQuestion, Toggle)

	WinnerID string `json:"winnerId,omitempty"` // ButtonResult
	Nobody   bool   `json:"nobody,omitempty"`   // ButtonResult: nobody pressed within the window

	Text        string   `json:"text,omitempty"`        // Answer/AnswerDraft: free text
	OptionLabel string   `json:"optionLabel,omitempty"` // Answer: select option label
	Number      *float64 `json:"number,omitempty"`      // Answer: numeric answer
	Point       *Point   `json:"point,omitempty"`       // Answer: point answer
	Right       bool     `json:"right,omitempty"`       // Validate/VoteAppeal/AISuggestion; Answer with answerType=client
	RightSet    bool     `json:"rightSet,omitempty"`    // Answer(client): Right is meaningful
	Factor      float64  `json:"factor,omitempty"`      // Validate/AISuggestion: 0 → treated as 1; exactly 0 must be sent as FactorZero
	FactorZero  bool     `json:"factorZero,omitempty"`  // Validate: factor is exactly 0 (counts as a pass)
	Source      string   `json:"source,omitempty"`      // Validate: showman|ai|auto|appeal (default showman)
	Reason      string   `json:"reason,omitempty"`      // AISuggestion

	PersonID string `json:"personId,omitempty"` // SelectPlayer/Validate/ChangeScore/SetChooser/PlayerLeft/PlayerJoined/KickPlayer/MediaCompleted; oral-mode target
	Name     string `json:"name,omitempty"`     // PlayerJoined: display name for a new player

	StakeMode StakeMode `json:"stakeMode,omitempty"` // SetStake
	Amount    int       `json:"amount,omitempty"`    // SetStake (mode=stake)

	For bool `json:"for,omitempty"` // Appellate: true = "I am right", false = "I disagree"

	TimerID string `json:"timerId,omitempty"` // Timeout

	On    bool `json:"on,omitempty"`    // ShowmanPause
	Dir   int  `json:"dir,omitempty"`   // Move: -2 round back, -1 back, 1 next, 2 round next, 3 to Round
	Round int  `json:"round,omitempty"` // Move dir=3: target round index

	NewSum int `json:"newSum,omitempty"` // ChangeScore

	Options *RulesPatch `json:"options,omitempty"` // SetOptions
}
