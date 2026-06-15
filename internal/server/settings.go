package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "", false)
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, message string, isErr bool) {
	cur := s.cfg.Get()
	d := web.SettingsData{
		Layout:         s.layout(r, web.NavSettings),
		Settings:       cur,
		Libraries:      cur.Libraries,
		HasJellyfinKey: cur.Jellyfin.APIKey != "",
		HasTMDBKey:     cur.TMDB.APIKey != "",
		Message:        message,
		IsError:        isErr,
	}
	s.render(w, r, web.Settings(d))
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t := s.translator(r)
	ns := s.parseSettingsForm(r)
	if err := s.cfg.Save(ns); err != nil {
		s.renderSettings(w, r, t.T("settings.saveError", err.Error()), true)
		return
	}
	// Reflect a locale change immediately via the cookie.
	if s.catalog.Has(ns.Locale) {
		http.SetCookie(w, &http.Cookie{
			Name: localeCookie, Value: ns.Locale, Path: "/",
			MaxAge: 60 * 60 * 24 * 365, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
	}
	s.renderSettings(w, r, s.catalog.For(ns.Locale).T("settings.saveSuccess"), false)
}

func (s *Server) handleRefreshLibraries(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t := s.translator(r)
	ns := s.parseSettingsForm(r)

	if ns.Jellyfin.URL == "" || ns.Jellyfin.APIKey == "" {
		s.renderSettings(w, r, t.T("settings.saveError", "jellyfin url and api key are required"), true)
		return
	}

	client := jellyfin.New(ns.Jellyfin.URL, ns.Jellyfin.APIKey)
	libs, err := client.Libraries(r.Context())
	if err != nil {
		s.renderSettings(w, r, t.T("settings.saveError", err.Error()), true)
		return
	}

	// Preserve enabled state by library ID.
	enabled := map[string]bool{}
	for _, l := range ns.Libraries {
		enabled[l.ID] = l.Enabled
	}
	merged := make([]config.Library, 0, len(libs))
	for _, l := range libs {
		merged = append(merged, config.Library{
			ID:      l.ID,
			Name:    l.Name,
			Type:    l.CollectionType,
			Enabled: enabled[l.ID],
		})
	}
	ns.Libraries = merged

	if err := s.cfg.Save(ns); err != nil {
		s.renderSettings(w, r, t.T("settings.saveError", err.Error()), true)
		return
	}
	s.renderSettings(w, r, t.T("settings.saveSuccess"), false)
}

// parseSettingsForm builds a Settings value from the submitted form, preserving
// existing API keys when the corresponding field is left blank.
func (s *Server) parseSettingsForm(r *http.Request) config.Settings {
	cur := s.cfg.Get()
	ns := cur.Clone()

	ns.Locale = config.NormalizeLocale(r.FormValue("locale"))
	ns.Jellyfin.URL = strings.TrimSpace(r.FormValue("jellyfin_url"))
	ns.Jellyfin.UserID = strings.TrimSpace(r.FormValue("jellyfin_user_id"))
	if k := strings.TrimSpace(r.FormValue("jellyfin_api_key")); k != "" {
		ns.Jellyfin.APIKey = k
	}

	ns.TMDB.Language = strings.TrimSpace(r.FormValue("tmdb_language"))
	ns.TMDB.Region = strings.TrimSpace(r.FormValue("tmdb_region"))
	if k := strings.TrimSpace(r.FormValue("tmdb_api_key")); k != "" {
		ns.TMDB.APIKey = k
	}

	ns.Scan.IntervalMinutes = atoiDefault(r.FormValue("scan_interval"), cur.Scan.IntervalMinutes)
	ns.Scan.TMDBRateLimitRPS = atofDefault(r.FormValue("scan_rate"), cur.Scan.TMDBRateLimitRPS)
	ns.Scan.RunOnStart = r.FormValue("scan_run_on_start") != ""
	ns.Scan.IncludeSpecials = r.FormValue("scan_include_specials") != ""

	selected := map[string]bool{}
	for _, id := range r.Form["library"] {
		selected[id] = true
	}
	for i := range ns.Libraries {
		ns.Libraries[i].Enabled = selected[ns.Libraries[i].ID]
	}
	return ns
}

func atoiDefault(s string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return v
	}
	return def
}

func atofDefault(s string, def float64) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return v
	}
	return def
}
