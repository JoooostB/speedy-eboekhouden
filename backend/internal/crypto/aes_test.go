package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func testKey(t *testing.T) AESKey {
	t.Helper()
	var key AESKey
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestParseKey_Valid(t *testing.T) {
	hexKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	key, err := ParseKey(hexKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, _ := hex.DecodeString(hexKey)
	if !bytes.Equal(key[:], expected) {
		t.Fatal("key does not match")
	}
}

func TestParseKey_TooShort(t *testing.T) {
	_, err := ParseKey("0123456789abcdef")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestParseKey_InvalidHex(t *testing.T) {
	_, err := ParseKey("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("sk-ant-api03-secret-key-value-here")

	ciphertext, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	decrypted, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("decrypted does not match: got %q, want %q", decrypted, plaintext)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := testKey(t)
	key2 := testKey(t)
	plaintext := []byte("secret")

	ciphertext, _ := Encrypt(key1, plaintext)
	_, err := Decrypt(key2, ciphertext)
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	key := testKey(t)
	_, err := Decrypt(key, []byte("short"))
	if err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}

func TestEncrypt_DifferentNonces(t *testing.T) {
	key := testKey(t)
	plaintext := []byte("same input")

	ct1, _ := Encrypt(key, plaintext)
	ct2, _ := Encrypt(key, plaintext)

	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of same plaintext should produce different ciphertext (different nonces)")
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	key := testKey(t)
	ciphertext, err := Encrypt(key, []byte{})
	if err != nil {
		t.Fatalf("encrypt empty: %v", err)
	}

	decrypted, err := Decrypt(key, ciphertext)
	if err != nil {
		t.Fatalf("decrypt empty: %v", err)
	}
	if len(decrypted) != 0 {
		t.Fatalf("expected empty, got %d bytes", len(decrypted))
	}
}
