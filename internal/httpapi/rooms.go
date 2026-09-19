package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/buzzer"
	"sigame/internal/engine"
	"sigame/internal/lan"
	"sigame/internal/room"
)

// Room endpoints (tag rooms). Flow: POST /rooms → hostToken; POST
// /rooms/{code}/join → sessionToken; GET /ws?room=CODE&token=SESSION
// (internal/ws) → play. The host-only operations take the host token in the
// X-Host-Token header; the manager verifies it against the room.

// hostSecurity marks host-only operations in the OpenAPI spec (apiKey in X-Host-Token).
var hostSecurity = []map[string][]string{{"hostToken": {}}}

const (
	// hostTokenHeader carries the room creator's secret.
	hostTokenHeader = "X-Host-Token"
	// defaultHybridConfirmMs mirrors the room package default.
	defaultHybridConfirmMs = 8000
	// wsPath is where internal/ws mounts the WebSocket handler.
	wsPath = "/ws"
)

// --- DTOs --------------------------------------------------------------------

// RoomView is the public projection of a room: room.Info under its own
// schema name (the type carries no secrets). players lists everyone in the
// room — showman, players and viewers — with role, connection state, score
// and host flag.
type RoomView room.Info

// RoomList is the answer of GET /rooms.
type RoomList struct {
	Items []RoomView `json:"items" doc:"Every open room, newest first; players[] carries the role, connected flag and score of each person"`
}

// CreateRoomRequest is the body of POST /rooms. Only packId is required.
type CreateRoomRequest struct {
	PackID          string               `json:"packId" minLength:"1" doc:"ID of the stored pack to play; it must have at least one round"`
	Name            string               `json:"name,omitempty" maxLength:"80" doc:"Room name shown in the lobby list; default = the pack name"`
	Password        string               `json:"password,omitempty" maxLength:"64" doc:"Join password; empty = open room. A valid X-Host-Token bypasses it"`
	Showman         room.ShowmanMode     `json:"showman,omitempty" enum:"human,ai,hybrid" doc:"Who validates answers: human (a person joins as showman; default), ai (the AI judge is the showman, nobody can take the seat) or hybrid (a human showman gets AI suggestions and the AI verdict applies after hybridConfirmMs). ai and hybrid need a configured AI judge (422 otherwise)"`
	Rules           *engine.Rules        `json:"rules,omitempty" doc:"Complete game rules; omitted = SIGame defaults. A partial object is rejected with 422"`
	Times           *engine.TimeSettings `json:"times,omitempty" doc:"Complete timer settings in milliseconds; omitted = SIGame defaults. A partial object is rejected with 422"`
	BuzzerPreset    string               `json:"buzzerPreset,omitempty" enum:"lanWired,wifiParty,internetFair,tournament,noRace" doc:"Named buzzer preset (GET /buzzer-presets); default wifiParty. Ignored when buzzer is given"`
	Buzzer          *buzzer.Settings     `json:"buzzer,omitempty" doc:"Explicit buzzer settings; when given they replace the preset entirely and are validated (422 on error)"`
	MaxPlayers      int                  `json:"maxPlayers,omitempty" minimum:"1" maximum:"12" doc:"Player seats, 1..maxPlayers of GET /system/info; default = the server maximum"`
	AllowViewers    *bool                `json:"allowViewers,omitempty" doc:"Whether viewers may join; default true"`
	HybridConfirmMs int64                `json:"hybridConfirmMs,omitempty" minimum:"0" maximum:"600000" doc:"hybrid showman: the AI verdict applies after this many ms without a human verdict; default 8000"`
	Language        string               `json:"language,omitempty" maxLength:"16" doc:"BCP-47 language hint for the AI judge, e.g. ru"`
}

// CreateRoomResponse is the answer of POST /rooms.
type CreateRoomResponse struct {
	Room      RoomView `json:"room"`
	HostToken string   `json:"hostToken" doc:"Secret of the room creator, shown only here: send it as X-Host-Token when joining to become the host and on every host-only operation"`
	JoinURL   string   `json:"joinUrl" doc:"Client-facing hint for sharing: the server's first join URL (public URL or first LAN address) with ?room=CODE appended; a client page can read the code from it. The API itself only needs the code"`
}

