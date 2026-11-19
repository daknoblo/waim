package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/daknoblo/waim/internal/maintenance"
	"github.com/daknoblo/waim/internal/reset"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

// Readers also hold admission, so an export cannot straddle config/database
// reset boundaries. CSRF and body limits wrap this middleware, not vice versa.
func (s *Server) admission(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/settings/reset" || r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		release, err := s.cfg.Gate().Enter()
		if err != nil {
			if errors.Is(err, maintenance.ErrRecovery) {
				http.Error(w, s.translator(r).T("settings.reset.failed"), http.StatusServiceUnavailable)
			} else {
				http.Error(w, s.translator(r).T("settings.reset.busy"), http.StatusConflict)
			}
			return
		}
		defer release()
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad request", 400)
				return
			}
			token := r.FormValue("_epoch")
			if token == "" {
				token = r.Header.Get("X-Waim-Epoch")
			}
			if err := s.cfg.Gate().Check(token); err != nil {
				http.Error(w, s.translator(r).T("settings.reset.stale"), http.StatusConflict)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	scope := store.ResetScope(r.PostForm.Get("scope"))
	if !scope.Valid() || r.PostForm.Get("confirm") != "yes" || r.PostForm.Get("confirmation") != "RESET" || r.PostForm.Get("_epoch") == "" {
		http.Error(w, s.translator(r).T("settings.reset.confirmError"), http.StatusBadRequest)
		return
	}
	err := reset.New(s.cfg, s.store).Apply(r.Context(), scope, r.FormValue("_epoch"), func() {
		s.suggest.Clear()
		s.sched.ResetState()
		s.activities.Clear()
		s.diagnosticCache.mu.Lock()
		s.diagnosticCache.data = web.DiagnosticsData{}
		s.diagnosticCache.sources = nil
		s.diagnosticCache.run = nil
		s.diagnosticCache.valid = false
		s.diagnosticCache.mu.Unlock()
		if scope == store.ResetFactory {
			s.logs.Clear()
			s.applyLogLevel(s.cfg.Get().LogLevel)
		}
	})
	if err != nil {
		code, message := http.StatusInternalServerError, s.translator(r).T("settings.reset.failed")
		if errors.Is(err, maintenance.ErrBusy) {
			code, message = http.StatusConflict, s.translator(r).T("settings.reset.busy")
		}
		if errors.Is(err, maintenance.ErrStale) {
			code, message = http.StatusConflict, s.translator(r).T("settings.reset.stale")
		}
		s.log.Warn("reset not completed", "scope", scope, "err", err)
		http.Error(w, message, code)
		return
	}
	if scope == store.ResetFactory {
		for _, name := range []string{localeCookie, localeGenerationCookie} {
			http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode})
		}
	}
	http.Redirect(w, r, "/settings?tab=other&reset="+string(scope), http.StatusSeeOther)
}

const localeGenerationCookie = "waim_locale_generation"
