package scanner

import (
	"context"
	"fmt"
	"testing"

	"github.com/daknoblo/waim/internal/activity"
	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
	"github.com/daknoblo/waim/internal/tmdb"
)

type membershipReporter struct {
	total, done map[string]int
}

func (*membershipReporter) SetCurrent(string)                      {}
func (r *membershipReporter) LibraryStart(id, _ string, total int) { r.total[id] = total }
func (r *membershipReporter) ItemDone(id string, _ int)            { r.done[id]++ }

type membershipTMDB struct {
	TMDBAPI
	reporter *membershipReporter
	tracker  *activity.Tracker
	calls    int
	t        *testing.T
}

func (f *membershipTMDB) Movie(_ context.Context, id int64) (tmdb.Movie, error) {
	for _, lib := range []string{"a/lib", "b/lib", media.VirtualID} {
		if f.reporter.total[lib] != 10 || f.reporter.done[lib] != f.calls {
			f.t.Errorf("library %s not live before title %d: total=%d done=%d", lib, f.calls, f.reporter.total[lib], f.reporter.done[lib])
		}
	}
	state := f.tracker.Snapshot()[0]
	if state.Total != 10 || state.Done != f.calls {
		f.t.Errorf("global progress duplicated memberships: %+v", state)
	}
	f.calls++
	return tmdb.Movie{ID: id, Title: fmt.Sprint(id)}, nil
}

func TestLiveLibraryCountersCountMembershipsButGlobalWorkIsUnique(t *testing.T) {
	c := media.Catalog{Libraries: []media.Library{{ID: "a/lib"}, {ID: "b/lib"}, {ID: media.VirtualID}}}
	for i := int64(1); i <= 10; i++ {
		for _, lib := range []string{"a/lib", "b/lib", media.VirtualID} {
			kind := media.Jellyfin
			if lib == media.VirtualID {
				kind = media.Virtual
			}
			ref := media.Reference{ID: lib, Type: kind, LibraryID: lib, ItemID: fmt.Sprint(i)}
			c.Items = append(c.Items, media.Item{ID: fmt.Sprintf("%s/%d", lib, i), Type: media.Movie, Name: fmt.Sprint(i), ProviderIDs: map[string]string{"Tmdb": fmt.Sprint(i)}, WatchOnly: kind == media.Virtual, References: []media.Reference{ref, ref}})
		}
		// Another physical occurrence in A must not increment A twice.
		c.Items = append(c.Items, media.Item{ID: fmt.Sprintf("a/duplicate/%d", i), Type: media.Movie, ProviderIDs: map[string]string{"Tmdb": fmt.Sprint(i)}, References: []media.Reference{{ID: "a/lib", Type: media.Jellyfin, LibraryID: "a/lib", ItemID: fmt.Sprintf("duplicate%d", i)}}})
	}
	tracker := activity.New()
	run := tracker.Start(activity.Scan)
	reporter := &membershipReporter{total: map[string]int{}, done: map[string]int{}}
	td := &membershipTMDB{reporter: reporter, tracker: tracker, t: t}
	sc := New(c, td, config.Defaults(), nil)
	sc.SetReporter(reporter)
	result, err := sc.Run(activity.WithRun(context.Background(), run))
	if err != nil {
		t.Fatal(err)
	}
	if td.calls != 10 || result.ItemsScanned != 10 || len(result.Media) != 10 || tracker.Snapshot()[0].Done != 10 {
		t.Fatalf("global title work inflated: calls=%d result=%+v", td.calls, result)
	}
	for _, lib := range result.Libraries {
		if reporter.total[lib.ID] != 10 || reporter.done[lib.ID] != 10 || lib.Total != 10 || lib.Scanned != 10 {
			t.Fatalf("live/final library counters differ: %+v", lib)
		}
	}
	for _, m := range result.Media {
		if m.WatchOnly || m.Type != store.MediaMovie {
			t.Fatal("real ownership lost to virtual membership")
		}
	}
}
