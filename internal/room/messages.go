package room

import (
	"sigame/internal/buzzer"
	"sigame/internal/engine"
)

// Message types of the room layer (server → client). Engine events use their
// engine.EvType as the message type; see docs/protocol-engine.md.
const (
	MsgWelcome         = "WELCOME"
	MsgSnapshot        = "SNAPSHOT"
	MsgResume          = "RESUME"
	MsgRoomPersons     = "ROOM_PERSONS"
	MsgRoomSettings    = "ROOM_SETTINGS"
	MsgChat            = "CHAT"
	MsgRoomClosed      = "ROOM_CLOSED"
	MsgSessionReplaced = "SESSION_REPLACED"
	MsgKicked          = "KICKED"
	MsgError           = "ERROR"
	MsgSyncAck         = "SYNC_ACK"
	MsgButtonArm       = "BUTTON_ARM"
	MsgPressAck        = "PRESS_ACK"
	MsgPressPending    = "PRESS_PENDING"
	MsgButtonResult    = "BUTTON_RESULT"
	MsgLockout         = "LOCKOUT"
	MsgConnQuality     = "CONN_QUALITY"
	MsgButtonAudit     = "BUTTON_AUDIT"
	MsgAIVerdict       = "AI_VERDICT"
)

// Client → server message types that the room handles itself (the rest map
// 1:1 to engine commands, see clientCommand in room.go).
const (
	InHello         = "HELLO"
	InSync          = "SYNC"
	InArmAck        = "ARM_ACK"
	InPress         = "PRESS"
	InMisfire       = "MISFIRE"
	InVisible       = "VISIBLE"
	InChat          = "CHAT"
	InReady         = "READY"
	InStart         = "START"
	InKick          = "KICK"
	InSetHost       = "SET_HOST"
	InButtonReopen  = "BUTTON_REOPEN"
	InSetTrust      = "SET_TRUST"
	InMediaComplete = "MEDIA_COMPLETED"
)

// Error codes carried by ERROR envelopes.
const (
	ErrCodeNotAllowed  = "notAllowed"
	ErrCodeBadState    = "badState"
	ErrCodeBadArgument = "badArgument"
	ErrCodeUnknownType = "unknownType"
	ErrCodeRateLimited = "rateLimited"
	ErrCodeBadPayload  = "badPayload"
)

// MaxChatLen is the maximum length of a chat message in runes.
const MaxChatLen = 500

// AIShowmanID is the fixed person ID of the AI showman pseudo-person.
const AIShowmanID = "ai-showman"

// ---- client → server payloads ---------------------------------------------

// HelloIn is the optional first client message (resume request).
type HelloIn struct {
	LastSeq int64 `json:"lastSeq" doc:"Last server seq the client processed on its previous connection (0 = none)"`
}

// SyncIn is a clock-sync sample; c4 of the previous sample rides in the next one.
type SyncIn struct {
	Seq     int64   `json:"seq" doc:"Client sync sequence number (monotonic per connection)"`
	C1      float64 `json:"c1" doc:"performance.now() when this SYNC was sent"`
	PrevSeq int64   `json:"prevSeq,omitempty" doc:"Seq of the previous SYNC whose ACK was received"`
	PrevC4  float64 `json:"prevC4,omitempty" doc:"performance.now() when the ACK of prevSeq arrived"`
}

// ArmAckIn acknowledges a BUTTON_ARM.
type ArmAckIn struct {
	ArmID     string  `json:"armId"`
	RecvLocal float64 `json:"recvLocal" doc:"performance.now() when BUTTON_ARM arrived"`
}

// PressIn is a button press.
type PressIn struct {
	ArmID      string  `json:"armId"`
	Seq        int64   `json:"seq" doc:"Client press sequence (idempotency)"`
	PressLocal float64 `json:"pressLocal" doc:"performance.now() at pointerdown"`
	LitLocal   float64 `json:"litLocal" doc:"performance.now() when the light was actually shown"`
	Src        string  `json:"src,omitempty" doc:"pointer | key (diagnostic)"`
}

