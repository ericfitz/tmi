// Package wwwauth builds RFC 6750 (OAuth 2.0 Bearer Token) WWW-Authenticate
// response headers. It is a small, dependency-free package so that both the
// api and auth packages can share a single RFC 6750 implementation without
// creating an import cycle (auth must not import api).
package wwwauth

import (
	"fmt"
	"strings"
)

// Realm identifies the protection space for Bearer token authentication.
// This is a static value for TMI's API - all protected endpoints share the same realm.
const Realm = "tmi"

// RFC 6750 section 3.1 error codes.
const (
	// ErrInvalidRequest indicates the request is malformed or missing parameters.
	ErrInvalidRequest = "invalid_request"
	// ErrInvalidToken indicates the token is expired, revoked, or malformed.
	ErrInvalidToken = "invalid_token"
	// ErrInsufficientScope indicates the request requires higher privileges.
	ErrInsufficientScope = "insufficient_scope"
)

// BuildHeader builds the value for a RFC 6750 compliant WWW-Authenticate header.
// Per RFC 6750 section 3, the value always includes the realm and optionally an
// error and error_description. This is the single source of truth for the
// header format so a compliance change lands in one place.
//
// Parameters:
//   - errType: Error code (invalid_request, invalid_token, insufficient_scope) or empty for a basic challenge
//   - description: Human-readable error description (optional, ignored if errType is empty)
//
// SEM@212287c6c02d99be7f8071b21a50666223646bec: build a RFC 6750 Bearer WWW-Authenticate header value (pure)
func BuildHeader(errType, description string) string {
	// Start with realm (always included per best practice).
	header := fmt.Sprintf(`Bearer realm="%s"`, Realm)

	// Add error and error_description if provided.
	if errType != "" {
		header += fmt.Sprintf(`, error="%s"`, errType)
		if description != "" {
			// Escape quotes in description per RFC 6750 auth-param ABNF.
			escapedDesc := strings.ReplaceAll(description, `"`, `\"`)
			header += fmt.Sprintf(`, error_description="%s"`, escapedDesc)
		}
	}

	return header
}
