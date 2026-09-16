package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
	"github.com/daknoblo/waim/internal/tmdbcache"
	"github.com/daknoblo/waim/internal/web"
)

func (s *Server) tmdbClient() *tmdb.Client {
	cfg := s.cfg.Get()
	return tmdb.New(cfg.TMDB.APIKey, cfg.TMDB.Language, cfg.TMDB.Region, cfg.Scan.TMDBRateLimitRPS).WithCache(tmdbcache.New(s.store))
}

func (s *Server) handleCollection(w http.ResponseWriter, r *http.Request) {
	t := s.translator(r)
	entries, revision, err := s.store.VirtualEntries(r.Context())
	if err != nil {
		http.Error(w, t.T("sources.storeError"), 500)
		return
	}
	d := web.CollectionData{Layout: s.layout(r, "collection"), Entries: entries, Query: strings.TrimSpace(r.URL.Query().Get("q")), Kind: r.URL.Query().Get("kind"), Configured: s.cfg.Get().TMDB.APIKey != ""}
	if d.Kind != media.Series {
		d.Kind = media.Movie
	}
	run, err := s.currentRun(r.Context())
	if err != nil {
		http.Error(w, t.T("sources.storeError"), 500)
		return
	}
	latest, err := s.store.LatestRun(r.Context())
	if err != nil {
		http.Error(w, t.T("sources.storeError"), 500)
		return
	}
	d.Updating = run == nil || run.Metadata.Pending || run.Metadata.Revision != revision || s.sched.Running()
	d.Ownership = collectionOwnership(entries, run, latest, s.cfg.Get().Scan.IncludeSpecials, d.Updating, time.Now())
	if d.Query != "" && d.Configured {
		if len(d.Query) > 200 {
			d.Message = t.T("sources.searchError")
		} else {
			results, err := s.tmdbClient().SearchTitles(r.Context(), d.Kind, d.Query)
			if err != nil {
				d.Message = t.T("sources.searchError")
			} else {
				for _, item := range results {
					d.Results = append(d.Results, store.VirtualEntry{Type: d.Kind, TMDBID: item.ID, Title: item.DisplayTitle(), Year: yearNumber(item.Year()), Poster: item.PosterPath})
				}
				d.Searched = true
			}
		}
	}
	s.render(w, r, web.Collection(d))
}

func yearNumber(s string) int {
	if len(s) >= 4 {
		n, _ := strconv.Atoi(s[:4])
		return n
	}
	return 0
}

func watchIdentity(r *http.Request) (store.VirtualEntry, error) {
	if err := r.ParseForm(); err != nil {
		return store.VirtualEntry{}, err
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if id <= 0 || (r.FormValue("kind") != media.Movie && r.FormValue("kind") != media.Series) {
		return store.VirtualEntry{}, strconv.ErrSyntax
	}
	return store.VirtualEntry{Type: r.FormValue("kind"), TMDBID: id}, err
}

func (s *Server) handleWatchAdd(w http.ResponseWriter, r *http.Request) {
	entry, err := watchIdentity(r)
	if err != nil {
		http.Error(w, "invalid media identity", 400)
		return
	}
	td := s.tmdbClient()
	if entry.Type == media.Movie {
		m, e := td.Movie(r.Context(), entry.TMDBID)
		err = e
		if err == nil && m.ID != entry.TMDBID {
			err = strconv.ErrSyntax
		}
		entry.Title, entry.Year, entry.Poster = m.Title, yearNumber(m.ReleaseDate), m.PosterPath
	} else {
		tv, e := td.TV(r.Context(), entry.TMDBID)
		err = e
		if err == nil && tv.ID != entry.TMDBID {
			err = strconv.ErrSyntax
		}
		entry.Title, entry.Year, entry.Poster = tv.Name, yearNumber(tv.FirstAirDate), tv.PosterPath
	}
	if err != nil {
		http.Error(w, s.translator(r).T("sources.searchError"), http.StatusBadGateway)
		return
	}
	if err := s.store.MutateVirtual(r.Context(), entry, false); err != nil {
		http.Error(w, s.translator(r).T("sources.storeError"), 500)
		return
	}
	s.changedCatalog()
	redirectBack(w, r)
}

func (s *Server) handleWatchRemove(w http.ResponseWriter, r *http.Request) {
	entry, err := watchIdentity(r)
	if err != nil {
		http.Error(w, "invalid media identity", 400)
		return
	}
	if err := s.store.MutateVirtual(r.Context(), entry, true); err != nil {
		http.Error(w, s.translator(r).T("sources.storeError"), 500)
		return
	}
	s.changedCatalog()
	redirectBack(w, r)
}
