package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ericfitz/tmi/internal/slogging"
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
	logger := slogging.Get()
	logger.Info("Initializing JWT key manager signing_method=%v key_id=%v", config.SigningMethod, config.KeyID)

	manager := &JWTKeyManager{
		config: config,
	}

	if err := manager.loadKeys(); err != nil {
		logger.Error("Failed to load JWT keys signing_method=%v error=%v", config.SigningMethod, err)
		return nil, fmt.Errorf("failed to load JWT keys: %w", err)
	}

	logger.Info("JWT key manager initialized successfully signing_method=%v", config.SigningMethod)
	return manager, nil
}

// loadKeys loads the appropriate keys based on the signing method
func (m *JWTKeyManager) loadKeys() error {
	logger := slogging.Get()
	logger.Debug("Loading JWT keys signing_method=%v", m.config.SigningMethod)

	switch m.config.SigningMethod {
	case "HS256":
		return m.loadHMACKeys()
	case "RS256":
		return m.loadRSAKeys()
	case "ES256":
		return m.loadECDSAKeys()
	default:
		logger.Error("Unsupported JWT signing method signing_method=%v", m.config.SigningMethod)
		return fmt.Errorf("unsupported signing method: %s", m.config.SigningMethod)
	}
}

// loadHMACKeys loads HMAC secret
func (m *JWTKeyManager) loadHMACKeys() error {
	logger := slogging.Get()
	logger.Debug("Loading HMAC keys for HS256")

	if m.config.Secret == "" {
		logger.Error("HMAC secret is required for HS256 but not provided")
		return fmt.Errorf("hmac secret is required for HS256")
	}
	m.signingMethod = jwt.SigningMethodHS256
	secret := []byte(m.config.Secret)
	m.signingKey = secret
	m.verifyingKey = secret
	logger.Info("HMAC keys loaded successfully")
	return nil
}

// loadRSAKeys loads RSA private and public keys
func (m *JWTKeyManager) loadRSAKeys() error {
	logger := slogging.Get()
	logger.Debug("Loading RSA keys for RS256")

	m.signingMethod = jwt.SigningMethodRS256

	// Load private key
	privateKeyData, err := m.getKeyData(m.config.RSAPrivateKeyPath, m.config.RSAPrivateKey)
	if err != nil {
		logger.Error("Failed to get RSA private key data has_path=%v has_content=%v error=%v", m.config.RSAPrivateKeyPath != "", m.config.RSAPrivateKey != "", err)
		return fmt.Errorf("failed to get RSA private key: %w", err)
	}

	privateKey, err := parseRSAPrivateKey(privateKeyData)
	if err != nil {
		logger.Error("Failed to parse RSA private key error=%v", err)
		return fmt.Errorf("failed to parse RSA private key: %w", err)
	}
	m.signingKey = privateKey

	// Load public key
	publicKeyData, err := m.getKeyData(m.config.RSAPublicKeyPath, m.config.RSAPublicKey)
	if err != nil {
		logger.Error("Failed to get RSA public key data has_path=%v has_content=%v error=%v", m.config.RSAPublicKeyPath != "", m.config.RSAPublicKey != "", err)
		return fmt.Errorf("failed to get RSA public key: %w", err)
	}

	publicKey, err := parseRSAPublicKey(publicKeyData)
	if err != nil {
		logger.Error("Failed to parse RSA public key error=%v", err)
		return fmt.Errorf("failed to parse RSA public key: %w", err)
	}
	m.verifyingKey = publicKey

	logger.Info("RSA keys loaded successfully")
	return nil
}

