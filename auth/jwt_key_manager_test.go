package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/repository"
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

func TestAuthTimeClaimRoundTrip(t *testing.T) {
	// Verifies that an auth_time claim set at mint time is preserved across
	// the JWT serialize/parse cycle. This is the substrate for #355 step-up.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

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

	want := time.Now().Truncate(time.Second).Add(-7 * time.Minute)
	claims := &Claims{
		Email:    "alice@example.com",
		Name:     "Alice",
		AuthTime: jwt.NewNumericDate(want),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://example.com",
			Subject:   "provider-user-id-123",
			Audience:  jwt.ClaimStrings{"https://example.com"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	tokenStr, err := keyManager.CreateToken(claims)
	require.NoError(t, err)

	parsed := &Claims{}
	_, err = keyManager.VerifyToken(tokenStr, parsed)
	require.NoError(t, err)

	require.NotNil(t, parsed.AuthTime, "AuthTime claim missing from parsed token")
	assert.True(t, parsed.AuthTime.Equal(want),
		"AuthTime: got %v, want %v", parsed.AuthTime.Time, want)
}

func TestGenerateTokensWithAuthTime_SetsClaim(t *testing.T) {
	// Verifies that GenerateTokensWithAuthTime sets the AuthTime claim
	// to the provided value (not time.Now()).
	svc, cleanup := setupTestServiceWithRepos(t, &stubUserRepo{}, &stubCredRepo{})
	defer cleanup()

	user := User{
		InternalUUID:   "uuid-1",
		Email:          "alice@example.com",
		Name:           "Alice",
		Provider:       "google",
		ProviderUserID: "google-sub-1",
	}
	want := time.Now().Truncate(time.Second).Add(-3 * time.Minute)

	pair, err := svc.GenerateTokensWithAuthTime(context.Background(), user, nil, want)
	require.NoError(t, err)

	parsed, err := svc.ValidateToken(pair.AccessToken)
	require.NoError(t, err)
	require.NotNil(t, parsed.AuthTime, "AuthTime claim missing")
	assert.True(t, parsed.AuthTime.Equal(want), "AuthTime: got %v, want %v", parsed.AuthTime.Time, want)
}

func TestRefreshToken_PreservesAuthTime(t *testing.T) {
	// Verifies that RefreshToken issues a new JWT whose auth_time matches
	// the auth_time of the original JWT (not time.Now()). This proves the
	// step-up auth_time invariant survives refresh-token rotation.
	const userID = "uuid-refresh-1"
	userRepo := &stubUserRepo{
		users: map[string]*repository.User{
			userID: {
				InternalUUID:   userID,
				Provider:       "google",
				ProviderUserID: "google-sub-refresh-1",
				Email:          "alice@example.com",
				Name:           "Alice",
				EmailVerified:  true,
				CreatedAt:      time.Now(),
				ModifiedAt:     time.Now(),
			},
		},
	}
	svc, cleanup := setupTestServiceWithRepos(t, userRepo, &stubCredRepo{})
	defer cleanup()

	ctx := context.Background()
	user := User{
		InternalUUID:   userID,
		Email:          "alice@example.com",
		Name:           "Alice",
		Provider:       "google",
		ProviderUserID: "google-sub-refresh-1",
	}

	originalAuthTime := time.Now().Truncate(time.Second).Add(-15 * time.Minute)
	pair, err := svc.GenerateTokensWithAuthTime(ctx, user, nil, originalAuthTime)
	require.NoError(t, err)

	// Sleep so any "auth_time = now" bug would produce a detectably different
	// value than originalAuthTime (which is 15 minutes in the past).
	time.Sleep(2 * time.Second)

	refreshed, err := svc.RefreshToken(ctx, pair.RefreshToken)
	require.NoError(t, err)

	parsed, err := svc.ValidateToken(refreshed.AccessToken)
	require.NoError(t, err)
	require.NotNil(t, parsed.AuthTime, "auth_time missing from refreshed token")
	assert.True(t, parsed.AuthTime.Equal(originalAuthTime),
		"refreshed auth_time = %v, want %v (was the value preserved across refresh?)",
		parsed.AuthTime.Time, originalAuthTime)
}
