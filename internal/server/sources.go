package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) changedCatalog() {
	s.sched.Recompute()
	s.suggest.Invalidate()
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	s.renderSources(w, r, "", false)
}

func (s *Server) renderSources(w http.ResponseWriter, r *http.Request, message string, failed bool) {
	d := web.SourcesData{Layout: s.layout(r, "sources"), Sources: s.cfg.Get().Redacted().Sources, Message: message, Failed: failed}
	for _, src := range s.cfg.Get().Sources {
		if src.Type == media.Virtual || !src.Enabled {
			continue
		}
		snapshot, err := s.store.SourceSnapshot(r.Context(), src.ID, src.Fingerprint())
		if err != nil {
			d.Message = s.translator(r).T("sources.storeError")
			d.Failed = true
			break
		}
		if snapshot.Snapshot == nil {
			d.Warnings = append(d.Warnings, src.Name+": "+s.translator(r).T("sources.unknown"))
		} else if snapshot.Error != "" {
			d.Warnings = append(d.Warnings, src.Name+": "+s.translator(r).T("sources.stale"))
		}
	}
	s.render(w, r, web.Sources(d))
}

func (s *Server) handleAddSource(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		http.Error(w, "ID generation failed", 500)
		return
	}
	src := config.Source{ID: hex.EncodeToString(id[:]), Type: media.Jellyfin, Name: strings.TrimSpace(r.FormValue("name")), Enabled: true, Jellyfin: config.JellyfinSettings{URL: strings.TrimRight(strings.TrimSpace(r.FormValue("url")), "/"), APIKey: strings.TrimSpace(r.FormValue("key")), UserID: strings.TrimSpace(r.FormValue("user"))}}
	if err := s.cfg.AddSource(src); err != nil {
		s.renderSources(w, r, err.Error(), true)
		return
	}
	s.changedCatalog()
	saved, ok := s.cfg.Get().Source(src.ID)
	if !ok {
		http.Error(w, "source not found", http.StatusNotFound)
		return
	}
	if err := s.refreshSourceLibraries(r.Context(), saved); err != nil {
		s.log.Warn("initial library discovery failed", "sourceId", src.ID, "err", err)
		s.renderSources(w, r, s.translator(r).T("sources.addedLibrariesFailed"), true)
		return
	}
	s.changedCatalog()
	http.Redirect(w, r, "/sources", http.StatusSeeOther)
}

func sourceRevision(r *http.Request) (int64, error) {
	if err := r.ParseForm(); err != nil {
		return 0, err
	}
	return strconv.ParseInt(r.FormValue("revision"), 10, 64)
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	rev, err := sourceRevision(r)
	if err != nil {
		http.Error(w, "invalid revision", 400)
		return
	}
	err = s.cfg.UpdateSourceWithKey(r.PathValue("id"), rev, strings.TrimSpace(r.FormValue("key")), func(src *config.Source) error {
		address := strings.TrimRight(strings.TrimSpace(r.FormValue("url")), "/")
		key := strings.TrimSpace(r.FormValue("key"))
		if address != src.Jellyfin.URL && key == "" {
			return errors.New("enter the API key for the new server address")
		}
		src.Name = strings.TrimSpace(r.FormValue("name"))
		src.Enabled = r.FormValue("enabled") == "on"
		if address != src.Jellyfin.URL {
			src.Libraries = nil
		}
		src.Jellyfin.URL, src.Jellyfin.UserID = address, strings.TrimSpace(r.FormValue("user"))
		if key != "" {
			src.Jellyfin.APIKey = key
		}
		selected := map[string]bool{}
		for _, id := range r.Form["library"] {
			selected[id] = true
		}
		for i := range src.Libraries {
			src.Libraries[i].Enabled = selected[src.Libraries[i].ID]
		}
		return nil
	})
	if err != nil {
		s.renderSources(w, r, err.Error(), true)
		return
	}
	s.changedCatalog()
	http.Redirect(w, r, "/sources", http.StatusSeeOther)
}

func (s *Server) handleRemoveSource(w http.ResponseWriter, r *http.Request) {
	rev, err := sourceRevision(r)
	if err != nil || r.FormValue("confirm") != "yes" {
		http.Error(w, "explicit confirmation required", 400)
		return
	}
	if err := s.cfg.RemoveSource(r.PathValue("id"), rev); err != nil {
		s.renderSources(w, r, err.Error(), true)
		return
	}
	s.changedCatalog()
	http.Redirect(w, r, "/sources", http.StatusSeeOther)
}

func (s *Server) handleSourceLibraries(w http.ResponseWriter, r *http.Request) {
	src, ok := s.cfg.Get().Source(r.PathValue("id"))
	if !ok || src.Type == media.Virtual {
		http.NotFound(w, r)
		return
	}
	if err := s.refreshSourceLibraries(r.Context(), src); err != nil {
		s.log.Warn("library refresh failed", "sourceId", src.ID, "err", err)
		s.renderSources(w, r, s.translator(r).T("sources.librariesFailed"), true)
		return
	}
	s.changedCatalog()
	http.Redirect(w, r, "/sources", http.StatusSeeOther)
}

func (s *Server) refreshSourceLibraries(ctx context.Context, src config.Source) (resultErr error) {
	run := s.activities.Start(activity.Sources)
	ctx = activity.WithRun(ctx, run)
	run.Phase(activity.Inventory, -1)
	run.Subject(src.Name)
	run.Operation(activity.Libraries)
	defer func() {
		status := activity.Completed
		if resultErr != nil {
			status = activity.Failed
		}
		run.Finish(ctx, status, 0)
	}()
	adapter, err := source.New(src)
	var libs []media.Library
	if err == nil {
		libs, err = adapter.Libraries(ctx)
	}
	if err != nil {
		return err
	}
	run.Phase(activity.Persistence, -1)
	run.Subject(src.Name)
	return s.cfg.UpdateSource(src.ID, src.Revision, func(current *config.Source) error {
		enabled := map[string]bool{}
		for _, lib := range current.Libraries {
			enabled[lib.ID] = lib.Enabled
		}
		current.Libraries = nil
		for _, lib := range libs {
			current.Libraries = append(current.Libraries, config.Library{ID: lib.ID, Name: lib.Name, Type: lib.Type, Enabled: enabled[lib.ID]})
		}
		return nil
	})
}

func (s *Server) handleTestSource(w http.ResponseWriter, r *http.Request) {
	src, ok := s.cfg.Get().Source(r.PathValue("id"))
	if !ok || src.Type == media.Virtual {
		http.NotFound(w, r)
		return
	}
	run := s.activities.Start(activity.Sources)
	run.Phase(activity.Inventory, -1)
	run.Subject(src.Name)
	run.Operation(activity.Test)
	status := activity.Failed
	defer func() { run.Finish(r.Context(), status, 0) }()
	adapter, err := source.New(src)
	if err == nil {
		err = adapter.(source.Tester).Test(r.Context())
	}
	if err != nil {
		s.renderSources(w, r, s.translator(r).T("sources.connectionError"), true)
		return
	}
	status = activity.Completed
	s.renderSources(w, r, s.translator(r).T("sources.connectionOK"), false)
}