// MisfireIn reports a press before the light (client-side false start).
type MisfireIn struct {
	ArmID string `json:"armId,omitempty"`
}

// ChatIn is a chat message.
type ChatIn struct {
	Text string `json:"text" maxLength:"500"`
}

// ChooseIn selects a table cell.
type ChooseIn struct {
	Theme int `json:"theme"`
	Q     int `json:"q"`
}

// AnswerIn is a player's answer; exactly one field is used depending on the answer type.
type AnswerIn struct {
	PersonID    string        `json:"personId,omitempty" doc:"Oral mode: the showman answers on behalf of this player"`
	Text        string        `json:"text,omitempty"`
	OptionLabel string        `json:"optionLabel,omitempty"`
	Number      *float64      `json:"number,omitempty"`
	Point       *engine.Point `json:"point,omitempty"`
	Right       *bool         `json:"right,omitempty" doc:"answerType=client: the client's own verdict"`
}

// DraftIn is a live typing preview.
type DraftIn struct {
	Text string `json:"text"`
}

// ValidateIn is the showman's verdict.
type ValidateIn struct {
	PersonID   string  `json:"personId,omitempty" doc:"Optional for the pending answer; required for hidden previews and custom questions"`
	Right      bool    `json:"right"`
	Factor     float64 `json:"factor,omitempty" doc:"Credit factor; 0 means 1 unless factorZero"`
	FactorZero bool    `json:"factorZero,omitempty" doc:"Explicit factor 0 (no score change)"`
}

// PersonIn carries a single person id (SELECT_PLAYER, SET_CHOOSER, SET_HOST).
type PersonIn struct {
	PersonID string `json:"personId"`
}

// SetStakeIn is a stake decision.
type SetStakeIn struct {
	StakeMode engine.StakeMode `json:"stakeMode" enum:"nominal,stake,allIn,pass"`
	Amount    int              `json:"amount,omitempty"`
}

// DeleteThemeIn removes a final-round theme.
type DeleteThemeIn struct {
	Theme int `json:"theme"`
}

// AppellateIn opens an appeal.
type AppellateIn struct {
	For bool `json:"for" doc:"true = \"I am right\", false = \"I disagree\""`
}

// VoteIn is an appeal vote.
type VoteIn struct {
	Right bool `json:"right"`
}

// PauseIn toggles the pause.
type PauseIn struct {
	On bool `json:"on"`
}

// MoveIn moves the game.
type MoveIn struct {
	Dir   int `json:"dir" doc:"-2 round back, -1 return question, 1 next, 2 next round, 3 go to round"`
	Round int `json:"round,omitempty"`
}

// ToggleIn removes/restores a cell.
type ToggleIn struct {
	Theme int `json:"theme"`
	Q     int `json:"q"`
}

// ChangeScoreIn sets a score.
type ChangeScoreIn struct {
	PersonID string `json:"personId"`
	NewSum   int    `json:"newSum"`
}

// SetOptionsIn patches the rules mid-game.
type SetOptionsIn struct {
	Options engine.RulesPatch `json:"options"`
}

// KickIn removes a person (host only).
type KickIn struct {
	PersonID string `json:"personId"`
	Ban      bool   `json:"ban,omitempty"`
}

// ButtonReopenIn forces a new arm for the current question (host only).
type ButtonReopenIn struct {
	ArmID string `json:"armId,omitempty" doc:"The arm to replace; empty = whichever is active"`
}

// SetTrustIn overrides the trust ladder of a player (host only).
type SetTrustIn struct {
	PersonID string            `json:"personId"`
	Level    buzzer.TrustLevel `json:"level" doc:"full | conservative | serverOnly | arrivalOnly; empty resets to automatic"`
}

// ---- server → client payloads ---------------------------------------------

// SyncPlan tells the client how often to send SYNC.
type SyncPlan struct {
	Burst      int   `json:"burst" doc:"SYNC messages to send right after connecting / becoming visible"`
	IntervalMs int64 `json:"intervalMs" doc:"Interval inside a burst"`
	SteadyMs   int64 `json:"steadyMs" doc:"Steady-state interval"`
}

