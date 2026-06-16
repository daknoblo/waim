package config

import (
	"encoding/base64"
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
	Salt          string `json:"salt"` // base64, not secret
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

	Scan      ScanSettings `json:"scan"`
	Libraries []Library    `json:"libraries"`
}

// Manager loads and persists the configuration and transparently handles
// encryption of API keys. It is safe for concurrent use.
type Manager struct {
	mu       sync.RWMutex
	path     string
	salt     []byte
	cipher   *crypto.Cipher
	settings Settings
}

// Load reads (or initialises) the configuration in dataDir. The masterKey is
// used to derive the encryption key; if it is empty, encryption is disabled and
// any previously encrypted API keys will be unavailable until a key is set.
//
// The returned Manager always contains a usable Settings value, even on first
// run, in which case a fresh config file is written.
func Load(dataDir, masterKey string) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("config: create data dir: %w", err)
	}
	path := filepath.Join(dataDir, "config.json")

	m := &Manager{path: path}

	st, err := readStored(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// First run: start from defaults and a fresh salt.
		st = storedFromSettings(Defaults())
	case err != nil:
		return nil, err
	}

	// Ensure a salt exists.
	if st.Salt == "" {
		salt, gerr := crypto.NewSalt()
		if gerr != nil {
			return nil, gerr
		}
		st.Salt = base64.StdEncoding.EncodeToString(salt)
		m.salt = salt
	} else {
		salt, derr := base64.StdEncoding.DecodeString(st.Salt)
		if derr != nil {
			return nil, fmt.Errorf("config: decode salt: %w", derr)
		}
		m.salt = salt
	}

	// Build the cipher (or a disabled one when no master key is provided).
	if masterKey == "" {
		m.cipher = crypto.NewDisabled()
	} else {
		c, cerr := crypto.New(masterKey, m.salt)
		if cerr != nil {
			return nil, cerr
		}
		m.cipher = c
	}

	m.settings = m.decryptStored(st)

	// Persist on first run or to backfill a freshly generated salt.
	if err := m.persist(st); err != nil {
		return nil, err
	}
	return m, nil
}

// CipherEnabled reports whether API keys can be encrypted/decrypted.
func (m *Manager) CipherEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cipher.Enabled()
}

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
	st.Salt = base64.StdEncoding.EncodeToString(m.salt)

	if m.cipher.Enabled() {
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
	} else if s.Jellyfin.APIKey != "" || s.TMDB.APIKey != "" || s.AI.APIKey != "" {
		return crypto.ErrNoKey
	}

	if err := m.persist(st); err != nil {
		return err
	}
	m.settings = s.Clone()
	return nil
}

// ExportStored returns the on-disk representation as JSON. API keys remain in
// their encrypted form (or empty if encryption is disabled). This never leaks
// plaintext secrets.
func (m *Manager) ExportStored() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := storedFromSettings(m.settings)
	st.Salt = base64.StdEncoding.EncodeToString(m.salt)
	if m.cipher.Enabled() {
		if jEnc, err := m.cipher.Encrypt(m.settings.Jellyfin.APIKey); err == nil {
			st.Jellyfin.APIKeyEnc = jEnc
		}
		if tEnc, err := m.cipher.Encrypt(m.settings.TMDB.APIKey); err == nil {
			st.TMDB.APIKeyEnc = tEnc
		}
		if aEnc, err := m.cipher.Encrypt(m.settings.AI.APIKey); err == nil {
			st.AI.APIKeyEnc = aEnc
		}
	}
	return json.MarshalIndent(st, "", "  ")
}

// decryptStored converts an on-disk representation into in-memory settings,
// decrypting API keys when possible. Decryption failures are tolerated and
// leave the corresponding key empty so the app can still start.
func (m *Manager) decryptStored(st stored) Settings {
	s := Settings{
		Locale:    NormalizeLocale(st.Locale),
		LogLevel:  NormalizeLogLevel(st.LogLevel),
		Libraries: append([]Library(nil), st.Libraries...),
		Scan:      st.Scan,
	}
	s.Jellyfin.URL = st.Jellyfin.URL
	s.Jellyfin.UserID = st.Jellyfin.UserID
	s.TMDB.Language = st.TMDB.Language
	s.TMDB.Region = st.TMDB.Region
	s.AI.Enabled = st.AI.Enabled
	s.AI.Endpoint = st.AI.Endpoint
	s.AI.Model = st.AI.Model

	if m.cipher.Enabled() {
		if v, err := m.cipher.Decrypt(st.Jellyfin.APIKeyEnc); err == nil {
			s.Jellyfin.APIKey = v
		}
		if v, err := m.cipher.Decrypt(st.TMDB.APIKeyEnc); err == nil {
			s.TMDB.APIKey = v
		}
		if v, err := m.cipher.Decrypt(st.AI.APIKeyEnc); err == nil {
			s.AI.APIKey = v
		}
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
	if s.Libraries == nil {
		s.Libraries = []Library{}
	}
	return s
}

// persist atomically writes the stored representation to disk.
func (m *Manager) persist(st stored) error {
	st.SchemaVersion = SchemaVersion
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("config: write temp: %w", err)
	}
	if err := os.Rename(tmp, m.path); err != nil {
		return fmt.Errorf("config: rename: %w", err)
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
	st.Libraries = append([]Library(nil), s.Libraries...)
	return st
}
