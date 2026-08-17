package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadGeneratesKeyAndRoundTripsSecrets(t *testing.T) {
	dir := t.TempDir()

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !m.KeyCreated() {
		t.Fatal("first Load should generate the encryption key")
	}
	if m.KeysUnreadable() {
		t.Fatal("a fresh config has no unreadable keys")
	}
	if _, err := os.Stat(filepath.Join(dir, KeyFileName)); err != nil {
		t.Fatalf("key file missing: %v", err)
	}

	s := m.Get()
	s.Jellyfin.URL = "http://jellyfin.local:8096"
	s.Jellyfin.APIKey = "jf-secret"
	s.TMDB.APIKey = "tmdb-secret"
	if err := m.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "jf-secret") || strings.Contains(string(raw), "tmdb-secret") {
		t.Fatal("api keys were written in plaintext")
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.KeyCreated() {
		t.Fatal("reload should reuse the existing key file")
	}
	if reloaded.KeysUnreadable() {
		t.Fatal("reload should decrypt the stored keys")
	}
	got := reloaded.Get()
	if got.Jellyfin.APIKey != "jf-secret" || got.TMDB.APIKey != "tmdb-secret" {
		t.Fatalf("api keys did not survive a reload: %+v", got.Redacted())
	}
}

func TestLoadReportsUnreadableKeysAfterKeyLoss(t *testing.T) {
	dir := t.TempDir()

	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := m.Get()
	s.TMDB.APIKey = "tmdb-secret"
	if err := m.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Simulate a lost data volume: the config survives, the key does not.
	if err := os.Remove(filepath.Join(dir, KeyFileName)); err != nil {
		t.Fatalf("remove key file: %v", err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if !reloaded.KeysUnreadable() {
		t.Fatal("stale ciphertext should be reported as unreadable")
	}
	if reloaded.Get().TMDB.APIKey != "" {
		t.Fatal("undecryptable key should not surface as a value")
	}

	// Re-entering the key clears the warning.
	s = reloaded.Get()
	s.TMDB.APIKey = "tmdb-new"
	if err := reloaded.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if reloaded.KeysUnreadable() {
		t.Fatal("saving fresh keys should clear the warning")
	}
}

func TestLoadFailsOnCorruptKeyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), []byte("garbage"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("Load should fail instead of replacing a corrupt key file")
	}
}

func TestExportStoredKeepsSecretsEncrypted(t *testing.T) {
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s := m.Get()
	s.TMDB.APIKey = "tmdb-secret"
	if err := m.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := m.ExportStored()
	if err != nil {
		t.Fatalf("ExportStored: %v", err)
	}
	if strings.Contains(string(data), "tmdb-secret") {
		t.Fatal("export leaked a plaintext api key")
	}

	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("export is not valid json: %v", err)
	}
	if _, ok := out["salt"]; ok {
		t.Fatal("export still contains the removed salt field")
	}
	if v, _ := out["schemaVersion"].(float64); int(v) != SchemaVersion {
		t.Fatalf("schema version: got %v want %d", out["schemaVersion"], SchemaVersion)
	}
}