// JoinRequest is the body of POST /rooms/{code}/join.
type JoinRequest struct {
	Name     string      `json:"name" minLength:"1" maxLength:"32" doc:"Display name, unique within the room (case-insensitive). Re-joining with the same name and role while that person is disconnected resumes it (score kept)"`
	Role     engine.Role `json:"role,omitempty" enum:"player,showman,viewer" doc:"Seat to take; default player. showman is refused in ai rooms and when the seat is taken; viewer needs allowViewers"`
	Password string      `json:"password,omitempty" doc:"Room password when the room has one; not needed with a valid X-Host-Token"`
}

// JoinResponse is the answer of POST /rooms/{code}/join.
type JoinResponse struct {
	SessionToken string      `json:"sessionToken" doc:"Opaque session secret: the token of GET /ws?room=CODE&token=SESSION and of POST /rooms/{code}/leave"`
	PersonID     string      `json:"personId" doc:"UUIDv7 of the person; appears in players[] of the room and in every room and game message"`
	Role         engine.Role `json:"role" enum:"showman,player,viewer" doc:"Seat actually taken"`
	IsHost       bool        `json:"isHost" doc:"True when this session holds host rights (joined with the host token, or the host was transferred to it)"`
	RoomCode     string      `json:"roomCode" doc:"5-character join code"`
	WsURL        string      `json:"wsUrl" doc:"Relative WebSocket URL (/ws?room=CODE&token=SESSION): prefix it with the server origin and switch the scheme to ws:// or wss://"`
}

// LeaveRequest is the body of POST /rooms/{code}/leave.
type LeaveRequest struct {
	SessionToken string `json:"sessionToken" minLength:"1" doc:"The sessionToken returned by join"`
}

// UpdateRoomSettingsRequest is the body of PATCH /rooms/{code}/settings:
// room.SettingsPatch plus a preset shortcut for the buzzer.
type UpdateRoomSettingsRequest struct {
	room.SettingsPatch
	BuzzerPreset string `json:"buzzerPreset,omitempty" enum:"lanWired,wifiParty,internetFair,tournament,noRace" doc:"Replace the buzzer settings with a named preset; ignored when buzzer is given"`
}

// KickRequest is the body of POST /rooms/{code}/kick.
type KickRequest struct {
	PersonID string `json:"personId" minLength:"1" doc:"ID of the person to remove (players[].id); the host cannot be kicked"`
	Ban      bool   `json:"ban,omitempty" doc:"Also block the name (case-insensitive) until unbanned"`
}

// PersonRequest carries one person id (unban, transfer-host).
type PersonRequest struct {
	PersonID string `json:"personId" minLength:"1" doc:"ID of the person (players[].id)"`
}

// BuzzLog is the answer of GET /rooms/{code}/buzz-log.
type BuzzLog struct {
	Arms []room.ArmRecord `json:"arms" doc:"Resolved button rounds, oldest first: the public result and the per-press audit of every arm"`
}

// --- inputs / outputs ----------------------------------------------------------

type roomCodeInput struct {
	Code string `path:"code" doc:"5-character join code (case-insensitive)"`
}

type hostInput struct {
	Code      string `path:"code" doc:"5-character join code (case-insensitive)"`
	HostToken string `header:"X-Host-Token" doc:"The hostToken returned by POST /rooms. 401 when missing, 403 when it does not match the room."`
}

type createRoomInput struct {
	Body CreateRoomRequest
}

type createRoomOutput struct {
	Location string `header:"Location" doc:"URL of the created room (/api/v1/rooms/{code})"`
	Body     CreateRoomResponse
}

type roomOutput struct {
	Body RoomView
}

type listRoomsOutput struct {
	Body RoomList
}

