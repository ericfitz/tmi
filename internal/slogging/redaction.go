package slogging

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
)

// RedactionAction defines how sensitive data should be handled
type RedactionAction string

const (
	// RedactionOmit removes the field entirely from logs
	RedactionOmit RedactionAction = "omit"
	// RedactionObfuscate replaces the value with [REDACTED]
	RedactionObfuscate RedactionAction = "obfuscate"
	// RedactionPartial shows first and last few characters with middle redacted
	RedactionPartial RedactionAction = "partial"
)

// RedactionRule defines a single redaction rule
type RedactionRule struct {
	// FieldPattern is a regex pattern to match field names
	FieldPattern string `yaml:"field_pattern" json:"field_pattern"`
	// Action specifies what to do with matching fields
	Action RedactionAction `yaml:"action" json:"action"`
	// LogLevels specifies which log levels this rule applies to (empty = all levels)
	LogLevels []string `yaml:"log_levels,omitempty" json:"log_levels,omitempty"`
	// Groups specifies which log groups this rule applies to (empty = all groups)
	Groups []string `yaml:"groups,omitempty" json:"groups,omitempty"`
	// compiled regex (not serialized)
	compiledPattern *regexp.Regexp `yaml:"-" json:"-"`
}

// RedactionConfig holds all redaction rules
type RedactionConfig struct {
	// Enabled controls whether redaction is active
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Rules contains all redaction rules
	Rules []RedactionRule `yaml:"rules" json:"rules"`
}

// DefaultRedactionConfig provides sensible defaults for security
func DefaultRedactionConfig() RedactionConfig {
	return RedactionConfig{
		Enabled: true,
		Rules: []RedactionRule{
			{
				FieldPattern: "(?i)(authorization|bearer|token|jwt|access_token|refresh_token)",
				Action:       RedactionPartial,
				LogLevels:    []string{"debug", "info", "warn", "error"},
			},
			{
				FieldPattern: "(?i)(password|secret|key|api_key|private_key|client_secret)",
				Action:       RedactionOmit,
				LogLevels:    []string{"debug", "info", "warn", "error"},
			},
			{
				FieldPattern: "(?i)(cookie|set-cookie)",
				Action:       RedactionPartial,
				LogLevels:    []string{"debug", "info", "warn", "error"},
			},
			{
				FieldPattern: "(?i)(x-auth-token|x-api-key)",
				Action:       RedactionPartial,
				LogLevels:    []string{"debug", "info", "warn", "error"},
			},
			{
				FieldPattern: "(?i)(svg_image)",
				Action:       RedactionOmit,
				LogLevels:    []string{"debug", "info", "warn", "error"},
			},
		},
	}
}

// CompileRules compiles regex patterns for all rules
func (rc *RedactionConfig) CompileRules() error {
	for i := range rc.Rules {
		pattern, err := regexp.Compile(rc.Rules[i].FieldPattern)
		if err != nil {
			return fmt.Errorf("failed to compile redaction pattern '%s': %w", rc.Rules[i].FieldPattern, err)
		}
		rc.Rules[i].compiledPattern = pattern
	}
	return nil
}

