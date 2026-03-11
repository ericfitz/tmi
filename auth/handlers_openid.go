package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// OpenIDConfiguration represents the OpenID Connect Discovery metadata
type OpenIDConfiguration struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	UserInfoEndpoint                  string   `json:"userinfo_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported  []string `json:"id_token_signing_alg_values_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	ClaimsSupported                   []string `json:"claims_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	IntrospectionEndpoint             string   `json:"introspection_endpoint,omitempty"`
	RevocationEndpoint                string   `json:"revocation_endpoint,omitempty"`
}

// OAuthAuthorizationServerMetadata represents OAuth 2.0 Authorization Server Metadata
type OAuthAuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	JWKSURI                           string   `json:"jwks_uri,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	ResponseModesSupported            []string `json:"response_modes_supported,omitempty"`
	GrantTypesSupported               []string `json:"grant_types_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	IntrospectionEndpoint             string   `json:"introspection_endpoint,omitempty"`
	RevocationEndpoint                string   `json:"revocation_endpoint,omitempty"`
}

// OAuthProtectedResourceMetadata represents OAuth 2.0 protected resource metadata as defined in RFC 9728
type OAuthProtectedResourceMetadata struct {
	Resource                              string   `json:"resource"`
	ScopesSupported                       []string `json:"scopes_supported,omitempty"`
	AuthorizationServers                  []string `json:"authorization_servers,omitempty"`
	JWKSURI                               string   `json:"jwks_uri,omitempty"`
	BearerMethodsSupported                []string `json:"bearer_methods_supported,omitempty"`
	ResourceName                          string   `json:"resource_name,omitempty"`
	ResourceDocumentation                 string   `json:"resource_documentation,omitempty"`
	TLSClientCertificateBoundAccessTokens bool     `json:"tls_client_certificate_bound_access_tokens"`
}

// GetOpenIDConfiguration returns OpenID Connect Discovery metadata
func (h *Handlers) GetOpenIDConfiguration(c *gin.Context) {
	baseURL := getBaseURL(c)

	config := OpenIDConfiguration{
		Issuer:                            baseURL,
		AuthorizationEndpoint:             fmt.Sprintf("%s/oauth2/authorize", baseURL),
		TokenEndpoint:                     fmt.Sprintf("%s/oauth2/token", baseURL),
		UserInfoEndpoint:                  fmt.Sprintf("%s/oauth2/userinfo", baseURL),
		JWKSURI:                           fmt.Sprintf("%s/.well-known/jwks.json", baseURL),
		ScopesSupported:                   []string{"openid", "profile", "email"},
		ResponseTypesSupported:            []string{"code"},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{"HS256"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "client_secret_basic"},
		ClaimsSupported: []string{
			"sub", "iss", "aud", "exp", "iat", "email", "email_verified",
			"name", "given_name", "family_name", "picture", "locale",
		},
		CodeChallengeMethodsSupported: []string{pkceMethodS256},
		GrantTypesSupported:           []string{"authorization_code", "refresh_token", "client_credentials"},
		RevocationEndpoint:            fmt.Sprintf("%s/oauth2/revoke", baseURL),
		IntrospectionEndpoint:         fmt.Sprintf("%s/oauth2/introspect", baseURL),
	}

	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, config)
}

// GetOAuthAuthorizationServerMetadata returns OAuth 2.0 Authorization Server metadata
func (h *Handlers) GetOAuthAuthorizationServerMetadata(c *gin.Context) {
	baseURL := getBaseURL(c)

	metadata := OAuthAuthorizationServerMetadata{
		Issuer:                            baseURL,
		AuthorizationEndpoint:             fmt.Sprintf("%s/oauth2/authorize", baseURL),
		TokenEndpoint:                     fmt.Sprintf("%s/oauth2/token", baseURL),
		JWKSURI:                           fmt.Sprintf("%s/.well-known/jwks.json", baseURL),
		ScopesSupported:                   []string{"openid", "profile", "email"},
		ResponseTypesSupported:            []string{"code"},
		CodeChallengeMethodsSupported:     []string{pkceMethodS256},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token", "client_credentials"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post", "client_secret_basic"},
		RevocationEndpoint:                fmt.Sprintf("%s/oauth2/revoke", baseURL),
		IntrospectionEndpoint:             fmt.Sprintf("%s/oauth2/introspect", baseURL),
	}

	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, metadata)
}

