package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/source"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
	"github.com/daknoblo/waim/internal/web"
)

type auditSeriesTMDB struct {
	scanner.TMDBAPI
	seasons int
}

func (f auditSeriesTMDB) TV(context.Context, int64) (tmdb.TVShow, error) {
	tv := tmdb.TVShow{ID: 42, Name: "Owned series", EpisodeRunTime: []int{25}}
	for sn := 1; sn <= f.seasons; sn++ {
		tv.Seasons = append(tv.Seasons, tmdb.SeasonSummary{SeasonNumber: sn, EpisodeCount: 2})
	}
	return tv, nil
}
func (auditSeriesTMDB) Season(_ context.Context, _ int64, sn int) (tmdb.Season, error) {
	return tmdb.Season{SeasonNumber: sn, Episodes: []tmdb.Episode{{EpisodeNumber: 1, AirDate: "2020-01-01"}, {EpisodeNumber: 2, AirDate: "2020-01-02"}}}, nil
}

func auditEpisode(sn, en int) media.Item { return media.Item{ParentIndexNumber: &sn, IndexNumber: &en} }

func auditSource(t *testing.T, s *Server, items []media.Item) config.Source {
	t.Helper()
	if err := s.cfg.AddSource(config.Source{ID: "audit", Type: media.Jellyfin, Name: "Audit source", Enabled: true, Libraries: []config.Library{{ID: "lib", Name: "Series", Enabled: true}}}); err != nil {
		t.Fatal(err)
	}
	src, _ := s.cfg.Get().Source("audit")
	for i := range items {
		items[i].References = []media.Reference{{ID: src.ID, Type: media.Jellyfin, Name: src.Name, LibraryID: "audit/lib", LibraryName: "Series", ItemID: items[i].ID}}
	}
	if err := s.store.SaveSourceAttempt(context.Background(), src.ID, src.Fingerprint(), &media.Snapshot{Libraries: []media.Library{{ID: "audit/lib", Name: "Series"}}, Items: items}, ""); err != nil {
		t.Fatal(err)
	}
	return src
}

func auditPublish(t *testing.T, s *Server, c media.Catalog, result scanner.Result) {
	t.Helper()
	ctx := context.Background()
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.PublishScan(ctx, id, result.Findings, result.Libraries, result.Media, result.Upcoming, store.RunMetadata{Basis: "owned-v1", Mode: "refresh", Revision: c.Revision, SourcesToken: s.cfg.Get().SourcesToken(), Warnings: result.Warnings}); err != nil {
		t.Fatal(err)
	}
}

func TestUnchangedSeriesOverlayStaysReadyAfterAllSeasons(t *testing.T) {
	for _, seasons := range []int{1, 3} {
		t.Run(fmt.Sprint(seasons), func(t *testing.T) {
			s := featureServer(t)
			item := media.Item{ID: "audit/series", Type: media.Series, Name: "Owned series", ProviderIDs: map[string]string{"Tmdb": "42"}}
			for sn := 1; sn <= seasons; sn++ {
				item.Episodes = append(item.Episodes, auditEpisode(sn, 1))
			}
			src := auditSource(t, s, []media.Item{item})
			ctx := context.Background()
			c, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := scanner.New(c, auditSeriesTMDB{seasons: seasons}, s.cfg.Get(), nil).Run(ctx)
			if err != nil {
				t.Fatal(err)
			}
			auditPublish(t, s, c, result)
			current, err := s.currentRun(ctx)
			if err != nil || current.Metadata.Pending || current.Media[0].Unconfirmed || current.Media[0].Episodes != seasons || s.dataState(ctx) != web.DataReady {
				t.Fatalf("unchanged series became pending: %+v %v", current, err)
			}
			stats := s.statsData(httptest.NewRequest("GET", "/stats", nil))
			if stats.EpisodePct != 50 || stats.Unconfirmed || stats.Completion[0].Pct != 50 {
				t.Fatalf("confirmed completeness hidden: %+v", stats)
			}
			rendered := httptest.NewRecorder()
			s.Handler().ServeHTTP(rendered, httptest.NewRequest("GET", "/stats", nil))
			if !strings.Contains(rendered.Body.String(), `data-pct="50"`) || !strings.Contains(rendered.Body.String(), "50%") {
				t.Fatal("ready series percentage was not rendered")
			}
			saved, err := s.store.SourceSnapshot(ctx, src.ID, src.Fingerprint())
			if err != nil {
				t.Fatal(err)
			}
			saved.Snapshot.Items[0].Episodes = saved.Snapshot.Items[0].Episodes[1:]
			if err := s.store.SaveSourceAttempt(ctx, src.ID, src.Fingerprint(), saved.Snapshot, ""); err != nil {
				t.Fatal(err)
			}
			current, err = s.currentRun(ctx)
			if err != nil || !current.Metadata.Pending || current.Media[0].Episodes != seasons-1 {
				t.Fatalf("real removal was not pending: %+v %v", current, err)
			}
			stats = s.statsData(httptest.NewRequest("GET", "/stats", nil))
			if stats.EpisodePct != -1 || !stats.Unconfirmed {
				t.Fatal("stale completeness presented as verified")
			}
		})
	}
}

