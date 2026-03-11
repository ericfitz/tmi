package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/unicodecheck"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// revokeTokenInternal handles the actual token revocation logic
// This is shared between RevokeToken (RFC 7009) and MeLogout endpoints
func (h *Handlers) revokeTokenInternal(ctx context.Context, tokenString string, tokenTypeHint string) error {
	logger := slogging.Get()

	// Check if blacklist service is available
	if h.service == nil || h.service.dbManager == nil || h.service.dbManager.Redis() == nil {
		logger.Error("Token blacklist service not available")
		return fmt.Errorf("blacklist service unavailable")
	}

	// Try to determine token type if hint not provided or is access_token
	if tokenTypeHint == "" || tokenTypeHint == "access_token" {
		// Try to parse as JWT to check if it's a valid access token
		claims := jwt.MapClaims{}
		token, err := h.service.GetKeyManager().VerifyToken(tokenString, claims)
		if err == nil && token.Valid {
			// It's a valid access token - blacklist it
			blacklist := NewTokenBlacklist(h.service.dbManager.Redis().GetClient(), h.service.GetKeyManager())
			if err := blacklist.BlacklistToken(ctx, tokenString); err != nil {
				logger.Error("Failed to blacklist access token: %v", err)
				return err
			}
			logger.Debug("Access token blacklisted successfully")
			return nil
		}
		// Not a valid access token, fall through to try as refresh token if no hint
		if tokenTypeHint == "access_token" {
			// Hint was explicitly access_token but it's not valid - still return success per RFC 7009
			logger.Debug("Token provided with access_token hint is not a valid access token")
			return nil
		}
	}

	// Try as refresh token
	if tokenTypeHint == "" || tokenTypeHint == "refresh_token" {
		if err := h.service.RevokeToken(ctx, tokenString); err != nil {
			logger.Debug("Failed to revoke as refresh token (may not exist): %v", err)
			// Per RFC 7009, we still return success even if token doesn't exist
		} else {
			logger.Debug("Refresh token revoked successfully")
		}
	}

	return nil
}

// validateTokenRevocationField validates a field value for the token revocation endpoint.
// Delegates to the consolidated unicodecheck package for consistent character detection.
func validateTokenRevocationField(value, fieldName string) string {
	if value == "" {
		return ""
	}

	// Check for zero-width characters
	if unicodecheck.ContainsZeroWidthChars(value) {
		return fmt.Sprintf("%s contains invalid zero-width characters", fieldName)
	}

	// Check for control characters
	if unicodecheck.ContainsControlChars(value) {
		return fmt.Sprintf("%s contains invalid control characters", fieldName)
	}

	return ""
}

// validateTokenTypeHint validates the token_type_hint parameter
func validateTokenTypeHint(hint string) string {
	if hint == "" {
		return "" // Optional field
	}

	// Per RFC 7009, valid values are "access_token" and "refresh_token"
	validHints := map[string]bool{
		"access_token":  true,
		"refresh_token": true,
	}

	if !validHints[hint] {
		return "token_type_hint must be 'access_token' or 'refresh_token'"
	}

	return ""
}