type joinInput struct {
	Code      string      `path:"code" doc:"5-character join code (case-insensitive)"`
	HostToken string      `header:"X-Host-Token" doc:"Optional: the hostToken returned by POST /rooms. The first joiner presenting it becomes the host (isHost) and bypasses the password and the join mode; 403 when it does not match the room."`
	Body      JoinRequest `doc:"Name, role and password"`
}

type joinOutput struct {
	Body JoinResponse
}

type leaveInput struct {
	Code string `path:"code" doc:"5-character join code (case-insensitive)"`
	Body LeaveRequest
}

type updateSettingsInput struct {
	Code      string                    `path:"code" doc:"5-character join code (case-insensitive)"`
	HostToken string                    `header:"X-Host-Token" doc:"Required: the hostToken returned by POST /rooms. 401 when missing, 403 when it does not match the room."`
	Body      UpdateRoomSettingsRequest `doc:"Only the given fields change; nil/omitted fields are untouched"`
}

type kickInput struct {
	Code      string `path:"code" doc:"5-character join code (case-insensitive)"`
	HostToken string `header:"X-Host-Token" doc:"Required: the hostToken returned by POST /rooms. 401 when missing, 403 when it does not match the room."`
	Body      KickRequest
}

type personInput struct {
	Code      string `path:"code" doc:"5-character join code (case-insensitive)"`
	HostToken string `header:"X-Host-Token" doc:"Required: the hostToken returned by POST /rooms. 401 when missing, 403 when it does not match the room."`
	Body      PersonRequest
}

type buzzLogInput struct {
	Code       string `path:"code" doc:"5-character join code (case-insensitive)"`
	HostToken  string `header:"X-Host-Token" doc:"Required: the hostToken returned by POST /rooms. 401 when missing, 403 when it does not match the room."`
	QuestionID string `query:"questionId" doc:"Only the arms of this question; omitted = every arm of the room"`
}

type buzzLogOutput struct {
	Body BuzzLog
}

// --- helpers ---------------------------------------------------------------------

func roomLocation(code string) string { return basePath + "/rooms/" + code }

// wsURL builds the relative WebSocket URL a client opens after joining.
func wsURL(code, token string) string {
	return wsPath + "?room=" + url.QueryEscape(code) + "&token=" + url.QueryEscape(token)
}

// rooms returns the manager or a 503 when rooms are not wired.
func (s *Server) rooms() (*room.Manager, error) {
	if s.deps.Rooms == nil {
		return nil, huma.Error503ServiceUnavailable("rooms are not enabled on this server")
	}
	return s.deps.Rooms, nil
}

// requireHostToken answers 401 for a missing X-Host-Token; the manager
// reports a mismatch (403).
func requireHostToken(token string) error {
	if strings.TrimSpace(token) == "" {
		return huma.Error401Unauthorized(hostTokenHeader+" header is required",
			&huma.ErrorDetail{Location: "header." + hostTokenHeader, Message: "missing host token", Value: "hostTokenRequired"})
	}
	return nil
}

// aiConfigured reports whether a real AI judge is available: an OpenRouter
// key in the configuration or a status provider that reports one. Deps.AI
// alone is not a signal — the server always wires a fuzzy-only judge.
func (s *Server) aiConfigured(ctx context.Context) bool {
	if s.deps.Cfg != nil && s.deps.Cfg.AIConfigured() {
		return true
	}
	return s.deps.AIStatus != nil && s.deps.AIStatus.Status(ctx).Configured
}

// joinURL builds the shareable hint: the server's first join URL (public
// URL first, then the listen host or the LAN addresses) with the room code
// as a query parameter. Without any known address it is relative.
func (s *Server) joinURL(code string) string {
	cfg := s.deps.Cfg
	addrs, err := lan.Addresses()
	if err != nil {
		s.log.Warn("join url: cannot list interfaces", "err", err)
	}
	base := ""
	if urls := lan.JoinURLs(cfg.Addr, cfg.PublicURL, addrs); len(urls) > 0 {
		base = strings.TrimRight(urls[0], "/")
	}
	return base + "/?room=" + url.QueryEscape(code)
}

