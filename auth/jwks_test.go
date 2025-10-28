package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateJWKFromRSAPublicKey(t *testing.T) {
	// Generate test RSA key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create test JWT config with custom Key ID
	config := JWTConfig{
		SigningMethod:     "RS256",
		KeyID:             "test-key-123",
		ExpirationSeconds: 3600,
	}

	// Create key manager
	keyManager := &JWTKeyManager{
		config:        config,
		signingKey:    privateKey,
		verifyingKey:  &privateKey.PublicKey,
		signingMethod: jwt.SigningMethodRS256,
	}

	// Create service
	service := &Service{
		config:     Config{JWT: config},
		keyManager: keyManager,
	}

	// Create handlers
	handlers := &Handlers{
		service: service,
	}

	// Test creating JWK from public key
	jwk, err := handlers.createJWKFromPublicKey(&privateKey.PublicKey, "RS256")
	require.NoError(t, err)
	require.NotNil(t, jwk)

	// Check RFC 7517 required fields
	assert.Equal(t, "RSA", jwk.KeyType, "kty should be RSA")
	assert.Equal(t, "test-key-123", jwk.KeyID, "kid should match config")
	assert.Equal(t, "RS256", jwk.Algorithm, "alg should be RS256")
	assert.Equal(t, "sig", jwk.Use, "use should be sig")
	assert.Equal(t, []string{"verify"}, jwk.KeyOps, "key_ops should be [verify]")

	// Check RSA-specific fields
	assert.NotEmpty(t, jwk.N, "n (modulus) should be present")
	assert.NotEmpty(t, jwk.E, "e (exponent) should be present")

	// Verify optional ECDSA fields are not present
	assert.Empty(t, jwk.Curve, "crv should not be present for RSA")
	assert.Empty(t, jwk.X, "x should not be present for RSA")
	assert.Empty(t, jwk.Y, "y should not be present for RSA")

	t.Logf("✅ All RFC 7517 required fields present")
	t.Logf("✅ Custom Key ID working: %s", jwk.KeyID)
	t.Logf("✅ Key operations field present: %v", jwk.KeyOps)
}

func TestJWKConfigDefaults(t *testing.T) {
	// Set required environment variables for config loading
	require.NoError(t, os.Setenv("POSTGRES_HOST", "localhost"))
	require.NoError(t, os.Setenv("POSTGRES_PORT", "5432"))
	require.NoError(t, os.Setenv("POSTGRES_USER", "test"))
	require.NoError(t, os.Setenv("POSTGRES_PASSWORD", "test"))
	require.NoError(t, os.Setenv("POSTGRES_DATABASE", "test"))
	require.NoError(t, os.Setenv("JWT_SIGNING_METHOD", "HS256"))
	require.NoError(t, os.Setenv("JWT_SECRET", "test-secret-key-for-testing-jwk-config-defaults"))
	defer func() {
		_ = os.Unsetenv("POSTGRES_HOST")
		_ = os.Unsetenv("POSTGRES_PORT")
		_ = os.Unsetenv("POSTGRES_USER")
		_ = os.Unsetenv("POSTGRES_PASSWORD")
		_ = os.Unsetenv("POSTGRES_DATABASE")
		_ = os.Unsetenv("JWT_SIGNING_METHOD")
		_ = os.Unsetenv("JWT_SECRET")
	}()

	// Test loading config with defaults
	defaultConfig, err := LoadConfig()
	require.NoError(t, err)

	// Should default to "1" if not set via environment
	assert.Equal(t, "1", defaultConfig.JWT.KeyID, "Default Key ID should be '1'")

	t.Logf("✅ Default Key ID configuration working")
}
