// Package packs defines the question-pack domain model used by every other
// package (engine, siq codec, HTTP API, room). The model is a strict superset
// of SIQ v5 semantics so that .siq files round-trip without loss.
//
// Conventions (see docs/design/00-constitution.md):
//   - all IDs are UUIDv7 strings; media IDs are lowercase hex sha256 of the file content;
//   - all durations are milliseconds (int64); wall-clock timestamps are Unix ms UTC;
//   - JSON field names are camelCase; zero values are omitted where a zero value is
//     semantically "unset".
package packs

// RoundType distinguishes table rounds from the theme-list (final) round.
type RoundType string

const (
	RoundStandard RoundType = "standard" // classic table: themes × questions with prices ("standart" in SIQ)
	RoundFinal    RoundType = "final"    // theme list; players delete themes, the last one is played with hidden stakes
)

// QuestionType is the well-known play script of a question. Any other non-empty
// value is a custom type that the host plays manually.
type QuestionType string

const (
	QSimple            QuestionType = "simple"            // buzzer question ("withButton")
	QStake             QuestionType = "stake"             // auction: highest bidder answers alone (legacy "auction")
	QStakeAll          QuestionType = "stakeAll"          // everyone makes a hidden stake and answers in writing (final-round default)
	QSecret            QuestionType = "secret"            // "кот в мешке": must be handed over; theme/price revealed after transfer (legacy "cat"/"bagcat")
	QSecretPublicPrice QuestionType = "secretPublicPrice" // theme and price announced before the transfer
	QSecretNoQuestion  QuestionType = "secretNoQuestion"  // no question: the recipient simply receives the price
	QNoRisk            QuestionType = "noRisk"            // only the chooser answers; price ×2, no penalty (legacy "sponsored", "forYourself")
	QForAll            QuestionType = "forAll"            // everyone answers in writing for ± nominal price
	QCustom            QuestionType = "custom"            // played manually by the showman
)

// KnownQuestionTypes lists the types the engine can play automatically.
var KnownQuestionTypes = []QuestionType{QSimple, QStake, QStakeAll, QSecret, QSecretPublicPrice, QSecretNoQuestion, QNoRisk, QForAll}

// IsKnown reports whether the engine has a built-in script for the type.
func (t QuestionType) IsKnown() bool {
	for _, k := range KnownQuestionTypes {
		if k == t {
			return true
		}
	}
	return false
}

// SelectionMode says whether the chooser may keep a secret question.
type SelectionMode string

const (
	SelectAny           SelectionMode = "any"           // may give it to anyone including themselves
	SelectExceptCurrent SelectionMode = "exceptCurrent" // must give it to another player
)

// AnswerType controls how an answer is entered and validated.
type AnswerType string

const (
	AnswerText   AnswerType = "text"   // free text, validated by showman/AI/fuzzy (default)
	AnswerSelect AnswerType = "select" // one of AnswerOptions; Right holds the option label
	AnswerNumber AnswerType = "number" // integer within ±AnswerDeviation of Right[0]
	AnswerPoint  AnswerType = "point"  // a point on the last image; Right[0] = "x,y,ratio"
	AnswerClient AnswerType = "client" // the client decides right/wrong itself
)

// ContentType is the kind of a content item.
type ContentType string

const (
	ContentText  ContentType = "text"
	ContentImage ContentType = "image"
	ContentAudio ContentType = "audio"
	ContentVideo ContentType = "video"
	ContentHTML  ContentType = "html"
)

// Placement says where a content item is presented.
type Placement string

const (
	PlaceScreen     Placement = "screen"     // main screen (text, image, video, html)
	PlaceReplic     Placement = "replic"     // spoken by the showman (text only)
	PlaceBackground Placement = "background" // invisible (audio only)
)

// Pack is a complete question package.
type Pack struct {
	ID          string   `json:"id" doc:"UUIDv7"`
	Version     int      `json:"version" doc:"Optimistic-concurrency version; incremented on every update"`
	Name        string   `json:"name" minLength:"1" maxLength:"350"`
	Language    string   `json:"language,omitempty" doc:"BCP-47 tag, e.g. ru-RU" maxLength:"16"`
	Difficulty  int      `json:"difficulty" minimum:"0" maximum:"10"`
	Restriction string   `json:"restriction,omitempty" doc:"Age restriction, e.g. 18+" maxLength:"16"`
	Date        string   `json:"date,omitempty" doc:"Free-form creation date as in SIQ" maxLength:"64"`
	Publisher   string   `json:"publisher,omitempty" maxLength:"350"`
	ContactURI  string   `json:"contactUri,omitempty" maxLength:"1500"`
	LogoMediaID string   `json:"logoMediaId,omitempty" doc:"Media ID of the logo image"`
	LogoURL     string   `json:"logoUrl,omitempty" doc:"External logo URL (alternative to logoMediaId)"`
	Tags        []string `json:"tags" maxItems:"10"`
	Info        Info     `json:"info"`
	Rounds      []Round  `json:"rounds" maxItems:"50"`
	Extra       *Extra   `json:"extra,omitempty" doc:"Opaque data preserved for .siq round-trip"`
	SIQID       string   `json:"siqId,omitempty" doc:"Original SIQ package id attribute, if imported"`
	CreatedAt   int64    `json:"createdAt" doc:"Unix ms UTC"`
	UpdatedAt   int64    `json:"updatedAt" doc:"Unix ms UTC"`
}

