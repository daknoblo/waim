package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/jellyfin"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/scanner"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

func sourceNumber(n int) *int { return &n }

type episodeTMDB struct{ scanner.TMDBAPI }

func (episodeTMDB) TV(context.Context, int64) (tmdb.TVShow, error) {
	return tmdb.TVShow{ID: 42, Seasons: []tmdb.SeasonSummary{{SeasonNumber: 1, EpisodeCount: 2}}}, nil
}
func (episodeTMDB) Season(context.Context, int64, int) (tmdb.Season, error) {
	return tmdb.Season{SeasonNumber: 1, Episodes: []tmdb.Episode{{EpisodeNumber: 1, AirDate: "2020-01-01"}, {EpisodeNumber: 2, AirDate: "2020-01-02"}}}, nil
}

func episodeServer(t *testing.T, episodes []jellyfin.Item, ignoreFilter bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	filtered := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users":
			_, _ = w.Write([]byte(`[{"Id":"user","Name":"Fixture"}]`))
		case "/Items":
			_, _ = w.Write([]byte(`{"Items":[{"Id":"series","Name":"Owned series","Type":"Series","ProviderIds":{"Tmdb":"42"}}],"TotalRecordCount":1}`))
		case "/Shows/series/Episodes":
			items := append([]jellyfin.Item(nil), episodes...)
			if r.URL.Query().Get("IsMissing") == "false" {
				filtered.Add(1)
			} else {
				items = append(items, jellyfin.Item{ID: "placeholder", IndexNumber: sourceNumber(2), ParentIndexNumber: sourceNumber(1), IsMissing: true})
			}
			if ignoreFilter {
				items = append(items,
					jellyfin.Item{ID: "missing", IndexNumber: sourceNumber(2), ParentIndexNumber: sourceNumber(1), IsMissing: true},
					jellyfin.Item{ID: "virtual", IndexNumber: sourceNumber(3), ParentIndexNumber: sourceNumber(1), IsVirtualItem: true},
					jellyfin.Item{ID: "location", IndexNumber: sourceNumber(4), ParentIndexNumber: sourceNumber(1), LocationType: "Virtual"})
			}
			if err := json.NewEncoder(w).Encode(struct {
				Items            []jellyfin.Item
				TotalRecordCount int
			}{items, len(items)}); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	return server, filtered
}

func episodeSource(id, address string) config.Source {
	return config.Source{ID: id, Type: media.Jellyfin, Name: id, Enabled: true, Jellyfin: config.JellyfinSettings{URL: address, APIKey: "fixture"}, Libraries: []config.Library{{ID: "lib", Name: "Series", Enabled: true}}}
}

func TestEpisodeFilterAndDefensiveNormalizationDoNotOwnPlaceholders(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		t.Run(fmt.Sprint(ignore), func(t *testing.T) {
			server, filtered := episodeServer(t, []jellyfin.Item{{ID: "real", Type: "Episode", IndexNumber: sourceNumber(1), ParentIndexNumber: sourceNumber(1)}}, ignore)
			st, err := store.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			settings := config.Defaults()
			settings.Sources = append(settings.Sources, episodeSource("a", server.URL))
			c, err := Catalog(context.Background(), st, settings, true, nil)
			if err != nil || len(c.Items) != 1 || len(c.Items[0].Episodes) != 1 || filtered.Load() != 1 {
				t.Fatalf("placeholder accepted or filter omitted: %+v %v requests=%d", c, err, filtered.Load())
			}
			result, err := scanner.New(c, episodeTMDB{}, settings, nil).Run(context.Background())
			if err != nil || result.Media[0].Episodes != 1 || len(result.Findings) != 1 {
				t.Fatalf("real gap suppressed: %+v %v", result, err)
			}
			var detail struct{ MissingEpisodes []int }
			if err := json.Unmarshal([]byte(result.Findings[0].Details), &detail); err != nil {
				t.Fatal(err)
			}
			if len(detail.MissingEpisodes) != 1 || detail.MissingEpisodes[0] != 2 {
				t.Fatalf("wrong missing episodes: %+v", detail)
			}
			if ignore && len(result.Warnings) == 0 {
				t.Fatal("ignored filter was not made explicit")
			}
		})
	}
}

func TestCombinedEpisodeRangeUnionsWithOtherSource(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(fmt.Sprint(overlap), func(t *testing.T) {
			server, _ := episodeServer(t, []jellyfin.Item{{ID: "combined", IndexNumber: sourceNumber(1), IndexNumberEnd: sourceNumber(2), ParentIndexNumber: sourceNumber(1)}}, false)
			settings := config.Defaults()
			settings.Sources = append(settings.Sources, episodeSource("a", server.URL))
			if overlap {
				second, _ := episodeServer(t, []jellyfin.Item{{ID: "separate2", IndexNumber: sourceNumber(2), ParentIndexNumber: sourceNumber(1)}}, false)
				settings.Sources = append(settings.Sources, episodeSource("b", second.URL))
			}
			st, err := store.Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = st.Close() }()
			c, err := Catalog(context.Background(), st, settings, true, nil)
			if err != nil || len(c.Items) != 1 || len(c.Items[0].Episodes) != 2 {
				t.Fatalf("combined range union wrong: %+v %v", c, err)
			}
			result, err := scanner.New(c, episodeTMDB{}, settings, nil).Run(context.Background())
			if err != nil || result.Media[0].Episodes != 2 || len(result.Findings) != 0 || len(result.Warnings) != 0 {
				t.Fatalf("combined file created false gaps: %+v %v", result, err)
			}
		})
	}
}

