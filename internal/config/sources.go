package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/daknoblo/waim/internal/media"
)

const LegacySourceID = "jellyfin-default"

var ErrSourcesChanged = errors.New("sources changed during computation")

// WithSourcesToken holds the configuration identity stable during result
// publication. The callback must not call a configuration mutation method.
func (m *Manager) WithSourcesToken(token string, publish func() error) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.settings.SourcesToken() != token {
		return ErrSourcesChanged
	}
	return publish()
}

type Source struct {
	CredentialGeneration string           `json:"credentialGeneration,omitempty"`
	ID                   string           `json:"id"`
	Type                 string           `json:"type"`
	Name                 string           `json:"name"`
	Enabled              bool             `json:"enabled"`
	Jellyfin             JellyfinSettings `json:"jellyfin,omitempty,omitzero"`
	Libraries            []Library        `json:"libraries,omitempty"`
	Revision             int64            `json:"revision"`
	KeyUnreadable        bool             `json:"-"`
}

func VirtualSource() Source {
	return Source{ID: media.VirtualID, Type: media.Virtual, Name: media.VirtualName, Enabled: true}
}

// Fingerprint hashes only non-secret identity metadata. The manager-owned
// credential generation changes on key replacement without exposing key material.
func (s Source) Fingerprint() string {
	ids := []string{}
	for _, l := range s.Libraries {
		if l.Enabled {
			ids = append(ids, l.ID)
		}
	}
	sort.Strings(ids)
	b, _ := json.Marshal([]any{s.ID, s.Type, strings.TrimRight(s.Jellyfin.URL, "/"), s.Jellyfin.UserID, s.CredentialGeneration, ids})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (s Settings) Source(id string) (Source, bool) {
	for _, src := range s.Sources {
		if src.ID == id {
			return src, true
		}
	}
	return Source{}, false
}

func (s Settings) SourcesToken() string {
	var identities []any
	for _, src := range s.Sources {
		identities = append(identities, []any{src.ID, src.Name, src.Enabled, src.Revision, src.Fingerprint()})
	}
	b, _ := json.Marshal(identities)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func normalizeSources(s *Settings) {
	found := false
	for i := range s.Sources {
		if s.Sources[i].ID == media.VirtualID {
			s.Sources[i] = VirtualSource()
			found = true
		}
	}
	if !found {
		s.Sources = append(s.Sources, VirtualSource())
	}
}

func validateSources(s Settings) error {
	seen := map[string]bool{}
	for _, src := range s.Sources {
		if src.ID == "" || seen[src.ID] || strings.TrimSpace(src.Name) == "" {
			return fmt.Errorf("config: source requires unique ID and name")
		}
		seen[src.ID] = true
		if src.ID == media.VirtualID {
			if src.Type != media.Virtual || !src.Enabled {
				return fmt.Errorf("config: virtual collection cannot be disabled")
			}
			continue
		}
		if src.Type != media.Jellyfin {
			return fmt.Errorf("config: unsupported source type")
		}
		if err := validateEndpoint("source url", src.Jellyfin.URL); err != nil {
			return err
		}
	}
	return nil
}

// UpdateSource is an atomic targeted edit; stale browser forms fail instead of
// overwriting another edit. Other source instances are never replaced.
func (m *Manager) UpdateSource(id string, revision int64, update func(*Source) error) error {
	return m.updateSource(id, revision, false, update)
}

// UpdateSourceWithKey distinguishes an explicitly supplied credential from an
// inherited one, even when its bytes happen to be the same after a URL change.
func (m *Manager) UpdateSourceWithKey(id string, revision int64, key string, update func(*Source) error) error {
	return m.updateSource(id, revision, key != "", func(src *Source) error {
		if err := update(src); err != nil {
			return err
		}
		if key != "" {
			src.Jellyfin.APIKey = key
		}
		return nil
	})
}

func (m *Manager) updateSource(id string, revision int64, explicitKey bool, update func(*Source) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings.Clone()
	for i := range s.Sources {
		if s.Sources[i].ID != id {
			continue
		}
		if id == media.VirtualID {
			return fmt.Errorf("virtual collection cannot be edited")
		}
		if s.Sources[i].Revision != revision {
			return fmt.Errorf("source changed; reload before saving")
		}
		old := s.Sources[i]
		if err := update(&s.Sources[i]); err != nil {
			return err
		}
		if !explicitKey && old.Jellyfin.URL != s.Sources[i].Jellyfin.URL && old.Jellyfin.APIKey == s.Sources[i].Jellyfin.APIKey {
			return fmt.Errorf("enter a new key when changing the server address")
		}
		s.Sources[i].Revision++
		if explicitKey {
			return m.saveLocked(s, id)
		}
		return m.saveLocked(s)
	}
	return fmt.Errorf("source not found")
}

func (m *Manager) AddSource(src Source) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings.Clone()
	if _, ok := s.Source(src.ID); ok {
		return fmt.Errorf("source already exists")
	}
	src.Revision = 1
	s.Sources = append(s.Sources, src)
	return m.saveLocked(s)
}

func (m *Manager) RemoveSource(id string, revision int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == media.VirtualID {
		return fmt.Errorf("virtual collection cannot be removed")
	}
	s := m.settings.Clone()
	for i, src := range s.Sources {
		if src.ID == id {
			if src.Revision != revision {
				return fmt.Errorf("source changed; reload before removing")
			}
			s.Sources = append(s.Sources[:i], s.Sources[i+1:]...)
			return m.saveLocked(s)
		}
	}
	return fmt.Errorf("source not found")
}
