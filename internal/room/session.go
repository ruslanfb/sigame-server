package room

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"

	"sigame/internal/buzzer"
	"sigame/internal/engine"
)

// Session is what a client receives from Join and presents on /ws.
type Session struct {
	Token    string      `json:"token" doc:"Opaque session token for GET /ws?room=&token="`
	PersonID string      `json:"personId"`
	Role     engine.Role `json:"role"`
	IsHost   bool        `json:"isHost"`
	RoomCode string      `json:"roomCode"`
}

// ConnInfo exposes the transport anchors of a live connection.
type ConnInfo struct {
	RemoteAddr string
	// KernelRTT reads the TCP stack's RTT estimate (nil or ok=false when unavailable).
	KernelRTT func() (srttMs, rttvarMs float64, ok bool)
	// WSPing sends a protocol ping and returns the measured round trip (nil when unsupported).
	WSPing func(ctx context.Context) (rttMs float64, err error)
}

// Conn is a live client connection as seen by the room. internal/ws
// implements it; tests use a fake. Implementations must be safe for
// concurrent use and must never block the caller for long.
type Conn interface {
	// Send enqueues an envelope. payload is already-encoded JSON
	// (json.RawMessage) or nil. It returns the per-connection seq assigned to
	// the envelope. An error means the connection is dead or too slow.
	Send(t string, payload json.RawMessage) (seq int64, err error)
	// Close closes the socket asynchronously with a WebSocket close code.
	Close(code int, reason string)
	// Info returns the transport anchors.
	Info() ConnInfo
}

// Inbound is one client envelope delivered to the room. TRecv is the server
// monotonic receive stamp taken by the reader before JSON decoding.
type Inbound struct {
	T     string
	Seq   int64
	P     json.RawMessage
	TRecv float64
}

// Attachment is returned by Manager.Attach.
type Attachment struct {
	Session Session
	// Deliver posts a client envelope to the room actor (blocks only while the
	// mailbox is full).
	Deliver func(Inbound)
	// Detach unregisters the connection; idempotent.
	Detach func()
}

// WebSocket close codes used by the room (the transport maps them 1:1).
const (
	CloseNormal          = 1000
	CloseGoingAway       = 1001
	ClosePolicy          = 1008
	CloseBadToken        = 4000
	CloseSessionReplaced = 4001
	CloseKicked          = 4002
)

// newToken returns 32 random bytes in base64url (no padding).
func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("room: crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) [32]byte { return sha256.Sum256([]byte(t)) }

// tokenEqual compares two token hashes in constant time.
func tokenEqual(a, b [32]byte) bool { return subtle.ConstantTimeCompare(a[:], b[:]) == 1 }

func newSeed() []byte {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("room: crypto/rand: " + err.Error())
	}
	return b
}

// person is a participant (host, showman, player or viewer). Owned by the actor.
type person struct {
	ID        string
	Name      string
	Role      engine.Role
	IsHost    bool
	IsAI      bool
	tokenHash [32]byte
	kicked    bool
	joinedAt  int64

	conn         Conn
	gen          int // connection generation; increments on every attach
	connected    bool
	cancelAnchor context.CancelFunc
	bconn        *buzzer.Conn
	lastSeq      int64 // last seq sent on the current connection
	prevLastSeq  int64 // last seq sent on the previous connection
	ring         *resumeRing
	lastChatAt   float64
	buttonsWon   int
	lockedUntil  float64 // room-level misfire lockout (server ms)
	lockoutSent  float64 // last LOCKOUT untilAt broadcast for this person (dedups misfire spam)
}

func (p *person) actor() engine.Actor {
	return engine.Actor{PersonID: p.ID, Role: p.Role, IsHost: p.IsHost}
}

func (p *person) view(score int) PersonView {
	return PersonView{ID: p.ID, Name: p.Name, Role: p.Role, Connected: p.connected || p.IsAI, Score: score, IsHost: p.IsHost}
}

// resumeRing keeps the last N outbound envelopes of a person with the
// connection generation and seq they were sent with.
type resumeRing struct {
	buf  []ringEntry
	head int
	n    int
}

type ringEntry struct {
	gen int
	seq int64
	t   string
	p   json.RawMessage
}

func newResumeRing(size int) *resumeRing {
	if size <= 0 {
		size = 1
	}
	return &resumeRing{buf: make([]ringEntry, size)}
}

func (r *resumeRing) push(e ringEntry) {
	r.buf[r.head] = e
	r.head = (r.head + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

// entries returns the buffered entries oldest first.
func (r *resumeRing) entries() []ringEntry {
	out := make([]ringEntry, 0, r.n)
	start := (r.head - r.n + len(r.buf)) % len(r.buf)
	for i := 0; i < r.n; i++ {
		out = append(out, r.buf[(start+i)%len(r.buf)])
	}
	return out
}

// replayable lists the message types worth replaying after a resume: the
// SNAPSHOT is authoritative for game state, so only conversational and timer
// progress messages are replayed (best effort).
var replayable = map[string]bool{
	MsgChat: true, MsgLockout: true,
	string(engine.EvTimerStart): true, string(engine.EvTimerStop): true,
	string(engine.EvTimerPause): true, string(engine.EvTimerResume): true,
}
