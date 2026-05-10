package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// TestJWTAuth_PopulatesUserAuthTime verifies that ExtractAndSetClaims
// sets userAuthTime on the Gin context when the JWT contains an auth_time claim.
func TestJWTAuth_PopulatesUserAuthTime(t *testing.T) {
	gin.SetMode(gin.TestMode)

	wantAuthTime := time.Now().Truncate(time.Second).Add(-2 * time.Minute)

	// Build a jwt.MapClaims that includes auth_time as a Unix timestamp float64
	// (the same representation the JWT library uses when parsing into MapClaims).
	claims := jwt.MapClaims{
		"sub":       "user123",
		"email":     "user@example.com",
		"idp":       "tmi",
		"auth_time": float64(wantAuthTime.Unix()),
		"exp":       float64(time.Now().Add(time.Hour).Unix()),
		"iat":       float64(time.Now().Unix()),
	}

	// Construct a *jwt.Token that ExtractAndSetClaims expects.
	// We mark it Valid=true — the validator has already checked it upstream.
	token := &jwt.Token{
		Claims: claims,
		Valid:  true,
	}

	// ClaimsExtractor with nil authHandlers — fetchAndSetUserObject will fail
	// (no database), but that is logged and execution continues. We only care
	// that the userAuthTime key was set before the DB lookup.
	extractor := &ClaimsExtractor{
		authHandlers: nil,
		config:       nil,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/some-protected-endpoint", nil)

	_ = extractor.ExtractAndSetClaims(c, token)

	got, ok := c.Get("userAuthTime")
	if !ok {
		t.Fatal("userAuthTime not set on context")
	}

	gotPtr, ok := got.(*time.Time)
	if !ok {
		t.Fatalf("userAuthTime is not *time.Time, got %T", got)
	}

	if gotPtr == nil {
		t.Fatal("userAuthTime *time.Time pointer is nil, expected non-nil")
	}

	if !gotPtr.Equal(wantAuthTime) {
		t.Errorf("userAuthTime mismatch: got %v, want %v", *gotPtr, wantAuthTime)
	}
}

// TestJWTAuth_PopulatesUserAuthTime_Missing verifies that when auth_time is absent
// from the JWT, userAuthTime is set to a typed nil *time.Time (not missing from context).
func TestJWTAuth_PopulatesUserAuthTime_Missing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Claims with no auth_time
	claims := jwt.MapClaims{
		"sub":   "user123",
		"email": "user@example.com",
		"idp":   "tmi",
		"exp":   float64(time.Now().Add(time.Hour).Unix()),
		"iat":   float64(time.Now().Unix()),
	}

	token := &jwt.Token{
		Claims: claims,
		Valid:  true,
	}

	extractor := &ClaimsExtractor{
		authHandlers: nil,
		config:       nil,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/some-protected-endpoint", nil)

	_ = extractor.ExtractAndSetClaims(c, token)

	got, ok := c.Get("userAuthTime")
	if !ok {
		t.Fatal("userAuthTime key not set on context (expected typed nil)")
	}

	gotPtr, ok := got.(*time.Time)
	if !ok {
		t.Fatalf("userAuthTime is not *time.Time, got %T", got)
	}

	if gotPtr != nil {
		t.Errorf("expected nil *time.Time for missing auth_time, got %v", *gotPtr)
	}
}
