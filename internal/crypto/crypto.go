// Package crypto provides authenticated encryption for sensitive settings
// (such as API keys) using AES-256-GCM with a key derived from a master
// passphrase via Argon2id.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// Errors returned by the package.
var (
	// ErrNoKey is returned when an operation requires a master key but none
	// has been configured.
	ErrNoKey = errors.New("crypto: no master key configured")
	// ErrMalformed is returned when ciphertext cannot be decoded or is too short.
	ErrMalformed = errors.New("crypto: malformed ciphertext")
)

// Argon2id parameters. These are deliberately conservative defaults suited for
// a long-running service that decrypts a handful of secrets occasionally.
const (
	argonTime    = 1
	argonMemory  = 64 * 1024 // 64 MiB
	argonThreads = 4
	keyLen       = 32 // AES-256
	// SaltLen is the length of the random salt stored alongside the config.
	SaltLen = 16
)

// Cipher encrypts and decrypts short secret strings.
//
// A Cipher with no key (created via NewDisabled) reports Enabled() == false and
// returns ErrNoKey from Encrypt/Decrypt. This lets the rest of the application
// run and surface a warning instead of crashing when WAIM_MASTER_KEY is unset.
type Cipher struct {
	aead    cipher.AEAD
	enabled bool
}

// NewDisabled returns a Cipher that cannot encrypt or decrypt. It is used when
// no master passphrase is available.
func NewDisabled() *Cipher {
	return &Cipher{enabled: false}
}

// New derives an AES-256-GCM cipher from the given passphrase and salt.
// The salt is not secret and should be persisted with the config so the same
// key can be reproduced on the next start.
func New(passphrase string, salt []byte) (*Cipher, error) {
	if passphrase == "" {
		return nil, ErrNoKey
	}
	if len(salt) < SaltLen {
		return nil, fmt.Errorf("crypto: salt must be at least %d bytes", SaltLen)
	}
	key := argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, keyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new gcm: %w", err)
	}
	return &Cipher{aead: aead, enabled: true}, nil
}

// Enabled reports whether the cipher can perform cryptographic operations.
func (c *Cipher) Enabled() bool { return c != nil && c.enabled }

// Encrypt encrypts plaintext and returns a base64-encoded string containing the
// nonce followed by the ciphertext. Empty plaintext yields an empty string.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if !c.Enabled() {
		return "", ErrNoKey
	}
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
	if !c.Enabled() {
		return "", ErrNoKey
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

// NewSalt generates a cryptographically random salt of SaltLen bytes.
func NewSalt() ([]byte, error) {
	salt := make([]byte, SaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("crypto: generate salt: %w", err)
	}
	return salt, nil
}
