package framework

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// IsRateLimitingActive probes the running server to determine whether rate
// limiting is effectively enforced for this client. Tests that depend on
// observing 429 responses should call this and skip when it returns false.
//
// The dev server is commonly run with disable_rate_limiting: true, the auth
// flow limiter no-ops in build_mode=test, and IP rate limits skip loopback
// addresses — in all of those cases these tests can never trigger a 429 and
// should not assert behavior the server is not configured to provide.
//
// Detection: if either the auth flow or IP rate limiter is active, the
// response will carry an X-RateLimit-Limit header. We send one valid request
// to /oauth2/authorize (which traverses the auth flow limiter when active)
// and check for the header.
func IsRateLimitingActive(serverURL string) bool {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		fmt.Sprintf("%s/oauth2/authorize?idp=tmi&scope=openid&state=ratelimit-probe", serverURL),
		nil,
	)
	if err != nil {
		return false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.Header.Get("X-RateLimit-Limit") != ""
}