// invalidBuzzer builds the 422 for buzzer settings that fail validation.
func invalidBuzzer(location string, err error) error {
	return huma.Error422UnprocessableEntity("invalid buzzer settings",
		&huma.ErrorDetail{Location: location, Message: strings.TrimPrefix(err.Error(), buzzer.ErrInvalidSettings.Error()+": "), Value: "invalidBuzzerSettings"})
}

// unknownPreset builds the 422 for a preset name the buzzer package does
// not know (the enum tag normally catches it first).
func unknownPreset(location, name string) error {
	return huma.Error422UnprocessableEntity("unknown buzzer preset",
		&huma.ErrorDetail{Location: location, Message: "unknown buzzer preset; see GET /buzzer-presets", Value: name})
}

// resolveBuzzer picks the room's buzzer settings: explicit settings win and
// are validated, otherwise the named preset (default wifiParty).
func resolveBuzzer(preset string, explicit *buzzer.Settings) (buzzer.Settings, error) {
	if explicit != nil {
		if err := explicit.Validate(); err != nil {
			return buzzer.Settings{}, invalidBuzzer("body.buzzer", err)
		}
		return *explicit, nil
	}
	if preset == "" {
		preset = buzzer.PresetWiFiParty
	}
	settings, ok := buzzer.Preset(preset)
	if !ok {
		return buzzer.Settings{}, unknownPreset("body.buzzerPreset", preset)
	}
	return settings, nil
}

// roomError translates the sentinel errors of internal/room (and the engine
// errors it lets through) into huma status errors; anything else is passed
// on unchanged so that mapError/fail treat it as a server failure. The
// problem `detail` keeps the room's message; `errors[0].value` carries the
// machine-readable code.
func roomError(err error) error {
	if err == nil {
		return nil
	}
	var se huma.StatusError
	if errors.As(err, &se) {
		return se
	}
	msg := strings.TrimPrefix(strings.TrimPrefix(err.Error(), "room: "), "engine: ")
	detail := func(location, code string) *huma.ErrorDetail {
		return &huma.ErrorDetail{Location: location, Message: msg, Value: code}
	}
	switch {
	case errors.Is(err, room.ErrRoomNotFound):
		return huma.Error404NotFound("room not found")
	case errors.Is(err, room.ErrRoomClosed):
		return huma.Error410Gone("room is closed")
	case errors.Is(err, room.ErrTooManyRooms):
		return huma.Error503ServiceUnavailable("too many open rooms; close one or try again later")
	case errors.Is(err, room.ErrBadPassword):
		return huma.Error403Forbidden("wrong room password", detail("body.password", "badPassword"))
	case errors.Is(err, room.ErrInvalidHostToken):
		return huma.Error403Forbidden("invalid host token", detail("header."+hostTokenHeader, "invalidHostToken"))
	case errors.Is(err, room.ErrBanned):
		return huma.Error403Forbidden("this name is banned from the room", detail("body.name", "banned"))
	case errors.Is(err, room.ErrBadSession):
		return huma.Error403Forbidden("unknown session token", detail("body.sessionToken", "badSession"))
	case errors.Is(err, room.ErrRoomFull):
		return huma.Error409Conflict("no free player seat", detail("body.role", "roomFull"))
	case errors.Is(err, room.ErrNameTaken):
		return huma.Error409Conflict("name already taken by a connected person", detail("body.name", "nameTaken"))
	case errors.Is(err, room.ErrJoinClosed):
		return huma.Error423Locked("joining is closed by the host", detail("body.role", "joinClosed"))
	case errors.Is(err, room.ErrBadRole):
		return huma.Error422UnprocessableEntity(msg, detail("body.role", "badRole"))
	case errors.Is(err, room.ErrPersonNotFound):
		return huma.Error404NotFound("person not found", detail("body.personId", "personNotFound"))
	case errors.Is(err, room.ErrPackNotFound):
		return huma.Error422UnprocessableEntity("pack not found", detail("body.packId", "packNotFound"))
	case errors.Is(err, room.ErrUnsupportedBuzzerMode):
		return huma.Error422UnprocessableEntity(msg, detail("body.buzzer", "unsupportedBuzzerMode"))
	case errors.Is(err, room.ErrInvalidParams), errors.Is(err, engine.ErrBadArgument):
		return huma.Error422UnprocessableEntity(msg, detail("body", "invalidParams"))
	case errors.Is(err, room.ErrBadState), errors.Is(err, engine.ErrBadState):
		return huma.Error409Conflict(msg, detail("body", "badState"))
	case errors.Is(err, engine.ErrNotAllowed):
		return huma.Error403Forbidden(msg, detail("body.personId", "notAllowed"))
	}
	return err
}

