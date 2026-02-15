package api

import (
	"fmt"
	"regexp"
	"strings"
)

// Compiled regexes for HTML/XSS detection (compiled once, used by all callers).
var htmlInjectionPatterns = []*regexp.Regexp{
	// Script/iframe/object/embed tags (regex for precision)
	regexp.MustCompile(`(?i)<script[^>]*>`),
	regexp.MustCompile(`(?i)</script>`),
	regexp.MustCompile(`(?i)<iframe[^>]*>`),
	regexp.MustCompile(`(?i)</iframe>`),
	regexp.MustCompile(`(?i)<object[^>]*>`),
	regexp.MustCompile(`(?i)<embed[^>]*>`),
	regexp.MustCompile(`(?i)<applet[^>]*>`),
	// javascript: URI scheme
	regexp.MustCompile(`(?i)javascript:`),
	// Generic on-event handler pattern (covers onclick, onmouseover, onfocus, onblur, onerror, onload, etc.)
	regexp.MustCompile(`(?i)on\w+\s*=`),
}

// templateInjectionPatterns detects server-side template injection (SSTI) attacks.
// Order matters: more specific patterns must come before less specific ones.
var templateInjectionPatterns = []struct {
	pattern string
	desc    string
}{
	{"${{", "GitHub Actions context"}, // GitHub Actions expression injection (check before ${)
	{"{{", "template expression"},     // Handlebars, Jinja2, Angular, Go templates
	{"}}", "template expression"},     // Closing template expression
	{"${", "template interpolation"},  // JavaScript template literals, Freemarker
	{"<%", "server template tag"},     // JSP, ASP, ERB
	{"%>", "server template tag"},     // Closing server template tag
	{"#{", "expression language"},     // Spring EL, JSF EL
}

// CheckHTMLInjection is the unified HTML/XSS and template injection checker.
// It combines regex precision from the validation registry with broader pattern
// coverage from addon validation, providing consistent security checking.
func CheckHTMLInjection(value, fieldName string) error {
	// Check compiled regex patterns for HTML/XSS
	for _, pattern := range htmlInjectionPatterns {
		if pattern.MatchString(value) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Field '%s' contains potentially unsafe content", fieldName),
			}
		}
	}

	// Check template injection patterns (string-based for exact matching)
	for _, tp := range templateInjectionPatterns {
		if strings.Contains(value, tp.pattern) {
			return &RequestError{
				Status:  400,
				Code:    "invalid_input",
				Message: fmt.Sprintf("Field '%s' contains potentially unsafe %s pattern (%s)", fieldName, tp.desc, tp.pattern),
			}
		}
	}

	return nil
}
