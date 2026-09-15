package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/daknoblo/waim/internal/crypto"
)

// stored is the on-disk representation of the configuration. API keys are
// stored only in their encrypted form.
type stored struct {
	Sources       []storedSource `json:"sources"`
	SchemaVersion int            `json:"schemaVersion"`
	Locale        string         `json:"locale"`
	LogLevel      string         `json:"logLevel"`

	Jellyfin struct {
		URL       string `json:"url"`
		APIKeyEnc string `json:"apiKeyEnc"`
		UserID    string `json:"userId"`
	} `json:"jellyfin,omitempty,omitzero"`

	TMDB struct {
		APIKeyEnc string `json:"apiKeyEnc"`
		Language  string `json:"language"`
		Region    string `json:"region"`
	} `json:"tmdb"`

	AI struct {
		Enabled   bool   `json:"enabled"`
		Endpoint  string `json:"endpoint"`
		APIKeyEnc string `json:"apiKeyEnc"`
		Model     string `json:"model"`
	} `json:"ai"`

	Scan      ScanSettings  `json:"scan"`
	Cache     CacheSettings `json:"cache"`
	Libraries []Library     `json:"libraries,omitempty"`
}

type storedSource struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Enabled   bool      `json:"enabled"`
	Revision  int64     `json:"revision"`
	Libraries []Library `json:"libraries,omitempty"`
	Jellyfin  struct {
		URL       string `json:"url"`
		UserID    string `json:"userId"`
		APIKeyEnc string `json:"apiKeyEnc"`
	} `json:"jellyfin,omitempty,omitzero"`
}

// KeyFileName is the name of the file inside the data directory that holds the
// automatically generated encryption key.
const KeyFileName = "master.key"

// Manager loads and persists the configuration and transparently handles
// encryption of API keys. It is safe for concurrent use.
type Manager struct {
	mu             sync.RWMutex
	path           string
	keyPath        string
	cipher         *crypto.Cipher
	keyCreated     bool
	keysUnreadable bool
	settings       Settings
	disk           stored
}

// Load reads (or initialises) the configuration in dataDir.
//
// The encryption key is read from dataDir/master.key and generated on first
// start. Losing that file makes previously stored API keys undecryptable; they
// are then reported via KeysUnreadable and must be entered again.
//
// The returned Manager always contains a usable Settings value, even on first
// run, in which case a fresh config file is written.
func Load(dataDir string) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("config: create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "config.json")

	m := &Manager{path: path, keyPath: filepath.Join(dataDir, KeyFileName)}

	cipher, created, err := crypto.LoadOrCreateKeyFile(m.keyPath)
	if err != nil {
		return nil, err
	}
	m.cipher = cipher
	m.keyCreated = created

	st, err := readStored(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// First run: start from defaults.
		st = storedFromSettings(Defaults())
	case err != nil:
		return nil, err
	}
	if st.SchemaVersion > SchemaVersion {
		return nil, fmt.Errorf("config: schema %d is newer than supported schema %d", st.SchemaVersion, SchemaVersion)
	}

	// Copy ciphertext, not decrypted values, while migrating legacy settings.
	if len(st.Sources) == 0 && (st.Jellyfin.URL != "" || st.Jellyfin.APIKeyEnc != "" || len(st.Libraries) > 0) {
		src := storedSource{ID: LegacySourceID, Type: "jellyfin", Name: "Jellyfin", Enabled: true, Revision: 1, Libraries: st.Libraries}
		src.Jellyfin.URL, src.Jellyfin.UserID, src.Jellyfin.APIKeyEnc = st.Jellyfin.URL, st.Jellyfin.UserID, st.Jellyfin.APIKeyEnc
		st.Sources = append(st.Sources, src)
	}
	foundVirtual := false
	for i, src := range st.Sources {
		if src.ID == "virtual" {
			foundVirtual = true
			st.Sources[i] = storedSource{ID: "virtual", Type: "virtual", Name: "Watch collection", Enabled: true}
		}
	}
	if !foundVirtual {
		st.Sources = append(st.Sources, storedSource{ID: "virtual", Type: "virtual", Name: "Watch collection", Enabled: true})
	}
	// The source entry now owns the ciphertext; do not retain a second writable
	// legacy connection or a second unreadable-key warning.
	st.Jellyfin.URL, st.Jellyfin.UserID, st.Jellyfin.APIKeyEnc = "", "", ""
	st.Libraries = nil
	m.settings, m.keysUnreadable = m.decryptStored(st)
	if err := validateSources(m.settings); err != nil {
		return nil, err
	}
	m.disk = st

	// Persist on first run and to upgrade the on-disk schema.
	if err := m.persist(st); err != nil {
		return nil, err
	}
	return m, nil
}