// failRoom maps a room error like fail maps the others.
func (s *Server) failRoom(err error) error { return s.fail(roomError(err)) }

// --- registration ------------------------------------------------------------------

func (s *Server) registerRooms() {
	huma.Register(s.API, huma.Operation{
		OperationID: "listRooms",
		Method:      http.MethodGet,
		Path:        basePath + "/rooms",
		Tags:        []string{tagRooms},
		Summary:     "List rooms",
		Description: "Returns every open room (lobby, playing or finished; closed rooms disappear), newest first. Each item is the public room view: code, name, pack, status, showman mode, whether a password is required, the people in it with role/connected/score, seat limits, the effective rules, timers and buzzer settings. Nothing secret is included.",
		Errors:      []int{http.StatusServiceUnavailable},
	}, s.listRooms)

	huma.Register(s.API, huma.Operation{
		OperationID:   "createRoom",
		Method:        http.MethodPost,
		Path:          basePath + "/rooms",
		Tags:          []string{tagRooms},
		Summary:       "Create a room",
		Description:   "Creates a room in the lobby state from a stored pack and returns it together with the secret hostToken, which is shown only in this response: keep it, send it as X-Host-Token when joining to become the host and on every host-only operation. Defaults: name = pack name, showman human, SIGame rules and timers, buzzer preset wifiParty, maxPlayers = server maximum, viewers allowed, hybridConfirmMs 8000. rules and times must be complete objects when given; buzzer replaces the preset entirely and is validated. ai/hybrid showman modes need a configured AI judge (422 otherwise). joinUrl is a sharing hint (server join URL + ?room=CODE). 422 for an unknown pack or invalid parameters, 503 when the room limit is reached.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, s.createRoom)

	huma.Register(s.API, huma.Operation{
		OperationID: "getRoom",
		Method:      http.MethodGet,
		Path:        basePath + "/rooms/{code}",
		Tags:        []string{tagRooms},
		Summary:     "Get a room",
		Description: "Returns the public view of one room by its join code (case-insensitive): status, people with role/connected/score, seat limits and the effective rules, timers and buzzer settings. 404 once the room is closed.",
		Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}, s.getRoom)

	huma.Register(s.API, huma.Operation{
		OperationID: "joinRoom",
		Method:      http.MethodPost,
		Path:        basePath + "/rooms/{code}/join",
		Tags:        []string{tagRooms},
		Summary:     "Join a room",
		Description: "Takes a seat (player, showman or viewer) under a display name and returns a sessionToken; then open GET /ws?room=CODE&token=SESSION (wsUrl is that relative URL). The room creator sends the hostToken in X-Host-Token: the first person presenting it becomes the host (isHost) and bypasses the password and the join mode. Re-joining with the same name and role while that person is disconnected resumes the person with a new token. Errors: 403 wrong password (value badPassword), banned name or invalid host token; 404 unknown room; 409 no free seat or name already in use; 410 room closed; 422 role not available (showman seat taken, AI showman, viewers disabled); 423 joining closed by the host.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusLocked, http.StatusServiceUnavailable},
	}, s.joinRoom)

	huma.Register(s.API, huma.Operation{
		OperationID: "leaveRoom",
		Method:      http.MethodPost,
		Path:        basePath + "/rooms/{code}/leave",
		Tags:        []string{tagRooms},
		Summary:     "Leave a room",
		Description: "Ends the session: the live connection (if any) is closed with code 1000 and the token stops working. A seated player of a running game keeps the seat and score (shown disconnected) so the same name can rejoin; everyone else is removed from the room. 403 for an unknown session token.",
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable},
	}, s.leaveRoom)

	huma.Register(s.API, huma.Operation{
		OperationID: "updateRoomSettings",
		Security:    hostSecurity,
		Method:      http.MethodPatch,
		Path:        basePath + "/rooms/{code}/settings",
		Tags:        []string{tagRooms},
		Summary:     "Change room settings (host)",
		Description: "Host-only (X-Host-Token). Patches the room: name, password (empty string removes it), joinMode (any, viewersOnly, closed), showman mode and times (lobby only), rules (the mid-game subset, applied via SET_OPTIONS while playing), and the buzzer either as explicit settings or as a named preset. Everyone in the room receives ROOM_SETTINGS. Returns the updated room view. 409 when the change is not allowed in the current state, 422 for invalid values.",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, s.updateRoomSettings)

	huma.Register(s.API, huma.Operation{
		OperationID: "kickPerson",
		Security:    hostSecurity,
		Method:      http.MethodPost,
		Path:        basePath + "/rooms/{code}/kick",
		Tags:        []string{tagRooms},
		Summary:     "Kick a person (host)",
		Description: "Host-only (X-Host-Token). Removes a person from the room: the socket receives KICKED and close code 4002, a seated player of a running game is marked kicked in the engine. With ban=true the name is blocked (case-insensitive) until POST /rooms/{code}/unban with the same personId. The host cannot be kicked (403). 404 for an unknown person.",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable},
	}, s.kickPerson)

	huma.Register(s.API, huma.Operation{
		OperationID: "unbanPerson",
		Security:    hostSecurity,
		Method:      http.MethodPost,
		Path:        basePath + "/rooms/{code}/unban",
		Tags:        []string{tagRooms},
		Summary:     "Lift a ban (host)",
		Description: "Host-only (X-Host-Token). Lifts the ban recorded by kick with ban=true so that the name can join again. 404 when no ban is recorded for the person.",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable},
	}, s.unbanPerson)

	huma.Register(s.API, huma.Operation{
		OperationID: "transferHost",
		Security:    hostSecurity,
		Method:      http.MethodPost,
		Path:        basePath + "/rooms/{code}/transfer-host",
		Tags:        []string{tagRooms},
		Summary:     "Transfer host rights (host)",
		Description: "Host-only (X-Host-Token). Makes another person the host: the previous host loses isHost, the room receives HOST_CHANGED and ROOM_PERSONS. The host token itself is room-level and keeps authorising these REST operations. 404 for an unknown person (the AI showman cannot be host).",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable},
	}, s.transferHost)

	huma.Register(s.API, huma.Operation{
		OperationID: "closeRoom",
		Security:    hostSecurity,
		Method:      http.MethodDelete,
		Path:        basePath + "/rooms/{code}",
		Tags:        []string{tagRooms},
		Summary:     "Close a room (host)",
		Description: "Host-only (X-Host-Token). Closes the room: everyone receives ROOM_CLOSED{reason:\"host\"} and close code 1000, the result is persisted and the code stops resolving (GET answers 404 shortly after). The close is asynchronous: 204 means it was accepted.",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable},
	}, s.closeRoom)

	huma.Register(s.API, huma.Operation{
		OperationID: "getBuzzLog",
		Security:    hostSecurity,
		Method:      http.MethodGet,
		Path:        basePath + "/rooms/{code}/buzz-log",
		Tags:        []string{tagRooms},
		Summary:     "Buzz log (host)",
		Description: "Host-only (X-Host-Token). Returns the resolved button rounds of the room, oldest first, optionally filtered by questionId: for each arm the public result (winner, ranking, margin, tie-break rule, seed) and the per-press audit (arrival and client stamps, credit, final time, trust source and flags, clock model). It is the same data the host receives live as BUTTON_AUDIT.",
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable},
	}, s.getBuzzLog)
}

