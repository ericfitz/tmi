package api

import "strings"

// ClientCallbackAllowList validates client_callback URLs against a set of allowed patterns.
// A pattern ending in "*" is a prefix match; all others are exact matches.
// SEM@7006f7394b23fdc67ccfe1776e6a78de52ab857b: holds a set of allowed OAuth client callback URL patterns for authorization checks
type ClientCallbackAllowList struct {
	patterns []string
}

// NewClientCallbackAllowList creates an allow-list from the given URL patterns.
// SEM@7006f7394b23fdc67ccfe1776e6a78de52ab857b: build a ClientCallbackAllowList from a slice of URL patterns (pure)
func NewClientCallbackAllowList(patterns []string) *ClientCallbackAllowList {
	return &ClientCallbackAllowList{patterns: patterns}
}

// Allowed returns true if the given URL matches at least one pattern in the allow-list.
// SEM@7006f7394b23fdc67ccfe1776e6a78de52ab857b: check whether a callback URL matches any configured allow-list pattern (pure)
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