// KeyCreated reports whether the encryption key was generated during Load.
func (m *Manager) KeyCreated() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.keyCreated
}

// KeysUnreadable reports whether stored API keys exist that cannot be decrypted
// with the current encryption key. They have to be entered again.
func (m *Manager) KeysUnreadable() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.keysUnreadable
}

// KeyPath returns the path of the encryption key file.
func (m *Manager) KeyPath() string { return m.keyPath }

// Path returns the config file path.
func (m *Manager) Path() string { return m.path }

// Get returns a copy of the current settings with decrypted API keys.
func (m *Manager) Get() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settings.Clone()
}

// Save validates and persists new settings, encrypting API keys at rest.
func (m *Manager) Save(s Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveLocked(s)
}

// SaveGlobals preserves sources even when a global settings form was opened
// before a concurrent source edit.
func (m *Manager) SaveGlobals(s Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.Sources = m.settings.Clone().Sources
	s.Jellyfin = m.settings.Jellyfin
	s.Libraries = m.settings.Libraries
	return m.saveLocked(s)
}

func (m *Manager) saveLocked(s Settings) error {
	s = s.Clone()
	s.Jellyfin = JellyfinSettings{}
	s.Libraries = nil
	normalizeSources(&s)
	s.Locale = NormalizeLocale(s.Locale)
	if err := validate(s); err != nil {
		return err
	}
	if err := validateSources(s); err != nil {
		return err
	}

	st := storedFromSettings(s)

	tEnc, err := m.cipher.Encrypt(s.TMDB.APIKey)
	if err != nil {
		return fmt.Errorf("config: encrypt tmdb key: %w", err)
	}
	aEnc, err := m.cipher.Encrypt(s.AI.APIKey)
	if err != nil {
		return fmt.Errorf("config: encrypt ai key: %w", err)
	}
	st.TMDB.APIKeyEnc = tEnc
	st.AI.APIKeyEnc = aEnc
	// A missing master key must never erase unreadable ciphertext on an
	// unrelated save. Keep it until a replacement key is explicitly supplied.
	preserve := func(plain, old string, enc *string) {
		if plain == "" && old != "" {
			if _, err := m.cipher.Decrypt(old); err != nil {
				*enc = old
			}
		}
	}
	preserve(s.TMDB.APIKey, m.disk.TMDB.APIKeyEnc, &st.TMDB.APIKeyEnc)
	preserve(s.AI.APIKey, m.disk.AI.APIKeyEnc, &st.AI.APIKeyEnc)
	for i, src := range s.Sources {
		enc, err := m.cipher.Encrypt(src.Jellyfin.APIKey)
		if err != nil {
			return err
		}
		for _, old := range m.disk.Sources {
			if old.ID == src.ID && old.Jellyfin.URL == src.Jellyfin.URL {
				preserve(src.Jellyfin.APIKey, old.Jellyfin.APIKeyEnc, &enc)
			}
		}
		st.Sources[i].Jellyfin.APIKeyEnc = enc
	}

	if err := m.persist(st); err != nil {
		return err
	}
	m.disk = st
	m.settings, m.keysUnreadable = m.decryptStored(st)
	return nil
}

// ExportStored returns the on-disk representation as JSON. API keys remain in
// their encrypted form and can only be decrypted by an instance that has the
// same key file. This never leaks plaintext secrets.
func (m *Manager) ExportStored() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := m.disk
	st.SchemaVersion = SchemaVersion
	return json.MarshalIndent(st, "", "  ")
}