// GetOAuthProtectedResourceMetadata returns OAuth 2.0 protected resource metadata as per RFC 9728
func (h *Handlers) GetOAuthProtectedResourceMetadata(c *gin.Context) {
	baseURL := getBaseURL(c)

	metadata := OAuthProtectedResourceMetadata{
		Resource:                              baseURL,
		ScopesSupported:                       []string{"openid", "profile", "email"},
		AuthorizationServers:                  []string{baseURL},
		JWKSURI:                               fmt.Sprintf("%s/.well-known/jwks.json", baseURL),
		BearerMethodsSupported:                []string{"header"},
		ResourceName:                          "TMI (Threat Modeling Improved) API",
		ResourceDocumentation:                 "https://github.com/ericfitz/tmi",
		TLSClientCertificateBoundAccessTokens: false,
	}

	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, metadata)
}

// JWKSResponse represents a JSON Web Key Set response
type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

// JWK represents a JSON Web Key
type JWK struct {
	KeyType   string   `json:"kty"`
	Use       string   `json:"use,omitempty"`
	KeyOps    []string `json:"key_ops,omitempty"`
	KeyID     string   `json:"kid,omitempty"`
	Algorithm string   `json:"alg,omitempty"`
	// RSA parameters
	N string `json:"n,omitempty"` // RSA modulus
	E string `json:"e,omitempty"` // RSA exponent
	// ECDSA parameters
	Curve string `json:"crv,omitempty"` // Elliptic curve
	X     string `json:"x,omitempty"`   // X coordinate
	Y     string `json:"y,omitempty"`   // Y coordinate
}

// createJWKFromPublicKey creates a JWK from a public key
func (h *Handlers) createJWKFromPublicKey(publicKey any, signingMethod string) (*JWK, error) {
	jwk := &JWK{
		Use:       "sig",
		KeyOps:    []string{"verify"},
		KeyID:     h.service.config.JWT.KeyID,
		Algorithm: signingMethod,
	}

	switch key := publicKey.(type) {
	case *rsa.PublicKey:
		jwk.KeyType = "RSA"
		// Encode RSA modulus and exponent in base64url format
		jwk.N = base64URLEncode(key.N.Bytes())
		jwk.E = base64URLEncode(intToBytes(key.E))

	case *ecdsa.PublicKey:
		jwk.KeyType = "EC"
		// Determine the curve name
		switch key.Curve.Params().Name {
		case "P-256":
			jwk.Curve = "P-256"
		case "P-384":
			jwk.Curve = "P-384"
		case "P-521":
			jwk.Curve = "P-521"
		default:
			return nil, fmt.Errorf("unsupported ECDSA curve: %s", key.Curve.Params().Name)
		}

		// Get uncompressed point bytes (0x04 || X || Y) using non-deprecated API
		pointBytes, err := key.Bytes()
		if err != nil {
			return nil, fmt.Errorf("failed to encode ECDSA public key: %w", err)
		}
		// Uncompressed point format: 1 byte prefix (0x04) + X + Y, each coordinate is byteLen bytes
		byteLen := (len(pointBytes) - 1) / 2
		jwk.X = base64URLEncode(pointBytes[1 : 1+byteLen])
		jwk.Y = base64URLEncode(pointBytes[1+byteLen:])

	default:
		return nil, fmt.Errorf("unsupported public key type: %T", publicKey)
	}

	return jwk, nil
}

// Helper functions for JWK creation
func base64URLEncode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func intToBytes(i int) []byte {
	// Convert int to big-endian bytes
	if i == 0 {
		return []byte{0}
	}

	var bytes []byte
	for i > 0 {
		bytes = append([]byte{byte(i)}, bytes...)
		i >>= 8
	}
	return bytes
}

// GetJWKS returns the JSON Web Key Set for JWT signature verification
func (h *Handlers) GetJWKS(c *gin.Context) {
	jwks := JWKSResponse{
		Keys: []JWK{},
	}

	// Get public key from the key manager
	publicKey := h.service.keyManager.GetPublicKey()
	if publicKey != nil {
		jwk, err := h.createJWKFromPublicKey(publicKey, h.service.keyManager.GetSigningMethod())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to create JWK",
			})
			return
		}
		jwks.Keys = append(jwks.Keys, *jwk)
	}

	// Cache the response for 1 hour since keys don't change frequently
	c.Header("Cache-Control", "public, max-age=3600")
	c.JSON(http.StatusOK, jwks)
}
