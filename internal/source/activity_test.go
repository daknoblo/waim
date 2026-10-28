package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestLiveFirstInventoryIncludesLibraryAndEpisodePagination(t *testing.T) {
	entered := make(chan string)
	release := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case entered <- r.URL.Path:
		case <-r.Context().Done():
			return
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		switch r.URL.Path {
		case "/Users":
			_, _ = fmt.Fprint(w, `[{"Id":"user","Name":"Test"}]`)
		case "/Items":
			if r.URL.Query().Get("StartIndex") == "0" {
				_, _ = fmt.Fprint(w, `{"TotalRecordCount":2,"Items":[{"Id":"movie","Name":"Movie","Type":"Movie","ProviderIds":{"Tmdb":"7"}}]}`)
			} else {
				_, _ = fmt.Fprint(w, `{"TotalRecordCount":2,"Items":[{"Id":"series","Name":"Series title","Type":"Series","ProviderIds":{"Tmdb":"8"}}]}`)
			}
		case "/Shows/series/Episodes":
			_, _ = fmt.Fprint(w, `{"TotalRecordCount":2,"Items":[{"Id":"ep","Type":"Episode","IndexNumber":1,"ParentIndexNumber":1}]}`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	settings := config.Defaults()
	settings.Sources = append(settings.Sources, config.Source{
		ID: "home", Name: "Home", Type: media.Jellyfin, Enabled: true,
		Jellyfin:  config.JellyfinSettings{URL: upstream.URL, APIKey: "secret"},
		Libraries: []config.Library{{ID: "library", Name: "Family library", Enabled: true}},
	})
	tracker := activity.New()
	run := tracker.Start(activity.Scan)
	ctx = activity.WithRun(ctx, run)
	done := make(chan error, 1)
	go func() {
		catalog, err := Catalog(ctx, st, settings, true, nil)
		if err == nil && len(catalog.Items) != 2 {
			err = fmt.Errorf("expected 2 unique titles, got %d", len(catalog.Items))
		}

		done <- err
	}()
	for i, expected := range []struct {
		path string
		page int
		op   activity.Operation
	}{
		{"/Users", 0, activity.Account},
		{"/Items", 1, activity.Libraries},
		{"/Items", 2, activity.Libraries},
		{"/Shows/series/Episodes", 1, activity.Episodes},
		{"/Shows/series/Episodes", 2, activity.Episodes},
	} {
		select {
		case path := <-entered:
			s := tracker.Snapshot()[0]
			if path != expected.path || s.Status != activity.Running || s.Phase != activity.Inventory || s.Known || s.Page != expected.page || s.Operation != expected.op {
				t.Errorf("request %d reported incorrectly: path=%s state=%+v", i, path, s)
			}
			if i > 0 && s.Subject != "Home · Family library" {
				t.Errorf("library context absent: %+v", s)
			}
			if i > 2 && s.Current != "Series title" {
				t.Errorf("series context absent: %+v", s)
			}
			release <- struct{}{}
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatal("inventory request never started")
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("inventory did not stop")
	}
}
