package config

import (
	"net/url"
	"strings"
)

// sanitizeURL parses a URL and replaces the password with "****".
// Returns the original string for empty input or bare host:port (no scheme).
// Returns "<invalid URL>" if parsing fails with a scheme present.
func sanitizeURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "<invalid URL>"
	}

	// If no scheme was detected, this is likely a bare host:port — pass through
	if parsed.Scheme == "" || parsed.Host == "" {
		return rawURL
	}

	// If there's a password, replace it by doing a string substitution to avoid
	// url.URL.String() percent-encoding the replacement marker.
	if parsed.User != nil {
		if password, hasPassword := parsed.User.Password(); hasPassword {
			username := parsed.User.Username()
			// Build the userinfo portion that appears in the raw URL and replace it.
			// We replace only the first occurrence to avoid affecting query params.
			var oldUserInfo, newUserInfo string
			if username == "" {
				oldUserInfo = ":" + password + "@"
				newUserInfo = ":****@"
			} else {
				oldUserInfo = username + ":" + password + "@"
				newUserInfo = username + ":****@"
			}
			return strings.Replace(rawURL, oldUserInfo, newUserInfo, 1)
		}
	}

	return parsed.String()
}