// Info holds authorship metadata; present at every level of the hierarchy and
// inherited downward (a round without authors has the pack's authors).
type Info struct {
	Authors         []string `json:"authors,omitempty" maxItems:"10"`
	Sources         []string `json:"sources,omitempty" maxItems:"10"`
	Comments        string   `json:"comments,omitempty" maxLength:"1500" doc:"Shown to everyone (e.g. at the end of a question)"`
	ShowmanComments string   `json:"showmanComments,omitempty" maxLength:"1500" doc:"Shown only to the showman"`
}

// Round is a table round or the final theme-list round.
type Round struct {
	ID     string    `json:"id" doc:"UUIDv7"`
	Name   string    `json:"name" maxLength:"350"`
	Type   RoundType `json:"type" enum:"standard,final"`
	Info   Info      `json:"info"`
	Themes []Theme   `json:"themes" maxItems:"30"`
}

// Theme is a category inside a round.
type Theme struct {
	ID        string     `json:"id" doc:"UUIDv7"`
	Name      string     `json:"name" maxLength:"350"`
	Info      Info       `json:"info"`
	Questions []Question `json:"questions" maxItems:"30"`
}

// Question is a single playable cell. Price -1 marks an empty slot.
type Question struct {
	ID     string         `json:"id" doc:"UUIDv7"`
	Price  int            `json:"price" doc:"Nominal price; -1 = empty slot; 0 is typical for final-round questions"`
	Type   QuestionType   `json:"type" doc:"simple|stake|stakeAll|secret|secretPublicPrice|secretNoQuestion|noRisk|forAll|custom or any custom name"`
	Params QuestionParams `json:"params"`
	Right  []string       `json:"right" doc:"Accepted answers (text) / option label (select) / number (number) / x,y,ratio (point)"`
	Wrong  []string       `json:"wrong,omitempty" doc:"Known wrong answers (help the AI/fuzzy judge reject them)"`
	Info   Info           `json:"info"`
	Script []ScriptStep   `json:"script,omitempty" doc:"Custom SIQ play script; preserved verbatim, not executed by the engine"`
	Extra  []Param        `json:"extraParams,omitempty" doc:"Unknown SIQ params preserved verbatim"`
}

// QuestionParams are the well-known SIQ v5 question parameters.
type QuestionParams struct {
	Question         []ContentItem  `json:"question" doc:"Question body, played in order"`
	Answer           []ContentItem  `json:"answer,omitempty" doc:"Optional media/text shown as the answer; falls back to Right[0]"`
	Theme            string         `json:"theme,omitempty" doc:"secret*: the question's own theme"`
	Price            *NumberSet     `json:"price,omitempty" doc:"secret*: price the recipient can choose from"`
	SelectionMode    SelectionMode  `json:"selectionMode,omitempty" enum:",any,exceptCurrent" doc:"secret*: may the chooser keep it"`
	AnswerType       AnswerType     `json:"answerType,omitempty" enum:",text,select,number,point,client" doc:"Empty = text"`
	AnswerOptions    []AnswerOption `json:"answerOptions,omitempty" doc:"select: labelled options"`
	AnswerDeviation  float64        `json:"answerDeviation,omitempty" doc:"number: ± tolerance; point: distance tolerance"`
	AnswerDurationMs int64          `json:"answerDurationMs,omitempty" doc:"Overrides the answering timer for this question"`
}

// AnswerOption is one choice of a select-type question. Labels are A, B, C, ...
type AnswerOption struct {
	Label   string        `json:"label" minLength:"1" maxLength:"8"`
	Content []ContentItem `json:"content"`
}

// NumberSet is a price choice. Min==Max==N: fixed N. Min==Max==0: the recipient
// picks the round's minimum or maximum price. Otherwise values are
// Min, Min+Step, ... ≤ Max; Step==0 or Step==Max-Min means only the two extremes.
type NumberSet struct {
	Min  int `json:"min" minimum:"0"`
	Max  int `json:"max" minimum:"0"`
	Step int `json:"step" minimum:"0"`
}

