package secretbox

import (
	"errors"
	"strings"
	"testing"
)

func TestNewRequiresSecret(t *testing.T) {
	_, err := New("")
	if !errors.Is(err, ErrMissingKey) {
		t.Fatalf("New empty secret error = %v, want %v", err, ErrMissingKey)
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	box, err := New("test-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	plain := "admin-key-123"
	ciphertext, err := box.Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext == "" {
		t.Fatal("ciphertext is empty")
	}
	if strings.Contains(ciphertext, plain) {
		t.Fatalf("ciphertext contains plaintext: %q", ciphertext)
	}

	got, err := box.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("Decrypt = %q, want %q", got, plain)
	}
}

func TestEncryptUsesRandomNonce(t *testing.T) {
	box, err := New("test-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	one, err := box.Encrypt("same-admin-key")
	if err != nil {
		t.Fatalf("Encrypt one: %v", err)
	}
	two, err := box.Encrypt("same-admin-key")
	if err != nil {
		t.Fatalf("Encrypt two: %v", err)
	}
	if one == two {
		t.Fatal("same plaintext encrypted to identical ciphertext")
	}
}

func TestDecryptRejectsWrongKeyAndInvalidCiphertext(t *testing.T) {
	box, err := New("test-secret")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ciphertext, err := box.Encrypt("admin-key-123")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	wrong, err := New("other-secret")
	if err != nil {
		t.Fatalf("New wrong: %v", err)
	}
	if _, err := wrong.Decrypt(ciphertext); err == nil {
		t.Fatal("Decrypt with wrong key succeeded")
	}
	if _, err := box.Decrypt("not-valid-base64!"); err == nil {
		t.Fatal("Decrypt invalid ciphertext succeeded")
	}
}
