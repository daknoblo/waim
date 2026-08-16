// Package server wires the HTTP layer together: routing, request-scoped
// localisation and rendering of the templ-based UI.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/suggest"
	"github.com/daknoblo/waim/internal/version"
	"github.com/daknoblo/waim/internal/web"
)

const (
	localeCookie = "waim_locale"
	repoURL      = "https://github.com/daknoblo/waim"
)

// Server holds the dependencies shared by all HTTP handlers.
type Server struct {
	cfg      *config.Manager
	store    *store.Store
	sched    *scheduler.Scheduler
	suggest  *suggest.Service
	logs     *logbuf.Buffer
	catalog  *i18n.Catalog
	log      *slog.Logger
	logLevel *slog.LevelVar
	info     version.Info
	assetVer string
}

// New constructs a Server.
func New(cfg *config.Manager, st *store.Store, sched *scheduler.Scheduler, sug *suggest.Service, logs *logbuf.Buffer, catalog *i18n.Catalog, log *slog.Logger, logLevel *slog.LevelVar) *Server {
	info := version.Get()
	return &Server{
		cfg:      cfg,
		store:    st,
		sched:    sched,
		suggest:  sug,
		logs:     logs,
		catalog:  catalog,
		log:      log,
		logLevel: logLevel,
		info:     info,
		assetVer: computeAssetVersion(info),
	}
}

// computeAssetVersion returns a token used to cache-bust static assets. It uses
// the build commit/version when available and falls back to the process start
// time so each container start serves fresh assets during development.
func computeAssetVersion(info version.Info) string {
	if info.Commit != "" && info.Commit != "unknown" {
		return info.Commit
	}
	if info.Version != "" && info.Version != "dev" {
		return info.Version
	}
	return strconv.FormatInt(time.Now().Unix(), 10)
}

// Handler builds the HTTP routing tree.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	staticSub, _ := fs.Sub(web.StaticFS, "assets/static")
	mux.Handle("GET /static/", cacheControl(http.StripPrefix("/static/", http.FileServerFS(staticSub))))

	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("GET /{$}", s.handleDashboard)
	mux.HandleFunc("GET /stats", s.handleStats)
	mux.HandleFunc("GET /suggestions", s.handleSuggestions)
	mux.HandleFunc("POST /suggestions/generate", s.handleGenerateSuggestions)
	mux.HandleFunc("GET /partials/suggestions", s.handlePartialSuggestions)
	mux.HandleFunc("GET /logs", s.handleLogs)
	mux.HandleFunc("GET /settings", s.handleSettings)
	mux.HandleFunc("POST /settings", s.handleSaveSettings)
	mux.HandleFunc("POST /settings/refresh-libraries", s.handleRefreshLibraries)
	mux.HandleFunc("GET /about", s.handleAbout)

	mux.HandleFunc("POST /locale", s.handleLocale)
	mux.HandleFunc("POST /scan", s.handleScan)

	mux.HandleFunc("GET /partials/status", s.handlePartialStatus)
	mux.HandleFunc("GET /partials/findings", s.handlePartialFindings)
	mux.HandleFunc("GET /partials/log", s.handlePartialLog)
	mux.HandleFunc("GET /partials/series-flow", s.handlePartialSeriesDetail)
	mux.HandleFunc("GET /partials/upcoming", s.handlePartialUpcoming)

	mux.HandleFunc("GET /export/settings", s.handleExportSettings)
	mux.HandleFunc("GET /export/sync", s.handleExportSync)

	// CrossOriginProtection rejects unsafe (state-changing) cross-origin browser
	// requests based on Sec-Fetch-Site / Origin, which protects every POST route
	// against CSRF without needing per-form tokens.
	csrf := http.NewCrossOriginProtection()
	return logRequests(s.log, securityHeaders(limitRequestBody(csrf.Handler(mux))))
}

// locale resolves the active locale from the cookie, then the configured
// default, then the package default.
func (s *Server) locale(r *http.Request) string {
	if c, err := r.Cookie(localeCookie); err == nil && s.catalog.Has(c.Value) {
		return c.Value
	}
	return config.NormalizeLocale(s.cfg.Get().Locale)
}

func (s *Server) translator(r *http.Request) *i18n.Translator {
	return s.catalog.For(s.locale(r))
}

func (s *Server) layout(r *http.Request, active string) web.Layout {
	t := s.translator(r)
	return web.Layout{
		T:                t,
		Active:           active,
		Version:          s.info.Version,
		AssetVersion:     s.assetVer,
		Repo:             repoURL,
		MasterKeyMissing: !s.cfg.CipherEnabled(),
		Languages:        web.LanguageOptions(t.Locale()),
	}
}

// render writes a templ component as an HTML response.
func (s *Server) render(w http.ResponseWriter, r *http.Request, comp templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := comp.Render(r.Context(), w); err != nil {
		s.log.Error("render failed", "path", r.URL.Path, "err", err)
	}
}

// viewTagHeader carries a fingerprint of a polled partial in both directions:
// the client echoes the fingerprint it currently displays, the server answers
// with the fingerprint of the freshly rendered markup.
const viewTagHeader = "X-Waim-View"

// renderPartial renders a polled partial and answers 204 No Content when the
// markup is identical to what the client already shows. htmx skips the swap on
// 204, so background polling no longer replaces unchanged DOM (which made the
// dashboard flicker and jump while reading).
func (s *Server) renderPartial(w http.ResponseWriter, r *http.Request, comp templ.Component) {
	var buf bytes.Buffer
	if err := comp.Render(r.Context(), &buf); err != nil {
		s.log.Error("render failed", "path", r.URL.Path, "err", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(buf.Bytes())
	tag := hex.EncodeToString(sum[:16])
	w.Header().Set(viewTagHeader, tag)
	if r.Header.Get(viewTagHeader) == tag {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}

// contentSecurityPolicy locks the page down to same-origin scripts and styles.
// The only external resource is the TMDB poster CDN. No inline scripts, styles
// or event handlers are used, so no 'unsafe-inline' is required.
const contentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data: https://image.tmdb.org; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'"

// securityHeaders applies conservative, framework-independent response headers.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		// same-origin keeps the Referer available for the post-locale redirect
		// while never leaking the URL to third parties.
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		if isSecureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// maxRequestBody bounds the size of an incoming request body. The largest form
// (settings, including the library list) is far below this.
const maxRequestBody = 1 << 20 // 1 MiB

// limitRequestBody caps the body of state-changing requests so a malicious or
// broken client cannot exhaust memory via ParseForm.
func limitRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		}
		next.ServeHTTP(w, r)
	})
}

// isSecureRequest reports whether the client connection is HTTPS, either
// directly or through a terminating reverse proxy. A spoofed header can only
// make cookies stricter, never weaker.
func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip the high-frequency paths before wrapping, so polling partials and
		// static assets cost no extra allocation.
		if skipRequestLog(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Debug(fmt.Sprintf("%s %s \u2192 %d (%s)",
			r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond)))
	})
}

// skipRequestLog suppresses high-frequency, low-value requests (static assets,
// the HTMX polling partials and the health check) from the activity log.
func skipRequestLog(path string) bool {
	switch {
	case path == "/healthz":
		return true
	case strings.HasPrefix(path, "/static/"):
		return true
	case strings.HasPrefix(path, "/partials/"):
		return true
	default:
		return false
	}
}

// statusRecorder wraps http.ResponseWriter to capture the response status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