// WelcomePayload is the first message on every connection.
type WelcomePayload struct {
	PersonID       string          `json:"personId"`
	Name           string          `json:"name"`
	Role           engine.Role     `json:"role"`
	IsHost         bool            `json:"isHost"`
	RoomCode       string          `json:"roomCode"`
	Showman        ShowmanMode     `json:"showman"`
	ServerTimeMs   float64         `json:"serverTimeMs" doc:"Server monotonic clock (ms); armAt/deadlineAt are on this clock"`
	ServerWallMs   int64           `json:"serverWallMs" doc:"Unix ms UTC"`
	SyncPlan       SyncPlan        `json:"syncPlan"`
	BuzzerSettings buzzer.Settings `json:"buzzerSettings"`
}

// BuzzerSnapshot is the buzzer state for a (re)connecting client.
type BuzzerSnapshot struct {
	State       string  `json:"state" doc:"idle | armed | collecting | draining | resolved"`
	ArmID       string  `json:"armId,omitempty"`
	ArmAt       float64 `json:"armAt,omitempty" doc:"Server time of the light (ms, monotonic)"`
	ArmAtLocal  float64 `json:"armAtLocal,omitempty" doc:"0 after a reconnect: the clock model was reset"`
	DeadlineAt  float64 `json:"deadlineAt,omitempty"`
	LockedUntil float64 `json:"lockedUntil,omitempty" doc:"Server time until which this player is locked out"`
}

// SnapshotPayload is the authoritative state sent after WELCOME.
type SnapshotPayload struct {
	Room    Info             `json:"room"`
	Game    *engine.Snapshot `json:"game" doc:"null in the lobby"`
	Buzzer  BuzzerSnapshot   `json:"buzzer"`
	LastSeq int64            `json:"lastSeq" doc:"Last seq sent on this person's previous connection (0 = none); send HELLO{lastSeq} to request a replay"`
}

// ResumePayload precedes replayed envelopes after a HELLO.
type ResumePayload struct {
	FromSeq int64 `json:"fromSeq"`
	Count   int   `json:"count" doc:"Envelopes replayed right after this message"`
	Covered bool  `json:"covered" doc:"false when the buffer no longer covers lastSeq (nothing replayed)"`
}

// RoomPersonsPayload lists everyone in the room.
type RoomPersonsPayload struct {
	Persons []PersonView `json:"persons"`
}

// ChatPayload is a chat line.
type ChatPayload struct {
	PersonID string `json:"personId"`
	Name     string `json:"name"`
	Text     string `json:"text"`
	AtMs     int64  `json:"atMs" doc:"Unix ms UTC"`
}

// RoomClosedPayload is the last message before the socket closes.
type RoomClosedPayload struct {
	Reason string `json:"reason" doc:"host | ttl | shutdown"`
}

// KickedPayload precedes close 4002.
type KickedPayload struct {
	Banned bool `json:"banned"`
}

// ErrorPayload is the payload of an ERROR envelope.
type ErrorPayload struct {
	Code    string `json:"code" enum:"notAllowed,badState,badArgument,unknownType,rateLimited,badPayload"`
	Message string `json:"message"`
	Ref     int64  `json:"ref,omitempty" doc:"Client seq of the message that caused the error"`
}

// SyncAckPayload answers a SYNC.
type SyncAckPayload struct {
	Seq   int64        `json:"seq"`
	C1    float64      `json:"c1"`
	S2    float64      `json:"s2" doc:"Server receive time"`
	S3    float64      `json:"s3" doc:"Server send time"`
	Model buzzer.Model `json:"model"`
}

// PressAckPayload answers a PRESS.
type PressAckPayload struct {
	ArmID string `json:"armId"`
	buzzer.PressAck
}

// PressPendingPayload announces a press that entered the fairness window.
type PressPendingPayload struct {
	ArmID    string `json:"armId"`
	PlayerID string `json:"playerId"`
}

