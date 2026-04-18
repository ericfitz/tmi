package api

import "strings"

// ClientCallbackAllowList validates client_callback URLs against a set of allowed patterns.
// A pattern ending in "*" is a prefix match; all others are exact matches.
type ClientCallbackAllowList struct {
	patterns []string
}

// NewClientCallbackAllowList creates an allow-list from the given URL patterns.
func NewClientCallbackAllowList(patterns []string) *ClientCallbackAllowList {
	return &ClientCallbackAllowList{patterns: patterns}
}

// Allowed returns true if the given URL matches at least one pattern in the allow-list.
func (a *ClientCallbackAllowList) Allowed(url string) bool {
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
