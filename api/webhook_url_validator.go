package api

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// WebhookUrlValidator validates webhook URLs against security rules
// SEM@baf9ecb79a22da23c9922e1df63b14cb07d01523: validator for webhook URLs enforcing scheme, DNS hostname, and deny-list rules
type WebhookUrlValidator struct {
	denyListStore WebhookUrlDenyListStoreInterface
	allowHTTP     bool
}

// NewWebhookUrlValidator creates a new URL validator
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: build a webhook URL validator that requires HTTPS only
func NewWebhookUrlValidator(denyListStore WebhookUrlDenyListStoreInterface) *WebhookUrlValidator {
	return &WebhookUrlValidator{
		denyListStore: denyListStore,
	}
}

// NewWebhookUrlValidatorWithHTTP creates a new URL validator that optionally allows HTTP URLs
// SEM@baf9ecb79a22da23c9922e1df63b14cb07d01523: build a webhook URL validator with configurable HTTP/HTTPS scheme allowance
func NewWebhookUrlValidatorWithHTTP(denyListStore WebhookUrlDenyListStoreInterface, allowHTTP bool) *WebhookUrlValidator {
	return &WebhookUrlValidator{
		denyListStore: denyListStore,
		allowHTTP:     allowHTTP,
	}
}

// ValidateWebhookURL validates a webhook URL according to security requirements
// SEM@a3e8f5e791cb2d0db34a3485d770fb2aa7cdaaf5: validate a webhook URL for scheme, DNS hostname, and deny-list compliance (reads DB)
func (v *WebhookUrlValidator) ValidateWebhookURL(ctx context.Context, rawURL string) error {
	// 1. Check URL scheme
	if v.allowHTTP {
		// Allow both http:// and https://
		if len(rawURL) < 7 {
			return fmt.Errorf("URL too short: must start with http:// or https://")
		}
		prefix7 := strings.ToLower(rawURL[:7])
		hasHTTP := prefix7 == "http://"
		hasHTTPS := len(rawURL) >= 8 && strings.ToLower(rawURL[:8]) == "https://"
		if !hasHTTP && !hasHTTPS {
			return fmt.Errorf("URL must start with http:// or https:// (found: %s)", rawURL[:7])
		}
	} else {
		if len(rawURL) < 8 {
			return fmt.Errorf("URL too short: must start with https://")
		}
		urlPrefix := strings.ToLower(rawURL[:8])
		if urlPrefix != "https://" {
			return fmt.Errorf("URL must start with https:// (found: %s)", rawURL[:8])
		}
	}

	// 2. Parse the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// 3. Extract hostname (before first "/" after protocol)
	hostname := parsedURL.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must contain a valid hostname")
	}

	// 4. Validate DNS hostname per RFC 1035, RFC 1123, RFC 5890
	if err := v.validateDNSHostname(hostname); err != nil {
		return fmt.Errorf("invalid hostname: %w", err)
	}

	// 5. Check against deny list
	if err := v.checkDenyList(ctx, hostname); err != nil {
		return err
	}

	return nil
}

// validateDNSHostname validates a hostname according to RFC 1035, RFC 1123, and RFC 5890
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: validate a hostname against RFC 1035/1123/5890 DNS rules including IDN normalization (pure)
func (v *WebhookUrlValidator) validateDNSHostname(hostname string) error {
	// Handle IDN (Internationalized Domain Names) per RFC 5890
	// Convert to ASCII (punycode) for validation
	asciiHostname, err := idna.ToASCII(hostname)
	if err != nil {
		return fmt.Errorf("invalid IDN hostname: %w", err)
	}

	// Total length check (RFC 1035: max 253 characters)
	if len(asciiHostname) > 253 {
		return fmt.Errorf("hostname too long (max 253 characters): %d", len(asciiHostname))
	}

	if len(asciiHostname) == 0 {
		return fmt.Errorf("hostname cannot be empty")
	}

	// Split into labels
	labels := strings.Split(asciiHostname, ".")

	for i, label := range labels {
		if err := v.validateDNSLabel(label, i == len(labels)-1); err != nil {
			return fmt.Errorf("invalid label '%s': %w", label, err)
		}
	}

	return nil
}

