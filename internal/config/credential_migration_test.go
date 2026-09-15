package config

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCredentialGenerationMigrationPreservesCiphertext(t *testing.T) {
	m, dir := credentialManager(t)
	data, err := m.ExportStored()
	if err != nil {
		t.Fatal(err)
	}
	var before stored
	if err := json.Unmarshal(data, &before); err != nil {
		t.Fatal(err)
	}
	var ciphertext string
	for i := range before.Sources {
		if before.Sources[i].ID == "a" {
			ciphertext = before.Sources[i].Jellyfin.APIKeyEnc
			before.Sources[i].CredentialGeneration = ""
		}
	}
	data, err = json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.Path(), data, 0600); err != nil {
		t.Fatal(err)
	}
	var generation string
	for i := 0; i < 3; i++ {
		m, err = Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		src := credentialSource(t, m)
		if src.Jellyfin.APIKey != "key-a" || (generation != "" && src.CredentialGeneration != generation) {
			t.Fatal("migration changed the key or regenerated a persisted generation")
		}
		generation = src.CredentialGeneration
		for _, storedSource := range m.disk.Sources {
			if storedSource.ID == "a" && storedSource.Jellyfin.APIKeyEnc != ciphertext {
				t.Fatal("migration rewrote the existing encrypted key")
			}
		}
	}
}
