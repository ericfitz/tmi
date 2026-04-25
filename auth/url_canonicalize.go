package auth

import (
	"net/url"
	"strings"
)

// canonicalizeURL normalizes a URL for equality comparison. Returns the input
// unchanged on parse error (callers should compare conservatively when
// canonicalization fails). Normalization steps:
//   - Lowercase scheme and host
//   - Strip default port (80 for http, 443 for https)
//   - Strip trailing slash from path (only when path is more than just "/")
//
// This does NOT canonicalize across different hosts (e.g. host aliases).
func canonicalizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}

	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)

	// Strip default port
	if i := strings.LastIndex(host, ":"); i != -1 {
		port := host[i+1:]
		if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
			host = host[:i]
		}
	}

	path := u.Path
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimRight(path, "/")
	}

	u.Scheme = scheme
	u.Host = host
	u.Path = path
	return u.String()
}
