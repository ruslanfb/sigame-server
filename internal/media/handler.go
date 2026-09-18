package media

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Handler serves GET/HEAD /{id} with Range, ETag/If-None-Match and immutable
// cache headers. The id is taken from the chi "id" URL parameter when present,
// otherwise from the request path (so it also works behind http.StripPrefix).
// allowScripts adds allow-scripts to the CSP sandbox of HTML objects.
func Handler(store Store, allowScripts bool) http.Handler {
	h := &handler{store: store, allowScripts: allowScripts}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			id = strings.Trim(r.URL.Path, "/")
		}
		h.serve(w, r, id)
	})
}

// Mount registers the media handler at GET/HEAD /media/{id} on r.
func Mount(r chi.Router, store Store, allowScripts bool) {
	h := Handler(store, allowScripts)
	r.Method(http.MethodGet, "/media/{id}", h)
	r.Method(http.MethodHead, "/media/{id}", h)
}

type handler struct {
	store        Store
	allowScripts bool
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !IsValidID(id) {
		http.NotFound(w, r)
		return
	}
	rs, meta, err := h.store.Open(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rs.Close()

	hdr := w.Header()
	hdr.Set("Content-Type", meta.MIME)
	hdr.Set("ETag", `"`+id+`"`)
	hdr.Set("Cache-Control", "public, max-age=31536000, immutable")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Accept-Ranges", "bytes")

	name := meta.OriginalName
	if name == "" {
		name = id + "." + meta.Ext
	}
	disposition := "inline"
	if v := r.URL.Query().Get("download"); v != "" && v != "0" && v != "false" {
		disposition = "attachment"
	}
	hdr.Set("Content-Disposition", contentDisposition(disposition, name))

	switch {
	case meta.Kind == KindHTML:
		csp := "sandbox allow-forms allow-pointer-lock allow-popups"
		if h.allowScripts {
			csp += " allow-scripts"
		}
		hdr.Set("Content-Security-Policy", csp)
		hdr.Set("Referrer-Policy", "no-referrer")
	case strings.HasPrefix(meta.MIME, "image/svg"):
		hdr.Set("Content-Security-Policy", "sandbox; script-src 'none'")
		hdr.Set("Referrer-Policy", "no-referrer")
	}

	modtime := time.Unix(0, 0)
	if meta.CreatedAt > 0 {
		modtime = time.UnixMilli(meta.CreatedAt).UTC()
	}
	http.ServeContent(w, r, "", modtime, rs)
}

// contentDisposition builds `<disposition>; filename="<ascii>"` plus an
// RFC 5987 filename* parameter (UTF-8, percent-encoded) when it is not ASCII.
func contentDisposition(disposition, name string) string {
	var ascii strings.Builder
	needsExt := false
	for _, r := range name {
		switch {
		case r == '"' || r == '\\':
			ascii.WriteByte('_')
			needsExt = true
		case r < 0x20 || r == 0x7f:
			// dropped by cleanOriginalName; skip defensively
			needsExt = true
		case r > 0x7e:
			ascii.WriteByte('_')
			needsExt = true
		default:
			ascii.WriteRune(r)
		}
	}
	v := disposition + `; filename="` + ascii.String() + `"`
	if needsExt {
		v += "; filename*=UTF-8''" + rfc5987Encode(name)
	}
	return v
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
