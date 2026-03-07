package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c, w
}

func TestSetTokenCookies_SetsBothCookies(t *testing.T) {
	c, w := setupTestContext()
	tokenPair := TokenPair{
		AccessToken:  "access-token-value",
		RefreshToken: "refresh-token-value",
		ExpiresIn:    3600,
		TokenType:    "Bearer",
	}
	opts := CookieOptions{
		Domain:     "example.com",
		Secure:     true,
		Enabled:    true,
		ExpiresIn:  3600,
		RefreshTTL: 604800,
	}

	SetTokenCookies(c, tokenPair, opts)

	cookies := w.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies, got %d", len(cookies))
	}

	// Find access token cookie
	var accessCookie, refreshCookie *http.Cookie
	for _, cookie := range cookies {
		switch cookie.Name {
		case AccessTokenCookieName:
			accessCookie = cookie
		case RefreshTokenCookieName:
			refreshCookie = cookie
		}
	}

	if accessCookie == nil {
		t.Fatal("access token cookie not found")
	}
	if accessCookie.Value != "access-token-value" {
		t.Errorf("access token value = %q, want %q", accessCookie.Value, "access-token-value")
	}
	if !accessCookie.HttpOnly {
		t.Error("access token cookie should be HttpOnly")
	}
	if !accessCookie.Secure {
		t.Error("access token cookie should be Secure")
	}
	if accessCookie.Path != "/" {
		t.Errorf("access token path = %q, want %q", accessCookie.Path, "/")
	}
	if accessCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("access token SameSite = %v, want Lax", accessCookie.SameSite)
	}
	if accessCookie.MaxAge != 3600 {
		t.Errorf("access token MaxAge = %d, want 3600", accessCookie.MaxAge)
	}

	if refreshCookie == nil {
		t.Fatal("refresh token cookie not found")
	}
	if refreshCookie.Value != "refresh-token-value" {
		t.Errorf("refresh token value = %q, want %q", refreshCookie.Value, "refresh-token-value")
	}
	if !refreshCookie.HttpOnly {
		t.Error("refresh token cookie should be HttpOnly")
	}
	if refreshCookie.Path != "/oauth2" {
		t.Errorf("refresh token path = %q, want %q", refreshCookie.Path, "/oauth2")
	}
	if refreshCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("refresh token SameSite = %v, want Strict", refreshCookie.SameSite)
	}
	if refreshCookie.MaxAge != 604800 {
		t.Errorf("refresh token MaxAge = %d, want 604800", refreshCookie.MaxAge)
	}
}

func TestSetTokenCookies_SkipsWhenDisabled(t *testing.T) {
	c, w := setupTestContext()
	tokenPair := TokenPair{
		AccessToken:  "access-token-value",
		RefreshToken: "refresh-token-value",
	}
	opts := CookieOptions{
		Enabled: false,
	}

	SetTokenCookies(c, tokenPair, opts)

	cookies := w.Result().Cookies()
	if len(cookies) != 0 {
		t.Errorf("expected 0 cookies when disabled, got %d", len(cookies))
	}
}

func TestSetTokenCookies_SkipsRefreshWhenEmpty(t *testing.T) {
	c, w := setupTestContext()
	tokenPair := TokenPair{
		AccessToken:  "access-token-value",
		RefreshToken: "", // Empty — e.g., client_credentials grant
	}
	opts := CookieOptions{
		Enabled:   true,
		ExpiresIn: 3600,
	}

	SetTokenCookies(c, tokenPair, opts)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie (access only), got %d", len(cookies))
	}
	if cookies[0].Name != AccessTokenCookieName {
		t.Errorf("expected access token cookie, got %q", cookies[0].Name)
	}
}

func TestClearTokenCookies_ClearsBothCookies(t *testing.T) {
	c, w := setupTestContext()
	opts := CookieOptions{
		Domain:  "example.com",
		Secure:  true,
		Enabled: true,
	}

	ClearTokenCookies(c, opts)

	cookies := w.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies for clearing, got %d", len(cookies))
	}

	for _, cookie := range cookies {
		if cookie.MaxAge != -1 {
			t.Errorf("cookie %q MaxAge = %d, want -1", cookie.Name, cookie.MaxAge)
		}
		if cookie.Value != "" {
			t.Errorf("cookie %q value should be empty, got %q", cookie.Name, cookie.Value)
		}
	}
}

func TestClearTokenCookies_SkipsWhenDisabled(t *testing.T) {
	c, w := setupTestContext()
	opts := CookieOptions{Enabled: false}

	ClearTokenCookies(c, opts)

	cookies := w.Result().Cookies()
	if len(cookies) != 0 {
		t.Errorf("expected 0 cookies when disabled, got %d", len(cookies))
	}
}

func TestExtractAccessTokenFromCookie(t *testing.T) {
	c, _ := setupTestContext()
	c.Request.AddCookie(&http.Cookie{
		Name:  AccessTokenCookieName,
		Value: "my-access-token",
	})

	token := ExtractAccessTokenFromCookie(c)
	if token != "my-access-token" {
		t.Errorf("got %q, want %q", token, "my-access-token")
	}
}

func TestExtractAccessTokenFromCookie_Missing(t *testing.T) {
	c, _ := setupTestContext()

	token := ExtractAccessTokenFromCookie(c)
	if token != "" {
		t.Errorf("expected empty string, got %q", token)
	}
}

func TestExtractRefreshTokenFromCookie(t *testing.T) {
	c, _ := setupTestContext()
	c.Request.AddCookie(&http.Cookie{
		Name:  RefreshTokenCookieName,
		Value: "my-refresh-token",
	})

	token := ExtractRefreshTokenFromCookie(c)
	if token != "my-refresh-token" {
		t.Errorf("got %q, want %q", token, "my-refresh-token")
	}
}

func TestExtractRefreshTokenFromCookie_Missing(t *testing.T) {
	c, _ := setupTestContext()

	token := ExtractRefreshTokenFromCookie(c)
	if token != "" {
		t.Errorf("expected empty string, got %q", token)
	}
}

func TestSetTokenCookies_InsecureInDev(t *testing.T) {
	c, w := setupTestContext()
	tokenPair := TokenPair{
		AccessToken:  "token",
		RefreshToken: "refresh",
	}
	opts := CookieOptions{
		Enabled:    true,
		Secure:     false, // Dev mode — no TLS
		ExpiresIn:  3600,
		RefreshTTL: 604800,
	}

	SetTokenCookies(c, tokenPair, opts)

	cookies := w.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Secure {
			t.Errorf("cookie %q should not be Secure in dev mode", cookie.Name)
		}
	}
}
