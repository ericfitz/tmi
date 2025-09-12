package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/golang-jwt/jwt/v5"
)

// JWTKeyManager manages JWT signing and verification keys
type JWTKeyManager struct {
	config        JWTConfig
	signingKey    interface{} // Private key for signing ([]byte, *rsa.PrivateKey, or *ecdsa.PrivateKey)
	verifyingKey  interface{} // Public key for verification ([]byte, *rsa.PublicKey, or *ecdsa.PublicKey)
	signingMethod jwt.SigningMethod
}

// NewJWTKeyManager creates a new JWT key manager
func NewJWTKeyManager(config JWTConfig) (*JWTKeyManager, error) {
	manager := &JWTKeyManager{
		config: config,
	}

	if err := manager.loadKeys(); err != nil {
		return nil, fmt.Errorf("failed to load JWT keys: %w", err)
	}

	return manager, nil
}

// loadKeys loads the appropriate keys based on the signing method
func (m *JWTKeyManager) loadKeys() error {
	switch m.config.SigningMethod {
	case "HS256":
		return m.loadHMACKeys()
	case "RS256":
		return m.loadRSAKeys()
	case "ES256":
		return m.loadECDSAKeys()
	default:
		return fmt.Errorf("unsupported signing method: %s", m.config.SigningMethod)
	}
}

// loadHMACKeys loads HMAC secret
func (m *JWTKeyManager) loadHMACKeys() error {
	if m.config.Secret == "" {
		return fmt.Errorf("hmac secret is required for HS256")
	}
	m.signingMethod = jwt.SigningMethodHS256
	secret := []byte(m.config.Secret)
	m.signingKey = secret
	m.verifyingKey = secret
	return nil
}

// loadRSAKeys loads RSA private and public keys
func (m *JWTKeyManager) loadRSAKeys() error {
	m.signingMethod = jwt.SigningMethodRS256

	// Load private key
	privateKeyData, err := m.getKeyData(m.config.RSAPrivateKeyPath, m.config.RSAPrivateKey)
	if err != nil {
		return fmt.Errorf("failed to get RSA private key: %w", err)
	}

	privateKey, err := parseRSAPrivateKey(privateKeyData)
	if err != nil {
		return fmt.Errorf("failed to parse RSA private key: %w", err)
	}
	m.signingKey = privateKey

	// Load public key
	publicKeyData, err := m.getKeyData(m.config.RSAPublicKeyPath, m.config.RSAPublicKey)
	if err != nil {
		return fmt.Errorf("failed to get RSA public key: %w", err)
	}

	publicKey, err := parseRSAPublicKey(publicKeyData)
	if err != nil {
		return fmt.Errorf("failed to parse RSA public key: %w", err)
	}
	m.verifyingKey = publicKey

	return nil
}

// loadECDSAKeys loads ECDSA private and public keys
func (m *JWTKeyManager) loadECDSAKeys() error {
	m.signingMethod = jwt.SigningMethodES256

	// Load private key
	privateKeyData, err := m.getKeyData(m.config.ECDSAPrivateKeyPath, m.config.ECDSAPrivateKey)
	if err != nil {
		return fmt.Errorf("failed to get ECDSA private key: %w", err)
	}

	privateKey, err := parseECDSAPrivateKey(privateKeyData)
	if err != nil {
		return fmt.Errorf("failed to parse ECDSA private key: %w", err)
	}
	m.signingKey = privateKey

	// Load public key
	publicKeyData, err := m.getKeyData(m.config.ECDSAPublicKeyPath, m.config.ECDSAPublicKey)
	if err != nil {
		return fmt.Errorf("failed to get ECDSA public key: %w", err)
	}

	publicKey, err := parseECDSAPublicKey(publicKeyData)
	if err != nil {
		return fmt.Errorf("failed to parse ECDSA public key: %w", err)
	}
	m.verifyingKey = publicKey

	return nil
}

// getKeyData retrieves key data from file path or direct content
func (m *JWTKeyManager) getKeyData(keyPath, keyContent string) ([]byte, error) {
	if keyContent != "" {
		return []byte(keyContent), nil
	}

	if keyPath != "" {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read key file %s: %w", keyPath, err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("neither key path nor key content provided")
}

// CreateToken creates a new JWT token with the configured signing method
func (m *JWTKeyManager) CreateToken(claims jwt.Claims) (string, error) {
	token := jwt.NewWithClaims(m.signingMethod, claims)
	return token.SignedString(m.signingKey)
}

// VerifyToken verifies a JWT token using the configured verification key
func (m *JWTKeyManager) VerifyToken(tokenString string, claims jwt.Claims) (*jwt.Token, error) {
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method matches what we expect
		if token.Method != m.signingMethod {
			return nil, fmt.Errorf("unexpected signing method: %v (expected %v)", token.Header["alg"], m.signingMethod.Alg())
		}
		return m.verifyingKey, nil
	})

	if err != nil {
		return nil, err
	}

	if !token.Valid {
		return nil, fmt.Errorf("token is invalid")
	}

	return token, nil
}

// GetPublicKey returns the public key for JWKS endpoint (for asymmetric methods)
func (m *JWTKeyManager) GetPublicKey() interface{} {
	switch m.config.SigningMethod {
	case "RS256", "ES256":
		return m.verifyingKey
	default:
		return nil // HMAC doesn't expose public keys
	}
}

// GetSigningMethod returns the current signing method
func (m *JWTKeyManager) GetSigningMethod() string {
	return m.config.SigningMethod
}

// Key parsing utility functions

func parseRSAPrivateKey(keyData []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	switch block.Type {
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA private key")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported private key type: %s", block.Type)
	}
}

func parseRSAPublicKey(keyData []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	switch block.Type {
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA public key")
		}
		return rsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported public key type: %s", block.Type)
	}
}

func parseECDSAPrivateKey(keyData []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		ecdsaKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not an ECDSA private key")
		}
		return ecdsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported private key type: %s", block.Type)
	}
}

func parseECDSAPublicKey(keyData []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(keyData)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	switch block.Type {
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		ecdsaKey, ok := key.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("not an ECDSA public key")
		}
		return ecdsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported public key type: %s", block.Type)
	}
}
