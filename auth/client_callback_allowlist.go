package auth

import (
	"net"
	"net/url"
	"strings"
)

// ClientCallbackAllowList validates client_callback URLs supplied to
// /oauth2/authorize against a configured set of allowed patterns:
//
//   - "http://host/cb"        exact match
//   - "http://host/*"         prefix match (the part before "*"); the URL's host
//     must also equal the pattern's host exactly
//   - "http://127.0.0.1:*"    loopback, any port, any path (RFC 8252 §7.3)
//   - "http://127.0.0.1:*/cb" loopback, any port, exact path ("/cb*" = path prefix)
//
// The ":*" port wildcard is honored only for loopback hosts (127.0.0.1,
// [::1], localhost); anywhere else the pattern matches nothing.
//
// Every candidate URL must parse as an absolute URL with a host and no
// userinfo, whatever the pattern. Without that, a prefix such as
// "http://127.0.0.1:" would admit "http://127.0.0.1:1@evil.example/",
// whose real host is evil.example.
//
// An allowlist with zero patterns rejects every URL (fail-closed). This
// closes the open-redirect / OAuth phishing surface (T16) by ensuring an
// attacker cannot smuggle a malicious client_callback through the
// authorize endpoint.
// SEM@72ef5c64a4ca8965f90ee105cc73893284c60b1a: allowlist for validating OAuth client_callback URLs against configured patterns (pure)
type ClientCallbackAllowList struct {
	patterns []string
}

// NewClientCallbackAllowList creates an allow-list from the given URL
// patterns. Empty entries are dropped.
// SEM@72ef5c64a4ca8965f90ee105cc73893284c60b1a: build a ClientCallbackAllowList from URL patterns, dropping empty entries (pure)
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

// Allowed returns true if rawURL matches at least one configured pattern.
// An empty allowlist always returns false (fail-closed).
// SEM@8ca5b826b2afb7199c5784087940df39d67cbaeb: check whether a callback URL matches an allowlist pattern; reject userinfo, fail closed (pure)
func (a *ClientCallbackAllowList) Allowed(rawURL string) bool {
	if a == nil || len(a.patterns) == 0 {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return false
	}
	for _, p := range a.patterns {
		if matchLoopbackAnyPort(p, u) {
			return true
		}
		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(rawURL, prefix) && u.Host == patternHost(prefix) {
				return true
			}
		} else if p == rawURL {
			return true
		}
	}
	return false
}

// matchLoopbackAnyPort matches patterns of the form "scheme://host:*[path]"
// where host is a loopback address. An empty path matches any path; a path
// ending in "*" is a prefix match; otherwise the path must match exactly.
// SEM@8ca5b826b2afb7199c5784087940df39d67cbaeb: match a parsed URL against a loopback any-port allowlist pattern (pure)
func matchLoopbackAnyPort(pattern string, u *url.URL) bool {
	scheme, rest, ok := strings.Cut(pattern, "://")
	if !ok {
		return false
	}
	authority, path := rest, ""
	if i := strings.Index(rest, "/"); i >= 0 {
		authority, path = rest[:i], rest[i:]
	}
	host, found := strings.CutSuffix(authority, ":*")
	if !found || !isLoopbackHost(host) {
		return false
	}
	if u.Scheme != scheme || u.Hostname() != strings.Trim(host, "[]") || u.Port() == "" {
		return false
	}
	switch {
	case path == "" || path == "*":
		return true
	case strings.HasSuffix(path, "*"):
		return strings.HasPrefix(u.Path, strings.TrimSuffix(path, "*"))
	default:
		return u.Path == path
	}
}

// patternHost returns the authority written in a prefix pattern ("" when the
// pattern has no "scheme://"). Requiring the candidate's host to equal it
// stops a pattern like "https://app.example.com*" from admitting
// "https://app.example.com.evil.net/", and "https://*" from admitting any host.
// SEM@8ca5b826b2afb7199c5784087940df39d67cbaeb: extract the host:port authority from an allowlist prefix pattern (pure)
func patternHost(prefix string) string {
	_, rest, ok := strings.Cut(prefix, "://")
	if !ok {
		return ""
	}
	host, _, _ := strings.Cut(rest, "/")
	return host
}

// isLoopbackHost reports whether an allowlist pattern host names a loopback
// interface.
// SEM@8ca5b826b2afb7199c5784087940df39d67cbaeb: report whether a pattern host is a loopback address or localhost (pure)
func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Configured returns true if the allowlist has at least one pattern.
// Used by /oauth2/authorize to surface a startup warning when the
// allowlist is empty.
// SEM@72ef5c64a4ca8965f90ee105cc73893284c60b1a: report whether the allowlist has at least one configured pattern (pure)
func (a *ClientCallbackAllowList) Configured() bool {
	return a != nil && len(a.patterns) > 0
}