// ContentItem is one fragment of question/answer content.
// Exactly one of Text, MediaID or URL is set, depending on Type.
type ContentItem struct {
	Type       ContentType `json:"type" enum:"text,image,audio,video,html"`
	Text       string      `json:"text,omitempty" maxLength:"1500" doc:"For type=text"`
	MediaID    string      `json:"mediaId,omitempty" doc:"Media stored on this server (sha256 hex)"`
	URL        string      `json:"url,omitempty" maxLength:"1500" doc:"External media URL"`
	Placement  Placement   `json:"placement,omitempty" enum:",screen,replic,background" doc:"Empty = screen (audio: background)"`
	DurationMs int64       `json:"durationMs,omitempty" doc:"How long the item stays; 0 = automatic"`
	NoWait     bool        `json:"noWait,omitempty" doc:"Play together with the next item (SIQ waitForFinish=False)"`
}

// EffectivePlacement returns the placement with SIQ defaults applied.
func (c ContentItem) EffectivePlacement() Placement {
	if c.Placement != "" {
		return c.Placement
	}
	if c.Type == ContentAudio {
		return PlaceBackground
	}
	return PlaceScreen
}

// IsMedia reports whether the item references a file or URL rather than inline text.
func (c ContentItem) IsMedia() bool { return c.Type != ContentText }

// Param is a generic SIQ parameter preserved for round-trip of unknown data and
// custom scripts. Exactly one of Value, Items, Params, NumberSet is meaningful
// according to Type.
type Param struct {
	Name      string        `json:"name"`
	Type      string        `json:"type,omitempty" doc:"simple|content|group|numberSet; empty = simple"`
	IsRef     bool          `json:"isRef,omitempty"`
	Value     string        `json:"value,omitempty"`
	Items     []ContentItem `json:"items,omitempty"`
	Params    []Param       `json:"params,omitempty"`
	NumberSet *NumberSet    `json:"numberSet,omitempty"`
}

// ScriptStep is one step of a custom SIQ question script (preserved, not executed).
type ScriptStep struct {
	Type   string  `json:"type"`
	Params []Param `json:"params"`
}

// Extra carries SIQ data that has no first-class representation here.
type Extra struct {
	GlobalAuthors []AuthorRecord `json:"globalAuthors,omitempty"`
	GlobalSources []SourceRecord `json:"globalSources,omitempty"`
}

// AuthorRecord mirrors SIQ <global><Authors><Author>.
type AuthorRecord struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	SecondName string `json:"secondName,omitempty"`
	Surname    string `json:"surname,omitempty"`
	Country    string `json:"country,omitempty"`
	City       string `json:"city,omitempty"`
}

// SourceRecord mirrors SIQ <global><Sources><Source>.
type SourceRecord struct {
	ID      string `json:"id"`
	Author  string `json:"author,omitempty"`
	Title   string `json:"title,omitempty"`
	Year    string `json:"year,omitempty"`
	Publish string `json:"publish,omitempty"`
	City    string `json:"city,omitempty"`
}

// Limits mirror SIGame's PackageLimits and are enforced by Validate.
const (
	MaxRounds            = 50
	MaxThemesPerRound    = 30
	MaxQuestionsPerTheme = 30
	MaxItemsPerParam     = 30
	MaxScriptSteps       = 30
	MaxTextLen           = 350
	MaxContentValueLen   = 1500
	MaxListItems         = 10 // tags, authors, sources
	MaxAnswers           = 30
)

// EffectiveType normalizes an empty type to simple.
func (q Question) EffectiveType() QuestionType {
	if q.Type == "" {
		return QSimple
	}
	return q.Type
}

// IsEmpty reports whether the question is an empty placeholder slot.
func (q Question) IsEmpty() bool { return q.Price < 0 }

// MediaIDs returns every media ID referenced by the pack (logo, question and
// answer content, select options, script/extra params), without duplicates.
func (p *Pack) MediaIDs() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(id string) {
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	var walkItems func(items []ContentItem)
	walkItems = func(items []ContentItem) {
		for _, it := range items {
			add(it.MediaID)
		}
	}
	var walkParams func(params []Param)
	walkParams = func(params []Param) {
		for _, pr := range params {
			walkItems(pr.Items)
			walkParams(pr.Params)
		}
	}
	add(p.LogoMediaID)
	for _, r := range p.Rounds {
		for _, t := range r.Themes {
			for _, q := range t.Questions {
				walkItems(q.Params.Question)
				walkItems(q.Params.Answer)
				for _, o := range q.Params.AnswerOptions {
					walkItems(o.Content)
				}
				walkParams(q.Extra)
				for _, s := range q.Script {
					walkParams(s.Params)
				}
			}
		}
	}
	return out
}
