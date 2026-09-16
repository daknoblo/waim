package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, web.Dashboard(s.dashboardData(r)))
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	d := web.LogPageData{
		Layout:      s.layout(r, web.NavLogs),
		Logs:        web.BuildLogViews(s.logs.Entries()),
		Activities:  s.activityViews(),
		Diagnostics: s.diagnostics(r.Context()),
	}
	s.render(w, r, web.Logs(d))
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, web.Stats(s.statsData(r)))
}

func (s *Server) statsData(r *http.Request) web.StatsData {
	t := s.translator(r)
	ctx := r.Context()
	run, _ := s.currentRun(ctx)
	var findings []store.Finding
	if run != nil {
		findings, _ = s.currentFindings(ctx, run)
	}
	libTypes := map[string]string{}
	for _, src := range s.cfg.Get().Sources {
		for _, l := range src.Libraries {
			libTypes[media.Qualify(src.ID, l.ID)] = l.Type
		}
	}
	history, _ := s.store.SuccessfulRunTotals(ctx, 12)
	d := web.BuildStats(t, web.StatsInput{
		Run:         run,
		Findings:    findings,
		LibTypes:    libTypes,
		History:     history,
		JellyfinURL: "",
	})
	d.Layout = s.layout(r, web.NavStats)
	d.DataState = s.dataState(ctx)
	d.Unconfirmed = d.Unconfirmed || d.DataState != web.DataReady
	if d.Unconfirmed {
		d.EpisodePct = -1
		for i := range d.Libraries {
			d.Libraries[i].Completeness = -1
		}
		for i := range d.Completion {
			d.Completion[i].Pct = -1
			for j := range d.Completion[i].Cells {
				d.Completion[i].Cells[j].Fill = "bg-slate-700"
				d.Completion[i].Cells[j].Hint = t.T("sources.incomplete")
			}
		}
		for i := range d.Facts {
			if d.Facts[i].Label == t.T("stats.factEpisodeCompletion") {
				d.Facts[i].Value = "—"
			}
		}
	}
	return d
}

func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	if s.suggestionsConfigured() && !s.suggest.Running() && s.suggest.NeedsRefresh(r.Context()) {
		s.suggest.Generate()
	}
	s.render(w, r, web.Suggestions(s.suggestionsData(r)))
}

func (s *Server) handleGenerateSuggestions(w http.ResponseWriter, r *http.Request) {
	if s.suggestionsConfigured() {
		s.suggest.Generate()
	}
	s.render(w, r, web.SuggestionsContent(s.suggestionsData(r)))
}

func (s *Server) handlePartialSuggestions(w http.ResponseWriter, r *http.Request) {
	s.renderPartial(w, r, web.SuggestionsContent(s.suggestionsData(r)))
}

func (s *Server) suggestionsConfigured() bool {
	return scanConfigured(s.cfg.Get())
}

// scanConfigured reports whether TMDB metadata can be requested. Media servers
// are optional because the virtual collection can be used independently.
func scanConfigured(settings config.Settings) bool {
	return settings.TMDB.APIKey != ""
}

// dataState explains why a view might have nothing to show, so an empty list is
// never presented as a statement about the library.
func (s *Server) dataState(ctx context.Context) string {
	if !scanConfigured(s.cfg.Get()) {
		return web.DataUnconfigured
	}
	if run, err := s.currentRun(ctx); err == nil && run != nil {
		if run.Metadata.Basis == "" {
			return web.DataLegacy
		}
		if run.Metadata.Pending {
			return web.DataIncomplete
		}
		if run.Metadata.SourcesToken != s.cfg.Get().SourcesToken() {
			return web.DataIncomplete
		}
		catalog, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
		if err != nil || len(catalog.Warnings) > 0 || len(run.Metadata.Warnings) > 0 || catalog.Revision != run.Metadata.Revision {
			return web.DataIncomplete
		}
		if run.FinishedAt != nil && catalog.UpdatedAt.After(*run.FinishedAt) {
			return web.DataIncomplete
		}
		return web.DataReady
	}
	if s.sched.Status().State == scheduler.StateRunning {
		return web.DataScanning
	}
	return web.DataNeverScanned
}

