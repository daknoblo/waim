package config

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/daknoblo/waim/internal/media"
)

func TestLegacyVirtualSourceNameUpdatesWithoutRenamingUserSources(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.AddSource(Source{ID: "real", Type: media.Jellyfin, Name: "Watch collection", Enabled: true, Jellyfin: JellyfinSettings{URL: "https://jf.example", APIKey: "fixture-key"}}); err != nil {
		t.Fatal(err)
	}
	data, err := m.ExportStored()
	if err != nil {
		t.Fatal(err)
	}
	var disk stored
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	for i := range disk.Sources {
		if disk.Sources[i].ID == media.VirtualID {
			disk.Sources[i].Name = "Watch collection"
		}
	}
	data, err = json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Path(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		m, err = Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		v, ok := m.Get().Source(media.VirtualID)
		if !ok || v.Name != media.VirtualName || !v.Enabled || len(m.Get().Sources) != 2 {
			t.Fatalf("virtual source rename changed identity or count: %+v", v)
		}
		real, _ := m.Get().Source("real")
		if real.Name != "Watch collection" || real.Jellyfin.APIKey != "fixture-key" {
			t.Fatal("renaming the built-in collection affected a user source")
		}
	}
	data, err = m.ExportStored()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	for _, src := range disk.Sources {
		if src.ID == media.VirtualID && src.Name != media.VirtualName {
			t.Fatal("configuration export retained the old built-in name")
		}
	}
}
