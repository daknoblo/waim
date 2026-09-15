package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestFindingLibraryLabelsIncludeTypeServerAndEachLibrary(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	refs := []media.Reference{
		{ID: "a", Type: media.Jellyfin, Name: "Living room", LibraryID: "a/movies", LibraryName: "Films", ServerURL: "https://jf.example/jellyfin", ItemID: "one"},
		{ID: "a", Type: media.Jellyfin, Name: "Living room", LibraryID: "a/movies", LibraryName: "Films", ServerURL: "https://jf.example/jellyfin", ItemID: "two", Stale: true},
		{ID: "a", Type: media.Jellyfin, LibraryID: "a/series", LibraryName: "Series", ServerURL: "https://jf.example/jellyfin"},
		{ID: "b", Type: "emby", LibraryID: "b/movies", LibraryName: "Cinema", ServerURL: "http://emby.example:8096"},
		{ID: "c", Type: "plex", LibraryID: "c/movies", LibraryName: "Movies", ServerURL: "https://plex.example"},
		{ID: media.VirtualID, Type: media.Virtual, LibraryID: media.VirtualID, URL: "https://www.themoviedb.org/movie/1"},
	}
	for _, locale := range []string{"en", "de"} {
		tr := cat.For(locale)
		labels := FindingLibraryLabels(tr, refs, "", "")
		want := []string{
			"Jellyfin / https://jf.example/jellyfin / Films",
			"Jellyfin / https://jf.example/jellyfin / Series",
			"Emby / http://emby.example:8096 / Cinema",
			"Plex / https://plex.example / Movies",
			tr.T("sources.typeVirtual") + " / " + tr.T("sources.collection"),
		}
		if len(labels) != len(want) {
			t.Fatalf("lost or duplicated memberships: %+v", labels)
		}
		for i, label := range labels {
			if label.Text != want[i] {
				t.Errorf("label %d = %q, want %q", i, label.Text, want[i])
			}
		}
		if !labels[0].Stale || labels[0].Name != "Living room" {
			t.Fatal("lost stale status or instance name tooltip")
		}
	}
}

func TestFindingLibraryURLLegacyAndSanitization(t *testing.T) {
	for _, tc := range []struct {
		ref  media.Reference
		want string
	}{
		{media.Reference{Type: media.Jellyfin, URL: "https://jf.example/prefix/web/#/details?id=one"}, "https://jf.example/prefix"},
		{media.Reference{ServerURL: "https://user:secret@jf.example/base?api_key=secret#token"}, "https://jf.example/base"},
		{media.Reference{ServerURL: "javascript:alert(1)"}, ""},
	} {
		if got := referenceServerURL(tc.ref); got != tc.want {
			t.Fatalf("server URL = %q, want %q", got, tc.want)
		}
	}
}

func TestCollectionFindingsShowReferencesOnlyInLibraryColumn(t *testing.T) {
	tr := testTranslator(t)
	ref := media.Reference{ID: "a", Type: media.Jellyfin, LibraryID: "a/movies", LibraryName: "Films", ServerURL: "https://jf.example", URL: "https://jf.example/web/#/details?id=one"}
	findings := []store.Finding{{
		Kind: store.KindMissingCollection, MediaType: store.MediaMovie, TMDBID: 100, Title: "Saga",
		LibraryID: "a/movies", LibraryName: "Films", Provenance: store.Provenance{ContextReferences: []media.Reference{ref, ref}},
		Details: `{"missingParts":[{"tmdbId":2,"title":"Missing movie","year":"2025","rating":7.5,"imdbId":"tt1234567"}]}`,
	}}
	rows := BuildFindingRows(tr, findings, "")
	var b bytes.Buffer
	if err := FindingsTable(tr, rows, SortTitle, DirAsc, DataReady).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()
	if strings.Count(html, "Jellyfin / https://jf.example / Films") != 1 {
		t.Fatal("expected one expanded library label")
	}
	for _, forbidden := range []string{tr.T("sources.context"), "/collection/add", "/collection/remove", `href="https://jf.example/web/`} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("unwanted collection context or per-movie watch action: %s", forbidden)
		}
	}
	for _, keep := range []string{"Missing movie", "7.5", "tt1234567", "https://www.themoviedb.org/collection/100"} {
		if !strings.Contains(html, keep) {
			t.Fatalf("lost existing finding content: %s", keep)
		}
	}
}

func TestGroupedSeriesAndVirtualFindingsKeepAllLibraryLabels(t *testing.T) {
	tr := testTranslator(t)
	a := media.Reference{ID: "a", Type: media.Jellyfin, LibraryID: "a/tv", LibraryName: "TV", ServerURL: "https://a.example"}
	b := media.Reference{ID: "b", Type: media.Jellyfin, LibraryID: "b/tv", LibraryName: "TV", ServerURL: "https://b.example"}
	base := store.Finding{Kind: store.KindMissingEpisodes, MediaType: store.MediaSeries, TMDBID: 1, Title: "Show", LibraryID: "a/tv", Details: `{"seasonNumber":1,"missingEpisodes":[1]}`, Provenance: store.Provenance{References: []media.Reference{a}}}
	second := base
	second.References = []media.Reference{b}
	v := store.VirtualEntry{Type: media.Movie, TMDBID: 2, Title: "Watched"}.Item()
	rows := BuildFindingRows(tr, []store.Finding{base, second, {Kind: store.KindMissingMovie, MediaType: store.MediaMovie, TMDBID: 2, Title: "Watched", LibraryID: media.VirtualID, Provenance: store.Provenance{References: v.References}}}, "")
	if len(rows) != 2 {
		t.Fatalf("unexpected row count: %d", len(rows))
	}
	for _, row := range rows {
		labels := FindingLibraryLabels(tr, row.LibraryReferences, row.LibraryID, row.Library)
		if row.Title == "Show" && len(labels) != 2 {
			t.Fatal("grouped seasons lost a source library")
		}
		if row.Title == "Watched" && (len(labels) != 1 || !strings.Contains(labels[0].Text, tr.T("sources.typeVirtual"))) {
			t.Fatal("virtual finding lost its source label")
		}
	}
}
