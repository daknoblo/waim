// Package crypto provides authenticated encryption for sensitive settings
// (such as API keys) using AES-256-GCM with a random key that is generated on
// first start and stored next to the configuration.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Errors returned by the package.
var (
	// ErrMalformed is returned when ciphertext cannot be decoded or is too short.
	ErrMalformed = errors.New("crypto: malformed ciphertext")
	// ErrKeySize is returned when a key does not have exactly KeyLen bytes.
	ErrKeySize = errors.New("crypto: key must be 32 bytes")
)

const (
	// KeyLen is the required key length (AES-256).
	KeyLen = 32
	// keyFilePrefix tags the key file with its format version so the encoding
	// can change later without misreading existing files.
	keyFilePrefix = "waim-key-v1:"
)

// Cipher encrypts and decrypts short secret strings.
type Cipher struct {
	aead cipher.AEAD
}

// New builds an AES-256-GCM cipher from a raw KeyLen-byte key.
func New(key []byte) (*Cipher, error) {
	if len(key) != KeyLen {
		return nil, ErrKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// NewKey generates a cryptographically random KeyLen-byte key.
func NewKey() ([]byte, error) {
	key := make([]byte, KeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("crypto: generate key: %w", err)
	}
	return key, nil
}

// LoadOrCreateKeyFile returns the cipher for the key stored at path, generating
// and persisting a new key when the file does not exist yet. The second return
// value reports whether a new key was created.
//
// An existing but unreadable or malformed key file is always an error: silently
// replacing it would render every stored API key undecryptable.
func LoadOrCreateKeyFile(path string) (*Cipher, bool, error) {
	key, err := readKeyFile(path)
	switch {
	case err == nil:
		c, cerr := New(key)
		return c, false, cerr
	case !errors.Is(err, os.ErrNotExist):
		return nil, false, err
	}

	key, err = NewKey()
	if err != nil {
		return nil, false, err
	}
	created := true
	if werr := writeKeyFile(path, key); werr != nil {
		if !errors.Is(werr, os.ErrExist) {
			return nil, false, werr
		}
		// Another process won the race; adopt the key it wrote.
		created = false
		if key, err = readKeyFile(path); err != nil {
			return nil, false, err
		}
	}
	c, err := New(key)
	if err != nil {
		return nil, false, err
	}
	return c, created, nil
}

// Encrypt encrypts plaintext and returns a base64-encoded string containing the
// nonce followed by the ciphertext. Empty plaintext yields an empty string.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: read nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. An empty input yields an empty string.
func (c *Cipher) Decrypt(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrMalformed
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", ErrMalformed
	}
	nonce, ciphertext := raw[:ns], raw[ns:]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// readKeyFile reads and validates the key file. A missing file is reported as
// os.ErrNotExist so callers can distinguish it from a corrupt one.
func readKeyFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is derived from the configured data dir
	if err != nil {
		return nil, err
	}
	encoded, ok := strings.CutPrefix(strings.TrimSpace(string(data)), keyFilePrefix)
	if !ok {
		return nil, fmt.Errorf("crypto: %s: unrecognised key file format", path)
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("crypto: %s: decode key: %w", path, err)
	}
	if len(key) != KeyLen {
		return nil, fmt.Errorf("crypto: %s: %w", path, ErrKeySize)
	}
	return key, nil
}

// writeKeyFile creates the key file with owner-only permissions. It never
// overwrites an existing file and returns an error wrapping os.ErrExist in that
// case. The file and its directory are fsynced so the key survives a crash.
func writeKeyFile(path string, key []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // path is derived from the configured data dir
	if err != nil {
		return fmt.Errorf("crypto: create key file: %w", err)
	}
	content := keyFilePrefix + base64.StdEncoding.EncodeToString(key) + "\n"
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("crypto: write key file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("crypto: sync key file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("crypto: close key file: %w", err)
	}
	syncDir(filepath.Dir(path))
	return nil
}

// syncDir flushes a directory entry so the new key file is durable. Failures
// are ignored: not every filesystem supports it.
func syncDir(dir string) {
	d, err := os.Open(dir) //nolint:gosec // path is derived from the configured data dir
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
