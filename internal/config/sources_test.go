package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLegacySourcesMigrationPreservesCiphertext(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	st := storedFromSettings(Defaults())
	st.SchemaVersion = 2
	st.Sources = nil
	st.Jellyfin.URL, st.Jellyfin.UserID = "https://old.example", "alice"
	st.Jellyfin.APIKeyEnc, err = m.cipher.Encrypt("old-secret")
	if err != nil {
		t.Fatal(err)
	}
	st.Libraries = []Library{{ID: "same", Name: "Films", Enabled: true}, {ID: "off", Name: "Other"}}
	original := st.Jellyfin.APIKeyEnc
	b, _ := json.Marshal(st)
	if err := os.WriteFile(m.Path(), b, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		m, err = Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		src, ok := m.Get().Source(LegacySourceID)
		if !ok || src.Jellyfin.APIKey != "old-secret" || src.Jellyfin.UserID != "alice" || len(src.Libraries) != 2 || !src.Libraries[0].Enabled || src.Libraries[1].Enabled {
			t.Fatalf("migration lost settings: %+v", src)
		}
		export, err := m.ExportStored()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(export), "old-secret") || !strings.Contains(string(export), original) {
			t.Fatal("ciphertext was lost or secret leaked")
		}
	}
	if err := os.Remove(filepath.Join(dir, KeyFileName)); err != nil {
		t.Fatal(err)
	}
	m, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	src, _ := m.Get().Source(LegacySourceID)
	if !src.KeyUnreadable || src.Jellyfin.APIKey != "" {
		t.Fatal("unreadable source key not reported")
	}
	if err := m.UpdateSource(src.ID, src.Revision, func(s *Source) error { s.Name = "Renamed"; return nil }); err != nil {
		t.Fatal(err)
	}
	b, err = m.ExportStored()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), original) || !m.KeysUnreadable() {
		t.Fatal("unreadable ciphertext overwritten")
	}
}

func TestSourceUpdatesAreTargetedAndRevisionChecked(t *testing.T) {
	m, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := m.AddSource(Source{ID: id, Type: "jellyfin", Name: id, Enabled: true, Jellyfin: JellyfinSettings{URL: "https://" + id + ".example", APIKey: "key"}}); err != nil {
			t.Fatal(err)
		}
	}
	staleGlobals := m.Get()
	var wg sync.WaitGroup
	for _, id := range []string{"a", "b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := m.UpdateSource(id, 1, func(s *Source) error { s.Name = id + " updated"; return nil }); err != nil {
				t.Error(err)
			}
		}(id)
	}
	wg.Wait()
	if err := m.SaveGlobals(staleGlobals); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		src, _ := m.Get().Source(id)
		if src.Name != id+" updated" {
			t.Fatal("lost concurrent source edit")
		}
	}
	if err := m.UpdateSource("a", 1, func(s *Source) error { return nil }); err == nil {
		t.Fatal("stale source edit accepted")
	}
	if err := m.RemoveSource("virtual", 0); err == nil {
		t.Fatal("virtual collection deleted")
	}
	if err := m.UpdateSource("virtual", 0, func(s *Source) error { s.Enabled = false; return nil }); err == nil {
		t.Fatal("virtual collection disabled")
	}
	if err := m.UpdateSource("a", 2, func(s *Source) error { s.Jellyfin.URL = "https://other.example"; return nil }); err == nil {
		t.Fatal("key rebound to another server")
	}
}
