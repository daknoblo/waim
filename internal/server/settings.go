package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/tmdb"
	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "", false)
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, message string, isErr bool, checks ...web.ConnCheck) {
	cur := s.cfg.Get()
	cacheEntries, _ := s.store.TMDBCacheCount(r.Context())
	d := web.SettingsData{
		Layout:         s.layout(r, web.NavSettings),
		Settings:       cur,
		Libraries:      cur.Libraries,
		HasJellyfinKey: cur.Jellyfin.APIKey != "",
		HasTMDBKey:     cur.TMDB.APIKey != "",
		HasAIKey:       cur.AI.APIKey != "",
		CacheEntries:   cacheEntries,
		Message:        message,
		IsError:        isErr,
	}
	if len(checks) > 0 {
		d.JellyfinCheck = checks[0]
	}
	if len(checks) > 1 {
		d.TMDBCheck = checks[1]
	}
	s.render(w, r, web.Settings(d))
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t := s.translator(r)
	ns, rebound := s.parseSettingsForm(r)
	if err := s.cfg.Save(ns); err != nil {
		s.renderSettings(w, r, t.T("settings.saveError", err.Error()), true)
		return
	}
	s.applyLogLevel(ns.LogLevel)
	// Reflect a locale change immediately via the cookie.
	if s.catalog.Has(ns.Locale) {
		setLocaleCookie(w, r, ns.Locale)
	}
	tt := s.catalog.For(ns.Locale)
	if len(rebound) > 0 {
		// Saved, but the connection test is skipped: there is no key to test
		// with, and the point of dropping it was not to contact the new host.
		s.renderSettings(w, r, tt.T("settings.keyClearedHostChanged", strings.Join(rebound, ", ")), true)
		return
	}
	jfCheck, tdCheck := s.testConnections(r.Context(), tt, ns)
	s.renderSettings(w, r, tt.T("settings.saveSuccess"), false, jfCheck, tdCheck)
}

func (s *Server) handleRefreshLibraries(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t := s.translator(r)
	ns, rebound := s.parseSettingsForm(r)

	if len(rebound) > 0 {
		s.renderSettings(w, r, t.T("settings.keyClearedHostChanged", strings.Join(rebound, ", ")), true)
		return
	}
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
	s.applyLogLevel(ns.LogLevel)
	s.renderSettings(w, r, t.T("settings.saveSuccess"), false)
}

// parseSettingsForm builds a Settings value from the submitted form, preserving
// existing API keys when the corresponding field is left blank.
//
// A stored key is only carried over while the endpoint keeps addressing the
// same host. Otherwise someone could point the URL at a host they control,
// leave the key field empty and have the connection test hand them the stored
// plaintext credential. The returned slice names the credentials that were
// dropped for that reason so the caller can explain it.
func (s *Server) parseSettingsForm(r *http.Request) (config.Settings, []string) {
	cur := s.cfg.Get()
	ns := cur.Clone()
	var rebound []string

	ns.Locale = config.NormalizeLocale(r.FormValue("locale"))
	ns.LogLevel = config.NormalizeLogLevel(r.FormValue("log_level"))
	ns.Jellyfin.URL = strings.TrimSpace(r.FormValue("jellyfin_url"))
	ns.Jellyfin.UserID = strings.TrimSpace(r.FormValue("jellyfin_user_id"))
	if k := strings.TrimSpace(r.FormValue("jellyfin_api_key")); k != "" {
		ns.Jellyfin.APIKey = k
	} else if ns.Jellyfin.APIKey != "" && !sameEndpointHost(cur.Jellyfin.URL, ns.Jellyfin.URL) {
		ns.Jellyfin.APIKey = ""
		rebound = append(rebound, "Jellyfin")
	}

	ns.TMDB.Language = strings.TrimSpace(r.FormValue("tmdb_language"))
	ns.TMDB.Region = strings.TrimSpace(r.FormValue("tmdb_region"))
	if k := strings.TrimSpace(r.FormValue("tmdb_api_key")); k != "" {
		ns.TMDB.APIKey = k
	}

	ns.AI.Enabled = r.FormValue("ai_enabled") != ""
	ns.AI.Endpoint = strings.TrimSpace(r.FormValue("ai_endpoint"))
	ns.AI.Model = strings.TrimSpace(r.FormValue("ai_model"))
	if k := strings.TrimSpace(r.FormValue("ai_api_key")); k != "" {
		ns.AI.APIKey = k
	} else if ns.AI.APIKey != "" && !sameEndpointHost(cur.AI.Endpoint, ns.AI.Endpoint) {
		ns.AI.APIKey = ""
		rebound = append(rebound, "AI")
	}

	ns.Scan.IntervalMinutes = atoiDefault(r.FormValue("scan_interval"), cur.Scan.IntervalMinutes)
	ns.Scan.TMDBRateLimitRPS = atofDefault(r.FormValue("scan_rate"), cur.Scan.TMDBRateLimitRPS)
	ns.Scan.RunOnStart = r.FormValue("scan_run_on_start") != ""
	ns.Scan.IncludeSpecials = r.FormValue("scan_include_specials") != ""
	ns.Scan.EpisodeRatings = r.FormValue("scan_episode_ratings") != ""

	ns.Cache.RefreshEnabled = r.FormValue("cache_refresh_enabled") != ""
	ns.Cache.RefreshIntervalMinutes = atoiDefault(r.FormValue("cache_refresh_interval"), cur.Cache.RefreshIntervalMinutes)
	ns.Cache.RefreshPercent = atoiDefault(r.FormValue("cache_refresh_percent"), cur.Cache.RefreshPercent)
	ns.Cache.CleanupEnabled = r.FormValue("cache_cleanup_enabled") != ""
	ns.Cache.CleanupMaxAgeDays = atoiDefault(r.FormValue("cache_cleanup_max_age"), cur.Cache.CleanupMaxAgeDays)

	selected := map[string]bool{}
	for _, id := range r.Form["library"] {
		selected[id] = true
	}
	for i := range ns.Libraries {
		ns.Libraries[i].Enabled = selected[ns.Libraries[i].ID]
	}
	return ns, rebound
}

