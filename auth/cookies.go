package auth

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

const (
	// AccessTokenCookieName is the cookie name for the JWT access token
	AccessTokenCookieName = "tmi_access_token"
	// RefreshTokenCookieName is the cookie name for the refresh token
	RefreshTokenCookieName = "tmi_refresh_token" //nolint:gosec // G101 - cookie name constant, not a credential
	// MaxCookieSize is the practical browser cookie size limit
	MaxCookieSize = 4093
)

// CookieOptions holds configuration for session cookie operations
type CookieOptions struct {
	Domain     string // Cookie domain (hostname)
	Secure     bool   // Require HTTPS
	Enabled    bool   // Whether cookie-based auth is enabled
	ExpiresIn  int    // Access token cookie MaxAge in seconds
	RefreshTTL int    // Refresh token cookie MaxAge in seconds
}

// SetTokenCookies sets HttpOnly cookies for access and refresh tokens on the response.
// Both cookies are HttpOnly to prevent JavaScript access (XSS protection).
// The access token cookie uses SameSite=Lax (safe for REST APIs that don't mutate on GET).
// The refresh token cookie uses SameSite=Strict with Path=/oauth2 for maximum protection.
func SetTokenCookies(c *gin.Context, tokenPair TokenPair, opts CookieOptions) {
	if !opts.Enabled {
		return
	}

	logger := slogging.Get()

	// Warn if access token exceeds typical browser cookie size limit
	if len(tokenPair.AccessToken) > MaxCookieSize {
		logger.Warn("Access token size (%d bytes) exceeds browser cookie limit (%d bytes); cookie may be silently dropped by browser. Bearer token auth remains available as fallback.",
			len(tokenPair.AccessToken), MaxCookieSize)
	}

	// Access token cookie: available to all API paths
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     AccessTokenCookieName,
		Value:    tokenPair.AccessToken,
		Path:     "/",
		Domain:   opts.Domain,
		MaxAge:   opts.ExpiresIn,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
	})

	// Refresh token cookie: restricted to token endpoints only
	if tokenPair.RefreshToken != "" {
		http.SetCookie(c.Writer, &http.Cookie{
			Name:     RefreshTokenCookieName,
			Value:    tokenPair.RefreshToken,
			Path:     "/oauth2",
			Domain:   opts.Domain,
			MaxAge:   opts.RefreshTTL,
			HttpOnly: true,
			Secure:   opts.Secure,
			SameSite: http.SameSiteStrictMode,
		})
	}
}

// ClearTokenCookies clears both token cookies by setting MaxAge=-1.
// Cookie attributes (Path, Domain, HttpOnly, Secure, SameSite) must match
// the values used when setting for browsers to clear correctly.
func ClearTokenCookies(c *gin.Context, opts CookieOptions) {
	if !opts.Enabled {
		return
	}

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     AccessTokenCookieName,
		Value:    "",
		Path:     "/",
		Domain:   opts.Domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
	})

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    "",
		Path:     "/oauth2",
		Domain:   opts.Domain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// ExtractAccessTokenFromCookie returns the access token from the request cookie, or empty string if not present.
func ExtractAccessTokenFromCookie(c *gin.Context) string {
	cookie, err := c.Cookie(AccessTokenCookieName)
	if err != nil {
		return ""
	}
	return cookie
}

// ExtractRefreshTokenFromCookie returns the refresh token from the request cookie, or empty string if not present.
func ExtractRefreshTokenFromCookie(c *gin.Context) string {
	cookie, err := c.Cookie(RefreshTokenCookieName)
	if err != nil {
		return ""
	}
	return cookie
}
