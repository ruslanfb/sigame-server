// Package ws implements the WebSocket transport: connection lifecycle,
// message envelopes, per-connection rate limiting, the protocol-ping RTT
// anchor and the kernel RTT anchor. Game semantics live in internal/room.
package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
)

// Envelope is the wire format of every WebSocket message in both directions:
//
//	{"t":"TYPE","seq":12,"p":{...}}
//
// Server→client seq is per-connection monotonic (for resume/dedup);
// client→server seq is per-connection monotonic (idempotency; echoed in ERROR.ref).
type Envelope struct {
	T   string          `json:"t"`
	Seq int64           `json:"seq,omitempty"`
	P   json.RawMessage `json:"p,omitempty"`
}

// ErrorPayload is the payload of an ERROR envelope.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Ref     int64  `json:"ref,omitempty" doc:"Client seq of the message that caused the error"`
}

var typeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

// Sentinel decode errors.
var (
	ErrEnvelopeTooLarge = errors.New("ws: message exceeds the size limit")
	ErrEnvelopeInvalid  = errors.New("ws: invalid envelope")
)

// Decode parses a text frame into an Envelope, enforcing the size limit and
// the SCREAMING_SNAKE type format.
func Decode(data []byte, maxBytes int64) (Envelope, error) {
	if int64(len(data)) > maxBytes {
		return Envelope{}, ErrEnvelopeTooLarge
	}
	var e Envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return Envelope{}, fmt.Errorf("%w: %v", ErrEnvelopeInvalid, err)
	}
	if !typeRe.MatchString(e.T) {
		return Envelope{}, fmt.Errorf("%w: bad type %q", ErrEnvelopeInvalid, e.T)
	}
	if e.Seq < 0 {
		return Envelope{}, fmt.Errorf("%w: negative seq", ErrEnvelopeInvalid)
	}
	return e, nil
}

// Encode marshals a typed payload into an envelope frame.
func Encode(t string, seq int64, payload any) ([]byte, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("ws: encode %s: %w", t, err)
		}
		raw = b
	}
	return json.Marshal(Envelope{T: t, Seq: seq, P: raw})
}

// DecodePayload unmarshals the envelope payload into v (nil payload → zero value).
func DecodePayload[T any](e Envelope) (T, error) {
	var v T
	if len(e.P) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(e.P, &v); err != nil {
		return v, fmt.Errorf("%w: payload of %s: %v", ErrEnvelopeInvalid, e.T, err)
	}
	return v, nil
}
