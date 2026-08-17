package crypto

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key, err := NewKey()
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	c, err := New(key)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	const secret = "tmdb-api-key-123"
	enc, err := c.Encrypt(secret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == secret {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := c.Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != secret {
		t.Fatalf("round trip mismatch: got %q want %q", got, secret)
	}
}

func TestEmptyValues(t *testing.T) {
	c := newTestCipher(t)
	enc, err := c.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("empty encrypt: enc=%q err=%v", enc, err)
	}
	dec, err := c.Decrypt("")
	if err != nil || dec != "" {
		t.Fatalf("empty decrypt: dec=%q err=%v", dec, err)
	}
}

func TestNewRejectsWrongKeySize(t *testing.T) {
	if _, err := New(make([]byte, KeyLen-1)); !errors.Is(err, ErrKeySize) {
		t.Fatalf("want ErrKeySize, got %v", err)
	}
}

func TestWrongKeyFails(t *testing.T) {
	c1 := newTestCipher(t)
	c2 := newTestCipher(t)
	enc, _ := c1.Encrypt("secret")
	if _, err := c2.Decrypt(enc); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestMalformedCiphertext(t *testing.T) {
	c := newTestCipher(t)
	if _, err := c.Decrypt("not-base64!!!"); !errors.Is(err, ErrMalformed) {
		t.Fatalf("want ErrMalformed, got %v", err)
	}
	if _, err := c.Decrypt("aGVsbG8="); err == nil {
		t.Fatal("short ciphertext should fail")
	}
}

func TestLoadOrCreateKeyFileGeneratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "master.key")

	c1, created, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("LoadOrCreateKeyFile: %v", err)
	}
	if !created {
		t.Fatal("first call should report a freshly created key")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key file permissions: got %o want 600", perm)
	}

	enc, err := c1.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	c2, created, err := LoadOrCreateKeyFile(path)
	if err != nil {
		t.Fatalf("second LoadOrCreateKeyFile: %v", err)
	}
	if created {
		t.Fatal("second call should reuse the existing key")
	}
	got, err := c2.Decrypt(enc)
	if err != nil || got != "secret" {
		t.Fatalf("reloaded key cannot decrypt: got %q err=%v", got, err)
	}
}

func TestLoadOrCreateKeyFileRejectsCorruptFile(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"empty":       "",
		"no-prefix":   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		"bad-base64":  keyFilePrefix + "not base64!!",
		"short-key":   keyFilePrefix + "aGVsbG8=",
		"binary-junk": "\x00\x01\x02",
	}
	for name, content := range cases {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if _, _, err := LoadOrCreateKeyFile(path); err == nil {
			t.Fatalf("%s: corrupt key file should not be replaced silently", name)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != content {
			t.Fatalf("%s: key file was modified: %q err=%v", name, string(data), err)
		}
	}
}
