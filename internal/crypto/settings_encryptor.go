// Package crypto provides encryption utilities for TMI.
package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/secrets"
	"github.com/ericfitz/tmi/internal/slogging"
)

// MaxEncryptedValueLength is the maximum length of an encrypted value string
// that fits in a varchar(4000) column.
const MaxEncryptedValueLength = 4000

// ErrValueTooLong is returned when an encrypted value exceeds the database column limit.
var ErrValueTooLong = errors.New("encrypted value exceeds maximum storage length")

// EncryptionContext tracks key version and cipher metadata.
type EncryptionContext struct {
	ContextID int
	Algorithm string
}

// SettingsEncryptor encrypts and decrypts setting values using AES-256-GCM.
type SettingsEncryptor struct {
	currentKey  []byte
	previousKey []byte // nil if no previous key configured
	context     EncryptionContext
	enabled     bool
}

// NewSettingsEncryptor creates a new encryptor using the secrets provider.
// If no encryption key is found, returns a disabled encryptor that passes values through.
func NewSettingsEncryptor(ctx context.Context, provider secrets.Provider) (*SettingsEncryptor, error) {
	logger := slogging.Get()

	// Retrieve current encryption key
	keyHex, err := provider.GetSecret(ctx, secrets.SecretKeys.SettingsEncryptionKey)
	if err != nil {
		if errors.Is(err, secrets.ErrSecretNotFound) {
			logger.Warn("No settings encryption key configured; settings will be stored in plaintext")
			return &SettingsEncryptor{enabled: false}, nil
		}
		return nil, fmt.Errorf("failed to retrieve settings encryption key: %w", err)
	}

	currentKey, err := decodeHexKey(keyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid settings encryption key: %w", err)
	}

	// Retrieve context ID (optional, defaults to 1)
	contextID := 1
	if cidStr, err := provider.GetSecret(ctx, secrets.SecretKeys.SettingsEncryptionContextID); err == nil {
		if parsed, err := strconv.Atoi(cidStr); err == nil && parsed > 0 {
			contextID = parsed
		} else {
			logger.Warn("Invalid settings encryption context ID '%s', using default 1", cidStr)
		}
	}

	enc := &SettingsEncryptor{
		currentKey: currentKey,
		enabled:    true,
		context: EncryptionContext{
			ContextID: contextID,
			Algorithm: "aes-256-gcm",
		},
	}

	// Retrieve previous encryption key (optional, for key rotation)
	prevKeyHex, err := provider.GetSecret(ctx, secrets.SecretKeys.SettingsEncryptionPreviousKey)
	if err == nil {
		prevKey, err := decodeHexKey(prevKeyHex)
		if err != nil {
			return nil, fmt.Errorf("invalid settings encryption previous key: %w", err)
		}
		enc.previousKey = prevKey
		logger.Info("Previous encryption key configured for key rotation")
	} else if !errors.Is(err, secrets.ErrSecretNotFound) {
		return nil, fmt.Errorf("failed to retrieve settings encryption previous key: %w", err)
	}

	logger.Info("Settings encryption enabled (context ID: %d, algorithm: %s, previous key: %v)",
		contextID, "aes-256-gcm", enc.previousKey != nil)

	return enc, nil
}

// NewSettingsEncryptorFromKeys creates an encryptor directly from key bytes.
// This is primarily for testing. contextID defaults to 1.
func NewSettingsEncryptorFromKeys(currentKey, previousKey []byte, contextID int) (*SettingsEncryptor, error) {
	if len(currentKey) != 32 {
		return nil, fmt.Errorf("current key must be 32 bytes, got %d", len(currentKey))
	}
	if previousKey != nil && len(previousKey) != 32 {
		return nil, fmt.Errorf("previous key must be 32 bytes, got %d", len(previousKey))
	}
	if contextID <= 0 {
		contextID = 1
	}
	return &SettingsEncryptor{
		currentKey:  currentKey,
		previousKey: previousKey,
		enabled:     true,
		context: EncryptionContext{
			ContextID: contextID,
			Algorithm: "aes-256-gcm",
		},
	}, nil
}

// Encrypt encrypts a plaintext value using AES-256-GCM with the current key.
// Returns the original value unchanged if encryption is disabled.
func (e *SettingsEncryptor) Encrypt(plaintext string) (string, error) {
	if !e.enabled {
		return plaintext, nil
	}

	ciphertext, err := encryptAESGCM(e.currentKey, []byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("encryption failed: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	timestamp := time.Now().Unix()

	result := fmt.Sprintf("ENC:v1:%d:%d:%s", e.context.ContextID, timestamp, encoded)

	if len(result) > MaxEncryptedValueLength {
		return "", fmt.Errorf("%w: %d chars (max %d)", ErrValueTooLong, len(result), MaxEncryptedValueLength)
	}

	return result, nil
}

// Decrypt decrypts an encrypted value. If the value doesn't have the ENC: prefix,
// it is returned as-is (plaintext passthrough). Tries the current key first,
// then the previous key if configured and the current key fails.
func (e *SettingsEncryptor) Decrypt(value string) (string, error) {
	if !IsEncrypted(value) {
		return value, nil
	}

	parts := strings.SplitN(value, ":", 5)
	if len(parts) != 5 || parts[0] != "ENC" || parts[1] != "v1" {
		return "", fmt.Errorf("invalid encrypted value format")
	}

	// parts[2] = contextId (informational)
	// parts[3] = timestamp (informational)
	// parts[4] = base64(nonce + ciphertext + tag)

	data, err := base64.StdEncoding.DecodeString(parts[4])
	if err != nil {
		return "", fmt.Errorf("failed to decode encrypted value: %w", err)
	}

	// Try current key
	if e.currentKey != nil {
		plaintext, err := decryptAESGCM(e.currentKey, data)
		if err == nil {
			return string(plaintext), nil
		}
	}

	// Try previous key if available
	if e.previousKey != nil {
		logger := slogging.Get()
		plaintext, err := decryptAESGCM(e.previousKey, data)
		if err == nil {
			logger.Debug("Decrypted setting with previous key (will re-encrypt with current key on next write)")
			return string(plaintext), nil
		}
	}

	return "", fmt.Errorf("decryption failed: value could not be decrypted with current or previous key")
}

// IsEncrypted returns true if the value has the ENC: prefix indicating encryption.
func IsEncrypted(value string) bool {
	return strings.HasPrefix(value, "ENC:")
}

// IsEnabled returns true if encryption is configured and active.
func (e *SettingsEncryptor) IsEnabled() bool {
	return e.enabled
}

// HasPreviousKey returns true if a previous key is configured for key rotation.
func (e *SettingsEncryptor) HasPreviousKey() bool {
	return e.previousKey != nil
}

// GetContext returns the current encryption context.
func (e *SettingsEncryptor) GetContext() EncryptionContext {
	return e.context
}

// encryptAESGCM encrypts plaintext using AES-256-GCM.
// Returns nonce + ciphertext + tag concatenated.
func encryptAESGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends ciphertext+tag to nonce, so result is nonce+ciphertext+tag
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptAESGCM decrypts data that is nonce + ciphertext + tag concatenated.
func decryptAESGCM(key, data []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// decodeHexKey decodes a hex-encoded encryption key and validates its length.
func decodeHexKey(hexKey string) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil {
		return nil, fmt.Errorf("key must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	return key, nil
}
