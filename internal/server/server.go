// Package server wires the HTTP layer together: routing, request-scoped
// localisation and rendering of the templ-based UI.
package server

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/logbuf"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/version"
	"github.com/daknoblo/waim/internal/web"
)

const (
	localeCookie = "waim_locale"
	repoURL      = "https://github.com/daknoblo/waim"
)

// Server holds the dependencies shared by all HTTP handlers.
type Server struct {
	cfg     *config.Manager
	store   *store.Store
	sched   *scheduler.Scheduler
	logs    *logbuf.Buffer
	catalog *i18n.Catalog
	log     *slog.Logger
	info    version.Info
}

// New constructs a Server.
func New(cfg *config.Manager, st *store.Store, sched *scheduler.Scheduler, logs *logbuf.Buffer, catalog *i18n.Catalog, log *slog.Logger) *Server {
	return &Server{
		cfg:     cfg,
		store:   st,
		sched:   sched,
		logs:    logs,
		catalog: catalog,
		log:     log,
		info:    version.Get(),
	}
}

// Handler builds the HTTP routing tree.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	staticSub, _ := fs.Sub(web.StaticFS, "assets/static")
	mux.Handle("GET /static/", cacheControl(http.StripPrefix("/static/", http.FileServerFS(staticSub))))

	mux.HandleFunc("GET /healthz", s.handleHealth)

	mux.HandleFunc("GET /{$}", s.handleDashboard)
	mux.HandleFunc("GET /settings", s.handleSettings)
	mux.HandleFunc("POST /settings", s.handleSaveSettings)
	mux.HandleFunc("POST /settings/refresh-libraries", s.handleRefreshLibraries)
	mux.HandleFunc("GET /about", s.handleAbout)

	mux.HandleFunc("POST /locale", s.handleLocale)
	mux.HandleFunc("POST /scan", s.handleScan)

	mux.HandleFunc("GET /partials/status", s.handlePartialStatus)
	mux.HandleFunc("GET /partials/findings", s.handlePartialFindings)
	mux.HandleFunc("GET /partials/log", s.handlePartialLog)

	mux.HandleFunc("GET /export/settings", s.handleExportSettings)
	mux.HandleFunc("GET /export/sync", s.handleExportSync)

	return logRequests(s.log, mux)
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

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Debug("http", "method", r.Method, "path", r.URL.Path)
	})
}
