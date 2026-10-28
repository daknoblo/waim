package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// This is a public cache-generation identifier, not an authentication secret.
func newCredentialGeneration() (string, error) {
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("config: generate credential generation: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

// Persist the last observed readability state so losing/restoring master.key
// invalidates snapshots once per transition, not on every subsequent reload.
// Ciphertexts are never modified here, including during legacy migration.
func (m *Manager) initializeCredentialGenerations(st *stored) error {
	for i := range st.Sources {
		disk := &st.Sources[i]
		current := &m.settings.Sources[i]
		if disk.ID == "virtual" {
			continue
		}
		if disk.CredentialGeneration == "" || disk.KeyUnreadable != current.KeyUnreadable {
			generation, err := newCredentialGeneration()
			if err != nil {
				return err
			}
			disk.CredentialGeneration = generation
		}
		disk.KeyUnreadable = current.KeyUnreadable
		current.CredentialGeneration = disk.CredentialGeneration
	}
	return nil
}
