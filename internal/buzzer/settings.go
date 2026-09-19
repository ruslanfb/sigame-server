package buzzer

import (
	"errors"
	"fmt"
)

// Mode selects the buzzer algorithm for a room.
type Mode string

// Buzzer modes. Only ModeAnchoredHybrid is ping-fair; the others exist for
// SIGame compatibility or for rooms that do not want a race at all.
const (
	ModeAnchoredHybrid Mode = "anchoredHybrid" // scheduled light + anchored client reaction stamps (default)
	ModeServerArrival  Mode = "serverArrival"  // SIGame FirstWins: first PRESS to reach the server wins
	ModeRandomWindow   Mode = "randomWindow"   // SIGame RandomWithinInterval: random among presses in a window
	ModeClientReaction Mode = "clientReaction" // SIGame FirstWinsClient: trust the client-reported reaction (unsafe)
	ModeWrittenAll     Mode = "writtenAll"     // no race: every question is played as a written duel
)

// TieMode chooses how wide the tie window is.
type TieMode string

// Tie modes.
const (
	TieResolution  TieMode = "resolution"  // fixed TieMs window (timer-resolution ties only)
	TieUncertainty TieMode = "uncertainty" // window from the per-press errors: clamp(2·hypot(err_i,err_j)+2, TieMinMs, TieMaxMs)
)

// TieBreak chooses who wins a contested (tied) button.
type TieBreak string

// Tie-break rules.
const (
	TieBreakLikelihood       TieBreak = "likelihood"       // weighted lottery, w_i = Π Φ((t_j − t_i)/hypot(err_i,err_j))
	TieBreakMostLikely       TieBreak = "mostLikely"       // deterministic argmax of the likelihood weights
	TieBreakRandom           TieBreak = "random"           // uniform lottery
	TieBreakLowestScore      TieBreak = "lowestScore"      // the tied player with the lowest score
	TieBreakFewestButtonsWon TieBreak = "fewestButtonsWon" // the tied player who won the fewest buttons
	TieBreakRotate           TieBreak = "rotate"           // next seat after the previous button winner
	TieBreakAllPlay          TieBreak = "allPlay"          // no winner: the tied players answer in writing (signalled as Result.Kind "allPlay")
)

// NetProfile is the host's description of the room network; it seeds the
// collection cap and the tolerance cap.
type NetProfile string

// Network profiles.
const (
	NetLAN  NetProfile = "lan"
	NetWiFi NetProfile = "wifi"
	NetWAN  NetProfile = "wan"
)

// Settings are the host-configurable buzzer parameters of a room. Everything
// else (tol, u, lead, σ, err) is derived from measurements and only displayed.
type Settings struct {
	Mode                Mode       `json:"mode" enum:"anchoredHybrid,serverArrival,randomWindow,clientReaction,writtenAll" doc:"Buzzer algorithm: anchoredHybrid (fair, default), serverArrival, randomWindow, clientReaction (unsafe), writtenAll."`
	NetProfile          NetProfile `json:"netProfile" enum:"lan,wifi,wan" doc:"Room network profile: lan, wifi or wan. Chooses collection/tolerance caps."`
	TieMode             TieMode    `json:"tieMode" enum:"resolution,uncertainty" doc:"resolution: only presses within tieMs tie; uncertainty: presses within their combined measurement error tie."`
	TieBreak            TieBreak   `json:"tieBreak" enum:"likelihood,mostLikely,random,lowestScore,fewestButtonsWon,rotate,allPlay" doc:"Rule for contested buttons: likelihood, mostLikely, random, lowestScore, fewestButtonsWon, rotate, allPlay."`
	ArmJitterMinMs      int64      `json:"armJitterMinMs" doc:"Minimum random delay between the end of reading and the light (anti-anticipation)."`
	ArmJitterMaxMs      int64      `json:"armJitterMaxMs" doc:"Maximum random delay between the end of reading and the light."`
	PressWindowMs       int64      `json:"pressWindowMs" doc:"How long the button stays armed when nobody presses."`
	MaxCollectMs        int64      `json:"maxCollectMs" doc:"Hard cap on the fairness window after the first press reaches the server."`
	TieMs               int64      `json:"tieMs" doc:"Tie window in resolution mode (ms)."`
	TieMinMs            int64      `json:"tieMinMs" doc:"Lower bound of the tie window in uncertainty mode (ms)."`
	TieMaxMs            int64      `json:"tieMaxMs" doc:"Upper bound of the tie window in uncertainty mode (ms)."`
	TolCapMs            int64      `json:"tolCapMs" doc:"Cap on the trust radius for client stamps; also the maximum one-shot cheat gain (ms)."`
	LateLightCreditMs   int64      `json:"lateLightCreditMs" doc:"Maximum credit for a light that lit late because the ARM message was delayed (0 for tournaments)."`
	MinHumanMs          int64      `json:"minHumanMs" doc:"Reactions below this are rejected as superhuman (>= 100)."`
	FalseStartLockoutMs int64      `json:"falseStartLockoutMs" doc:"Lockout after a false start (ms)."`
	LockoutGrowth       float64    `json:"lockoutGrowth" doc:"Multiplier applied per repeated false start within 10 s."`
	LockoutCapMs        int64      `json:"lockoutCapMs" doc:"Maximum lockout (ms)."`
	MaxRttMs            int64      `json:"maxRttMs" doc:"Players with a reference RTT above this are armed on receipt and scored server-side (bounds non-browser RTT lies)."`
	AutoTrust           bool       `json:"autoTrust" doc:"Apply the trust ladder automatically (conservative/serverOnly/arrivalOnly). Off = flags only, host decides."`
	StrictTrust         bool       `json:"strictTrust" doc:"Tournament ladder thresholds (-1/-2/-3 instead of -2/-4/-6)."`
	ShowPing            bool       `json:"showPing" doc:"Show every player's ping, jitter and ±u to the whole room."`
	RandomWindowMs      int64      `json:"randomWindowMs" doc:"randomWindow mode: window after the first press within which presses enter the lottery."`
	AcceptWindowMs      int64      `json:"acceptWindowMs" doc:"clientReaction mode: window after the first press within which client reactions are compared."`
}

