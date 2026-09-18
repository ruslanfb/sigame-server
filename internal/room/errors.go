package room

import "errors"

// Sentinel errors returned by Manager methods. The HTTP layer maps them to
// problem+json statuses; the WebSocket layer maps them to close codes.
var (
	ErrRoomNotFound          = errors.New("room: not found")
	ErrRoomClosed            = errors.New("room: closed")
	ErrTooManyRooms          = errors.New("room: too many rooms")
	ErrBadPassword           = errors.New("room: bad password")
	ErrRoomFull              = errors.New("room: no free seat")
	ErrNameTaken             = errors.New("room: name already taken")
	ErrJoinClosed            = errors.New("room: joining is closed")
	ErrBadRole               = errors.New("room: role not available")
	ErrInvalidHostToken      = errors.New("room: invalid host token")
	ErrBadSession            = errors.New("room: unknown session")
	ErrPersonNotFound        = errors.New("room: person not found")
	ErrBanned                = errors.New("room: banned")
	ErrInvalidParams         = errors.New("room: invalid parameters")
	ErrUnsupportedBuzzerMode = errors.New("room: buzzer mode not supported in this version")
	ErrPackNotFound          = errors.New("room: pack not found")
	ErrBadState              = errors.New("room: bad state")
)
