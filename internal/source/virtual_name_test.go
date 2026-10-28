package source

import (
	"context"
	"testing"

	"github.com/daknoblo/waim/internal/config"
	"github.com/daknoblo/waim/internal/media"
	"github.com/daknoblo/waim/internal/store"
)

func TestVirtualCollectionUsesConsistentNameAndIdentity(t *testing.T) {
	if config.VirtualSource().Name != media.VirtualName {
		t.Fatal("configuration name differs from the virtual source")
	}
	entry := store.VirtualEntry{Type: media.Movie, TMDBID: 42, Title: "Tracked film"}
	snapshot, err := Virtual([]store.VirtualEntry{entry}).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Libraries) != 1 || snapshot.Libraries[0].ID != media.VirtualID || snapshot.Libraries[0].Name != media.VirtualName {
		t.Fatal("virtual library uses an inconsistent name or identity")
	}
	if len(snapshot.Items) != 1 || !snapshot.Items[0].WatchOnly || snapshot.Items[0].TMDBID() != 42 {
		t.Fatal("collection rename changed tracking behavior")
	}
	ref := snapshot.Items[0].References[0]
	if ref.ID != media.VirtualID || ref.Name != media.VirtualName || ref.LibraryName != media.VirtualName {
		t.Fatal("virtual provenance uses an inconsistent name or identity")
	}
}