// sameEndpointHost reports whether two configured endpoints address the same
// place, comparing hostname, port and transport security.
//
// This is stricter than the redirect policy on purpose: here the target comes
// straight from the settings form, so whoever submits it picks where the stored
// key would be sent. Both a different port and a downgrade from https to http
// therefore count as a change — the latter because reusing the key would put it
// on the wire in the clear. Upgrading http to https is fine, and an endpoint
// that was not configured before is treated as unchanged so first-time setup
// works.
func sameEndpointHost(oldRaw, newRaw string) bool {
	oldRaw = strings.TrimSpace(oldRaw)
	if oldRaw == "" {
		return true
	}
	oldURL, oerr := url.Parse(oldRaw)
	newURL, nerr := url.Parse(strings.TrimSpace(newRaw))
	if oerr != nil || nerr != nil {
		return false
	}
	if !strings.EqualFold(oldURL.Host, newURL.Host) {
		return false
	}
	// Anything other than keeping the scheme or upgrading to https is a change.
	return !strings.EqualFold(oldURL.Scheme, "https") || strings.EqualFold(newURL.Scheme, "https")
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

// applyLogLevel updates the live logging verbosity to match the given level.
func (s *Server) applyLogLevel(level string) {
	if s.logLevel != nil {
		s.logLevel.Set(config.ParseLogLevel(level))
	}
}

// testConnections verifies the Jellyfin and TMDB credentials in ns and returns
// localised, display-ready results. A credential that is not configured yields
// an unchecked (hidden) result.
func (s *Server) testConnections(ctx context.Context, t *i18n.Translator, ns config.Settings) (web.ConnCheck, web.ConnCheck) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var jf, td web.ConnCheck

	if ns.Jellyfin.URL != "" && ns.Jellyfin.APIKey != "" {
		jf.Checked = true
		if info, err := jellyfin.New(ns.Jellyfin.URL, ns.Jellyfin.APIKey).SystemInfo(ctx); err != nil {
			jf.Message = t.T("settings.connJellyfinFail", err.Error())
		} else {
			jf.OK = true
			jf.Message = t.T("settings.connJellyfinOk", strings.TrimSpace(info.ServerName+" "+info.Version))
		}
	}

	if ns.TMDB.APIKey != "" {
		td.Checked = true
		if err := tmdb.New(ns.TMDB.APIKey, ns.TMDB.Language, ns.TMDB.Region, ns.Scan.TMDBRateLimitRPS).Ping(ctx); err != nil {
			td.Message = t.T("settings.connTmdbFail", err.Error())
		} else {
			td.OK = true
			td.Message = t.T("settings.connTmdbOk")
		}
	}

	return jf, td
}
