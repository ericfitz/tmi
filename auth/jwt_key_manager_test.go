package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWTKeyManager_HMAC(t *testing.T) {
	config := JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret-key",
	}

	keyManager, err := NewJWTKeyManager(config)
	require.NoError(t, err)

	// Test token creation and verification
	claims := jwt.MapClaims{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	tokenString, err := keyManager.CreateToken(claims)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenString)

	// Verify the token
	verifyClaims := jwt.MapClaims{}
	token, err := keyManager.VerifyToken(tokenString, verifyClaims)
	require.NoError(t, err)
	assert.True(t, token.Valid)
	assert.Equal(t, "test-user", verifyClaims["sub"])

	// Test public key (should be nil for HMAC)
	publicKey := keyManager.GetPublicKey()
	assert.Nil(t, publicKey)

	assert.Equal(t, "HS256", keyManager.GetSigningMethod())
}

func TestJWTKeyManager_RSA(t *testing.T) {
	// Generate RSA key pair for testing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Convert to PEM format
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	publicKeyPKIX, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	publicKeyPEM := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyPKIX,
	}
	publicKeyBytes := pem.EncodeToMemory(publicKeyPEM)

	config := JWTConfig{
		SigningMethod: "RS256",
		RSAPrivateKey: string(privateKeyBytes),
		RSAPublicKey:  string(publicKeyBytes),
	}

	keyManager, err := NewJWTKeyManager(config)
	require.NoError(t, err)

	// Test token creation and verification
	claims := jwt.MapClaims{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	tokenString, err := keyManager.CreateToken(claims)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenString)

	// Verify the token
	verifyClaims := jwt.MapClaims{}
	token, err := keyManager.VerifyToken(tokenString, verifyClaims)
	require.NoError(t, err)
	assert.True(t, token.Valid)
	assert.Equal(t, "test-user", verifyClaims["sub"])

	// Test public key
	publicKey := keyManager.GetPublicKey()
	assert.NotNil(t, publicKey)
	_, ok := publicKey.(*rsa.PublicKey)
	assert.True(t, ok, "Expected RSA public key")

	assert.Equal(t, "RS256", keyManager.GetSigningMethod())
}

func TestJWTKeyManager_ECDSA(t *testing.T) {
	// Generate ECDSA key pair for testing
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Convert to PEM format
	privateKeyBytes, err := x509.MarshalECPrivateKey(privateKey)
	require.NoError(t, err)
	privateKeyPEM := &pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyBytes,
	}
	privateKeyPEMBytes := pem.EncodeToMemory(privateKeyPEM)

	publicKeyPKIX, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	publicKeyPEM := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyPKIX,
	}
	publicKeyPEMBytes := pem.EncodeToMemory(publicKeyPEM)

	config := JWTConfig{
		SigningMethod:   "ES256",
		ECDSAPrivateKey: string(privateKeyPEMBytes),
		ECDSAPublicKey:  string(publicKeyPEMBytes),
	}

	keyManager, err := NewJWTKeyManager(config)
	require.NoError(t, err)

	// Test token creation and verification
	claims := jwt.MapClaims{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}

	tokenString, err := keyManager.CreateToken(claims)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenString)

	// Verify the token
	verifyClaims := jwt.MapClaims{}
	token, err := keyManager.VerifyToken(tokenString, verifyClaims)
	require.NoError(t, err)
	assert.True(t, token.Valid)
	assert.Equal(t, "test-user", verifyClaims["sub"])

	// Test public key
	publicKey := keyManager.GetPublicKey()
	assert.NotNil(t, publicKey)
	_, ok := publicKey.(*ecdsa.PublicKey)
	assert.True(t, ok, "Expected ECDSA public key")

	assert.Equal(t, "ES256", keyManager.GetSigningMethod())
}

func TestJWTKeyManager_InvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		config JWTConfig
	}{
		{
			name: "Unsupported signing method",
			config: JWTConfig{
				SigningMethod: "UNSUPPORTED",
			},
		},
		{
			name: "Missing RSA keys",
			config: JWTConfig{
				SigningMethod: "RS256",
			},
		},
		{
			name: "Missing ECDSA keys",
			config: JWTConfig{
				SigningMethod: "ES256",
			},
		},
		{
			name: "Missing HMAC secret",
			config: JWTConfig{
				SigningMethod: "HS256",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewJWTKeyManager(tt.config)
			assert.Error(t, err)
		})
	}
}

func TestJWTKeyManager_InvalidToken(t *testing.T) {
	config := JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret-key",
	}

	keyManager, err := NewJWTKeyManager(config)
	require.NoError(t, err)

	// Test with invalid token
	claims := jwt.MapClaims{}
	token, err := keyManager.VerifyToken("invalid.token.here", claims)
	assert.Error(t, err)
	assert.Nil(t, token)
}

func TestJWTKeyManager_ExpiredToken(t *testing.T) {
	config := JWTConfig{
		SigningMethod: "HS256",
		Secret:        "test-secret-key",
	}

	keyManager, err := NewJWTKeyManager(config)
	require.NoError(t, err)

	// Create expired token
	claims := jwt.MapClaims{
		"sub": "test-user",
		"exp": time.Now().Add(-time.Hour).Unix(), // Expired 1 hour ago
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
	}

	tokenString, err := keyManager.CreateToken(claims)
	require.NoError(t, err)

	// Verify the expired token
	verifyClaims := jwt.MapClaims{}
	_, err = keyManager.VerifyToken(tokenString, verifyClaims)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "token is expired")
}