// shouldApplyRule checks if a rule should be applied for the given level and groups
func (rule *RedactionRule) shouldApplyRule(level slog.Level, groups []string) bool {
	// Check log levels
	if len(rule.LogLevels) > 0 {
		levelStr := strings.ToLower(level.String())
		found := false
		for _, allowedLevel := range rule.LogLevels {
			if strings.ToLower(allowedLevel) == levelStr {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check groups
	if len(rule.Groups) > 0 && len(groups) > 0 {
		found := false
		for _, ruleGroup := range rule.Groups {
			for _, logGroup := range groups {
				if strings.EqualFold(ruleGroup, logGroup) {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// applyRedaction applies redaction to a field value
func (rule *RedactionRule) applyRedaction(value slog.Value) slog.Value {
	switch rule.Action {
	case RedactionOmit:
		// Return a special marker that the handler will recognize to omit
		return slog.StringValue("__REDACT_OMIT__")
	case RedactionObfuscate:
		return slog.StringValue("[REDACTED]")
	case RedactionPartial:
		return slog.StringValue(partialRedactValue(value.String()))
	default:
		return value
	}
}

// partialRedactValue applies partial redaction similar to the original implementation
func partialRedactValue(value string) string {
	if value == "" {
		return value
	}

	// For very short values, just fully redact
	if len(value) <= 12 {
		return "[REDACTED]"
	}

	// For Bearer tokens, handle the "Bearer " prefix specially
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		prefix := value[:7] // "Bearer "
		actualToken := value[7:]
		return prefix + partialRedactValue(actualToken)
	}

	// For JWT tokens (3 parts separated by dots)
	if strings.Count(value, ".") == 2 && strings.HasPrefix(value, "eyJ") {
		parts := strings.Split(value, ".")
		// Keep first 8 chars of header, last 4 of signature
		headerPart := parts[0]
		signaturePart := parts[2]

		redactedHeader := headerPart
		if len(headerPart) > 8 {
			redactedHeader = headerPart[:8] + "...REDACTED..."
		}

		redactedSignature := signaturePart
		if len(signaturePart) > 4 {
			redactedSignature = "...REDACTED..." + signaturePart[len(signaturePart)-4:]
		}

		return redactedHeader + ".REDACTED." + redactedSignature
	}

	// For other tokens, keep first 6 and last 4 characters
	visibleStart := 6
	visibleEnd := 4

	// Ensure we don't expose too much of short tokens
	if len(value) < visibleStart+visibleEnd+10 {
		visibleStart = 3
		visibleEnd = 2
	}

	return value[:visibleStart] + "...REDACTED..." + value[len(value)-visibleEnd:]
}

// redactionHandler wraps another slog.Handler to apply redaction rules
type redactionHandler struct {
	handler slog.Handler
	config  RedactionConfig
	groups  []string // Track current group hierarchy
}

// NewRedactionHandler creates a new redaction handler
func NewRedactionHandler(handler slog.Handler, config RedactionConfig) (slog.Handler, error) {
	if err := config.CompileRules(); err != nil {
		return nil, err
	}

	return &redactionHandler{
		handler: handler,
		config:  config,
		groups:  make([]string, 0),
	}, nil
}

func (h *redactionHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *redactionHandler) Handle(ctx context.Context, record slog.Record) error {
	if !h.config.Enabled {
		return h.handler.Handle(ctx, record)
	}

	// Create a new record with potentially redacted attributes
	newRecord := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)

	// Process each attribute
	record.Attrs(func(attr slog.Attr) bool {
		redactedAttr := h.redactAttribute(attr, record.Level)
		if redactedAttr.Key != "" { // Only add if not omitted
			newRecord.AddAttrs(redactedAttr)
		}
		return true
	})

	return h.handler.Handle(ctx, newRecord)
}

func (h *redactionHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Apply redaction to attrs before passing to underlying handler
	redactedAttrs := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redactedAttr := h.redactAttribute(attr, slog.LevelInfo) // Use Info as default level
		if redactedAttr.Key != "" {                             // Only add if not omitted
			redactedAttrs = append(redactedAttrs, redactedAttr)
		}
	}

	return &redactionHandler{
		handler: h.handler.WithAttrs(redactedAttrs),
		config:  h.config,
		groups:  h.groups,
	}
}

func (h *redactionHandler) WithGroup(name string) slog.Handler {
	newGroups := make([]string, len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups[len(h.groups)] = name

	return &redactionHandler{
		handler: h.handler.WithGroup(name),
		config:  h.config,
		groups:  newGroups,
	}
}

// redactAttribute applies redaction rules to a single attribute
func (h *redactionHandler) redactAttribute(attr slog.Attr, level slog.Level) slog.Attr {
	// Check each rule
	for _, rule := range h.config.Rules {
		if rule.compiledPattern != nil &&
			rule.compiledPattern.MatchString(attr.Key) &&
			rule.shouldApplyRule(level, h.groups) {

			redactedValue := rule.applyRedaction(attr.Value)

			// Special handling for omission
			if redactedValue.String() == "__REDACT_OMIT__" {
				return slog.Attr{} // Return empty attr to signal omission
			}

			return slog.Attr{
				Key:   attr.Key,
				Value: redactedValue,
			}
		}
	}

	// No redaction rule matched, return original
	return attr
}

// Legacy functions for compatibility with existing code

// SanitizeLogMessage removes newlines and other control characters from log messages
func SanitizeLogMessage(message string) string {
	// Replace newlines with space
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.ReplaceAll(message, "\r", " ")

	// Replace tabs with space
	message = strings.ReplaceAll(message, "\t", " ")

	// Collapse multiple spaces into one and trim whitespace
	message = strings.TrimSpace(strings.Join(strings.Fields(message), " "))

	// Return empty string if only whitespace remains
	if message == "" {
		return ""
	}

	return message
}

// RedactSensitiveInfo removes or masks sensitive information from strings (legacy compatibility)
func RedactSensitiveInfo(input string) string {
	if input == "" {
		return input
	}

	// Use default redaction rules to process the string
	// This is a simplified version for legacy string processing
	config := DefaultRedactionConfig()

	for _, rule := range config.Rules {
		if rule.compiledPattern == nil {
			pattern, err := regexp.Compile(rule.FieldPattern)
			if err != nil {
				continue
			}
			rule.compiledPattern = pattern
		}

		// Apply pattern matching to the input string
		if rule.compiledPattern.MatchString(input) {
			switch rule.Action {
			case RedactionOmit:
				return "[REDACTED]"
			case RedactionObfuscate:
				return "[REDACTED]"
			case RedactionPartial:
				return partialRedactValue(input)
			}
		}
	}

	return input
}

// RedactHeaders creates a copy of headers map with sensitive values redacted
func RedactHeaders(headers map[string][]string) map[string][]string {
	if headers == nil {
		return nil
	}

	redacted := make(map[string][]string)
	sensitiveHeaders := map[string]bool{
		"authorization": true,
		"x-auth-token":  true,
		"x-api-key":     true,
		"cookie":        true,
		"set-cookie":    true,
	}

	for key, values := range headers {
		lowerKey := strings.ToLower(key)
		if sensitiveHeaders[lowerKey] {
			// Use partial redaction for sensitive headers to keep some identifying info
			redactedValues := make([]string, len(values))
			for i, value := range values {
				redactedValues[i] = partialRedactValue(value)
			}
			redacted[key] = redactedValues
		} else {
			// Still check individual values for embedded tokens
			redactedValues := make([]string, len(values))
			for i, value := range values {
				redactedValues[i] = RedactSensitiveInfo(value)
			}
			redacted[key] = redactedValues
		}
	}

	return redacted
}