// RevokeToken revokes a token per RFC 7009 OAuth 2.0 Token Revocation
// The token to revoke is passed in the request body, not the Authorization header.
// Authentication: Bearer token OR client credentials (client_id/client_secret)
func (h *Handlers) RevokeToken(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Define allowed fields for strict validation (prevents mass assignment)
	allowedFields := map[string]bool{
		"token":           true,
		"token_type_hint": true,
		"client_id":       true,
		"client_secret":   true,
	}

	// Bind request body (supports both JSON and form-urlencoded)
	var req struct {
		Token         string `json:"token" form:"token" binding:"required"`
		TokenTypeHint string `json:"token_type_hint" form:"token_type_hint"`
		ClientID      string `json:"client_id" form:"client_id"`
		ClientSecret  string `json:"client_secret" form:"client_secret"` //nolint:gosec // G117 - OAuth revocation request field
	}

	// Check content type to determine binding method
	contentType := c.ContentType()
	if strings.Contains(contentType, "application/json") {
		// For JSON, use strict binding that rejects unknown fields
		if errMsg := strictJSONBindForRevoke(c, &req); errMsg != "" {
			logger.Warn("Invalid JSON request body: %s", errMsg)
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": errMsg,
			})
			return
		}
	} else {
		// For form-urlencoded, bind first then check for unknown fields
		if err := c.ShouldBind(&req); err != nil {
			// Per RFC 7009 Section 2.2.1: Return 400 for missing token parameter
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_request",
				"error_description": "Missing required 'token' parameter",
			})
			return
		}

		// Check for unknown fields in form data
		if err := c.Request.ParseForm(); err == nil {
			for field := range c.Request.PostForm {
				if !allowedFields[field] {
					logger.Warn("Unknown field in revocation request: %s", field)
					c.JSON(http.StatusBadRequest, gin.H{
						"error":             "invalid_request",
						"error_description": fmt.Sprintf("Unknown field in request: %s", field),
					})
					return
				}
			}
		}
	}

	// Validate token field for malicious content
	if errMsg := validateTokenRevocationField(req.Token, "token"); errMsg != "" {
		logger.Warn("Invalid token in revocation request: %s", errMsg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": errMsg,
		})
		return
	}

	// Validate token_type_hint if provided
	if errMsg := validateTokenTypeHint(req.TokenTypeHint); errMsg != "" {
		logger.Warn("Invalid token_type_hint in revocation request: %s", errMsg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": errMsg,
		})
		return
	}

	// Validate client_id if provided
	if errMsg := validateTokenRevocationField(req.ClientID, "client_id"); errMsg != "" {
		logger.Warn("Invalid client_id in revocation request: %s", errMsg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": errMsg,
		})
		return
	}

	// Validate client_secret if provided
	if errMsg := validateTokenRevocationField(req.ClientSecret, "client_secret"); errMsg != "" {
		logger.Warn("Invalid client_secret in revocation request: %s", errMsg)
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": errMsg,
		})
		return
	}

	// Authenticate the request (one of: Bearer token OR client credentials)
	isAuthenticated := false

	// Method 1: Check for Bearer token in Authorization header
	authHeader := c.GetHeader("Authorization")
	if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
		bearerToken := after
		claims := jwt.MapClaims{}
		token, err := h.service.GetKeyManager().VerifyToken(bearerToken, claims)
		if err == nil && token.Valid {
			isAuthenticated = true
			logger.Debug("Revocation request authenticated via Bearer token")
		}
	}

	// Method 2: Check for client credentials in request body
	if !isAuthenticated && req.ClientID != "" && req.ClientSecret != "" {
		// Validate client credentials using existing service method
		_, err := h.service.HandleClientCredentialsGrant(c.Request.Context(), req.ClientID, req.ClientSecret)
		if err == nil {
			isAuthenticated = true
			logger.Debug("Revocation request authenticated via client credentials")
		} else {
			logger.Debug("Client credentials validation failed: %v", err)
		}
	}

	if !isAuthenticated {
		// RFC 7009 Section 2.2.1: 401 for invalid client credentials
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_client",
			"error_description": "Client authentication failed",
		})
		return
	}

	// Attempt to revoke the token
	// Per RFC 7009 Section 2.2: Always return 200 OK (don't leak token validity)
	_ = h.revokeTokenInternal(c.Request.Context(), req.Token, req.TokenTypeHint)

	// Clear session cookies on revocation
	if h.cookieOpts.Enabled {
		ClearTokenCookies(c, h.cookieOpts)
	}

	// RFC 7009 Section 2.2: "The authorization server responds with HTTP status code 200"
	c.JSON(http.StatusOK, gin.H{})
}