// Preset names.
const (
	PresetLANWired     = "lanWired"
	PresetWiFiParty    = "wifiParty"
	PresetInternetFair = "internetFair"
	PresetTournament   = "tournament"
	PresetNoRace       = "noRace"
)

// PresetNames lists the presets in display order.
var PresetNames = []string{PresetLANWired, PresetWiFiParty, PresetInternetFair, PresetTournament, PresetNoRace}

// Preset returns a named preset.
func Preset(name string) (Settings, bool) {
	base := Settings{
		Mode:                ModeAnchoredHybrid,
		NetProfile:          NetWiFi,
		TieMode:             TieUncertainty,
		TieBreak:            TieBreakLikelihood,
		ArmJitterMinMs:      200,
		ArmJitterMaxMs:      1200,
		PressWindowMs:       5000,
		MaxCollectMs:        400,
		TieMs:               2,
		TieMinMs:            3,
		TieMaxMs:            40,
		TolCapMs:            60,
		LateLightCreditMs:   20,
		MinHumanMs:          100,
		FalseStartLockoutMs: 3000,
		LockoutGrowth:       2,
		LockoutCapMs:        6000,
		MaxRttMs:            200,
		AutoTrust:           true,
		ShowPing:            true,
		RandomWindowMs:      300,
		AcceptWindowMs:      300,
	}
	switch name {
	case PresetWiFiParty:
		return base, true
	case PresetLANWired:
		s := base
		s.NetProfile = NetLAN
		s.TieMode = TieResolution
		s.TieBreak = TieBreakMostLikely
		s.ArmJitterMinMs, s.ArmJitterMaxMs = 300, 800
		s.MaxCollectMs = 200
		s.TieMinMs = 2
		s.TolCapMs = 40
		s.FalseStartLockoutMs = 1000
		s.MaxRttMs = 80
		return s, true
	case PresetInternetFair:
		s := base
		s.NetProfile = NetWAN
		s.MaxCollectMs = 600
		s.TieMaxMs = 60
		s.TolCapMs = 80
		s.MaxRttMs = 350
		return s, true
	case PresetTournament:
		s := base
		s.NetProfile = NetLAN
		s.TieMode = TieResolution
		s.TieBreak = TieBreakMostLikely
		s.ArmJitterMinMs, s.ArmJitterMaxMs = 300, 1200
		s.MaxCollectMs = 200
		s.TieMinMs = 2
		s.TolCapMs = 40
		s.LateLightCreditMs = 0
		s.FalseStartLockoutMs = 1000
		s.MaxRttMs = 80
		s.StrictTrust = true
		return s, true
	case PresetNoRace:
		s := base
		s.Mode = ModeWrittenAll
		s.TieBreak = TieBreakAllPlay
		return s, true
	}
	return Settings{}, false
}

// DefaultSettings returns the wifiParty preset.
func DefaultSettings() Settings {
	s, _ := Preset(PresetWiFiParty)
	return s
}

// ErrInvalidSettings is wrapped by every Validate error.
var ErrInvalidSettings = errors.New("invalid buzzer settings")