func TestEpisodeRangesAreBoundedAndInvalidMetadataIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end int
		want       int
		warning    bool
	}{
		{"pair", 1, 2, 2, false}, {"reverse", 4, 2, 1, true}, {"huge", 1, math.MaxInt, 1, true}, {"limit", 1, maxEpisodeRange, maxEpisodeRange, false}, {"over-limit", 1, maxEpisodeRange + 1, 1, true}, {"max-single", math.MaxInt, math.MaxInt, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			episodes, warnings := normalizeEpisodes([]jellyfin.Item{{IndexNumber: sourceNumber(tc.start), IndexNumberEnd: sourceNumber(tc.end), ParentIndexNumber: sourceNumber(1)}}, "Series")
			if len(episodes) != tc.want || (len(warnings) > 0) != tc.warning {
				t.Fatalf("bad bounded expansion: count=%d warnings=%v", len(episodes), warnings)
			}
			if *episodes[0].IndexNumber != tc.start {
				t.Fatal("valid start was lost")
			}
			if !tc.warning && *episodes[len(episodes)-1].IndexNumber != tc.end {
				t.Fatal("expanded indices alias one pointer")
			}
		})
	}
	episodes, warnings := normalizeEpisodes([]jellyfin.Item{
		{ParentIndexNumber: sourceNumber(1)},
		{IndexNumber: sourceNumber(1), ParentIndexNumber: sourceNumber(-1)},
		{IndexNumber: sourceNumber(1), IndexNumberEnd: sourceNumber(math.MaxInt), ParentIndexNumber: sourceNumber(1), IsVirtualItem: true},
	}, "Series")
	if len(episodes) != 0 || len(warnings) != 2 {
		t.Fatalf("unidentified/virtual files counted: %+v %v", episodes, warnings)
	}
}

func TestOldEpisodeInventoriesAreUnknownUntilFreshNormalization(t *testing.T) {
	server, filtered := episodeServer(t, []jellyfin.Item{{ID: "real", IndexNumber: sourceNumber(1), ParentIndexNumber: sourceNumber(1)}}, false)
	src := episodeSource("a", server.URL)
	oldIdentity, _ := json.Marshal([]any{src.ID, src.Type, strings.TrimRight(src.Jellyfin.URL, "/"), src.Jellyfin.UserID, src.CredentialGeneration, []string{"lib"}})
	hash := sha256.Sum256(oldIdentity)
	oldFingerprint := hex.EncodeToString(hash[:])
	if oldFingerprint == src.Fingerprint() {
		t.Fatal("incompatible snapshot identity was not versioned")
	}
	virtual := config.VirtualSource()
	virtualIdentity, _ := json.Marshal([]any{virtual.ID, virtual.Type, "", "", "", []string{}})
	virtualHash := sha256.Sum256(virtualIdentity)
	if virtual.Fingerprint() != hex.EncodeToString(virtualHash[:]) {
		t.Fatal("episode normalization changed virtual-only identity")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx := context.Background()
	if err := st.SaveSourceAttempt(ctx, src.ID, oldFingerprint, &media.Snapshot{Items: []media.Item{{
		ID: "old", Type: media.Series, Name: "Unsafe old ownership", ProviderIDs: map[string]string{"Tmdb": "42"},
		Episodes: []media.Item{
			{IndexNumber: sourceNumber(1), ParentIndexNumber: sourceNumber(1)},
			{IndexNumber: sourceNumber(2), ParentIndexNumber: sourceNumber(1)},
		},
	}}}, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.MutateVirtual(ctx, store.VirtualEntry{Type: media.Movie, TMDBID: 9, Title: "Retained virtual entry"}, false); err != nil {
		t.Fatal(err)
	}
	settings := config.Defaults()
	settings.Sources = append(settings.Sources, src)
	c, err := Catalog(ctx, st, settings, false, nil)
	if err != nil || len(c.Items) != 1 || !c.Items[0].WatchOnly || len(c.Warnings) == 0 || filtered.Load() != 0 {
		t.Fatalf("old inventory trusted or virtual data lost: %+v %v", c, err)
	}
	old, err := st.SourceSnapshot(ctx, src.ID, oldFingerprint)
	if err != nil || old.Snapshot == nil {
		t.Fatal("old snapshot was destructively deleted")
	}
	c, err = Catalog(ctx, st, settings, true, func(config.Source) (Adapter, error) {
		return nil, fmt.Errorf("fixture refresh failure")
	})
	if err != nil || len(c.Items) != 1 || !c.Items[0].WatchOnly || len(c.Warnings) == 0 || filtered.Load() != 0 {
		t.Fatal("failed refresh fell back to incompatible episode ownership")
	}
	c, err = Catalog(ctx, st, settings, true, nil)
	if err != nil || len(c.Items) != 2 || len(c.Warnings) != 0 || filtered.Load() != 1 {
		t.Fatalf("fresh normalization did not restore inventory: %+v %v", c, err)
	}
	for _, item := range c.Items {
		if item.Type == media.Series && len(item.Episodes) != 1 {
			t.Fatal("fresh snapshot retained fabricated episodes")
		}
	}
	if settings.Sources[1].Jellyfin.APIKey != "fixture" {
		t.Fatal("normalization altered credentials")
	}
}
