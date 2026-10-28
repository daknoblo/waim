package web

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/i18n"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestFindingLibraryLabelsIncludeTypeInstanceNameAndEachLibrary(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	refs := []media.Reference{
		{ID: "a", Type: media.Jellyfin, Name: "Living room", LibraryID: "a/movies", LibraryName: "Films", ServerURL: "https://jf.example/jellyfin", ItemID: "one"},
		{ID: "a", Type: media.Jellyfin, Name: "Living room", LibraryID: "a/movies", LibraryName: "Films", ServerURL: "https://jf.example/jellyfin", ItemID: "two", Stale: true},
		{ID: "a", Type: media.Jellyfin, Name: "Living room", LibraryID: "a/series", LibraryName: "Series", ServerURL: "https://jf.example/jellyfin"},
		{ID: "b", Type: "emby", Name: "Archive", LibraryID: "b/movies", LibraryName: "Cinema", ServerURL: "http://emby.example:8096"},
		{ID: "c", Type: "plex", Name: "Home cinema", LibraryID: "c/movies", LibraryName: "Movies", ServerURL: "https://plex.example"},
		{ID: media.VirtualID, Type: media.Virtual, Name: "Watch collection", LibraryID: media.VirtualID, URL: "https://www.themoviedb.org/movie/1"},
	}
	for _, locale := range []string{"en", "de"} {
		tr := cat.For(locale)
		labels := FindingLibraryLabels(tr, refs, "", "")
		want := []string{
			"Jellyfin \u00b7 Living room \u00b7 Films",
			"Jellyfin \u00b7 Living room \u00b7 Series",
			"Emby \u00b7 Archive \u00b7 Cinema",
			"Plex \u00b7 Home cinema \u00b7 Movies",
			tr.T("sources.collection"),
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
		if labels[len(labels)-1].Name != tr.T("sources.collection") {
			t.Fatal("virtual library tooltip retained its legacy name")
		}
	}
}

func TestFindingLibraryLabelsIgnoreServerAndLegacyURLs(t *testing.T) {
	for _, tc := range []struct {
		ref  media.Reference
		want string
	}{
		{media.Reference{Type: media.Jellyfin, URL: "https://jf.example/prefix/web/#/details?id=one"}, "https://jf.example/prefix"},
		{media.Reference{ServerURL: "https://user:secret@jf.example/base?api_key=secret#token"}, "https://jf.example/base"},
		{media.Reference{ServerURL: "javascript:alert(1)"}, ""},
	} {
		tc.ref.Name = "Instance"
		labels := FindingLibraryLabels(testTranslator(t), []media.Reference{tc.ref}, "", "")
		if len(labels) != 1 || !strings.Contains(labels[0].Text, "Instance") || (tc.want != "" && strings.Contains(labels[0].Text, tc.want)) {
			t.Fatal("labels should show the instance name without a server address")
		}
	}
}

func TestCollectionFindingsShowReferencesOnlyInLibraryColumn(t *testing.T) {
	tr := testTranslator(t)
	ref := media.Reference{ID: "a", Type: media.Jellyfin, Name: "Living room", LibraryID: "a/movies", LibraryName: "Films", ServerURL: "https://jf.example", URL: "https://jf.example/web/#/details?id=one"}
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
	if strings.Count(html, "Jellyfin \u00b7 Living room \u00b7 Films") != 1 || strings.Contains(html, "https://jf.example") {
		t.Fatal("expected one expanded library label")
	}
	if !strings.Contains(html, "badge whitespace-nowrap") || strings.Contains(html, "whitespace-normal") || strings.Contains(html, "break-words") {
		t.Fatal("library labels must remain on a single line")
	}
	if !strings.Contains(html, "overflow-x-auto sm:overflow-visible") {
		t.Fatal("long mobile labels must remain accessible without expanding the page")
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

func TestDashboardIncompleteInventoryUsesHeaderInsteadOfRepeatedBanners(t *testing.T) {
	cat, err := i18n.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "de"} {
		tr := cat.For(locale)
		for _, populated := range []bool{true, false} {
			d := DashboardData{
				Layout:    Layout{T: tr, Active: NavDashboard, HealthSeverity: activity.Warning},
				DataState: DataIncomplete,
			}
			wantMessages := 1
			if populated {
				d.Findings = []FindingRow{{Title: "Example", Library: "Films"}}
				wantMessages = 0
			}
			var b bytes.Buffer
			if err := Dashboard(d).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			if got := strings.Count(html, tr.T("sources.incomplete")); got != wantMessages {
				t.Fatalf("%s populated=%v: got %d incomplete messages, want %d", locale, populated, got, wantMessages)
			}
			if strings.Count(html, `data-health="warning"`) != 1 || strings.Contains(html, tr.T("dashboard.noFindings")) {
				t.Fatal("incomplete inventory needs one header indicator, never an all-clear message")
			}
			b.Reset()
			if err := StatusCard(tr, d.Status).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(b.String(), tr.T("sources.incomplete")) {
				t.Fatal("status polling must not restore the duplicate inventory warning")
			}
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
		if row.Title == "Watched" && (len(labels) != 1 || labels[0].Text != tr.T("sources.collection")) {
			t.Fatal("virtual finding lost its source label")
		}
	}
}