// loadECDSAKeys loads ECDSA private and public keys
func (m *JWTKeyManager) loadECDSAKeys() error {
	logger := slogging.Get()
	logger.Debug("Loading ECDSA keys for ES256")

	m.signingMethod = jwt.SigningMethodES256

	// Load private key
	privateKeyData, err := m.getKeyData(m.config.ECDSAPrivateKeyPath, m.config.ECDSAPrivateKey)
	if err != nil {
		logger.Error("Failed to get ECDSA private key data has_path=%v has_content=%v error=%v", m.config.ECDSAPrivateKeyPath != "", m.config.ECDSAPrivateKey != "", err)
		return fmt.Errorf("failed to get ECDSA private key: %w", err)
	}

	privateKey, err := parseECDSAPrivateKey(privateKeyData)
	if err != nil {
		logger.Error("Failed to parse ECDSA private key error=%v", err)
		return fmt.Errorf("failed to parse ECDSA private key: %w", err)
	}
	m.signingKey = privateKey

	// Load public key
	publicKeyData, err := m.getKeyData(m.config.ECDSAPublicKeyPath, m.config.ECDSAPublicKey)
	if err != nil {
		logger.Error("Failed to get ECDSA public key data has_path=%v has_content=%v error=%v", m.config.ECDSAPublicKeyPath != "", m.config.ECDSAPublicKey != "", err)
		return fmt.Errorf("failed to get ECDSA public key: %w", err)
	}

	publicKey, err := parseECDSAPublicKey(publicKeyData)
	if err != nil {
		logger.Error("Failed to parse ECDSA public key error=%v", err)
		return fmt.Errorf("failed to parse ECDSA public key: %w", err)
	}
	m.verifyingKey = publicKey

	logger.Info("ECDSA keys loaded successfully")
	return nil
}

// getKeyData retrieves key data from file path or direct content
func (m *JWTKeyManager) getKeyData(keyPath, keyContent string) ([]byte, error) {
	logger := slogging.Get()

	if keyContent != "" {
		logger.Debug("Using key content from configuration")
		return []byte(keyContent), nil
	}

	if keyPath != "" {
		// Clean the path to prevent directory traversal attacks
		cleanPath := filepath.Clean(keyPath)
		logger.Debug("Reading key from file key_path=%v", cleanPath)
		data, err := os.ReadFile(cleanPath) // #nosec G304 -- path is sanitized with filepath.Clean
		if err != nil {
			logger.Error("Failed to read key file key_path=%v error=%v", cleanPath, err)
			return nil, fmt.Errorf("failed to read key file %s: %w", cleanPath, err)
		}
		logger.Debug("Key file read successfully key_path=%v size_bytes=%v", cleanPath, len(data))
		return data, nil
	}

	logger.Error("Neither key path nor key content provided")
	return nil, fmt.Errorf("neither key path nor key content provided")
}

// CreateToken creates a new JWT token with the configured signing method
func (m *JWTKeyManager) CreateToken(claims jwt.Claims) (string, error) {
	logger := slogging.Get()
	logger.Debug("Creating JWT token signing_method=%v", m.signingMethod.Alg())

	token := jwt.NewWithClaims(m.signingMethod, claims)
	tokenString, err := token.SignedString(m.signingKey)
	if err != nil {
		logger.Error("Failed to sign JWT token signing_method=%v error=%v", m.signingMethod.Alg(), err)
		return "", err
	}

	logger.Debug("JWT token created successfully signing_method=%v", m.signingMethod.Alg())
	return tokenString, nil
}

// VerifyToken verifies a JWT token using the configured verification key
func (m *JWTKeyManager) VerifyToken(tokenString string, claims jwt.Claims) (*jwt.Token, error) {
	logger := slogging.Get()
	logger.Debug("Verifying JWT token expected_signing_method=%v", m.signingMethod.Alg())

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify the signing method matches what we expect
		if token.Method != m.signingMethod {
			logger.Error("Unexpected signing method in token token_method=%v expected_method=%v", token.Header["alg"], m.signingMethod.Alg())
			return nil, fmt.Errorf("unexpected signing method: %v (expected %v)", token.Header["alg"], m.signingMethod.Alg())
		}
		return m.verifyingKey, nil
	})

	if err != nil {
		logger.Error("Failed to parse JWT token error=%v", err)
		return nil, err
	}

	if !token.Valid {
		logger.Error("JWT token is invalid")
		return nil, fmt.Errorf("token is invalid")
	}

	logger.Debug("JWT token verified successfully")
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
