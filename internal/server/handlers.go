package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/scheduler"
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

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	d := web.AboutData{
		Layout:    s.layout(r, web.NavAbout),
		Version:   s.info.Version,
		Commit:    s.info.Commit,
		BuildDate: s.info.Date,
		GoVersion: s.info.GoVer,
		Repo:      repoURL,
	}
	s.render(w, r, web.About(d))
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
	s.render(w, r, web.FindingsTable(t, s.findingRows(r.Context(), t)))
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
	return web.DashboardData{
		Layout:   s.layout(r, web.NavDashboard),
		Status:   s.statusView(r.Context(), t),
		Findings: s.findingRows(r.Context(), t),
		Logs:     web.BuildLogViews(s.logs.Entries()),
	}
}

func (s *Server) findingRows(ctx context.Context, t *i18n.Translator) []web.FindingRow {
	run, err := s.store.LatestSuccessfulRun(ctx)
	if err != nil || run == nil {
		return nil
	}
	fs, err := s.store.FindingsForRun(ctx, run.ID)
	if err != nil {
		return nil
	}
	return web.BuildFindingRows(t, fs)
}

func (s *Server) statusView(ctx context.Context, t *i18n.Translator) web.StatusView {
	st := s.sched.Status()
	sv := web.StatusView{
		State:     st.State,
		Running:   st.State == scheduler.StateRunning,
		LastError: st.LastError,
		LastScan:  web.FormatTime(t, st.LastFinished),
		NextScan:  web.FormatTime(t, st.NextRun),
	}
	if sv.Running {
		sv.StateLabel = t.T("dashboard.state.running")
	} else {
		sv.StateLabel = t.T("dashboard.state.idle")
	}
	if run, err := s.store.LatestSuccessfulRun(ctx); err == nil && run != nil {
		sv.ItemsScanned = run.ItemsScanned
		sv.LibrariesScanned = run.LibrariesScanned
		sv.MissingTotal = run.MissingCount
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