func (s *Server) suggestionsData(r *http.Request) web.SuggestionsData {
	res, _ := s.suggest.Result()
	return web.SuggestionsData{
		Layout:     s.layout(r, web.NavSuggestions),
		Running:    s.suggest.Running(),
		Configured: s.suggestionsConfigured(),
		Result:     res,
	}
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	dbPath := s.store.Path()
	dbSize := fileSize(dbPath) + fileSize(dbPath+"-wal") + fileSize(dbPath+"-shm")
	d := web.AboutData{
		Layout:     s.layout(r, web.NavAbout),
		Version:    s.info.Version,
		DBSize:     web.HumanSize(dbSize),
		ConfigSize: web.HumanSize(fileSize(s.cfg.Path())),
		GoVersion:  s.info.GoVer,
		Repo:       repoURL,
	}
	// Every stable version, including patches, has a release page.
	if s.info.IsRelease() {
		d.VersionURL = repoURL + "/releases/tag/" + s.info.Version
		d.Ref = s.info.Version
		d.RefURL = repoURL + "/tree/" + s.info.Version
		d.RefIsTag = true
	} else {
		commit := s.info.Commit
		if len(commit) > 10 {
			d.Ref = commit[:10]
		} else {
			d.Ref = commit
		}
		if commit != "" && commit != "unknown" {
			d.RefURL = repoURL + "/commit/" + commit
		}
	}
	s.render(w, r, web.About(d))
}

func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	s.sched.Trigger()
	t := s.translator(r)
	s.render(w, r, web.StatusCard(t, s.statusView(r.Context(), t)))
}

func (s *Server) handlePartialStatus(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	s.renderPartial(w, r, web.StatusCard(t, s.statusView(r.Context(), t)))
}

func (s *Server) handlePartialFindings(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	sortKey := web.NormalizeSort(r.URL.Query().Get("sort"))
	dir := web.NormalizeDir(r.URL.Query().Get("dir"))
	s.renderPartial(w, r, web.FindingsTable(t, s.findingRows(r.Context(), t, sortKey, dir), sortKey, dir, s.dataState(r.Context())))
}

func (s *Server) handlePartialLog(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	s.renderPartial(w, r, web.LogPanel(t, web.BuildLogViews(s.logs.Entries())))
}

func (s *Server) handlePartialSeriesDetail(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	run, _ := s.currentRun(r.Context())
	detail := web.BuildSeriesDetail(t, run, r.URL.Query().Get("series"), "")
	s.render(w, r, web.SeriesDetailCharts(t, detail))
}

func (s *Server) handlePartialUpcoming(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	ctx := r.Context()
	run, _ := s.currentRun(ctx)
	q := web.NormalizeUpcomingQuery(
		r.URL.Query().Get("direction"),
		r.URL.Query().Get("range"),
		r.URL.Query().Get("type"),
	)
	// The retrospective is derived from the gaps, so they are only loaded when
	// that direction is actually requested.
	var findings []store.Finding
	if q.IsPast() && run != nil {
		findings, _ = s.currentFindings(ctx, run)
	}
	s.render(w, r, web.UpcomingContent(t, web.BuildUpcomingSection(t, run, findings, q)))
}

func (s *Server) handleLocale(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	loc := r.FormValue("locale")
	if s.catalog.Has(loc) {
		setLocaleCookie(w, r, loc, s.cfg.Gate().FactoryEpoch())
	}
	redirectBack(w, r)
}

