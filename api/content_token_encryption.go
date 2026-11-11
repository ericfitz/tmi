package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const contentTokenKeyLen = 32 // AES-256

// ContentTokenEncryptor performs AES-256-GCM encryption for per-user content
// OAuth tokens. The nonce is prepended to the ciphertext.
type ContentTokenEncryptor struct {
	aead cipher.AEAD
}

// NewContentTokenEncryptor constructs an encryptor from a hex-encoded 32-byte key.
func NewContentTokenEncryptor(hexKey string) (*ContentTokenEncryptor, error) {
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("content token encryption key is not valid hex: %w", err)
	}
	if len(key) != contentTokenKeyLen {
		return nil, fmt.Errorf("content token encryption key must be %d bytes (got %d)", contentTokenKeyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &ContentTokenEncryptor{aead: aead}, nil
}

// Encrypt returns nonce || ciphertext.
func (e *ContentTokenEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return e.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt parses nonce || ciphertext and returns the plaintext.
func (e *ContentTokenEncryptor) Decrypt(nonceAndCiphertext []byte) ([]byte, error) {
	ns := e.aead.NonceSize()
	if len(nonceAndCiphertext) < ns {
		return nil, errors.New("content token ciphertext too short")
	}
	nonce := nonceAndCiphertext[:ns]
	ciphertext := nonceAndCiphertext[ns:]
	return e.aead.Open(nil, nonce, ciphertext, nil)
}