// --- handlers ------------------------------------------------------------------------

func (s *Server) listRooms(_ context.Context, _ *struct{}) (*listRoomsOutput, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	infos := mgr.List()
	items := make([]RoomView, 0, len(infos))
	for _, info := range infos {
		items = append(items, RoomView(info))
	}
	return &listRoomsOutput{Body: RoomList{Items: items}}, nil
}

func (s *Server) createRoom(ctx context.Context, in *createRoomInput) (*createRoomOutput, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	b := in.Body

	showman := b.Showman
	if showman == "" {
		showman = room.ShowmanHuman
	}
	if showman != room.ShowmanHuman && !s.aiConfigured(ctx) {
		return nil, huma.Error422UnprocessableEntity("ai showman not configured",
			&huma.ErrorDetail{Location: "body.showman", Message: "ai and hybrid showman modes need a configured AI judge (OPENROUTER_API_KEY)", Value: "aiNotConfigured"})
	}

	rules := engine.DefaultRules()
	if b.Rules != nil {
		rules = *b.Rules
	}
	times := engine.DefaultTimeSettings()
	if b.Times != nil {
		times = *b.Times
	}
	bz, err := resolveBuzzer(b.BuzzerPreset, b.Buzzer)
	if err != nil {
		return nil, err
	}

	limit := s.deps.Cfg.MaxPlayersPerRoom
	if limit <= 0 || limit > engine.MaxPlayers {
		limit = engine.MaxPlayers
	}
	maxPlayers := limit
	if b.MaxPlayers != 0 {
		if b.MaxPlayers < 1 || b.MaxPlayers > limit {
			return nil, huma.Error422UnprocessableEntity("maxPlayers out of range",
				&huma.ErrorDetail{Location: "body.maxPlayers", Message: "must be between 1 and the server maximum " + strconv.Itoa(limit), Value: b.MaxPlayers})
		}
		maxPlayers = b.MaxPlayers
	}
	allowViewers := true
	if b.AllowViewers != nil {
		allowViewers = *b.AllowViewers
	}
	hybrid := b.HybridConfirmMs
	if hybrid <= 0 {
		hybrid = defaultHybridConfirmMs
	}

	info, hostToken, err := mgr.Create(ctx, room.CreateParams{
		PackID:          b.PackID,
		Name:            b.Name,
		Password:        b.Password,
		Rules:           rules,
		Times:           times,
		Buzzer:          bz,
		Showman:         showman,
		HybridConfirmMs: hybrid,
		MaxPlayers:      maxPlayers,
		AllowViewers:    allowViewers,
		Language:        b.Language,
	})
	if err != nil {
		if errors.Is(err, room.ErrUnsupportedBuzzerMode) && b.Buzzer == nil {
			// The settings came from a preset: point at that field.
			return nil, huma.Error422UnprocessableEntity("buzzer preset not supported",
				&huma.ErrorDetail{Location: "body.buzzerPreset", Message: strings.TrimPrefix(err.Error(), "room: "), Value: "unsupportedBuzzerMode"})
		}
		return nil, s.failRoom(err)
	}
	return &createRoomOutput{
		Location: roomLocation(info.Code),
		Body:     CreateRoomResponse{Room: RoomView(info), HostToken: hostToken, JoinURL: s.joinURL(info.Code)},
	}, nil
}

