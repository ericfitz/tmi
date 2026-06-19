package auth

import (
	"io"

	"github.com/ericfitz/tmi/internal/slogging"
)

// closeBody is a helper function to close a response body and check the error
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: close an HTTP response body and log any error (pure)
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
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: read at most maxBytes from a reader, reporting truncation (pure)
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
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: format a response body for logging, appending a truncation marker if needed (pure)
func logBodyString(body []byte, truncated bool) string {
	if truncated {
		return string(body) + "...(truncated)"
	}
	return string(body)
}
