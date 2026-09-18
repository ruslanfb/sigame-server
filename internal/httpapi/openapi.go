package httpapi

import (
	"net/http"
	"reflect"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"sigame/internal/media"
)

// The streaming routes (import, export, upload, media serving) are plain chi
// handlers; this file adds their OpenAPI operations by hand so that /docs
// and generated clients still see them.

const (
	mimeJSON    = "application/json"
	mimeProblem = "application/problem+json"
	mimeForm    = "multipart/form-data"
)

func schemaRef(reg huma.Registry, v any, hint string) *huma.Schema {
	return reg.Schema(reflect.TypeOf(v), true, hint)
}

func (s *Server) documentRawOperations() {
	oapi := s.API.OpenAPI()
	reg := oapi.Components.Schemas

	errRef := schemaRef(reg, huma.ErrorModel{}, "ErrorModel")
	problem := func(desc string) *huma.Response {
		return &huma.Response{Description: desc, Content: map[string]*huma.MediaType{mimeProblem: {Schema: errRef}}}
	}
	jsonResp := func(desc string, ref *huma.Schema, headers map[string]*huma.Param) *huma.Response {
		return &huma.Response{Description: desc, Headers: headers, Content: map[string]*huma.MediaType{mimeJSON: {Schema: ref}}}
	}
	fileBody := func(desc string) *huma.RequestBody {
		return &huma.RequestBody{
			Description: desc,
			Required:    true,
			Content: map[string]*huma.MediaType{mimeForm: {Schema: &huma.Schema{
				Type:     "object",
				Required: []string{"file"},
				Properties: map[string]*huma.Schema{
					"file": {Type: "string", Format: "binary", Description: "The file to upload"},
				},
			}}},
		}
	}
	strHeader := func(desc string) *huma.Param {
		return &huma.Param{Description: desc, Schema: &huma.Schema{Type: "string"}}
	}
	pathID := func(desc string) *huma.Param {
		return &huma.Param{Name: "id", In: "path", Required: true, Description: desc, Schema: &huma.Schema{Type: "string"}}
	}

	// POST /api/v1/packs/import
	importRef := schemaRef(reg, ImportResult{}, "ImportResult")
	oapi.AddOperation(&huma.Operation{
		OperationID: "importSiq",
		Method:      http.MethodPost,
		Path:        basePath + "/packs/import",
		Tags:        []string{tagPacks},
		Summary:     "Import a .siq package",
		Description: "Uploads a SIGame .siq archive (v3/v4/v5) as multipart/form-data with the file in the `file` field, converts it to the pack model, stores the referenced media and saves the pack. " +
			"Media files that cannot be stored (missing in the archive, too large, unsupported type) are dropped and listed in the report; the pack is still saved. " +
			"Where the pack validator is stricter than real-world .siq files (a numberSet whose step does not divide max-min, a question without any content) the pack is repaired mechanically and each change is listed in the report with code `autoFixed`; problems that cannot be repaired yield 422 and nothing is saved. " +
			"With dryRun=true nothing is written: the raw converted pack (fresh unsaved ids, version 0, empty mediaId fields, not validated) and the report are returned with 200. " +
			"The whole request body is limited by SIGAME_MAX_SIQ_MB (413 when exceeded).",
		Parameters: []*huma.Param{{
			Name: "dryRun", In: "query", Description: "Only convert and report; do not store media or save the pack",
			Schema: &huma.Schema{Type: "boolean", Default: false},
		}},
		RequestBody: fileBody("multipart/form-data with the .siq archive in the `file` field"),
		Responses: map[string]*huma.Response{
			"201": jsonResp("Pack imported and saved", importRef, map[string]*huma.Param{
				"Location": strHeader("URL of the created pack"),
				"ETag":     strHeader("Quoted pack version"),
			}),
			"200": jsonResp("Dry run: converted pack and report, nothing saved", importRef, nil),
			"400": problem("Not a zip archive, unsafe entry names or a malformed multipart body"),
			"413": problem("Archive or request body too large, too many entries or suspicious compression ratio"),
			"415": problem("Request is not multipart/form-data"),
			"422": problem("No content.xml, malformed content.xml, missing file field, or the converted pack failed validation"),
			"500": problem("Unexpected error"),
		},
	})

	// GET /api/v1/packs/{id}/export
	oapi.AddOperation(&huma.Operation{
		OperationID: "exportSiq",
		Method:      http.MethodGet,
		Path:        basePath + "/packs/{id}/export",
		Tags:        []string{tagPacks},
		Summary:     "Export a pack as .siq",
		Description: "Streams the pack as a SIQ version 5 archive (content.xml plus every referenced media file) with Content-Disposition: attachment and the pack name as file name.",
		Parameters:  []*huma.Param{pathID("Pack ID (UUIDv7)")},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The .siq archive",
				Headers:     map[string]*huma.Param{"Content-Disposition": strHeader("attachment; filename=\"<name>.siq\"; filename*=UTF-8''<name>.siq")},
				Content:     map[string]*huma.MediaType{"application/zip": {Schema: &huma.Schema{Type: "string", Format: "binary"}}},
			},
			"404": problem("Pack not found"),
			"500": problem("A referenced media object is missing from the store or the export failed"),
		},
	})

	// POST /api/v1/media
	metaRef := schemaRef(reg, media.Meta{}, "Meta")
	oapi.AddOperation(&huma.Operation{
		OperationID: "uploadMedia",
		Method:      http.MethodPost,
		Path:        basePath + "/media",
		Tags:        []string{tagMedia},
		Summary:     "Upload a media file",
		Description: "Stores a file (multipart/form-data, field `file`) content-addressed by sha256. The type is sniffed from the content, not from the file name or declared MIME type; " +
			"accepted: png, jpeg, gif, webp, mp3, ogg, m4a, wav, mp4, webm, html (svg only when SIGAME_ALLOW_SVG is set). Per-kind size limits come from SIGAME_MAX_*_MB. " +
			"Identical content is stored once: uploading it again answers 200 with the existing metadata instead of 201. The bytes are then served by GET /media/{id}; " +
			"reference the object from a pack via mediaId.",
		Parameters: []*huma.Param{{
			Name: "kind", In: "query", Description: "Reject the upload unless the sniffed kind matches",
			Schema: &huma.Schema{Type: "string", Enum: []any{"image", "audio", "video", "html"}},
		}},
		RequestBody: fileBody("multipart/form-data with the media file in the `file` field"),
		Responses: map[string]*huma.Response{
			"201": jsonResp("Stored a new object", metaRef, map[string]*huma.Param{"Location": strHeader("URL of the metadata (/api/v1/media/{id})")}),
			"200": jsonResp("Identical content was already stored; its metadata is returned", metaRef, map[string]*huma.Param{"Location": strHeader("URL of the metadata (/api/v1/media/{id})")}),
			"400": problem("Malformed multipart body"),
			"413": problem("File exceeds the limit for its kind or the request body cap"),
			"415": problem("Unsupported content type, svg disabled, or the content does not match `kind`; also when the request is not multipart/form-data"),
			"422": problem("Missing file field or invalid `kind`"),
			"500": problem("Unexpected error"),
		},
	})

	// GET /media/{id} (outside /api/v1)
	binary := &huma.MediaType{Schema: &huma.Schema{Type: "string", Format: "binary"}}
	oapi.AddOperation(&huma.Operation{
		OperationID: "streamMedia",
		Method:      http.MethodGet,
		Path:        "/media/{id}",
		Tags:        []string{tagMedia},
		Summary:     "Stream a media file",
		Description: "Serves the bytes of a stored media object with its sniffed Content-Type. Supports HTTP Range requests (206 Partial Content, needed for audio/video seeking), " +
			"conditional requests via ETag/If-None-Match (304) and HEAD. Responses are immutable: Cache-Control: public, max-age=31536000, immutable, ETag = quoted media id. " +
			"HTML objects are served with a Content-Security-Policy sandbox; add ?download=1 to force Content-Disposition: attachment.",
		Parameters: []*huma.Param{
			pathID("Media ID (sha256 hex)"),
			{Name: "download", In: "query", Description: "Any value except 0/false forces a download", Schema: &huma.Schema{Type: "string"}},
			{Name: "Range", In: "header", Description: "Byte range, e.g. bytes=0-1023", Schema: &huma.Schema{Type: "string"}},
			{Name: "If-None-Match", In: "header", Description: "Quoted media id; answers 304 when it matches", Schema: &huma.Schema{Type: "string"}},
		},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The whole object",
				Headers: map[string]*huma.Param{
					"Content-Type":        strHeader("Sniffed MIME type"),
					"ETag":                strHeader("Quoted media id"),
					"Cache-Control":       strHeader("public, max-age=31536000, immutable"),
					"Accept-Ranges":       strHeader("bytes"),
					"Content-Disposition": strHeader("inline (or attachment with ?download=1) with the original file name"),
				},
				Content: map[string]*huma.MediaType{"*/*": binary},
			},
			"206": {
				Description: "The requested byte range",
				Headers:     map[string]*huma.Param{"Content-Range": strHeader("bytes <start>-<end>/<size>")},
				Content:     map[string]*huma.MediaType{"*/*": binary},
			},
			"304": {Description: "Not modified (If-None-Match matched)"},
			"404": {Description: "Unknown or malformed media id (plain text body)"},
			"416": {Description: "Range not satisfiable"},
		},
	})
}

// relaxServerFields marks the server-assigned fields of the pack model as
// readOnly and drops them from the required lists, so that the request
// schemas in /docs match what the handlers accept (huma treats every field
// without omitempty as required; those bodies are validated by the packs
// package instead of the JSON schema).
func relaxServerFields(oapi *huma.OpenAPI) {
	serverFields := map[string][]string{
		"Pack":     {"id", "version", "createdAt", "updatedAt"},
		"Round":    {"id"},
		"Theme":    {"id"},
		"Question": {"id"},
	}
	schemas := oapi.Components.Schemas.Map()
	for name, fields := range serverFields {
		sch := schemas[name]
		if sch == nil {
			continue
		}
		for _, f := range fields {
			sch.Required = slices.DeleteFunc(sch.Required, func(r string) bool { return r == f })
			if p := sch.Properties[f]; p != nil {
				p.ReadOnly = true
			}
		}
	}
}
