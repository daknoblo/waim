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
	SchemaVersion int    `json:"schemaVersion"`
	Locale        string `json:"locale"`
	LogLevel      string `json:"logLevel"`

	Jellyfin struct {
		URL       string `json:"url"`
		APIKeyEnc string `json:"apiKeyEnc"`
		UserID    string `json:"userId"`
	} `json:"jellyfin"`

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
	Libraries []Library     `json:"libraries"`
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

	m.settings, m.keysUnreadable = m.decryptStored(st)

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
	s.Locale = NormalizeLocale(s.Locale)
	if err := validate(s); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	st := storedFromSettings(s)

	jEnc, err := m.cipher.Encrypt(s.Jellyfin.APIKey)
	if err != nil {
		return fmt.Errorf("config: encrypt jellyfin key: %w", err)
	}
	tEnc, err := m.cipher.Encrypt(s.TMDB.APIKey)
	if err != nil {
		return fmt.Errorf("config: encrypt tmdb key: %w", err)
	}
	aEnc, err := m.cipher.Encrypt(s.AI.APIKey)
	if err != nil {
		return fmt.Errorf("config: encrypt ai key: %w", err)
	}
	st.Jellyfin.APIKeyEnc = jEnc
	st.TMDB.APIKeyEnc = tEnc
	st.AI.APIKeyEnc = aEnc

	if err := m.persist(st); err != nil {
		return err
	}
	m.settings = s.Clone()
	// Every stored ciphertext has just been rewritten with the current key.
	m.keysUnreadable = false
	return nil
}

// ExportStored returns the on-disk representation as JSON. API keys remain in
// their encrypted form and can only be decrypted by an instance that has the
// same key file. This never leaks plaintext secrets.
func (m *Manager) ExportStored() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := storedFromSettings(m.settings)
	if jEnc, err := m.cipher.Encrypt(m.settings.Jellyfin.APIKey); err == nil {
		st.Jellyfin.APIKeyEnc = jEnc
	}
	if tEnc, err := m.cipher.Encrypt(m.settings.TMDB.APIKey); err == nil {
		st.TMDB.APIKeyEnc = tEnc
	}
	if aEnc, err := m.cipher.Encrypt(m.settings.AI.APIKey); err == nil {
		st.AI.APIKeyEnc = aEnc
	}
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
	st.Jellyfin.URL = s.Jellyfin.URL
	st.Jellyfin.UserID = s.Jellyfin.UserID
	st.TMDB.Language = s.TMDB.Language
	st.TMDB.Region = s.TMDB.Region
	st.AI.Enabled = s.AI.Enabled
	st.AI.Endpoint = s.AI.Endpoint
	st.AI.Model = s.AI.Model
	st.Scan = s.Scan
	st.Cache = s.Cache
	st.Libraries = append([]Library(nil), s.Libraries...)
	return st
}
