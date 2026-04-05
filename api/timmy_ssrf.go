package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// SSRFValidator validates URLs to prevent Server-Side Request Forgery attacks
type SSRFValidator struct {
	allowlist map[string]bool
}

// NewSSRFValidator creates a new SSRF validator with an optional allowlist of hosts
func NewSSRFValidator(allowedHosts []string) *SSRFValidator {
	al := make(map[string]bool)
	for _, host := range allowedHosts {
		al[strings.ToLower(host)] = true
	}
	return &SSRFValidator{allowlist: al}
}

// Validate checks if the URL is safe to fetch (not targeting internal resources)
func (v *SSRFValidator) Validate(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow HTTP and HTTPS
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s (only http and https are allowed)", parsed.Scheme)
	}

	hostname := parsed.Hostname()

	// Check allowlist first — allowlisted hosts bypass all checks
	if v.allowlist[strings.ToLower(hostname)] {
		return nil
	}

	// Block localhost variants
	lower := strings.ToLower(hostname)
	if lower == "localhost" || lower == "ip6-localhost" || lower == "ip6-loopback" {
		return fmt.Errorf("blocked: localhost is not allowed")
	}

	// Resolve hostname to IP and check
	ips, err := net.LookupHost(hostname)
	if err != nil {
		// If we can't resolve, check if it's already an IP
		ip := net.ParseIP(hostname)
		if ip == nil {
			return fmt.Errorf("cannot resolve hostname: %s", hostname)
		}
		return v.checkIP(ip)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if err := v.checkIP(ip); err != nil {
			return err
		}
	}

	return nil
}

// checkIP verifies an IP address is not in a blocked range
func (v *SSRFValidator) checkIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("blocked: loopback address %s", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("blocked: private address %s", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("blocked: link-local address %s", ip)
	}
	// Cloud metadata endpoint
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("blocked: cloud metadata endpoint %s", ip)
	}
	return nil
}
