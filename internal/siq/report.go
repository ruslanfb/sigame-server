package siq

import "errors"

// Sentinel errors returned by Import/Export.
var (
	ErrBadZip          = errors.New("siq: not a zip archive")
	ErrNoContent       = errors.New("siq: content.xml not found")
	ErrBadXML          = errors.New("siq: malformed content.xml")
	ErrUnsafeEntry     = errors.New("siq: unsafe zip entry name")
	ErrTooManyEntries  = errors.New("siq: too many zip entries")
	ErrTooLarge        = errors.New("siq: archive too large")
	ErrSuspiciousRatio = errors.New("siq: suspicious compression ratio")
	ErrMediaNotFound   = errors.New("siq: referenced media not found in store")
)

// Entry levels.
const (
	LevelError   = "error"
	LevelWarning = "warning"
	LevelInfo    = "info"
)

// Report entry codes.
const (
	CodeMediaMissing        = "mediaMissing"        // referenced file is not in the archive; the item was dropped
	CodeMediaTooLarge       = "mediaTooLarge"       // store rejected the file (media.ErrTooLarge); the item was dropped
	CodeMediaUnsupported    = "mediaUnsupported"    // store rejected the content type/kind; the item was dropped
	CodeUnknownQuestionType = "unknownQuestionType" // custom type name kept verbatim; played manually
	CodeUnknownParam        = "unknownParam"        // unknown <param> preserved in Question.Extra
	CodeCustomScript        = "customScript"        // <script> preserved verbatim, not executed by the engine
	CodeV4Upgraded          = "v4Upgraded"          // legacy package upgraded to v5 semantics
	CodeHTMLContent         = "htmlContent"         // html item present (clients must allow html content)
	CodeExternalURL         = "externalUrl"         // item references an external URL
	CodeTruncated           = "truncated"           // a value exceeded a limit and was truncated
	CodeInvalidNumberSet    = "invalidNumberSet"    // price/cost could not be parsed; nominal price used
	CodeEmptyRight          = "emptyRight"          // playable question without a right answer
	CodeExtraFolderEntry    = "extraFolderEntry"    // media found outside its category folder, or an ignored entry
	CodeInvalidPrice        = "invalidPrice"        // unparsable price attribute; 0 used
	CodeUnknownRoundType    = "unknownRoundType"    // unknown round type; standard assumed
	CodeUnsupportedVersion  = "unsupportedVersion"  // package version newer than 5; parsed as v5
)

// Report is the compatibility report produced by Import. It is returned to
// API clients as JSON.
type Report struct {
	Version int     `json:"version" doc:"Detected content.xml format version (3, 4 or 5)"`
	Entries []Entry `json:"entries" doc:"Problems and notes found during import"`
	Stats   Stats   `json:"stats"`
}

// Entry is one report line.
type Entry struct {
	Level   string `json:"level" enum:"error,warning,info" doc:"error = data was dropped or changed; warning = worth a look; info = note"`
	Code    string `json:"code" doc:"Machine-readable code, e.g. mediaMissing"`
	Path    string `json:"path,omitempty" doc:"JSON-pointer-like location, e.g. /rounds/0/themes/1/questions/2"`
	Message string `json:"message"`
}

// Stats summarises what was imported.
type Stats struct {
	Rounds        int            `json:"rounds"`
	Themes        int            `json:"themes"`
	Questions     int            `json:"questions"`
	ByType        map[string]int `json:"byType" doc:"Question count per effective type"`
	MediaImported int            `json:"mediaImported" doc:"Distinct media files stored"`
	MediaMissing  int            `json:"mediaMissing" doc:"Media references that could not be resolved or stored"`
	Bytes         int64          `json:"bytes" doc:"Total size of the stored media files"`
}

func newReport() *Report {
	return &Report{Entries: []Entry{}, Stats: Stats{ByType: map[string]int{}}}
}

func (r *Report) add(level, code, path, msg string) {
	r.Entries = append(r.Entries, Entry{Level: level, Code: code, Path: path, Message: msg})
}

func (r *Report) errorf(code, path, msg string) { r.add(LevelError, code, path, msg) }
func (r *Report) warnf(code, path, msg string)  { r.add(LevelWarning, code, path, msg) }
func (r *Report) infof(code, path, msg string)  { r.add(LevelInfo, code, path, msg) }

// HasErrors reports whether any error-level entry was recorded.
func (r *Report) HasErrors() bool {
	for _, e := range r.Entries {
		if e.Level == LevelError {
			return true
		}
	}
	return false
}

// Count returns the number of entries with the given code.
func (r *Report) Count(code string) int {
	n := 0
	for _, e := range r.Entries {
		if e.Code == code {
			n++
		}
	}
	return n
}