func (s *Server) getRoom(_ context.Context, in *roomCodeInput) (*roomOutput, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	info, ok := mgr.Get(in.Code)
	if !ok {
		return nil, roomError(room.ErrRoomNotFound)
	}
	return &roomOutput{Body: RoomView(info)}, nil
}

func (s *Server) joinRoom(ctx context.Context, in *joinInput) (*joinOutput, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	role := in.Body.Role
	if role == "" {
		role = engine.RolePlayer
	}
	sess, err := mgr.Join(ctx, in.Code, room.JoinParams{Name: in.Body.Name, Role: role, Password: in.Body.Password}, strings.TrimSpace(in.HostToken))
	if err != nil {
		return nil, s.failRoom(err)
	}
	return &joinOutput{Body: JoinResponse{
		SessionToken: sess.Token,
		PersonID:     sess.PersonID,
		Role:         sess.Role,
		IsHost:       sess.IsHost,
		RoomCode:     sess.RoomCode,
		WsURL:        wsURL(sess.RoomCode, sess.Token),
	}}, nil
}

func (s *Server) leaveRoom(_ context.Context, in *leaveInput) (*struct{}, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	if err := mgr.Leave(in.Code, in.Body.SessionToken); err != nil {
		return nil, s.failRoom(err)
	}
	return nil, nil
}

