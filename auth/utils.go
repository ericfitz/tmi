package auth

import (
	"io"

	"github.com/ericfitz/tmi/internal/slogging"
)

// closeBody is a helper function to close a response body and check the error
func closeBody(c io.Closer) {
	if err := c.Close(); err != nil {
		slogging.Get().Error("Error closing response body: %v", err)
	}
}

// maxOAuthResponseBytes caps how many bytes are read from an external OAuth/
// OIDC provider response (token, userinfo, discovery). Legitimate payloads
// are a few KB; 1 MiB is generous and bounds gzip-amplified responses
// (Go's transport transparently gunzips, so the cap applies post-inflation).
const maxOAuthResponseBytes = 1 << 20 // 1 MiB

// maxLoggedBodyBytes caps how much of an upstream response body is copied
// into log messages and error strings.
const maxLoggedBodyBytes = 2 * 1024 // 2 KiB

// readCappedBody reads at most maxBytes from r and reports whether the input
// was truncated at the cap. maxBytes must be > 0. Mirrors
// api/safe_http_client.go's readCappedBody; kept local to avoid importing the
// api package from auth.
func readCappedBody(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	buf, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > maxBytes {
		return buf[:maxBytes], true, nil
	}
	return buf, false, nil
}

// logBodyString renders a (possibly truncated) response body for inclusion in
// a log message or error string, marking truncation explicitly.
func logBodyString(body []byte, truncated bool) string {
	if truncated {
		return string(body) + "...(truncated)"
	}
	return string(body)
}
