package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseRSAPrivateKey_EdgeCases tests RSA key parsing with malformed inputs.
func TestParseRSAPrivateKey_EdgeCases(t *testing.T) {
	t.Run("valid_pkcs1_format", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		})

		parsed, err := parseRSAPrivateKey(pemBytes)
		require.NoError(t, err)
		assert.NotNil(t, parsed)
	})

	t.Run("valid_pkcs8_format", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: pkcs8Bytes,
		})

		parsed, err := parseRSAPrivateKey(pemBytes)
		require.NoError(t, err)
		assert.NotNil(t, parsed)
	})

	t.Run("empty_input_fails", func(t *testing.T) {
		_, err := parseRSAPrivateKey([]byte{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode PEM block")
	})

	t.Run("non_pem_data_fails", func(t *testing.T) {
		_, err := parseRSAPrivateKey([]byte("not a PEM block"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode PEM block")
	})

	t.Run("wrong_pem_type_fails", func(t *testing.T) {
		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: []byte("fake certificate data"),
		})

		_, err := parseRSAPrivateKey(pemBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported private key type")
	})

	t.Run("corrupted_key_data_fails", func(t *testing.T) {
		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: []byte("corrupted key data that is not valid ASN.1"),
		})

		_, err := parseRSAPrivateKey(pemBytes)
		assert.Error(t, err)
	})

	t.Run("ecdsa_key_in_pkcs8_format_fails", func(t *testing.T) {
		// Put an ECDSA key in a PRIVATE KEY PEM — should fail the RSA type assertion
		ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(ecKey)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: pkcs8Bytes,
		})

		_, err = parseRSAPrivateKey(pemBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an RSA private key")
	})
}

// TestParseECDSAPrivateKey_EdgeCases tests ECDSA key parsing with malformed inputs.
func TestParseECDSAPrivateKey_EdgeCases(t *testing.T) {
	t.Run("valid_ec_private_key_format", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		ecBytes, err := x509.MarshalECPrivateKey(key)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: ecBytes,
		})

		parsed, err := parseECDSAPrivateKey(pemBytes)
		require.NoError(t, err)
		assert.NotNil(t, parsed)
	})

	t.Run("valid_pkcs8_format", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(key)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: pkcs8Bytes,
		})

		parsed, err := parseECDSAPrivateKey(pemBytes)
		require.NoError(t, err)
		assert.NotNil(t, parsed)
	})

	t.Run("empty_input_fails", func(t *testing.T) {
		_, err := parseECDSAPrivateKey([]byte{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode PEM block")
	})

	t.Run("non_pem_data_fails", func(t *testing.T) {
		_, err := parseECDSAPrivateKey([]byte("not a PEM block at all"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode PEM block")
	})

	t.Run("wrong_pem_type_fails", func(t *testing.T) {
		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: []byte("fake rsa data"),
		})

		_, err := parseECDSAPrivateKey(pemBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported private key type")
	})

	t.Run("corrupted_key_data_fails", func(t *testing.T) {
		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "EC PRIVATE KEY",
			Bytes: []byte("corrupted data"),
		})

		_, err := parseECDSAPrivateKey(pemBytes)
		assert.Error(t, err)
	})

	t.Run("rsa_key_in_pkcs8_format_fails", func(t *testing.T) {
		// Put an RSA key in a PRIVATE KEY PEM — should fail the ECDSA type assertion
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(rsaKey)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: pkcs8Bytes,
		})

		_, err = parseECDSAPrivateKey(pemBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an ECDSA private key")
	})

	t.Run("different_curves", func(t *testing.T) {
		curves := []struct {
			name  string
			curve elliptic.Curve
		}{
			{"P256", elliptic.P256()},
			{"P384", elliptic.P384()},
			{"P521", elliptic.P521()},
		}

		for _, c := range curves {
			t.Run(c.name, func(t *testing.T) {
				key, err := ecdsa.GenerateKey(c.curve, rand.Reader)
				require.NoError(t, err)

				ecBytes, err := x509.MarshalECPrivateKey(key)
				require.NoError(t, err)

				pemBytes := pem.EncodeToMemory(&pem.Block{
					Type:  "EC PRIVATE KEY",
					Bytes: ecBytes,
				})

				parsed, err := parseECDSAPrivateKey(pemBytes)
				require.NoError(t, err)
				assert.NotNil(t, parsed)
				assert.Equal(t, c.curve, parsed.Curve)
			})
		}
	})
}

// TestParseRSAPublicKey_EdgeCases tests RSA public key parsing.
func TestParseRSAPublicKey_EdgeCases(t *testing.T) {
	t.Run("valid_pkix_format", func(t *testing.T) {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		pkixBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pkixBytes,
		})

		parsed, err := parseRSAPublicKey(pemBytes)
		require.NoError(t, err)
		assert.NotNil(t, parsed)
	})

	t.Run("empty_input_fails", func(t *testing.T) {
		_, err := parseRSAPublicKey([]byte{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode PEM block")
	})

	t.Run("wrong_pem_type_fails", func(t *testing.T) {
		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: []byte("fake"),
		})

		_, err := parseRSAPublicKey(pemBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported public key type")
	})

	t.Run("ecdsa_public_key_fails_rsa_parse", func(t *testing.T) {
		ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		pkixBytes, err := x509.MarshalPKIXPublicKey(&ecKey.PublicKey)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pkixBytes,
		})

		_, err = parseRSAPublicKey(pemBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an RSA public key")
	})
}

// TestParseECDSAPublicKey_EdgeCases tests ECDSA public key parsing.
func TestParseECDSAPublicKey_EdgeCases(t *testing.T) {
	t.Run("valid_pkix_format", func(t *testing.T) {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		pkixBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pkixBytes,
		})

		parsed, err := parseECDSAPublicKey(pemBytes)
		require.NoError(t, err)
		assert.NotNil(t, parsed)
	})

	t.Run("empty_input_fails", func(t *testing.T) {
		_, err := parseECDSAPublicKey([]byte{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode PEM block")
	})

	t.Run("rsa_public_key_fails_ecdsa_parse", func(t *testing.T) {
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		pkixBytes, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
		require.NoError(t, err)

		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pkixBytes,
		})

		_, err = parseECDSAPublicKey(pemBytes)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not an ECDSA public key")
	})
}

// TestJWTKeyManager_WrongKeyVerification documents that tokens signed with one key
// cannot be verified with a different key.
func TestJWTKeyManager_WrongKeyVerification(t *testing.T) {
	t.Run("hmac_wrong_secret_fails", func(t *testing.T) {
		km1, err := NewJWTKeyManager(JWTConfig{
			SigningMethod: "HS256",
			Secret:        "secret-key-one",
		})
		require.NoError(t, err)

		km2, err := NewJWTKeyManager(JWTConfig{
			SigningMethod: "HS256",
			Secret:        "secret-key-two",
		})
		require.NoError(t, err)

		// Create token with key 1
		claims := jwt.MapClaims{
			"sub": "user",
			"exp": float64(9999999999),
		}
		tokenStr, err := km1.CreateToken(claims)
		require.NoError(t, err)

		// Verify with key 2 should fail
		verifyClaims := jwt.MapClaims{}
		_, err = km2.VerifyToken(tokenStr, verifyClaims)
		assert.Error(t, err, "Token signed with different key should fail verification")
	})
}