func (s *Server) updateRoomSettings(_ context.Context, in *updateSettingsInput) (*roomOutput, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	if err := requireHostToken(in.HostToken); err != nil {
		return nil, err
	}
	patch := in.Body.SettingsPatch
	switch {
	case patch.Buzzer != nil:
		if err := patch.Buzzer.Validate(); err != nil {
			return nil, invalidBuzzer("body.buzzer", err)
		}
	case in.Body.BuzzerPreset != "":
		settings, ok := buzzer.Preset(in.Body.BuzzerPreset)
		if !ok {
			return nil, unknownPreset("body.buzzerPreset", in.Body.BuzzerPreset)
		}
		patch.Buzzer = &settings
	}
	info, err := mgr.UpdateSettings(in.Code, in.HostToken, patch)
	if err != nil {
		if errors.Is(err, room.ErrUnsupportedBuzzerMode) && in.Body.SettingsPatch.Buzzer == nil {
			return nil, huma.Error422UnprocessableEntity("buzzer preset not supported",
				&huma.ErrorDetail{Location: "body.buzzerPreset", Message: strings.TrimPrefix(err.Error(), "room: "), Value: "unsupportedBuzzerMode"})
		}
		return nil, s.failRoom(err)
	}
	return &roomOutput{Body: RoomView(info)}, nil
}

func (s *Server) kickPerson(_ context.Context, in *kickInput) (*struct{}, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	if err := requireHostToken(in.HostToken); err != nil {
		return nil, err
	}
	if err := mgr.Kick(in.Code, in.HostToken, in.Body.PersonID, in.Body.Ban); err != nil {
		return nil, s.failRoom(err)
	}
	return nil, nil
}

func (s *Server) unbanPerson(_ context.Context, in *personInput) (*struct{}, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	if err := requireHostToken(in.HostToken); err != nil {
		return nil, err
	}
	if err := mgr.Unban(in.Code, in.HostToken, in.Body.PersonID); err != nil {
		return nil, s.failRoom(err)
	}
	return nil, nil
}

func (s *Server) transferHost(_ context.Context, in *personInput) (*struct{}, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	if err := requireHostToken(in.HostToken); err != nil {
		return nil, err
	}
	if err := mgr.TransferHost(in.Code, in.HostToken, in.Body.PersonID); err != nil {
		return nil, s.failRoom(err)
	}
	return nil, nil
}

func (s *Server) closeRoom(_ context.Context, in *hostInput) (*struct{}, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	if err := requireHostToken(in.HostToken); err != nil {
		return nil, err
	}
	if err := mgr.Close(in.Code, in.HostToken); err != nil {
		return nil, s.failRoom(err)
	}
	return nil, nil
}

func (s *Server) getBuzzLog(_ context.Context, in *buzzLogInput) (*buzzLogOutput, error) {
	mgr, err := s.rooms()
	if err != nil {
		return nil, err
	}
	if err := requireHostToken(in.HostToken); err != nil {
		return nil, err
	}
	arms, err := mgr.BuzzLog(in.Code, in.HostToken, in.QuestionID)
	if err != nil {
		return nil, s.failRoom(err)
	}
	if arms == nil {
		arms = []room.ArmRecord{}
	}
	return &buzzLogOutput{Body: BuzzLog{Arms: arms}}, nil
}