// setLocaleCookie persists the selected UI language for a year. It is HttpOnly
// (no script needs it), SameSite=Lax and Secure whenever the request arrived
// over HTTPS.
func setLocaleCookie(w http.ResponseWriter, r *http.Request, locale string, generations ...int64) {
	if len(generations) > 0 {
		http.SetCookie(w, &http.Cookie{Name: localeGenerationCookie, Value: strconv.FormatInt(generations[0], 10), Path: "/", MaxAge: int((365 * 24 * time.Hour).Seconds()), HttpOnly: true, Secure: isSecureRequest(r), SameSite: http.SameSiteLaxMode})
	}
	http.SetCookie(w, &http.Cookie{
		Name:     localeCookie,
		Value:    locale,
		Path:     "/",
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) dashboardData(r *http.Request) web.DashboardData {
	t := s.translator(r)
	sortKey := web.NormalizeSort(r.URL.Query().Get("sort"))
	dir := web.NormalizeDir(r.URL.Query().Get("dir"))
	return web.DashboardData{
		Layout:    s.layout(r, web.NavDashboard),
		Status:    s.statusView(r.Context(), t),
		Findings:  s.findingRows(r.Context(), t, sortKey, dir),
		Libraries: s.libraryFilters(t),
		Logs:      web.BuildLogViews(s.logs.Entries()),
		Sort:      sortKey,
		Dir:       dir,
		DataState: s.dataState(r.Context()),
	}
}

// libraryFilters lists the enabled, configured libraries for the dashboard filter.
func (s *Server) libraryFilters(t *i18n.Translator) []web.LibraryFilter {
	var out []web.LibraryFilter
	for _, src := range s.cfg.Get().Sources {
		if !src.Enabled {
			continue
		}
		if src.Type != media.Virtual {
			out = append(out, web.LibraryFilter{ID: src.ID, Name: src.Name})
		}
		for _, l := range src.Libraries {
			if l.Enabled {
				out = append(out, web.LibraryFilter{ID: media.Qualify(src.ID, l.ID), Name: src.Name + " · " + l.Name})
			}
		}
	}
	out = append(out, web.LibraryFilter{ID: media.VirtualID, Name: t.T("sources.collection")})
	return out
}

func (s *Server) findingRows(ctx context.Context, t *i18n.Translator, sortKey, dir string) []web.FindingRow {
	run, err := s.currentRun(ctx)
	if err != nil || run == nil {
		return nil
	}
	fs, err := s.currentFindings(ctx, run)
	if err != nil {
		return nil
	}
	rows := web.BuildFindingRows(t, fs, "")
	web.SortFindingRows(rows, sortKey, dir)
	return rows
}

func (s *Server) statusView(ctx context.Context, t *i18n.Translator) web.StatusView {
	st := s.sched.Status()
	sv := web.StatusView{
		SetupRequired: !scanConfigured(s.cfg.Get()),
		State:         st.State,
		Running:       st.State == scheduler.StateRunning,
		LastError:     st.LastError,
		LastScan:      web.FormatRelative(t, st.LastFinished),
		NextScan:      web.FormatRelative(t, st.NextRun),
	}
	if sv.SetupRequired {
		sv.StateLabel = t.T("dashboard.state.setup")
		sv.NextScan = web.FormatRelative(t, nil)
	} else if sv.Running {
		sv.StateLabel = t.T("dashboard.state.running")
	} else {
		sv.StateLabel = t.T("dashboard.state.idle")
	}

	if sv.Running {
		prog := s.sched.Progress()
		sv.CurrentItem = prog.Current
		if !prog.StartedAt.IsZero() {
			sv.Duration = web.FormatDuration(time.Since(prog.StartedAt))
		}
		sv.LibrariesScanned = len(prog.Libraries)
		for _, l := range prog.Libraries {
			sv.ItemsScanned += l.Scanned
			sv.MissingTotal += l.Missing
			sv.Libraries = append(sv.Libraries, web.LibraryStatusView{
				Name: web.LibraryDisplayName(t, l.ID, l.Name), Color: web.LibraryColor(l.ID),
				Scanned: l.Scanned, Total: l.Total, Missing: l.Missing,
			})
		}
		return sv
	}

	if run, err := s.currentRun(ctx); err == nil && run != nil {
		sv.ItemsScanned = run.ItemsScanned
		sv.LibrariesScanned = run.LibrariesScanned
		sv.MissingTotal = run.MissingCount
		sv.Duration = web.FormatDuration(run.Duration())
		for _, l := range run.Libraries {
			sv.Libraries = append(sv.Libraries, web.LibraryStatusView{
				Name: web.LibraryDisplayName(t, l.ID, l.Name), Color: web.LibraryColor(l.ID),
				Scanned: l.Scanned, Total: l.Total, Missing: l.Missing,
			})
		}
	}
	return sv
}

func (s *Server) handleExportSettings(w http.ResponseWriter, _ *http.Request) {
	data, err := s.cfg.ExportStored()
	if err != nil {
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="waim-settings.json"`)
	_, _ = w.Write(data)
}

func (s *Server) handleExportSync(w http.ResponseWriter, r *http.Request) {
	state, err := s.store.ExportSyncState(r.Context())
	if err != nil {
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}
	catalog, err := source.Catalog(r.Context(), s.store, s.cfg.Get(), false, nil)
	if err != nil {
		http.Error(w, "catalog export failed", 500)
		return
	}
	state.Catalog = &catalog
	state.Run, err = s.currentRun(r.Context())
	if err != nil {
		http.Error(w, "scan export failed", 500)
		return
	}
	if state.Run != nil {
		state.Findings, err = s.currentFindings(r.Context(), state.Run)
		if err != nil {
			http.Error(w, "findings export failed", 500)
			return
		}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="waim-sync-state.json"`)
	_, _ = w.Write(data)
}

// redirectBack returns the user to the page they came from (after a locale
// change), or the dashboard. The Referer is matched against the known set of
// routes and a constant path is used, so a crafted Referer header cannot turn
// this into an open redirect (CWE-601).
func redirectBack(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, safeReturnPath(r), http.StatusSeeOther)
}

// knownRoutes are the top-level pages a locale change may return to. Mapping to
// constant values keeps the redirect target free of request-derived data.
var knownRoutes = map[string]string{
	"/sources":     "/settings?tab=media",
	"/collection":  "/collection",
	"/":            "/",
	"/suggestions": "/suggestions",
	"/stats":       "/stats",
	"/logs":        "/logs",
	"/settings":    "/settings",
	"/about":       "/about",
}

func safeReturnPath(r *http.Request) string {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return "/"
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Host != "" && u.Host != r.Host) {
		return "/"
	}
	if dest, ok := knownRoutes[u.EscapedPath()]; ok {
		if dest == "/settings" {
			return web.SettingsURL(u.Query().Get("tab"))
		}
		return dest
	}
	return "/"
}