// ButtonResultPublic is the BUTTON_RESULT variant players and viewers receive.
type ButtonResultPublic struct {
	ArmID      string  `json:"armId"`
	WinnerID   string  `json:"winnerId,omitempty"`
	Kind       string  `json:"kind" doc:"winner | nobody"`
	Contested  bool    `json:"contested"`
	MarginMs   float64 `json:"marginMs"`
	Rule       string  `json:"rule"`
	ReactionMs float64 `json:"reactionMs,omitempty" doc:"The receiving player's own scored reaction (players only)"`
	Source     string  `json:"source,omitempty" doc:"How the receiving player's press was scored"`
}

// LockoutPayload announces a false-start lockout.
type LockoutPayload struct {
	PlayerID   string  `json:"playerId"`
	UntilAt    float64 `json:"untilAt" doc:"Server time (ms)"`
	DurationMs int64   `json:"durationMs"`
	Reason     string  `json:"reason" doc:"falseStart | tooEarly | misfire"`
}

// ConnQualityEntry is one player's link quality.
type ConnQualityEntry struct {
	PlayerID  string         `json:"playerId"`
	RttMs     float64        `json:"rttMs,omitempty"`
	JitterMs  float64        `json:"jitterMs,omitempty"`
	UMs       float64        `json:"uMs,omitempty"`
	Quality   buzzer.Quality `json:"quality"`
	Connected bool           `json:"connected"`
	Flags     []string       `json:"flags,omitempty" doc:"Showman/host only"`
	Trust     *buzzer.Trust  `json:"trust,omitempty" doc:"Showman/host only"`
}

// ConnQualityPayload is broadcast periodically.
type ConnQualityPayload struct {
	Players []ConnQualityEntry `json:"players"`
}

// AuditEntry is one press evaluation (camelCase view of buzzer.AuditEntry).
type AuditEntry struct {
	PlayerID string       `json:"playerId"`
	Status   string       `json:"status"`
	TRecv    float64      `json:"tRecv"`
	TArr     float64      `json:"tArr"`
	TClient  float64      `json:"tClient"`
	Lo       float64      `json:"lo"`
	Hi       float64      `json:"hi"`
	Credit   float64      `json:"credit"`
	TFinal   float64      `json:"tFinal"`
	Reaction float64      `json:"reaction"`
	Source   string       `json:"source"`
	Flags    []string     `json:"flags,omitempty"`
	Model    buzzer.Model `json:"model"`
}

// ButtonAuditPayload is sent to the host after every resolved arm.
type ButtonAuditPayload struct {
	ArmID      string       `json:"armId"`
	QuestionID string       `json:"questionId"`
	Seed       string       `json:"seed"`
	TArm       float64      `json:"tArm"`
	Entries    []AuditEntry `json:"entries"`
}

// ArmRecord is one entry of the buzz log (host REST).
type ArmRecord struct {
	ArmID      string        `json:"armId"`
	QuestionID string        `json:"questionId"`
	TArm       float64       `json:"tArm"`
	Result     buzzer.Result `json:"result"`
	Audit      []AuditEntry  `json:"audit"`
	ResolvedAt int64         `json:"resolvedAt" doc:"Unix ms UTC"`
}

// AIVerdictPayload reports an automatic verdict (AI or fuzzy).
type AIVerdictPayload struct {
	PersonID  string  `json:"personId"`
	Right     bool    `json:"right"`
	Factor    float64 `json:"factor"`
	Uncertain bool    `json:"uncertain"`
	Reason    string  `json:"reason,omitempty"`
	Source    string  `json:"source" doc:"ai | fuzzy | fallback | exact | ..."`
	Applied   bool    `json:"applied" doc:"true when the verdict was applied immediately; false = suggestion for the human showman"`
}

func auditView(entries []buzzer.AuditEntry) []AuditEntry {
	out := make([]AuditEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, AuditEntry{PlayerID: e.PlayerID, Status: e.Status, TRecv: e.TRecv, TArr: e.TArr, TClient: e.TClient,
			Lo: e.Lo, Hi: e.Hi, Credit: e.Credit, TFinal: e.TFinal, Reaction: e.Reaction, Source: e.Source, Flags: e.Flags, Model: e.Model})
	}
	return out
}
