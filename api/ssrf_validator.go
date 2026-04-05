package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// URIValidator validates URIs against an allowlist and SSRF protection rules.
// It supports exact host matching, wildcard subdomain matching, and configurable
// URL schemes. When an allowlist is configured, only matching hosts are permitted
// (and they bypass IP checks). When no allowlist is configured, all hosts are
// permitted subject to SSRF IP checks.
type URIValidator struct {
	exactHosts    map[string]bool // case-insensitive exact domain + single subdomain match
	wildcardHosts []string        // suffix match for *.domain entries (stored without "*." prefix)
	schemes       map[string]bool // allowed URL schemes
	hasAllowlist  bool            // true if any valid allowlist entries were configured
}

// NewURIValidator creates a new URIValidator with the given allowlist and scheme configuration.
// Allowlist entries may be exact hostnames ("mycompany.com") or wildcard entries ("*.mycompany.com").
// Invalid entries are skipped with a warning log. If schemes is nil or empty, defaults to ["https"].
func NewURIValidator(allowlist []string, schemes []string) *URIValidator {
	v := &URIValidator{
		exactHosts: make(map[string]bool),
		schemes:    make(map[string]bool),
	}

	logger := slogging.Get()

	for _, entry := range allowlist {
		entry = strings.TrimSpace(entry)
		entry = strings.ToLower(entry)

		if entry == "" {
			logger.Warn("URIValidator: skipping empty allowlist entry")
			continue
		}

		switch {
		case strings.HasPrefix(entry, "*."):
			// Wildcard entry — validate the suffix
			suffix := entry[2:]
			if suffix == "" || strings.Contains(suffix, "*") {
				logger.Warn("URIValidator: skipping invalid wildcard allowlist entry: %s", entry)
				continue
			}
			v.wildcardHosts = append(v.wildcardHosts, suffix)
		case strings.Contains(entry, "*"):
			// Wildcard character present but not in valid "*.domain" form
			logger.Warn("URIValidator: skipping invalid allowlist entry (misplaced wildcard): %s", entry)
			continue
		default:
			v.exactHosts[entry] = true
		}
	}

	v.hasAllowlist = len(v.exactHosts) > 0 || len(v.wildcardHosts) > 0

	// Set schemes — default to https only
	if len(schemes) == 0 {
		v.schemes["https"] = true
	} else {
		for _, s := range schemes {
			v.schemes[strings.ToLower(s)] = true
		}
	}

	return v
}

// matchHost checks whether the given hostname matches the allowlist.
// If no allowlist is configured, returns true (open mode).
// For exact entries, matches the domain itself and any single subdomain.
// For wildcard entries, matches the domain and any depth of subdomains.
// Hostname comparison is case-insensitive. Port numbers must be stripped before calling.
func (v *URIValidator) matchHost(hostname string) bool {
	if !v.hasAllowlist {
		return true
	}

	hostname = strings.ToLower(hostname)

	// Check exact hosts: matches domain itself or a single subdomain
	if v.exactHosts[hostname] {
		return true
	}
	// Check if hostname is a single subdomain of an exact entry:
	// e.g., "www.mycompany.com" matches "mycompany.com"
	if idx := strings.IndexByte(hostname, '.'); idx >= 0 {
		parent := hostname[idx+1:]
		// parent must be the exact entry AND there must be no further dots in the prefix
		// (the prefix is hostname[:idx] which we got from the first dot, so it has no dots)
		if v.exactHosts[parent] {
			return true
		}
	}

	// Check wildcard hosts: matches domain itself or any depth of subdomains
	for _, suffix := range v.wildcardHosts {
		if hostname == suffix {
			return true
		}
		if strings.HasSuffix(hostname, "."+suffix) {
			return true
		}
	}

	return false
}

// Validate checks whether the given raw URL is safe to access.
// It enforces scheme restrictions, allowlist matching, localhost blocking,
// and SSRF protection against private/internal IP addresses.
func (v *URIValidator) Validate(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Reject URLs without a scheme or host
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("invalid URL: missing scheme or host")
	}

	// Check scheme
	if !v.schemes[strings.ToLower(parsed.Scheme)] {
		allowedSchemes := make([]string, 0, len(v.schemes))
		for s := range v.schemes {
			allowedSchemes = append(allowedSchemes, s)
		}
		return fmt.Errorf("unsupported scheme: %s (allowed: %s)", parsed.Scheme, strings.Join(allowedSchemes, ", "))
	}

	hostname := parsed.Hostname() // strips port

	// If allowlist configured and host matches, bypass all IP checks
	if v.hasAllowlist {
		if v.matchHost(hostname) {
			return nil
		}
		return fmt.Errorf("host %q is not in allowlist", hostname)
	}

	// No allowlist — apply SSRF protections

	// Block localhost variants
	lower := strings.ToLower(hostname)
	if lower == "localhost" || lower == "ip6-localhost" || lower == "ip6-loopback" {
		return fmt.Errorf("blocked: localhost is not allowed")
	}

	// Check if hostname is a literal IP
	ip := net.ParseIP(hostname)
	if ip != nil {
		return v.checkIP(ip)
	}

	// Resolve hostname and check all IPs
	ips, err := net.LookupHost(hostname)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname: %s", hostname)
	}

	for _, ipStr := range ips {
		resolved := net.ParseIP(ipStr)
		if resolved == nil {
			continue
		}
		if err := v.checkIP(resolved); err != nil {
			return err
		}
	}

	return nil
}

// checkIP verifies an IP address is not in a blocked range (loopback, private,
// link-local, or cloud metadata endpoint).
func (v *URIValidator) checkIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("blocked: loopback address %s", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("blocked: private address %s", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("blocked: link-local address %s", ip)
	}
	// Cloud metadata endpoint (AWS, GCP, Azure)
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("blocked: cloud metadata endpoint %s", ip)
	}
	return nil
}