func TestUnresolvedSeriesPublicationAndOverlayRetainRawOwnership(t *testing.T) {
	for _, specials := range []bool{false, true} {
		t.Run(fmt.Sprint(specials), func(t *testing.T) {
			s := featureServer(t)
			settings := s.cfg.Get()
			settings.Scan.IncludeSpecials = specials
			if err := s.cfg.Save(settings); err != nil {
				t.Fatal(err)
			}
			auditSource(t, s, []media.Item{{ID: "audit/unresolved", Type: media.Series, Name: "Unresolved series", Episodes: []media.Item{auditEpisode(0, 1), auditEpisode(1, 1), auditEpisode(1, 2), auditEpisode(1, 2)}}})
			ctx := context.Background()
			c, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := scanner.New(c, nil, s.cfg.Get(), nil).Run(ctx)
			want := 2
			if specials {
				want++
			}
			if err != nil || len(result.Media) != 1 || result.Media[0].Episodes != want || result.Media[0].TMDBID != 0 || result.Media[0].CatalogID != "audit/unresolved" || !result.Media[0].MetadataUnavailable || len(result.Warnings) == 0 {
				t.Fatalf("unresolved ownership lost: %+v %v", result, err)
			}
			auditPublish(t, s, c, result)
			current, err := s.currentRun(ctx)
			if err != nil || current.Media[0].Episodes != want || !current.Media[0].Unconfirmed || current.Media[0].CatalogID != "audit/unresolved" {
				t.Fatalf("overlay lost unresolved ownership: %+v %v", current, err)
			}
			// A pre-fix saved statistic may have omitted its raw season counts.
			result.Media[0].Episodes = 0
			result.Media[0].Seasons = nil
			auditPublish(t, s, c, result)
			current, err = s.currentRun(ctx)
			if err != nil || current.Media[0].Episodes != want || !current.Metadata.Pending {
				t.Fatal("legacy unresolved result erased raw inventory")
			}
		})
	}
}

func TestUpcomingCollectionUsesOnlyCurrentProvenance(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, direct := range []bool{false, true} {
			t.Run(fmt.Sprintf("remove=%t/direct=%t", remove, direct), func(t *testing.T) {
				s := featureServer(t)
				ctx := context.Background()
				src := auditSource(t, s, []media.Item{{ID: "audit/movie", Type: media.Movie, Name: "Owned member", ProviderIDs: map[string]string{"Tmdb": "7"}}})
				if err := s.store.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 8, Title: "Virtual member"}, false); err != nil {
					t.Fatal(err)
				}
				if direct {
					if err := s.store.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 9, Title: "Directly watched part"}, false); err != nil {
						t.Fatal(err)
					}
				}
				c, err := source.Catalog(ctx, s.store, s.cfg.Get(), false, nil)
				if err != nil {
					t.Fatal(err)
				}
				var stats []store.MediaStat
				var oldRefs []media.Reference
				for _, item := range c.Items {
					stats = append(stats, store.MediaStat{Type: store.MediaMovie, TMDBID: item.TMDBID(), CollectionID: 99, Provenance: store.Provenance{CatalogID: item.ID, References: item.References, WatchOnly: item.WatchOnly}})
					oldRefs = append(oldRefs, item.References...)
				}
				up := store.UpcomingItem{Kind: store.UpcomingCollectionPart, MediaType: store.MediaMovie, Title: "Future part", TMDBID: 9, SourceTMDBID: 99, LibraryID: "audit/lib", LibraryName: "Old source library", Provenance: store.Provenance{ContextReferences: oldRefs}}
				auditPublish(t, s, c, scanner.Result{Media: stats, Upcoming: []store.UpcomingItem{up}})
				if remove {
					err = s.cfg.RemoveSource(src.ID, src.Revision)
				} else {
					err = s.cfg.UpdateSource(src.ID, src.Revision, func(src *config.Source) error { src.Enabled = false; return nil })
				}
				if err != nil {
					t.Fatal(err)
				}
				check := func(run *store.ScanRun) {
					t.Helper()
					if len(run.Upcoming) != 1 {
						t.Fatalf("lost current collection context: %+v", run)
					}
					u := run.Upcoming[0]
					refs := u.ContextReferences
					if direct {
						refs = u.References
						if len(u.ContextReferences) != 0 {
							t.Fatal("collection context overrode direct title")
						}
					}
					if !u.WatchOnly || len(refs) != 1 || refs[0].ID != media.VirtualID || u.LibraryID != media.VirtualID || !u.Unconfirmed {
						t.Fatalf("stale upcoming provenance: %+v", u)
					}
					for _, ref := range append(u.References, u.ContextReferences...) {
						if ref.ID == "audit" {
							t.Fatal("removed real provenance survived")
						}
					}
				}
				current, err := s.currentRun(ctx)
				if err != nil {
					t.Fatal(err)
				}
				check(current)
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/export/sync", nil))
				var exported store.SyncState
				if err := json.Unmarshal(w.Body.Bytes(), &exported); err != nil {
					t.Fatal(err)
				}
				check(exported.Run)
				if strings.Contains(w.Body.String(), "Old source library") {
					t.Fatal("obsolete upcoming library exported")
				}
			})
		}
	}
}