// TokenIntrospectionResponse represents the response from token introspection
type TokenIntrospectionResponse struct {
	Active    bool   `json:"active"`
	Sub       string `json:"sub,omitempty"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	Iat       int64  `json:"iat,omitempty"`
	Exp       int64  `json:"exp,omitempty"`
	Aud       string `json:"aud,omitempty"`
	Iss       string `json:"iss,omitempty"`
	TokenType string `json:"token_type,omitempty"`
	Scope     string `json:"scope,omitempty"`
}

// IntrospectToken handles token introspection requests per RFC 7662
func (h *Handlers) IntrospectToken(c *gin.Context) {
	var req struct {
		Token string `json:"token" form:"token" binding:"required"`
	}

	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request: token parameter is required",
		})
		return
	}

	// Parse and validate the JWT token using centralized verification
	claims := jwt.MapClaims{}
	token, err := h.service.GetKeyManager().VerifyToken(req.Token, claims)

	// If token parsing failed or token is invalid, return inactive
	if err != nil || !token.Valid {
		c.JSON(http.StatusOK, TokenIntrospectionResponse{
			Active: false,
		})
		return
	}

	// Extract claims from the token
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusOK, TokenIntrospectionResponse{
			Active: false,
		})
		return
	}

	// Check if token is blacklisted (if blacklist service is available)
	if h.service.dbManager != nil && h.service.dbManager.Redis() != nil {
		blacklist := NewTokenBlacklist(h.service.dbManager.Redis().GetClient(), h.service.GetKeyManager())
		isBlacklisted, err := blacklist.IsTokenBlacklisted(c.Request.Context(), req.Token)
		if err == nil && isBlacklisted {
			c.JSON(http.StatusOK, TokenIntrospectionResponse{
				Active: false,
			})
			return
		}
	}

	// Extract standard claims
	baseURL := getBaseURL(c)
	response := TokenIntrospectionResponse{
		Active:    true,
		TokenType: "bearer",
		Iss:       baseURL,
		Scope:     "openid profile email",
	}

	// Extract subject (user identifier)
	if sub, ok := claims["sub"].(string); ok {
		response.Sub = sub
	}

	// Extract email
	if email, ok := claims["email"].(string); ok {
		response.Email = email
	}

	// Extract name
	if name, ok := claims["name"].(string); ok {
		response.Name = name
	}

	// Extract issued at time
	if iat, ok := claims["iat"].(float64); ok {
		response.Iat = int64(iat)
	}

	// Extract expiration time
	if exp, ok := claims["exp"].(float64); ok {
		response.Exp = int64(exp)
	}

	// Extract audience
	if aud, ok := claims["aud"].(string); ok {
		response.Aud = aud
	}

	c.JSON(http.StatusOK, response)
}

// strictJSONBindForRevoke binds JSON request body with strict field validation.
// Rejects requests containing unknown fields to prevent mass assignment vulnerabilities.
// Also validates that required fields are present, rejects duplicate keys, and rejects
// trailing garbage after the JSON object.
func strictJSONBindForRevoke(c *gin.Context, target any) string {
	// Read body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return "Failed to read request body"
	}

	// Empty body check
	if len(body) == 0 {
		return "Request body is required"
	}

	// Check for duplicate keys in JSON (Go's json.Decoder silently overwrites duplicates)
	if errMsg := detectDuplicateJSONKeys(body); errMsg != "" {
		return errMsg
	}

	// Use strict decoder that rejects unknown fields
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		// Check for syntax errors
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return fmt.Sprintf("Invalid JSON syntax at position %d", syntaxErr.Offset)
		}
		// Unknown field errors or other decoding errors
		return fmt.Sprintf("Invalid request: %s", err.Error())
	}

	// Check for trailing garbage after the JSON object
	// decoder.More() returns true if there's more content to decode
	if decoder.More() {
		return "Invalid JSON: trailing data after object"
	}

	// Also check if there's any non-whitespace content remaining
	var trailing json.RawMessage
	if decoder.Decode(&trailing) == nil {
		return "Invalid JSON: trailing data after object"
	}

	// After decoding, check if the token field (required) is present
	// We need to check the raw JSON to see if "token" was provided
	var rawJSON map[string]any
	if err := json.Unmarshal(body, &rawJSON); err == nil {
		if _, hasToken := rawJSON["token"]; !hasToken {
			return "Missing required 'token' parameter"
		}
	}

	return ""
}

// detectDuplicateJSONKeys checks for duplicate keys in a JSON object.
// Go's standard json.Decoder silently overwrites duplicate keys with the last value,
// so we need to manually detect this.
func detectDuplicateJSONKeys(data []byte) string {
	decoder := json.NewDecoder(strings.NewReader(string(data)))

	// Use Token() to read tokens and track keys
	keys := make(map[string]bool)
	depth := 0

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Let the main decoder handle syntax errors
			return ""
		}

		switch t := token.(type) {
		case json.Delim:
			switch t {
			case '{':
				if depth == 0 {
					// Reset keys for the root object
					keys = make(map[string]bool)
				}
				depth++
			case '}':
				depth--
			case '[':
				depth++
			case ']':
				depth--
			}
		case string:
			// This could be a key (if we just entered an object) or a value
			// We only check for duplicates at the root level (depth == 1)
			if depth == 1 {
				// Check if next token is a value (meaning this string was a key)
				// We track all strings at depth 1 as potential keys
				if keys[t] {
					return fmt.Sprintf("Invalid JSON: duplicate key '%s'", t)
				}
				keys[t] = true
			}
		}
	}

	return ""
}
