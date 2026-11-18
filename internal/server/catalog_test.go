package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/web"
)

func TestRequestReusesScanUntilNextPoll(t *testing.T) {
	s := featureServer(t)
	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	s.withScanView(func(_ http.ResponseWriter, r *http.Request) {
		if s.dataState(r.Context()) != web.DataNeverScanned {
			t.Fatal("expected initially empty store")
		}
		id, err := s.store.StartScanRun(r.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := s.store.FinishScanRun(r.Context(), id, store.StatusSuccess, "", 0, 0, 0, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		if d := s.statsData(r); d.HasData || d.DataState != web.DataNeverScanned {
			t.Fatal("statistics mixed two scan versions in one request")
		}
	})(httptest.NewRecorder(), req)
	if d := s.statsData(req); !d.HasData || d.DataState != web.DataLegacy {
		t.Fatal("subsequent request did not see the new scan")
	}
}

func TestStatsReadErrorsAreNotEmptyResults(t *testing.T) {
	s := featureServer(t)
	setTestLocale(t, s, "en")
	ctx := t.Context()
	db, err := sql.Open("sqlite", s.store.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// Older malformed metadata breaks the SQL history query but not the current scan.
	for range 2 {
		id, err := s.store.StartScanRun(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.store.FinishScanRun(ctx, id, store.StatusSuccess, "", 0, 0, 0, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`UPDATE scan_runs SET metadata_json = '{' WHERE id = (SELECT min(id) FROM scan_runs)`); err != nil {
		t.Fatal(err)
	}
	render := func() string {
		w := httptest.NewRecorder()
		s.withScanView(s.handleStats)(w, httptest.NewRequest("GET", "/stats", nil))
		return w.Body.String()
	}
	tr := s.catalog.For("en")
	if body := render(); !strings.Contains(body, tr.T("common.stateUnavailable")) || !strings.Contains(body, tr.T("stats.growthHint")) {
		t.Fatal("history error should affect only the growth section")
	}
	if _, err := db.Exec("DROP TABLE findings"); err != nil {
		t.Fatal(err)
	}
	if body := render(); !strings.Contains(body, tr.T("common.stateUnavailable")) || strings.Contains(body, tr.T("stats.growthHint")) {
		t.Fatal("findings error rendered misleading statistics")
	}
	if err := s.store.Close(); err != nil {
		t.Fatal(err)
	}
	if s.dataState(ctx) != web.DataUnavailable {
		t.Fatal("closed store reported as never scanned")
	}
}

func TestCurrentOwnershipDropsDisabledSourceBeforeRecompute(t *testing.T) {
	s := featureServer(t)
	ctx := context.Background()
	src := config.Source{ID: "a", Type: media.Jellyfin, Name: "A", Enabled: true, Jellyfin: config.JellyfinSettings{URL: "https://a.invalid"}, Libraries: []config.Library{{ID: "lib", Enabled: true}}}
	if err := s.cfg.AddSource(src); err != nil {
		t.Fatal(err)
	}
	src, _ = s.cfg.Get().Source("a")
	item := media.Item{ID: "a/title", Type: media.Movie, Name: "Title", ProviderIDs: map[string]string{"Tmdb": "42"}, References: []media.Reference{{ID: "a", Type: media.Jellyfin, LibraryID: "a/lib", URL: "https://a.invalid/web/#/details?id=title"}}}
	if err := s.store.SaveSourceAttempt(ctx, "a", src.Fingerprint(), &media.Snapshot{Items: []media.Item{item}}, ""); err != nil {
		t.Fatal(err)
	}
	v := store.VirtualEntry{Type: media.Movie, TMDBID: 42, Title: "Title"}
	if err := s.store.MutateVirtual(ctx, v, false); err != nil {
		t.Fatal(err)
	}
	id, err := s.store.StartScanRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.store.PublishScan(ctx, id, nil, nil, []store.MediaStat{{Type: store.MediaMovie, Title: "Title", TMDBID: 42, Provenance: store.Provenance{CatalogID: item.ID, References: item.References}}}, nil, store.RunMetadata{Basis: "owned-v1", Mode: "refresh", Revision: 1, SourcesToken: s.cfg.Get().SourcesToken()}); err != nil {
		t.Fatal(err)
	}
	if err := s.cfg.UpdateSource("a", src.Revision, func(src *config.Source) error { src.Enabled = false; return nil }); err != nil {
		t.Fatal(err)
	}
	run, err := s.currentRun(ctx)
	if err != nil || len(run.Media) != 1 || !run.Media[0].WatchOnly || run.ItemsScanned != 0 || !run.Metadata.Pending || len(run.Media[0].References) != 1 || run.Media[0].References[0].ID != media.VirtualID {
		t.Fatalf("disabled source leaked current ownership: %+v %v", run, err)
	}
	if err := s.store.MutateVirtual(ctx, v, true); err != nil {
		t.Fatal(err)
	}
	run, err = s.currentRun(ctx)
	if err != nil || len(run.Media) != 0 {
		t.Fatal("removed observation remained in current results")
	}
}
