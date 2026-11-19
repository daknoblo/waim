package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/daknoblo/waim/internal/maintenance"
)

// Gate is shared by all services receiving this configuration manager.
func (m *Manager) Gate() *maintenance.Gate { return m.gate }

// UpdateGlobals applies a section's fields to the latest settings while holding
// the configuration lock, rather than saving a stale whole-form snapshot.
func (m *Manager) UpdateGlobals(update func(*Settings) error) (Settings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := m.settings.Clone()
	if err := update(&next); err != nil {
		return next, err
	}
	return next, m.saveLocked(next)
}

// FactoryDefaults is only used under an exclusive maintenance lease after the
// database's recoverable factory-reset marker is committed. Unlike normal saves
// it deliberately discards unreadable ciphertext as well as decrypted keys.
func (m *Manager) FactoryDefaults() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	defaults := Defaults()
	disk := storedFromSettings(defaults)
	if err := m.persist(disk); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(m.path))
	if err != nil {
		return fmt.Errorf("config: open reset directory: %w", err)
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil {
		return fmt.Errorf("config: sync reset directory: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}
	m.settings, m.disk, m.keysUnreadable = defaults, disk, false
	return nil
}
