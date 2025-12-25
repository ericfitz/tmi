package api

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// WebhookUrlValidator validates webhook URLs against security rules
type WebhookUrlValidator struct {
	denyListStore WebhookUrlDenyListStoreInterface
}

// NewWebhookUrlValidator creates a new URL validator
func NewWebhookUrlValidator(denyListStore WebhookUrlDenyListStoreInterface) *WebhookUrlValidator {
	return &WebhookUrlValidator{
		denyListStore: denyListStore,
	}
}

// ValidateWebhookURL validates a webhook URL according to security requirements
func (v *WebhookUrlValidator) ValidateWebhookURL(rawURL string) error {
	// 1. Check that URL starts with "https://" (case-insensitive for first 8 characters)
	if len(rawURL) < 8 {
		return fmt.Errorf("URL too short: must start with https://")
	}

	urlPrefix := strings.ToLower(rawURL[:8])
	if urlPrefix != "https://" {
		return fmt.Errorf("URL must start with https:// (found: %s)", rawURL[:8])
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
	if err := v.checkDenyList(hostname); err != nil {
		return err
	}

	return nil
}

// validateDNSHostname validates a hostname according to RFC 1035, RFC 1123, and RFC 5890
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
func isAlphanumeric(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

// checkDenyList checks if the hostname matches any deny list pattern
func (v *WebhookUrlValidator) checkDenyList(hostname string) error {
	// Load deny list entries
	entries, err := v.denyListStore.List()
	if err != nil {
		// If we can't load deny list, fail closed (deny access)
		return fmt.Errorf("unable to verify URL against security policy: %w", err)
	}

	// Normalize hostname for comparison (lowercase)
	normalizedHostname := strings.ToLower(hostname)

	for _, entry := range entries {
		matched := false
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
func (v *WebhookUrlValidator) matchRegex(hostname, pattern string) (bool, error) {
	// Compile and match regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, fmt.Errorf("invalid regex pattern: %w", err)
	}
	return re.MatchString(hostname), nil
}

// IsIPv4 checks if a string is an IPv4 address
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
func IsIPv6(hostname string) bool {
	// Basic check: contains colons and hex digits
	if !strings.Contains(hostname, ":") {
		return false
	}
	// More thorough validation would use net.ParseIP
	return true
}
