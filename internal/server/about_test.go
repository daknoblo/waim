package server

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/version"
)

func TestAboutReleaseLinks(t *testing.T) {
	s := newTestServer(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "waim.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	catalog, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.store = st
	s.catalog = catalog
	s.log = slog.New(slog.NewTextHandler(io.Discard, nil))

	for _, tc := range []struct {
		build   string
		release bool
	}{
		{"1.5.0", true},
		{"1.5.1", true},
		{"dev-20260915-1200", false},
		{"stable-20260915-1200", false},
	} {
		t.Run(tc.build, func(t *testing.T) {
			s.info = version.Info{Version: tc.build, Commit: "abc1234"}
			rec := httptest.NewRecorder()
			s.handleAbout(rec, httptest.NewRequest("GET", "/about", nil))
			link := `href="` + repoURL + "/releases/tag/" + tc.build + `"`
			if got := strings.Contains(rec.Body.String(), link); got != tc.release {
				t.Errorf("release link present = %v, want %v", got, tc.release)
			}
			if !tc.release && strings.Contains(rec.Body.String(), "/releases/tag/") {
				t.Error("branch build should not link to a tagged release")
			}
		})
	}
}
