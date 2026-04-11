package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// AESKey holds a 32-byte AES-256 key.
type AESKey [32]byte

// ParseKey decodes a 64-char hex string into an AES-256 key.
func ParseKey(hexKey string) (AESKey, error) {
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return AESKey{}, fmt.Errorf("decoding hex key: %w", err)
	}
	if len(b) != 32 {
		return AESKey{}, fmt.Errorf("key must be 32 bytes (64 hex chars), got %d bytes", len(b))
	}
	var key AESKey
	copy(key[:], b)
	return key, nil
}

// Encrypt encrypts plaintext using AES-256-GCM. Returns nonce+ciphertext.
func Encrypt(key AESKey, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts AES-256-GCM ciphertext (nonce prepended).
func Decrypt(key AESKey, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting: %w", err)
	}

	return plaintext, nil
}
