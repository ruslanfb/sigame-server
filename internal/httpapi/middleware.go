package httpapi

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5/middleware"
)

// requestLogger logs one line per request via slog: method, path, status,
// duration and bytes written, plus the request id and remote address.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				log.Info("http request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", ww.Status(),
					"durationMs", time.Since(start).Milliseconds(),
					"bytes", ww.BytesWritten(),
					"reqId", middleware.GetReqID(r.Context()),
					"remote", r.RemoteAddr,
				)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}

// recoverer turns a handler panic into a problem+json 500 and logs the
// stack. http.ErrAbortHandler is re-raised as net/http expects.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				log.Error("http handler panic",
					"method", r.Method, "path", r.URL.Path,
					"reqId", middleware.GetReqID(r.Context()),
					"panic", rec, "stack", string(debug.Stack()))
				writeProblem(w, huma.Error500InternalServerError("internal error"))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// CORS policy shared by every route.
const (
	corsAllowMethods  = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	corsAllowHeaders  = "Content-Type, If-Match, X-Host-Token, Range"
	corsExposeHeaders = "ETag, Content-Range, Location"
	corsMaxAge        = "600"
)

// corsMiddleware allows every origin when origins is empty (LAN default),
// otherwise only exact (case-insensitive) matches. Preflight requests are
// answered with 204 without reaching the router.
func corsMiddleware(origins []string) func(http.Handler) http.Handler {
	allowAll := len(origins) == 0
	allowed := func(origin string) bool {
		if allowAll {
			return true
		}
		for _, o := range origins {
			if strings.EqualFold(strings.TrimRight(o, "/"), origin) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" || !allowed(origin) {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			if allowAll {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
			}
			h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", corsAllowMethods)
				h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
				h.Set("Access-Control-Max-Age", corsMaxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
