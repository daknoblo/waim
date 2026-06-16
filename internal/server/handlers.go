package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/scheduler"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, web.Dashboard(s.dashboardData(r)))
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	d := web.LogPageData{
		Layout: s.layout(r, web.NavLogs),
		Logs:   web.BuildLogViews(s.logs.Entries()),
	}
	s.render(w, r, web.Logs(d))
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, web.Stats(s.statsData(r)))
}

func (s *Server) statsData(r *http.Request) web.StatsData {
	t := s.translator(r)
	ctx := r.Context()
	run, _ := s.store.LatestSuccessfulRun(ctx)
	var findings []store.Finding
	if run != nil {
		findings, _ = s.store.FindingsForRun(ctx, run.ID)
	}
	libTypes := map[string]string{}
	for _, l := range s.cfg.Get().Libraries {
		libTypes[l.ID] = l.Type
	}
	d := web.BuildStats(t, run, findings, libTypes)
	d.Layout = s.layout(r, web.NavStats)
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
	s.render(w, r, web.SuggestionsContent(s.suggestionsData(r)))
}

func (s *Server) suggestionsConfigured() bool {
	settings := s.cfg.Get()
	return settings.Jellyfin.URL != "" && settings.Jellyfin.APIKey != "" &&
		settings.TMDB.APIKey != "" && s.cfg.CipherEnabled() && len(settings.EnabledLibraryIDs()) > 0
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
	commit := s.info.Commit
	short := commit
	if len(short) > 10 {
		short = short[:10]
	}
	commitURL := ""
	if commit != "" && commit != "unknown" {
		commitURL = repoURL + "/commit/" + commit
	}
	dbPath := s.store.Path()
	dbSize := fileSize(dbPath) + fileSize(dbPath+"-wal") + fileSize(dbPath+"-shm")
	d := web.AboutData{
		Layout:     s.layout(r, web.NavAbout),
		Channel:    s.info.Channel,
		Version:    s.info.Version,
		Commit:     short,
		CommitURL:  commitURL,
		DBSize:     web.HumanSize(dbSize),
		ConfigSize: web.HumanSize(fileSize(s.cfg.Path())),
		GoVersion:  s.info.GoVer,
		Repo:       repoURL,
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
	s.render(w, r, web.StatusCard(t, s.statusView(r.Context(), t)))
}

func (s *Server) handlePartialFindings(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	sortKey := web.NormalizeSort(r.URL.Query().Get("sort"))
	dir := web.NormalizeDir(r.URL.Query().Get("dir"))
	s.render(w, r, web.FindingsTable(t, s.findingRows(r.Context(), t, sortKey, dir), sortKey, dir))
}

func (s *Server) handlePartialLog(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	s.render(w, r, web.LogPanel(t, web.BuildLogViews(s.logs.Entries())))
}

func (s *Server) handleLocale(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	loc := r.FormValue("locale")
	if s.catalog.Has(loc) {
		http.SetCookie(w, &http.Cookie{
			Name:     localeCookie,
			Value:    loc,
			Path:     "/",
			MaxAge:   60 * 60 * 24 * 365,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
	redirectBack(w, r)
}

func (s *Server) dashboardData(r *http.Request) web.DashboardData {
	t := s.translator(r)
	sortKey := web.NormalizeSort(r.URL.Query().Get("sort"))
	dir := web.NormalizeDir(r.URL.Query().Get("dir"))
	return web.DashboardData{
		Layout:    s.layout(r, web.NavDashboard),
		Status:    s.statusView(r.Context(), t),
		Findings:  s.findingRows(r.Context(), t, sortKey, dir),
		Libraries: s.libraryFilters(),
		Logs:      web.BuildLogViews(s.logs.Entries()),
		Sort:      sortKey,
		Dir:       dir,
	}
}

// libraryFilters lists the enabled, configured libraries for the dashboard filter.
func (s *Server) libraryFilters() []web.LibraryFilter {
	var out []web.LibraryFilter
	for _, l := range s.cfg.Get().Libraries {
		if l.Enabled {
			out = append(out, web.LibraryFilter{ID: l.ID, Name: l.Name})
		}
	}
	return out
}

func (s *Server) findingRows(ctx context.Context, t *i18n.Translator, sortKey, dir string) []web.FindingRow {
	run, err := s.store.LatestSuccessfulRun(ctx)
	if err != nil || run == nil {
		return nil
	}
	fs, err := s.store.FindingsForRun(ctx, run.ID)
	if err != nil {
		return nil
	}
	cfg := s.cfg.Get()
	rows := web.BuildFindingRows(t, fs, cfg.Jellyfin.URL, cfg.Search.URLTemplate != "")
	web.SortFindingRows(rows, sortKey, dir)
	return rows
}

// handleSearch builds the external search URL server-side (so any API key stays
// out of the rendered page) and redirects the browser to the configured
// provider.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	cfg := s.cfg.Get()
	target := web.BuildSearchURL(cfg.Search.URLTemplate, cfg.Search.APIKey, r.URL.Query().Get("q"))
	if target == "" {
		http.Error(w, "search not configured", http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Server) statusView(ctx context.Context, t *i18n.Translator) web.StatusView {
	st := s.sched.Status()
	sv := web.StatusView{
		State:     st.State,
		Running:   st.State == scheduler.StateRunning,
		LastError: st.LastError,
		LastScan:  web.FormatRelative(t, st.LastFinished),
		NextScan:  web.FormatRelative(t, st.NextRun),
	}
	if sv.Running {
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
				Name: l.Name, Color: web.LibraryColor(l.ID),
				Scanned: l.Scanned, Total: l.Total, Missing: l.Missing,
			})
		}
		return sv
	}

	if run, err := s.store.LatestSuccessfulRun(ctx); err == nil && run != nil {
		sv.ItemsScanned = run.ItemsScanned
		sv.LibrariesScanned = run.LibrariesScanned
		sv.MissingTotal = run.MissingCount
		sv.Duration = web.FormatDuration(run.Duration())
		for _, l := range run.Libraries {
			sv.Libraries = append(sv.Libraries, web.LibraryStatusView{
				Name: l.Name, Color: web.LibraryColor(l.ID),
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
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		http.Error(w, "export failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="waim-sync-state.json"`)
	_, _ = w.Write(data)
}

// redirectBack returns the user to the referring page, or the dashboard.
func redirectBack(w http.ResponseWriter, r *http.Request) {
	target := r.Header.Get("Referer")
	if target == "" {
		target = "/"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
