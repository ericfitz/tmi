package api

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestClientCredentialIDGeneration tests the client_id generation logic
func TestClientCredentialIDGeneration(t *testing.T) {
	t.Run("generates unique client_id format", func(t *testing.T) {
		// Test the ID generation logic directly
		clientIDs := make(map[string]bool)
		for i := 0; i < 100; i++ {
			// Generate client_id: tmi_cc_{base64url(16_bytes)}
			clientIDBytes := make([]byte, 16)
			if _, err := rand.Read(clientIDBytes); err != nil {
				t.Fatalf("Failed to generate client_id: %v", err)
			}
			clientID := "tmi_cc_" + base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(clientIDBytes)

			// Verify uniqueness
			if clientIDs[clientID] {
				t.Errorf("Duplicate client_id generated: %s", clientID)
			}
			clientIDs[clientID] = true

			// Verify format: tmi_cc_{base64url}
			if !strings.HasPrefix(clientID, "tmi_cc_") {
				t.Errorf("Invalid client_id format: %s", clientID)
			}

			// Base64url encoding of 16 bytes = 22 chars (no padding)
			idPart := strings.TrimPrefix(clientID, "tmi_cc_")
			if len(idPart) != 22 {
				t.Errorf("Expected 22 chars after prefix, got %d: %s", len(idPart), idPart)
			}

			// Verify no padding characters
			if strings.Contains(idPart, "=") {
				t.Errorf("client_id should not contain padding: %s", idPart)
			}
		}
	})

	t.Run("uses URL-safe base64 encoding", func(t *testing.T) {
		// Generate a client_id
		clientIDBytes := make([]byte, 16)
		_, _ = rand.Read(clientIDBytes)
		clientID := "tmi_cc_" + base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(clientIDBytes)

		// Verify it only contains URL-safe characters
		idPart := strings.TrimPrefix(clientID, "tmi_cc_")
		for _, char := range idPart {
			if !((char >= 'a' && char <= 'z') ||
				(char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') ||
				char == '-' || char == '_') {
				t.Errorf("client_id contains non-URL-safe character '%c' in %s", char, idPart)
			}
		}
	})
}

// TestClientSecretGeneration tests the client_secret generation logic
func TestClientSecretGeneration(t *testing.T) {
	t.Run("generates unique client_secret", func(t *testing.T) {
		// Test the secret generation logic directly
		secrets := make(map[string]bool)
		for i := 0; i < 100; i++ {
			// Generate client_secret: 32 bytes
			secretBytes := make([]byte, 32)
			if _, err := rand.Read(secretBytes); err != nil {
				t.Fatalf("Failed to generate client_secret: %v", err)
			}
			clientSecret := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(secretBytes)

			// Verify uniqueness
			if secrets[clientSecret] {
				t.Errorf("Duplicate client_secret generated: %s", clientSecret)
			}
			secrets[clientSecret] = true

			// Base64url encoding of 32 bytes = 43 chars (no padding)
			if len(clientSecret) != 43 {
				t.Errorf("Expected 43 chars, got %d: %s", len(clientSecret), clientSecret)
			}

			// Verify no padding characters
			if strings.Contains(clientSecret, "=") {
				t.Errorf("client_secret should not contain padding: %s", clientSecret)
			}
		}
	})

	t.Run("uses URL-safe base64 encoding", func(t *testing.T) {
		// Generate a client_secret
		secretBytes := make([]byte, 32)
		_, _ = rand.Read(secretBytes)
		clientSecret := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(secretBytes)

		// Verify it only contains URL-safe characters
		for _, char := range clientSecret {
			if !((char >= 'a' && char <= 'z') ||
				(char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') ||
				char == '-' || char == '_') {
				t.Errorf("client_secret contains non-URL-safe character '%c' in %s", char, clientSecret)
			}
		}
	})
}

// TestBcryptHashing tests the bcrypt hashing logic
func TestBcryptHashing(t *testing.T) {
	t.Run("hashes client_secret with bcrypt", func(t *testing.T) {
		secret := "test-secret-12345"

		// Hash with bcrypt cost 10
		hash, err := bcrypt.GenerateFromPassword([]byte(secret), 10)
		if err != nil {
			t.Fatalf("Failed to hash secret: %v", err)
		}

		// Verify hash is not empty
		if len(hash) == 0 {
			t.Error("Expected non-empty hash")
		}

		// Verify hash can verify correct secret
		if err := bcrypt.CompareHashAndPassword(hash, []byte(secret)); err != nil {
			t.Errorf("Hash should verify correct secret: %v", err)
		}

		// Verify hash rejects incorrect secret
		if err := bcrypt.CompareHashAndPassword(hash, []byte("wrong-secret")); err == nil {
			t.Error("Hash should reject incorrect secret")
		}
	})

	t.Run("generates different hashes for same secret", func(t *testing.T) {
		secret := "test-secret"

		// Generate multiple hashes
		hashes := make(map[string]bool)
		for i := 0; i < 10; i++ {
			hash, err := bcrypt.GenerateFromPassword([]byte(secret), 10)
			if err != nil {
				t.Fatalf("Failed to hash secret: %v", err)
			}

			hashStr := string(hash)
			if hashes[hashStr] {
				t.Errorf("Duplicate hash generated for same secret")
			}
			hashes[hashStr] = true

			// All hashes should verify the secret
			if err := bcrypt.CompareHashAndPassword(hash, []byte(secret)); err != nil {
				t.Errorf("Hash should verify secret: %v", err)
			}
		}
	})
}

// TestHelperFunctions tests the pointer conversion helpers
func TestHelperFunctions(t *testing.T) {
	t.Run("StrPtr returns nil for empty string", func(t *testing.T) {
		result := StrPtr("")
		if result != nil {
			t.Error("Expected nil for empty string")
		}
	})

	t.Run("StrPtr returns pointer for non-empty string", func(t *testing.T) {
		result := StrPtr("test")
		if result == nil {
			t.Error("Expected non-nil for non-empty string")
		} else if *result != "test" {
			t.Errorf("Expected 'test', got '%s'", *result)
		}
	})

	t.Run("StrPtrOrEmpty always returns pointer", func(t *testing.T) {
		result := StrPtrOrEmpty("")
		if result == nil {
			t.Error("Expected non-nil even for empty string")
		} else if *result != "" {
			t.Errorf("Expected empty string, got '%s'", *result)
		}

		result2 := StrPtrOrEmpty("test")
		if result2 == nil {
			t.Error("Expected non-nil")
		} else if *result2 != "test" {
			t.Errorf("Expected 'test', got '%s'", *result2)
		}
	})

	t.Run("StrFromPtr returns empty for nil", func(t *testing.T) {
		result := StrFromPtr(nil)
		if result != "" {
			t.Errorf("Expected empty string, got '%s'", result)
		}
	})

	t.Run("StrFromPtr returns value for non-nil", func(t *testing.T) {
		str := "test"
		result := StrFromPtr(&str)
		if result != "test" {
			t.Errorf("Expected 'test', got '%s'", result)
		}
	})
}
