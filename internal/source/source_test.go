package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestJellyfinSnapshotsAndPartialFailures(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("X-Emby-Token") != "test" {
			t.Error("missing auth")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users":
			_, _ = fmt.Fprint(w, `[{"Id":"u","Name":"test"}]`)
		case "/Items":
			_, _ = fmt.Fprint(w, `{"Items":[{"Id":"same","Name":"Show","Type":"Series","ProviderIds":{"Tmdb":"42"}}],"TotalRecordCount":1}`)
		case "/Shows/same/Episodes":
			if fail.Load() {
				http.Error(w, "private upstream body", 500)
				return
			}
			_, _ = fmt.Fprint(w, `{"Items":[{"Id":"episode","Type":"Episode","ParentIndexNumber":1,"IndexNumber":1}],"TotalRecordCount":1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	settings := config.Defaults()
	settings.Sources = nil
	for _, id := range []string{"a", "b"} {
		settings.Sources = append(settings.Sources, config.Source{ID: id, Type: media.Jellyfin, Name: id, Enabled: true, Jellyfin: config.JellyfinSettings{URL: server.URL, APIKey: "test"}, Libraries: []config.Library{{ID: "same", Name: "TV", Enabled: true}}})
	}
	ctx := context.Background()
	c, err := Catalog(ctx, st, settings, true, nil)
	if err != nil || len(c.Items) != 1 || len(c.Items[0].References) != 2 || len(c.Items[0].Episodes) != 1 {
		t.Fatalf("merge failed: %+v %v", c, err)
	}
	if c.Items[0].References[0].LibraryID == c.Items[0].References[1].LibraryID {
		t.Fatal("local libraries collided")
	}
	if c.Items[0].References[0].ServerURL != server.URL {
		t.Fatal("source reference did not retain its server address")
	}
	fail.Store(true)
	c, err = Catalog(ctx, st, settings, true, nil)
	if err != nil || len(c.Items) != 1 || len(c.Items[0].Episodes) != 1 || len(c.Warnings) != 2 {
		t.Fatalf("partial failure destroyed ownership: %+v %v", c, err)
	}
	before := calls.Load()
	v := store.VirtualEntry{Type: media.Series, TMDBID: 42, Title: "Show"}
	if err := st.MutateVirtual(ctx, v, false); err != nil {
		t.Fatal(err)
	}
	c, err = Catalog(ctx, st, settings, false, nil)
	if err != nil || calls.Load() != before || len(c.Items[0].References) != 3 || c.Items[0].WatchOnly {
		t.Fatal("recompute scanned server or lost virtual membership")
	}
	settings.Sources[0].Enabled = false
	settings.Sources[1].Jellyfin.UserID = "different"
	c, err = Catalog(ctx, st, settings, false, nil)
	if err != nil || len(c.Items) != 1 || !c.Items[0].WatchOnly || len(c.Warnings) != 1 {
		t.Fatalf("disabled/changed source leaked ownership: %+v %v", c, err)
	}
}
