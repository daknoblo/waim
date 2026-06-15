package crypto

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	salt, err := NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}
	c, err := New("correct horse battery staple", salt)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
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
	salt, _ := NewSalt()
	c, _ := New("pw", salt)
	enc, err := c.Encrypt("")
	if err != nil || enc != "" {
		t.Fatalf("empty encrypt: enc=%q err=%v", enc, err)
	}
	dec, err := c.Decrypt("")
	if err != nil || dec != "" {
		t.Fatalf("empty decrypt: dec=%q err=%v", dec, err)
	}
}

func TestDisabledCipher(t *testing.T) {
	c := NewDisabled()
	if c.Enabled() {
		t.Fatal("disabled cipher reports enabled")
	}
	if _, err := c.Encrypt("x"); err != ErrNoKey {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
	if _, err := c.Decrypt("abc"); err != ErrNoKey {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
}

func TestWrongKeyFails(t *testing.T) {
	salt, _ := NewSalt()
	c1, _ := New("pw-one", salt)
	c2, _ := New("pw-two", salt)
	enc, _ := c1.Encrypt("secret")
	if _, err := c2.Decrypt(enc); err == nil {
		t.Fatal("decrypt with wrong key should fail")
	}
}

func TestMalformedCiphertext(t *testing.T) {
	salt, _ := NewSalt()
	c, _ := New("pw", salt)
	if _, err := c.Decrypt("not-base64!!!"); err != ErrMalformed {
		t.Fatalf("want ErrMalformed, got %v", err)
	}
	if _, err := c.Decrypt("aGVsbG8="); err == nil {
		t.Fatal("short ciphertext should fail")
	}
}
