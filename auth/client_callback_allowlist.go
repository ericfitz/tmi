package auth

import "strings"

// ClientCallbackAllowList validates client_callback URLs supplied to
// /oauth2/authorize against a configured set of allowed patterns. A
// pattern ending in "*" is a prefix match; all others are exact matches.
//
// An allowlist with zero patterns rejects every URL (fail-closed). This
// closes the open-redirect / OAuth phishing surface (T16) by ensuring an
// attacker cannot smuggle a malicious client_callback through the
// authorize endpoint.
type ClientCallbackAllowList struct {
	patterns []string
}

// NewClientCallbackAllowList creates an allow-list from the given URL
// patterns. Empty entries are dropped.
func NewClientCallbackAllowList(patterns []string) *ClientCallbackAllowList {
	cleaned := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return &ClientCallbackAllowList{patterns: cleaned}
}

// Allowed returns true if url matches at least one configured pattern.
// An empty allowlist always returns false (fail-closed).
func (a *ClientCallbackAllowList) Allowed(url string) bool {
	if a == nil || len(a.patterns) == 0 {
		return false
	}
	for _, p := range a.patterns {
		if strings.HasSuffix(p, "*") {
			if strings.HasPrefix(url, strings.TrimSuffix(p, "*")) {
				return true
			}
		} else if p == url {
			return true
		}
	}
	return false
}

// Configured returns true if the allowlist has at least one pattern.
// Used by /oauth2/authorize to surface a startup warning when the
// allowlist is empty.
func (a *ClientCallbackAllowList) Configured() bool {
	return a != nil && len(a.patterns) > 0
}
