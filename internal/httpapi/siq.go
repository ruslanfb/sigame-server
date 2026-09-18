package httpapi

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"sigame/internal/packs"
	"sigame/internal/siq"
)

// ImportResult is the answer of POST /packs/import.
type ImportResult struct {
	Pack   *packs.Pack `json:"pack" doc:"The imported pack. Persisted unless dryRun=true; a dry run returns the raw conversion with fresh but unsaved ids, version 0, no timestamps and empty mediaId fields"`
	Report *siq.Report `json:"report" doc:"Compatibility report: detected version, dropped media, unknown types, statistics"`
}

// importSiq handles POST /api/v1/packs/import (multipart/form-data, field
// "file"). The upload is spooled to <DataDir>/tmp to obtain an io.ReaderAt
// for the zip reader, converted with siq.Import (media streamed into the
// store), mechanically repaired where the validator is stricter than real
// .siq files (see repair.go) and persisted with Repo.Create. With
// dryRun=true nothing is stored: media is not written, the raw conversion
// is returned without validation.
func (s *Server) importSiq(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dryRun := queryBool(r, "dryRun")

	r.Body = http.MaxBytesReader(w, r.Body, s.siqBodyCap)
	part, err := openFilePart(r, "file")
	if err != nil {
		writeProblem(w, err)
		return
	}
	defer part.Close()

	tmp, size, err := s.spoolToTemp(part, "siq-*.zip")
	if err != nil {
		writeProblem(w, err)
		return
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	pack, report, err := siq.Import(ctx, tmp, size, s.deps.Media, siq.ImportOptions{SkipMedia: dryRun})
	if err != nil {
		writeProblem(w, s.fail(err))
		return
	}
	if strings.TrimSpace(pack.Name) == "" {
		pack.Name = strings.TrimSuffix(path.Base(part.FileName()), path.Ext(part.FileName()))
	}
	status := http.StatusOK
	if !dryRun {
		if err := repairImportedPack(pack, report); err != nil {
			writeProblem(w, s.fail(err))
			return
		}
		if err := s.deps.Packs.Create(ctx, pack); err != nil {
			writeProblem(w, s.fail(err))
			return
		}
		status = http.StatusCreated
		w.Header().Set("Location", packLocation(pack.ID))
		w.Header().Set("ETag", etag(pack.Version))
	}
	writeJSON(w, status, ImportResult{Pack: pack, Report: report})
}

// exportSiq handles GET /api/v1/packs/{id}/export: streams the pack as a
// SIQ v5 archive. Headers are written on the first byte so that failures
// before that (e.g. missing media) still produce a problem document.
func (s *Server) exportSiq(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := s.deps.Packs.Get(r.Context(), id)
	if err != nil {
		writeProblem(w, s.fail(err))
		return
	}
	name := exportFileName(p.Name, p.ID)
	lw := &lazyWriter{w: w, before: func(h http.Header) {
		h.Set("Content-Type", "application/zip")
		h.Set("Content-Disposition", attachmentDisposition(name+".siq"))
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
	}}
	if err := siq.Export(r.Context(), p, s.deps.Media, lw); err != nil {
		if !lw.started {
			writeProblem(w, s.fail(err))
			return
		}
		s.log.Error("siq export aborted mid-stream", "packId", id, "err", err)
	}
}

// --- helpers -------------------------------------------------------------------

// openFilePart returns the first multipart part named field. The caller
// must close it. Errors are ready-made huma status errors.
func openFilePart(r *http.Request, field string) (*multipart.Part, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		if errors.Is(err, http.ErrNotMultipart) {
			return nil, huma.Error415UnsupportedMediaType("expected a multipart/form-data body with a \"" + field + "\" file field")
		}
		return nil, huma.Error400BadRequest("cannot read multipart form: " + err.Error())
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, huma.Error422UnprocessableEntity("file field is required",
				&huma.ErrorDetail{Location: "body." + field, Message: "expected a file part named \"" + field + "\""})
		}
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				return nil, mapError(err)
			}
			return nil, huma.Error400BadRequest("cannot read multipart form: " + err.Error())
		}
		if part.FormName() == field {
			return part, nil
		}
		_ = part.Close()
	}
}

// spoolToTemp copies r into a temp file under the server's tmp dir and
// returns it positioned at 0 together with its size.
func (s *Server) spoolToTemp(r io.Reader, pattern string) (*os.File, int64, error) {
	tmp, err := os.CreateTemp(s.tmpDir, pattern)
	if err != nil {
		return nil, 0, fmt.Errorf("httpapi: create temp file: %w", err)
	}
	size, err := io.Copy(tmp, r)
	if err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, 0, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, 0, fmt.Errorf("httpapi: rewind temp file: %w", err)
	}
	return tmp, size, nil
}

func queryBool(r *http.Request, name string) bool {
	v := r.URL.Query().Get(name)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}

// lazyWriter delays the response headers until the first byte is written so
// that an early failure can still be reported as a problem document.
type lazyWriter struct {
	w       http.ResponseWriter
	before  func(h http.Header)
	started bool
}

func (l *lazyWriter) Write(p []byte) (int, error) {
	if !l.started {
		l.started = true
		l.before(l.w.Header())
		l.w.WriteHeader(http.StatusOK)
	}
	return l.w.Write(p)
}

// exportFileName derives a download name from the pack name: path
// separators, quotes and control characters become underscores; an empty
// result falls back to the pack id.
func exportFileName(name, id string) string {
	name = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == '"' || r == ':' || r == '*' || r == '?' || r == '<' || r == '>' || r == '|':
			return '_'
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	name = strings.Trim(name, ". ")
	if name == "" {
		return id
	}
	if runes := []rune(name); len(runes) > 120 {
		name = string(runes[:120])
	}
	return name
}

// attachmentDisposition builds `attachment; filename="<ascii>"; filename*=UTF-8”<encoded>`.
func attachmentDisposition(name string) string {
	var ascii strings.Builder
	for _, r := range name {
		switch {
		case r == '"' || r == '\\' || r > 0x7e || r < 0x20:
			ascii.WriteByte('_')
		default:
			ascii.WriteRune(r)
		}
	}
	return `attachment; filename="` + ascii.String() + `"; filename*=UTF-8''` + rfc5987Encode(name)
}

// rfc5987Encode percent-encodes everything outside attr-char (RFC 5987 §3.2).
func rfc5987Encode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAttrChar(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0x0f])
	}
	return b.String()
}

func isAttrChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}