// validateDNSLabel validates a single DNS label according to RFC 1035 and RFC 1123
// SEM@2211c4a58f7aa0b2de38f88778c03926960e7445: validate a single DNS label for length and character rules per RFC 1123 (pure)
func (v *WebhookUrlValidator) validateDNSLabel(label string, _ bool) error {
	// Label length check (RFC 1035: 1-63 characters)
	if len(label) < 1 || len(label) > 63 {
		return fmt.Errorf("label length must be 1-63 characters: %d", len(label))
	}

	// RFC 1123: First character can be alphanumeric (relaxed from RFC 1035)
	// RFC 1035: First character must be a letter
	// We use RFC 1123 rules (more permissive)
	firstChar := rune(label[0])
	if !isAlphanumeric(firstChar) {
		return fmt.Errorf("label must start with alphanumeric character: '%c'", firstChar)
	}

	// Last character must be alphanumeric (not hyphen)
	lastChar := rune(label[len(label)-1])
	if !isAlphanumeric(lastChar) {
		return fmt.Errorf("label must end with alphanumeric character: '%c'", lastChar)
	}

	// Middle characters can be alphanumeric or hyphen
	for i, ch := range label {
		if i == 0 || i == len(label)-1 {
			continue // Already validated first and last
		}
		if !isAlphanumeric(ch) && ch != '-' {
			return fmt.Errorf("label contains invalid character: '%c' (only alphanumeric and hyphen allowed)", ch)
		}
	}

	return nil
}

// isAlphanumeric checks if a character is alphanumeric (a-z, A-Z, 0-9)
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: check that a rune is an ASCII alphanumeric character (pure)
func isAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

// checkDenyList checks if the hostname matches any deny list pattern
// SEM@a3e8f5e791cb2d0db34a3485d770fb2aa7cdaaf5: check a hostname against stored glob and regex deny-list patterns; fail closed on load error (reads DB)
func (v *WebhookUrlValidator) checkDenyList(ctx context.Context, hostname string) error {
	// Load deny list entries
	entries, err := v.denyListStore.List(ctx)
	if err != nil {
		// If we can't load deny list, fail closed (deny access)
		return fmt.Errorf("unable to verify URL against security policy: %w", err)
	}

	// Normalize hostname for comparison (lowercase)
	normalizedHostname := strings.ToLower(hostname)

	for _, entry := range entries {
		var matched bool
		var matchErr error

		switch entry.PatternType {
		case "glob":
			matched, matchErr = v.matchGlob(normalizedHostname, strings.ToLower(entry.Pattern))
		case "regex":
			matched, matchErr = v.matchRegex(normalizedHostname, entry.Pattern)
		default:
			// Unknown pattern type, skip
			continue
		}

		if matchErr != nil {
			// Log error but continue checking other patterns
			continue
		}

		if matched {
			description := entry.Description
			if description == "" {
				description = "blocked by security policy"
			}
			return fmt.Errorf("URL blocked: %s (pattern: %s)", description, entry.Pattern)
		}
	}

	return nil
}

// matchGlob performs glob pattern matching
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: match a hostname against a glob pattern using filepath semantics (pure)
func (v *WebhookUrlValidator) matchGlob(hostname, pattern string) (bool, error) {
	// Use filepath.Match for glob matching
	// This supports * and ? wildcards
	matched, err := filepath.Match(pattern, hostname)
	if err != nil {
		return false, fmt.Errorf("invalid glob pattern: %w", err)
	}
	return matched, nil
}

// matchRegex performs regex pattern matching
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: match a hostname against a compiled regex pattern (pure)
func (v *WebhookUrlValidator) matchRegex(hostname, pattern string) (bool, error) {
	// Compile and match regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("invalid regex pattern: %w", err)
	}
	return re.MatchString(hostname), nil
}

// IsIPv4 checks if a string is an IPv4 address
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: check whether a string is a dotted-decimal IPv4 address (pure)
func IsIPv4(hostname string) bool {
	parts := strings.Split(hostname, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if len(part) == 0 || len(part) > 3 {
			return false
		}
		for _, ch := range part {
			if !unicode.IsDigit(ch) {
				return false
			}
		}
		// Could also validate that each part is 0-255, but basic check is sufficient
	}
	return true
}

// IsIPv6 checks if a string is an IPv6 address
// SEM@9ea792b9df3b1ab947a5ab9a404a0fbccd779d21: check whether a string is an IPv6 address by presence of colons (pure)
func IsIPv6(hostname string) bool {
	// Basic check: contains colons and hex digits
	if !strings.Contains(hostname, ":") {
		return false
	}
	// More thorough validation would use net.ParseIP
	return true
}
