package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/daknoblo/waim/internal/ai"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/tmdb"
	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, "", false)
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, message string, isErr bool, checks ...map[string]web.ConnCheck) {
	cur := s.cfg.Get()
	cacheEntries, _ := s.store.TMDBCacheCount(r.Context())
	d := web.SettingsData{
		Layout:       s.layout(r, web.NavSettings),
		Settings:     cur,
		HasTMDBKey:   cur.TMDB.APIKey != "",
		HasAIKey:     cur.AI.APIKey != "",
		CacheEntries: cacheEntries,
		Message:      message,
		IsError:      isErr,
		Checks:       map[string]web.ConnCheck{},
	}
	if len(checks) > 0 && checks[0] != nil {
		d.Checks = checks[0]
	}
	s.render(w, r, web.Settings(d))
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t := s.translator(r)
	ns, pending := s.parseSettingsForm(r)
	localeChanged := ns.Locale != s.cfg.Get().Locale

	if err := s.cfg.SaveGlobals(ns); err != nil {
		s.settingsResponse(w, r, t, settingsResult{Err: err})
		return
	}
	s.applyLogLevel(ns.LogLevel)
	s.sched.Recompute()
	s.suggest.Invalidate()
	if s.catalog.Has(ns.Locale) {
		setLocaleCookie(w, r, ns.Locale)
	}
	tt := s.catalog.For(ns.Locale)

	// Every label changes with the language, so the page has to be rebuilt.
	if localeChanged && isHTMX(r) {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	res := settingsResult{Settings: ns, Pending: pending}
	sections := changedSections(r, pending)
	res.Checks = s.testConnections(r.Context(), tt, ns, sections)
	// A held-back section was never contacted; say what it is waiting for.
	for _, p := range pending {
		res.Checks[p] = web.ConnCheck{
			Checked: true,
			State:   web.ConnNeedsKey,
			Message: tt.T("settings.connNeedsKey"),
		}
	}
	s.settingsResponse(w, r, tt, res)
}

// settingsResult carries what a save produced back to the renderer.
type settingsResult struct {
	Settings config.Settings
	Pending  []string
	Checks   map[string]web.ConnCheck
	Err      error
}

// settingsResponse answers a save. Auto-save sends only the connection chips
// and the save indicator out of band, so typing is never interrupted by a
// re-rendered form; a plain form post (no JavaScript) still gets the full page.
func (s *Server) settingsResponse(w http.ResponseWriter, r *http.Request, t *i18n.Translator, res settingsResult) {
	if !isHTMX(r) {
		if res.Err != nil {
			s.renderSettings(w, r, t.T("settings.saveError", res.Err.Error()), true)
			return
		}
		s.renderSettings(w, r, t.T("settings.saveSuccess"), false, res.Checks)
		return
	}
	fb := web.SettingsFeedback{
		Checks:  res.Checks,
		Pending: res.Pending,
	}
	switch {
	case res.Err != nil:
		fb.SaveState = web.SaveFailed
		fb.SaveMessage = t.T("settings.saveError", res.Err.Error())
	case len(res.Pending) > 0:
		fb.SaveState = web.SaveNeedsKey
		fb.SaveMessage = t.T("settings.savedPendingKey")
	default:
		fb.SaveState = web.SaveOK
		fb.SaveMessage = t.T("settings.savedAt", time.Now().Format("15:04"))
	}
	s.render(w, r, web.SettingsFeedbackFragment(t, fb))
}

// isHTMX reports whether the request came from the auto-save wiring rather than
// a plain form post.
func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// Sections of the settings form that own a connection.
const (
	sectionTMDB = "tmdb"
	sectionAI   = "ai"
)

// changedSections decides which connections are worth probing. HTMX names the
// field that triggered the save, so changing the log level does not cause three
// network calls. Sections that were held back are always reported so their chip
// can ask for the key.
func changedSections(r *http.Request, pending []string) map[string]bool {
	out := map[string]bool{}
	for _, p := range pending {
		out[p] = true
	}
	if !isHTMX(r) {
		// A plain form post carries no trigger, so check everything.
		out[sectionTMDB], out[sectionAI] = true, true
		return out
	}
	switch field := r.Header.Get("HX-Trigger-Name"); {
	case strings.HasPrefix(field, "tmdb_"):
		out[sectionTMDB] = true
	case strings.HasPrefix(field, "ai_"):
		out[sectionAI] = true
	}
	return out
}

// parseSettingsForm builds a Settings value from the submitted form, preserving
// existing API keys when the corresponding field is left blank.
//
// A stored key only stays with an endpoint that keeps addressing the same
// place. When the address changes and no new key was supplied, that section is
// left as it was instead: the secret field is always rendered empty, so
// applying the change would either send the stored credential to an address
// the user just typed, or silently drop it. The returned slice names the
// sections that were held back so the caller can ask for the key.
func (s *Server) parseSettingsForm(r *http.Request) (config.Settings, []string) {
	cur := s.cfg.Get()
	ns := cur.Clone()
	var rebound []string

	ns.Locale = config.NormalizeLocale(r.FormValue("locale"))
	ns.LogLevel = config.NormalizeLogLevel(r.FormValue("log_level"))

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
		ns.AI.Endpoint = cur.AI.Endpoint
		rebound = append(rebound, sectionAI)
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

// testConnections verifies the global TMDB and AI credentials and returns
// localised, display-ready results. A credential that is not configured yields
// an unchecked (hidden) result.
// testConnections probes the endpoints of the given sections and returns one
// check per section. A section that was held back for a missing key reports
// that instead of being contacted.
func (s *Server) testConnections(ctx context.Context, t *i18n.Translator, ns config.Settings, want map[string]bool) map[string]web.ConnCheck {
	out := map[string]web.ConnCheck{}
	if len(want) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if want[sectionTMDB] {
		var c web.ConnCheck
		if ns.TMDB.APIKey == "" {
			c = web.ConnCheck{Checked: true, State: web.ConnIncomplete, Message: t.T("settings.connIncomplete")}
		} else {
			c.Checked = true
			if err := tmdb.New(ns.TMDB.APIKey, ns.TMDB.Language, ns.TMDB.Region, ns.Scan.TMDBRateLimitRPS).Ping(ctx); err != nil {
				c.State = web.ConnError
				c.Message = t.T("settings.connTmdbFail", err.Error())
			} else {
				c.OK, c.State = true, web.ConnOK
				c.Message = t.T("settings.connTmdbOk")
			}
		}
		out[sectionTMDB] = c
	}

	if want[sectionAI] {
		var c web.ConnCheck
		switch {
		case !ns.AI.Enabled:
			// Probing costs a real model call, so it stays off while the
			// feature is off.
			c = web.ConnCheck{Checked: true, State: web.ConnIdle, Message: t.T("settings.connAiDisabled")}
		case ns.AI.Endpoint == "" || ns.AI.APIKey == "":
			c = web.ConnCheck{Checked: true, State: web.ConnIncomplete, Message: t.T("settings.connIncomplete")}
		default:
			c.Checked = true
			if err := ai.New(ns.AI.Endpoint, ns.AI.APIKey, ns.AI.Model).Ping(ctx); err != nil {
				c.State = web.ConnError
				c.Message = t.T("settings.connAiFail", err.Error())
			} else {
				c.OK, c.State = true, web.ConnOK
				c.Message = t.T("settings.connAiOk")
			}
		}
		out[sectionAI] = c
	}

	return out
}