// decryptStored converts an on-disk representation into in-memory settings,
// decrypting API keys when possible. Decryption failures are tolerated and
// leave the corresponding key empty so the app can still start; the second
// return value then reports that unreadable ciphertext was found.
func (m *Manager) decryptStored(st stored) (Settings, bool) {
	s := Settings{
		Locale:    NormalizeLocale(st.Locale),
		LogLevel:  NormalizeLogLevel(st.LogLevel),
		Libraries: append([]Library(nil), st.Libraries...),
		Scan:      st.Scan,
		Cache:     st.Cache,
	}
	s.Jellyfin.URL = st.Jellyfin.URL
	s.Jellyfin.UserID = st.Jellyfin.UserID
	s.TMDB.Language = st.TMDB.Language
	s.TMDB.Region = st.TMDB.Region
	s.AI.Enabled = st.AI.Enabled
	s.AI.Endpoint = st.AI.Endpoint
	s.AI.Model = st.AI.Model

	unreadable := false
	decrypt := func(enc string) string {
		v, err := m.cipher.Decrypt(enc)
		if err != nil {
			unreadable = true
			return ""
		}
		return v
	}
	s.Jellyfin.APIKey = decrypt(st.Jellyfin.APIKeyEnc)
	s.TMDB.APIKey = decrypt(st.TMDB.APIKeyEnc)
	s.AI.APIKey = decrypt(st.AI.APIKeyEnc)
	for _, src := range st.Sources {
		key, err := m.cipher.Decrypt(src.Jellyfin.APIKeyEnc)
		if err != nil {
			unreadable = true
		}
		s.Sources = append(s.Sources, Source{ID: src.ID, Type: src.Type, Name: src.Name, Enabled: src.Enabled, Revision: src.Revision, Libraries: append([]Library(nil), src.Libraries...), Jellyfin: JellyfinSettings{URL: src.Jellyfin.URL, UserID: src.Jellyfin.UserID, APIKey: key}, KeyUnreadable: err != nil})
	}

	// Backfill defaults for zero values that should not be empty.
	def := Defaults()
	if s.Locale == "" {
		s.Locale = def.Locale
	}
	if s.TMDB.Language == "" {
		s.TMDB.Language = def.TMDB.Language
	}
	if s.Scan.TMDBRateLimitRPS == 0 {
		s.Scan.TMDBRateLimitRPS = def.Scan.TMDBRateLimitRPS
	}
	// A fully zero cache config means it predates this setting; restore defaults
	// (including RefreshEnabled, whose false zero value is otherwise ambiguous).
	if s.Cache.RefreshIntervalMinutes == 0 && s.Cache.RefreshPercent == 0 {
		s.Cache = def.Cache
	} else {
		if s.Cache.RefreshIntervalMinutes == 0 {
			s.Cache.RefreshIntervalMinutes = def.Cache.RefreshIntervalMinutes
		}
		if s.Cache.RefreshPercent == 0 {
			s.Cache.RefreshPercent = def.Cache.RefreshPercent
		}
		// Cleanup config added later; a zero max-age means it was never set, so
		// backfill its defaults (including the CleanupEnabled bool).
		if s.Cache.CleanupMaxAgeDays == 0 {
			s.Cache.CleanupEnabled = def.Cache.CleanupEnabled
			s.Cache.CleanupMaxAgeDays = def.Cache.CleanupMaxAgeDays
		}
	}
	if s.Libraries == nil {
		s.Libraries = []Library{}
	}
	return s, unreadable
}

// persist atomically writes the stored representation to disk. The temp file is
// fsynced before the rename so a crash or power loss cannot leave a truncated
// config.
func (m *Manager) persist(st stored) error {
	st.SchemaVersion = SchemaVersion
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := writeFileSync(tmp, data); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, m.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}

// writeFileSync writes data to path with owner-only permissions and flushes it
// to stable storage.
func writeFileSync(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("config: write temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("config: sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("config: close temp: %w", err)
	}
	return nil
}

func readStored(path string) (stored, error) {
	var st stored
	data, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return st, nil
}

func storedFromSettings(s Settings) stored {
	var st stored
	st.SchemaVersion = SchemaVersion
	st.Locale = s.Locale
	st.LogLevel = s.LogLevel
	st.TMDB.Language = s.TMDB.Language
	st.TMDB.Region = s.TMDB.Region
	st.AI.Enabled = s.AI.Enabled
	st.AI.Endpoint = s.AI.Endpoint
	st.AI.Model = s.AI.Model
	st.Scan = s.Scan
	st.Cache = s.Cache
	for _, src := range s.Sources {
		ss := storedSource{ID: src.ID, Type: src.Type, Name: src.Name, Enabled: src.Enabled, Revision: src.Revision, Libraries: src.Libraries}
		ss.Jellyfin.URL, ss.Jellyfin.UserID = src.Jellyfin.URL, src.Jellyfin.UserID
		st.Sources = append(st.Sources, ss)
	}
	return st
}
