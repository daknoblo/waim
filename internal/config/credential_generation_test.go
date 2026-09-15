package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func credentialManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.AddSource(Source{
		ID: "a", Type: "jellyfin", Name: "A", Enabled: true,
		CredentialGeneration: "untrusted-input",
		Jellyfin:             JellyfinSettings{URL: "https://a.invalid", APIKey: "key-a"},
		Libraries:            []Library{{ID: "movies", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func credentialSource(t *testing.T, m *Manager) Source {
	t.Helper()
	s, ok := m.Get().Source("a")
	if !ok || s.CredentialGeneration == "" || s.CredentialGeneration == "untrusted-input" {
		t.Fatal("source generation was missing or accepted from caller")
	}
	return s
}

func credentialSetters() map[string]func(*Manager, string) error {
	return map[string]func(*Manager, string) error{
		"Save": func(m *Manager, key string) error {
			s := m.Get()
			for i := range s.Sources {
				if s.Sources[i].ID == "a" {
					s.Sources[i].Jellyfin.APIKey = key
					s.Sources[i].CredentialGeneration = "untrusted-input"
				}
			}
			return m.Save(s)
		},
		"UpdateSource": func(m *Manager, key string) error {
			s, _ := m.Get().Source("a")
			return m.UpdateSource(s.ID, s.Revision, func(src *Source) error {
				src.Jellyfin.APIKey = key
				src.CredentialGeneration = "untrusted-input"
				return nil
			})
		},
		"UpdateSourceWithKey": func(m *Manager, key string) error {
			s, _ := m.Get().Source("a")
			return m.UpdateSourceWithKey(s.ID, s.Revision, key, func(src *Source) error {
				src.CredentialGeneration = "untrusted-input"
				return nil
			})
		},
	}
}

func TestCredentialChangesAdvanceGenerationAcrossManagerPaths(t *testing.T) {
	for name, setKey := range credentialSetters() {
		t.Run(name, func(t *testing.T) {
			m, dir := credentialManager(t)
			initial := credentialSource(t, m)
			if err := setKey(m, "key-b"); err != nil {
				t.Fatal(err)
			}
			changed := credentialSource(t, m)
			if changed.CredentialGeneration == initial.CredentialGeneration || changed.Fingerprint() == initial.Fingerprint() {
				t.Fatal("key change did not invalidate source snapshots")
			}
			reloaded, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if credentialSource(t, reloaded).CredentialGeneration != changed.CredentialGeneration {
				t.Fatal("reload changed the credential generation")
			}
			if err := setKey(reloaded, "key-a"); err != nil {
				t.Fatal(err)
			}
			restored := credentialSource(t, reloaded)
			if restored.CredentialGeneration == changed.CredentialGeneration || restored.Fingerprint() == initial.Fingerprint() {
				t.Fatal("restoring an earlier key resurrected an earlier fingerprint")
			}
		})
	}
}

func TestRenameAndGlobalSavesPreserveGenerationButIgnoreSubmittedTokens(t *testing.T) {
	m, dir := credentialManager(t)
	initial := credentialSource(t, m)
	if err := m.UpdateSource("a", initial.Revision, func(src *Source) error {
		src.Name = "Renamed"
		src.CredentialGeneration = "untrusted-input"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if current := credentialSource(t, m); current.Fingerprint() != initial.Fingerprint() {
		t.Fatal("ordinary rename invalidated snapshots")
	}
	settings := m.Get()
	for i := range settings.Sources {
		if settings.Sources[i].ID == "a" {
			settings.Sources[i].CredentialGeneration = ""
		}
	}
	if err := m.Save(settings); err != nil {
		t.Fatal(err)
	}
	if credentialSource(t, m).CredentialGeneration != initial.CredentialGeneration {
		t.Fatal("plain save trusted a submitted generation")
	}
	stale := m.Get()
	current := credentialSource(t, m)
	if err := m.UpdateSourceWithKey("a", current.Revision, "key-b", func(*Source) error { return nil }); err != nil {
		t.Fatal(err)
	}
	changed := credentialSource(t, m)
	stale.Locale = LocaleDE
	if err := m.SaveGlobals(stale); err != nil {
		t.Fatal(err)
	}
	if current := credentialSource(t, m); current.CredentialGeneration != changed.CredentialGeneration || current.Jellyfin.APIKey != "key-b" {
		t.Fatal("stale global form reset the key or generation")
	}
	if err := m.UpdateSource("a", initial.Revision, func(src *Source) error {
		src.CredentialGeneration = initial.CredentialGeneration
		return nil
	}); err == nil {
		t.Fatal("stale source form was accepted")
	}
	// Full Save deliberately replaces settings, but restoring old key bytes
	// through it still must never restore the old cache generation.
	if err := m.Save(stale); err != nil {
		t.Fatal(err)
	}
	restored := credentialSource(t, m)
	if restored.CredentialGeneration == initial.CredentialGeneration || restored.CredentialGeneration == changed.CredentialGeneration {
		t.Fatal("stale full save reset the credential generation")
	}
	reloaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if credentialSource(t, reloaded).CredentialGeneration != restored.CredentialGeneration {
		t.Fatal("restored-key generation did not survive reload")
	}
}

func TestExplicitReplacementAndReaddedSourceReceiveFreshGenerations(t *testing.T) {
	m, _ := credentialManager(t)
	initial := credentialSource(t, m)
	if err := m.UpdateSourceWithKey("a", initial.Revision, "key-a", func(*Source) error { return nil }); err != nil {
		t.Fatal(err)
	}
	replaced := credentialSource(t, m)
	if replaced.Fingerprint() == initial.Fingerprint() {
		t.Fatal("explicit same-key replacement reused old snapshots")
	}
	if err := m.RemoveSource("a", replaced.Revision); err != nil {
		t.Fatal(err)
	}
	if err := m.AddSource(initial); err != nil {
		t.Fatal(err)
	}
	if credentialSource(t, m).CredentialGeneration == initial.CredentialGeneration {
		t.Fatal("readding an ID trusted the caller's old generation")
	}
}

func TestReplacingUnreadableSourceKeyAdvancesGeneration(t *testing.T) {
	for name, setKey := range credentialSetters() {
		t.Run(name, func(t *testing.T) {
			m, dir := credentialManager(t)
			initial := credentialSource(t, m)
			if err := os.Remove(m.KeyPath()); err != nil {
				t.Fatal(err)
			}
			m, err := Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			unreadable := credentialSource(t, m)
			if !unreadable.KeyUnreadable || unreadable.Fingerprint() == initial.Fingerprint() {
				t.Fatal("unreadable credentials reused a readable-key snapshot")
			}
			before, err := m.ExportStored()
			if err != nil {
				t.Fatal(err)
			}
			var disk stored
			if err := json.Unmarshal(before, &disk); err != nil {
				t.Fatal(err)
			}
			var ciphertext string
			for _, src := range disk.Sources {
				if src.ID == "a" {
					ciphertext = src.Jellyfin.APIKeyEnc
				}
			}
			if err := m.UpdateSource("a", unreadable.Revision, func(src *Source) error { src.Name = "Renamed"; return nil }); err != nil {
				t.Fatal(err)
			}
			m, err = Load(dir)
			if err != nil {
				t.Fatal(err)
			}
			if current := credentialSource(t, m); !current.KeyUnreadable || current.CredentialGeneration != unreadable.CredentialGeneration {
				t.Fatal("unreadable reload/rename changed generation or warning")
			}
			for _, src := range m.disk.Sources {
				if src.ID == "a" && src.Jellyfin.APIKeyEnc != ciphertext {
					t.Fatal("unrelated save destroyed unreadable ciphertext")
				}
			}
			if err := setKey(m, "key-a"); err != nil {
				t.Fatal(err)
			}
			replaced := credentialSource(t, m)
			if replaced.KeyUnreadable || replaced.Jellyfin.APIKey != "key-a" ||
				replaced.CredentialGeneration == unreadable.CredentialGeneration || replaced.Fingerprint() == initial.Fingerprint() {
				t.Fatal("replacing the unreadable key did not invalidate snapshots")
			}
			m, err = Load(dir)
			if err != nil || credentialSource(t, m).CredentialGeneration != replaced.CredentialGeneration {
				t.Fatal("replacement generation did not persist")
			}
		})
	}
}

func TestMasterKeyRestorationAdvancesGenerationOnce(t *testing.T) {
	m, dir := credentialManager(t)
	key, err := os.ReadFile(m.KeyPath())
	if err != nil {
		t.Fatal(err)
	}
	original := credentialSource(t, m)
	if err := os.Remove(m.KeyPath()); err != nil {
		t.Fatal(err)
	}
	m, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	unreadable := credentialSource(t, m)
	if err := os.WriteFile(filepath.Join(dir, KeyFileName), key, 0600); err != nil {
		t.Fatal(err)
	}
	m, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	restored := credentialSource(t, m)
	if restored.KeyUnreadable || restored.CredentialGeneration == unreadable.CredentialGeneration || restored.CredentialGeneration == original.CredentialGeneration {
		t.Fatal("master-key restoration reused an earlier credential generation")
	}
	m, err = Load(dir)
	if err != nil || credentialSource(t, m).CredentialGeneration != restored.CredentialGeneration {
		t.Fatal("unchanged readable reload advanced the generation again")
	}
}

func TestFingerprintIsIndependentOfRawKeyMaterial(t *testing.T) {
	m, _ := credentialManager(t)
	src := credentialSource(t, m)
	fingerprint := src.Fingerprint()
	for _, key := range []string{"different-private-key", "", "key-a"} {
		src.Jellyfin.APIKey = key
		if src.Fingerprint() != fingerprint {
			t.Fatal("public fingerprint depends on raw key material")
		}
	}
	src.CredentialGeneration = "different-public-generation"
	if src.Fingerprint() == fingerprint {
		t.Fatal("public fingerprint does not include credential generation")
	}
}