func bad(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidSettings, fmt.Sprintf(format, args...))
}

// Validate checks ranges and forbidden combinations.
func (s Settings) Validate() error {
	switch s.Mode {
	case ModeAnchoredHybrid, ModeServerArrival, ModeRandomWindow, ModeClientReaction, ModeWrittenAll:
	default:
		return bad("unknown mode %q", s.Mode)
	}
	switch s.NetProfile {
	case NetLAN, NetWiFi, NetWAN:
	default:
		return bad("unknown netProfile %q", s.NetProfile)
	}
	switch s.TieMode {
	case TieResolution, TieUncertainty:
	default:
		return bad("unknown tieMode %q", s.TieMode)
	}
	switch s.TieBreak {
	case TieBreakLikelihood, TieBreakMostLikely, TieBreakRandom, TieBreakLowestScore, TieBreakFewestButtonsWon, TieBreakRotate, TieBreakAllPlay:
	default:
		return bad("unknown tieBreak %q", s.TieBreak)
	}
	if s.ArmJitterMinMs < 0 || s.ArmJitterMaxMs < s.ArmJitterMinMs || s.ArmJitterMaxMs > 10000 {
		return bad("armJitter must satisfy 0 <= min <= max <= 10000, got %d..%d", s.ArmJitterMinMs, s.ArmJitterMaxMs)
	}
	if s.PressWindowMs < 500 || s.PressWindowMs > 60000 {
		return bad("pressWindowMs must be in [500, 60000], got %d", s.PressWindowMs)
	}
	if s.MaxCollectMs < 50 || s.MaxCollectMs > 2000 {
		return bad("maxCollectMs must be in [50, 2000], got %d", s.MaxCollectMs)
	}
	if s.TieMs < 0 || s.TieMs > 1000 {
		return bad("tieMs must be in [0, 1000], got %d", s.TieMs)
	}
	if s.TieMinMs < 0 || s.TieMaxMs < s.TieMinMs || s.TieMaxMs > 1000 {
		return bad("tie window must satisfy 0 <= tieMinMs <= tieMaxMs <= 1000, got %d..%d", s.TieMinMs, s.TieMaxMs)
	}
	if s.TolCapMs < TolMinMs || s.TolCapMs > 200 {
		return bad("tolCapMs must be in [%d, 200], got %d", int(TolMinMs), s.TolCapMs)
	}
	if s.LateLightCreditMs < 0 || s.LateLightCreditMs > 200 {
		return bad("lateLightCreditMs must be in [0, 200], got %d", s.LateLightCreditMs)
	}
	if s.Mode == ModeAnchoredHybrid && (s.MinHumanMs < 100 || s.MinHumanMs > 300) {
		return bad("minHumanMs must be in [100, 300] (safety invariant), got %d", s.MinHumanMs)
	}
	if s.FalseStartLockoutMs < 0 || s.FalseStartLockoutMs > 60000 {
		return bad("falseStartLockoutMs must be in [0, 60000], got %d", s.FalseStartLockoutMs)
	}
	if s.LockoutGrowth < 1 || s.LockoutGrowth > 10 {
		return bad("lockoutGrowth must be in [1, 10], got %g", s.LockoutGrowth)
	}
	if s.LockoutCapMs < s.FalseStartLockoutMs || s.LockoutCapMs > 120000 {
		return bad("lockoutCapMs must be in [falseStartLockoutMs, 120000], got %d", s.LockoutCapMs)
	}
	if s.MaxRttMs < 20 || s.MaxRttMs > 5000 {
		return bad("maxRttMs must be in [20, 5000], got %d", s.MaxRttMs)
	}
	if s.Mode == ModeRandomWindow && (s.RandomWindowMs < 50 || s.RandomWindowMs > 2000) {
		return bad("randomWindowMs must be in [50, 2000], got %d", s.RandomWindowMs)
	}
	if s.Mode == ModeClientReaction && (s.AcceptWindowMs < 50 || s.AcceptWindowMs > 2000) {
		return bad("acceptWindowMs must be in [50, 2000], got %d", s.AcceptWindowMs)
	}
	return nil
}

// DefaultMaxCollectMs returns the collection cap the syntheses recommend per profile.
func DefaultMaxCollectMs(p NetProfile) int64 {
	switch p {
	case NetLAN:
		return 200
	case NetWAN:
		return 600
	default:
		return 400
	}
}

// DefaultTolCapForProfile returns the tolerance cap the syntheses recommend per profile.
func DefaultTolCapForProfile(p NetProfile) int64 {
	switch p {
	case NetLAN:
		return 40
	case NetWAN:
		return 80
	default:
		return 60
	}
}
