package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/buzzer"
)

// Buzzer presets (tag buzzer): the named configurations of
// internal/buzzer/settings.go with a human title and a one-line hint so
// that a client can offer them in a picker. A room is created from a preset
// (createRoom.buzzerPreset, default wifiParty) or from explicit settings.

// BuzzerPreset is one named buzzer configuration.
type BuzzerPreset struct {
	Name        string          `json:"name" enum:"lanWired,wifiParty,internetFair,tournament,noRace" doc:"Preset name; pass it as buzzerPreset to POST /rooms or PATCH /rooms/{code}/settings"`
	Title       string          `json:"title" doc:"Short human title, Russian / English"`
	Description string          `json:"description" doc:"When to use the preset (English one-liner)"`
	Supported   bool            `json:"supported" doc:"False when this server version cannot play the preset (noRace needs the written duel, which is not implemented yet): creating a room with it answers 422"`
	Settings    buzzer.Settings `json:"settings" doc:"The full settings the preset expands to; copy and adjust them for the buzzer field of POST /rooms"`
}

// BuzzerPresetList is the answer of GET /buzzer-presets.
type BuzzerPresetList struct {
	Default string         `json:"default" enum:"lanWired,wifiParty,internetFair,tournament,noRace" doc:"Preset applied when POST /rooms gets neither buzzerPreset nor buzzer"`
	Presets []BuzzerPreset `json:"presets" doc:"Every preset in display order"`
}

// presetText is the human-facing part of a preset that the buzzer package
// does not carry.
type presetText struct {
	title, description string
}

var presetTexts = map[string]presetText{
	buzzer.PresetLANWired: {
		title:       "Проводная сеть / Wired LAN",
		description: "Everyone is on the same wired LAN: tight 2 ms resolution ties, short lockouts, most-likely tie-break.",
	},
	buzzer.PresetWiFiParty: {
		title:       "Wi-Fi вечеринка / Wi-Fi party",
		description: "Default for a living-room game over Wi-Fi: uncertainty-based ties, likelihood lottery, ping shown to everyone.",
	},
	buzzer.PresetInternetFair: {
		title:       "Через интернет / Fair internet",
		description: "Use when someone joins over the internet: wider collection window and tolerance so remote players are not disadvantaged.",
	},
	buzzer.PresetTournament: {
		title:       "Турнир / Tournament",
		description: "Competitive play on a wired LAN: strict trust ladder, no late-light credit, deterministic most-likely tie-break.",
	},
	buzzer.PresetNoRace: {
		title:       "Без гонки / No race",
		description: "No button race at all: every question is answered in writing by everyone (written duel; not playable in this server version).",
	},
}

// buzzerSupported mirrors room.checkBuzzer: the written duel (writtenAll
// mode, allPlay tie-break) is not implemented in this version.
func buzzerSupported(s buzzer.Settings) bool {
	return s.Mode != buzzer.ModeWrittenAll && s.TieBreak != buzzer.TieBreakAllPlay
}

// presetView returns the DTO of a named preset.
func presetView(name string) (BuzzerPreset, bool) {
	settings, ok := buzzer.Preset(name)
	if !ok {
		return BuzzerPreset{}, false
	}
	text := presetTexts[name]
	return BuzzerPreset{
		Name:        name,
		Title:       text.title,
		Description: text.description,
		Supported:   buzzerSupported(settings),
		Settings:    settings,
	}, true
}

type presetNameInput struct {
	Name string `path:"name" doc:"Preset name: lanWired, wifiParty, internetFair, tournament or noRace"`
}

type presetListOutput struct {
	Body BuzzerPresetList
}

type presetOutput struct {
	Body BuzzerPreset
}

func (s *Server) registerPresets() {
	huma.Register(s.API, huma.Operation{
		OperationID: "listBuzzerPresets",
		Method:      http.MethodGet,
		Path:        basePath + "/buzzer-presets",
		Tags:        []string{tagBuzzer},
		Summary:     "List buzzer presets",
		Description: "Returns the five named buzzer configurations (lanWired, wifiParty, internetFair, tournament, noRace) with a title, a one-line hint and the full settings each expands to, plus the name of the default preset. Use a name as buzzerPreset when creating a room, or start from its settings to build explicit buzzer settings. noRace is listed for completeness but is marked unsupported until the written duel is implemented.",
	}, s.listBuzzerPresets)

	huma.Register(s.API, huma.Operation{
		OperationID: "getBuzzerPreset",
		Method:      http.MethodGet,
		Path:        basePath + "/buzzer-presets/{name}",
		Tags:        []string{tagBuzzer},
		Summary:     "Get a buzzer preset",
		Description: "Returns one named buzzer preset with its title, hint, support flag and full settings; 404 for an unknown name.",
		Errors:      []int{http.StatusNotFound},
	}, s.getBuzzerPreset)
}

func (s *Server) listBuzzerPresets(_ context.Context, _ *struct{}) (*presetListOutput, error) {
	presets := make([]BuzzerPreset, 0, len(buzzer.PresetNames))
	for _, name := range buzzer.PresetNames {
		if v, ok := presetView(name); ok {
			presets = append(presets, v)
		}
	}
	return &presetListOutput{Body: BuzzerPresetList{Default: buzzer.PresetWiFiParty, Presets: presets}}, nil
}

func (s *Server) getBuzzerPreset(_ context.Context, in *presetNameInput) (*presetOutput, error) {
	v, ok := presetView(in.Name)
	if !ok {
		return nil, huma.Error404NotFound("buzzer preset not found")
	}
	return &presetOutput{Body: v}, nil
}
