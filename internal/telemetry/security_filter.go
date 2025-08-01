package telemetry

import (
	"fmt"
	"regexp"
	"strings"
)

// SecurityFilter handles filtering and sanitization of sensitive data in logs and traces
type SecurityFilter struct {
	sensitivePatterns    []*regexp.Regexp
	sensitiveKeys        map[string]bool
	piiPatterns          []*regexp.Regexp
	tokenPatterns        []*regexp.Regexp
	urlPatterns          []*regexp.Regexp
	replacementText      string
	enablePIIDetection   bool
	enableTokenFiltering bool
	enableURLFiltering   bool
}

// NewSecurityFilter creates a new security filter with default patterns
func NewSecurityFilter() (*SecurityFilter, error) {
	sf := &SecurityFilter{
		sensitiveKeys:        make(map[string]bool),
		replacementText:      "[REDACTED]",
		enablePIIDetection:   true,
		enableTokenFiltering: true,
		enableURLFiltering:   true,
	}

	// Initialize sensitive key patterns
	sf.initializeSensitiveKeys()

	// Initialize regex patterns
	if err := sf.initializePatterns(); err != nil {
		return nil, err
	}

	return sf, nil
}

// initializeSensitiveKeys sets up the list of sensitive keys
func (sf *SecurityFilter) initializeSensitiveKeys() {
	sensitiveKeys := []string{
		// Authentication and authorization
		"password", "passwd", "pwd", "secret", "token", "auth", "authorization",
		"api_key", "apikey", "access_key", "private_key", "public_key", "key",
		"jwt", "bearer", "oauth", "session", "cookie", "csrf",

		// Personal Identifiable Information
		"ssn", "social_security", "passport", "license", "credit_card", "cc_number",
		"account_number", "routing_number", "iban", "swift", "email", "phone",
		"address", "birth_date", "birthday", "dob",

		// Database credentials
		"db_password", "database_password", "connection_string", "dsn",
		"username", "user", "login", "credentials",

		// Application secrets
		"encryption_key", "signing_key", "hmac_key", "salt", "hash",
		"client_secret", "client_id", "app_secret", "webhook_secret",

		// Cloud and infrastructure
		"aws_secret", "aws_access_key", "gcp_key", "azure_key",
		"docker_auth", "k8s_secret", "tls_key", "ssl_key",
	}

	for _, key := range sensitiveKeys {
		sf.sensitiveKeys[strings.ToLower(key)] = true
	}
}

// initializePatterns compiles regex patterns for different types of sensitive data
func (sf *SecurityFilter) initializePatterns() error {
	var err error

	// Sensitive data patterns (generic)
	sensitivePatterns := []string{
		`(?i)(password|passwd|pwd|secret|token|key|auth|authorization)[\s]*[:=][\s]*["']?([^"'\s]+)["']?`,
		`(?i)(api[_-]?key|apikey|access[_-]?key)[\s]*[:=][\s]*["']?([^"'\s]+)["']?`,
		`(?i)(client[_-]?secret|app[_-]?secret)[\s]*[:=][\s]*["']?([^"'\s]+)["']?`,
	}

	sf.sensitivePatterns = make([]*regexp.Regexp, len(sensitivePatterns))
	for i, pattern := range sensitivePatterns {
		sf.sensitivePatterns[i], err = regexp.Compile(pattern)
		if err != nil {
			return err
		}
	}

	// PII patterns
	piiPatterns := []string{
		// Email addresses
		`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`,
		// Phone numbers (various formats)
		`(?:\+?1[-.\s]?)?\(?([0-9]{3})\)?[-.\s]?([0-9]{3})[-.\s]?([0-9]{4})`,
		// SSN (XXX-XX-XXXX)
		`\b\d{3}-\d{2}-\d{4}\b`,
		// Credit card numbers (basic pattern)
		`\b(?:\d{4}[-\s]?){3,4}\d{4}\b`,
		// IP addresses (more detailed)
		`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`,
	}

	sf.piiPatterns = make([]*regexp.Regexp, len(piiPatterns))
	for i, pattern := range piiPatterns {
		sf.piiPatterns[i], err = regexp.Compile(pattern)
		if err != nil {
			return err
		}
	}

	// Token patterns (JWT, OAuth, etc.)
	tokenPatterns := []string{
		// JWT tokens
		`eyJ[A-Za-z0-9_-]*\.eyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]*`,
		// Bearer tokens
		`Bearer\s+[A-Za-z0-9_-]+`,
		// API keys (common patterns)
		`[A-Za-z0-9]{32,}`,
		// OAuth tokens
		`oauth_token=([^&\s]+)`,
		// Session tokens
		`session[_-]?id=([^&;\s]+)`,
	}

	sf.tokenPatterns = make([]*regexp.Regexp, len(tokenPatterns))
	for i, pattern := range tokenPatterns {
		sf.tokenPatterns[i], err = regexp.Compile(pattern)
		if err != nil {
			return err
		}
	}

	// URL patterns (to sanitize sensitive parameters)
	urlPatterns := []string{
		// Query parameters with sensitive names
		`(?i)([?&])(password|token|key|secret|auth)=([^&\s]+)`,
		// Path parameters that might be sensitive
		`(?i)/(token|key|secret)/([^/\s]+)`,
	}

	sf.urlPatterns = make([]*regexp.Regexp, len(urlPatterns))
	for i, pattern := range urlPatterns {
		sf.urlPatterns[i], err = regexp.Compile(pattern)
		if err != nil {
			return err
		}
	}

	return nil
}

// FilterAttribute filters sensitive data from log/trace attributes
func (sf *SecurityFilter) FilterAttribute(key string, value interface{}) (string, interface{}) {
	keyLower := strings.ToLower(key)

	// Check if key is in sensitive keys list
	if sf.sensitiveKeys[keyLower] {
		return key, sf.replacementText
	}

	// Check for sensitive patterns in key name
	for _, pattern := range sf.sensitivePatterns {
		if pattern.MatchString(keyLower) {
			return key, sf.replacementText
		}
	}

	// Filter value content
	valueStr := strings.TrimSpace(strings.ToLower(sf.toString(value)))
	if sf.containsSensitiveData(valueStr) {
		return key, sf.replacementText
	}

	// Apply PII filtering to string values
	if sf.enablePIIDetection {
		if filteredValue := sf.filterPII(sf.toString(value)); filteredValue != sf.toString(value) {
			return key, filteredValue
		}
	}

	return key, value
}

// SanitizeMessage sanitizes sensitive data from log messages
func (sf *SecurityFilter) SanitizeMessage(message string) string {
	sanitized := message

	// Apply sensitive pattern filtering
	for _, pattern := range sf.sensitivePatterns {
		sanitized = pattern.ReplaceAllStringFunc(sanitized, func(match string) string {
			parts := pattern.FindStringSubmatch(match)
			if len(parts) >= 3 {
				return parts[1] + "=" + sf.replacementText
			}
			return sf.replacementText
		})
	}

	// Apply PII filtering
	if sf.enablePIIDetection {
		sanitized = sf.filterPII(sanitized)
	}

	// Apply token filtering
	if sf.enableTokenFiltering {
		sanitized = sf.filterTokens(sanitized)
	}

	// Apply URL filtering
	if sf.enableURLFiltering {
		sanitized = sf.filterURLs(sanitized)
	}

	return sanitized
}

// SanitizeKey sanitizes cache keys and other identifiers
func (sf *SecurityFilter) SanitizeKey(key string) string {
	// For session and token keys, show prefix and suffix only
	if strings.Contains(strings.ToLower(key), "session") ||
		strings.Contains(strings.ToLower(key), "token") ||
		strings.Contains(strings.ToLower(key), "auth") {
		return sf.sanitizeIdentifier(key)
	}

	// Apply general PII filtering
	if sf.enablePIIDetection {
		key = sf.filterPII(key)
	}

	return key
}

// SanitizeURL sanitizes URLs by removing sensitive query parameters
func (sf *SecurityFilter) SanitizeURL(url string) string {
	if !sf.enableURLFiltering {
		return url
	}

	sanitized := url
	for _, pattern := range sf.urlPatterns {
		sanitized = pattern.ReplaceAllStringFunc(sanitized, func(match string) string {
			if strings.Contains(match, "=") {
				parts := strings.SplitN(match, "=", 2)
				return parts[0] + "=" + sf.replacementText
			}
			return sf.replacementText
		})
	}

	return sanitized
}

// SanitizeSQL sanitizes SQL queries by removing sensitive values
func (sf *SecurityFilter) SanitizeSQL(query string) string {
	sanitized := query

	// Replace string literals that might contain sensitive data
	stringLiteralPattern := regexp.MustCompile(`'([^']*)'`)
	sanitized = stringLiteralPattern.ReplaceAllStringFunc(sanitized, func(match string) string {
		content := strings.Trim(match, "'")
		if sf.containsSensitiveData(strings.ToLower(content)) {
			return "'" + sf.replacementText + "'"
		}
		return match
	})

	// Replace WHERE clauses with sensitive column names
	wherePattern := regexp.MustCompile(`(?i)\bWHERE\s+(\w+)\s*=\s*('[^']*'|\S+)`)
	sanitized = wherePattern.ReplaceAllStringFunc(sanitized, func(match string) string {
		parts := wherePattern.FindStringSubmatch(match)
		if len(parts) >= 3 {
			column := strings.ToLower(parts[1])
			if sf.sensitiveKeys[column] {
				return "WHERE " + parts[1] + " = '" + sf.replacementText + "'"
			}
		}
		return match
	})

	return sanitized
}

// ValidateLogLevel checks if logging should be performed based on security policy
func (sf *SecurityFilter) ValidateLogLevel(level LogLevel, message string) bool {
	// Always allow error and fatal logs
	if level >= LevelError {
		return true
	}

	// For debug and info logs, check if they contain sensitive data
	if level <= LevelInfo {
		return !sf.containsSensitiveData(strings.ToLower(message))
	}

	return true
}

// AuditSecurityEvent logs security-related events
func (sf *SecurityFilter) AuditSecurityEvent(eventType, message string, attributes map[string]interface{}) {
	// This would typically integrate with a security audit system
	// For now, we'll just ensure the event is properly sanitized
	sanitizedMessage := sf.SanitizeMessage(message)
	sanitizedAttrs := make(map[string]interface{})

	for key, value := range attributes {
		sanitizedKey, sanitizedValue := sf.FilterAttribute(key, value)
		sanitizedAttrs[sanitizedKey] = sanitizedValue
	}

	// In a real implementation, this would send to security audit system
	_ = sanitizedMessage
	_ = sanitizedAttrs
}

// Helper functions

func (sf *SecurityFilter) containsSensitiveData(content string) bool {
	contentLower := strings.ToLower(content)

	// Check against known sensitive keywords
	sensitiveKeywords := []string{
		"password", "secret", "token", "key", "auth", "credential",
		"private", "confidential", "sensitive", "restricted",
	}

	for _, keyword := range sensitiveKeywords {
		if strings.Contains(contentLower, keyword) {
			return true
		}
	}

	// Check for potential sensitive patterns
	if sf.enablePIIDetection {
		for _, pattern := range sf.piiPatterns {
			if pattern.MatchString(content) {
				return true
			}
		}
	}

	if sf.enableTokenFiltering {
		for _, pattern := range sf.tokenPatterns {
			if pattern.MatchString(content) {
				return true
			}
		}
	}

	return false
}

func (sf *SecurityFilter) filterPII(content string) string {
	if !sf.enablePIIDetection {
		return content
	}

	filtered := content
	for _, pattern := range sf.piiPatterns {
		filtered = pattern.ReplaceAllString(filtered, sf.replacementText)
	}
	return filtered
}

func (sf *SecurityFilter) filterTokens(content string) string {
	if !sf.enableTokenFiltering {
		return content
	}

	filtered := content
	for _, pattern := range sf.tokenPatterns {
		filtered = pattern.ReplaceAllString(filtered, sf.replacementText)
	}
	return filtered
}

func (sf *SecurityFilter) filterURLs(content string) string {
	if !sf.enableURLFiltering {
		return content
	}

	filtered := content
	for _, pattern := range sf.urlPatterns {
		filtered = pattern.ReplaceAllStringFunc(filtered, func(match string) string {
			if strings.Contains(match, "=") {
				parts := strings.SplitN(match, "=", 2)
				return parts[0] + "=" + sf.replacementText
			}
			return sf.replacementText
		})
	}
	return filtered
}

func (sf *SecurityFilter) sanitizeIdentifier(identifier string) string {
	if len(identifier) <= 8 {
		return sf.replacementText
	}

	// Show first 4 and last 4 characters
	return identifier[:4] + "***" + identifier[len(identifier)-4:]
}

func (sf *SecurityFilter) toString(value interface{}) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return strings.TrimSpace(strings.ToLower(fmt.Sprintf("%v", value)))
	}
}

// AddSensitiveKey adds a new sensitive key to the filter
func (sf *SecurityFilter) AddSensitiveKey(key string) {
	sf.sensitiveKeys[strings.ToLower(key)] = true
}

// RemoveSensitiveKey removes a sensitive key from the filter
func (sf *SecurityFilter) RemoveSensitiveKey(key string) {
	delete(sf.sensitiveKeys, strings.ToLower(key))
}

// SetReplacementText sets the text used to replace sensitive data
func (sf *SecurityFilter) SetReplacementText(text string) {
	sf.replacementText = text
}

// EnablePIIDetection enables or disables PII detection
func (sf *SecurityFilter) EnablePIIDetection(enabled bool) {
	sf.enablePIIDetection = enabled
}

// EnableTokenFiltering enables or disables token filtering
func (sf *SecurityFilter) EnableTokenFiltering(enabled bool) {
	sf.enableTokenFiltering = enabled
}

// EnableURLFiltering enables or disables URL filtering
func (sf *SecurityFilter) EnableURLFiltering(enabled bool) {
	sf.enableURLFiltering = enabled
}

// GetSensitiveKeys returns a copy of the sensitive keys map
func (sf *SecurityFilter) GetSensitiveKeys() map[string]bool {
	keys := make(map[string]bool)
	for k, v := range sf.sensitiveKeys {
		keys[k] = v
	}
	return keys
}

// SecurityPolicy defines security filtering policies
type SecurityPolicy struct {
	EnablePIIDetection    bool
	EnableTokenFiltering  bool
	EnableURLFiltering    bool
	EnableSQLSanitization bool
	ReplacementText       string
	CustomSensitiveKeys   []string
	LogSecurityEvents     bool
	AuditFailedFiltering  bool
}

// ApplySecurityPolicy applies a security policy to the filter
func (sf *SecurityFilter) ApplySecurityPolicy(policy *SecurityPolicy) {
	sf.enablePIIDetection = policy.EnablePIIDetection
	sf.enableTokenFiltering = policy.EnableTokenFiltering
	sf.enableURLFiltering = policy.EnableURLFiltering

	if policy.ReplacementText != "" {
		sf.replacementText = policy.ReplacementText
	}

	// Add custom sensitive keys
	for _, key := range policy.CustomSensitiveKeys {
		sf.AddSensitiveKey(key)
	}
}

// GetDefaultSecurityPolicy returns a default security policy
func GetDefaultSecurityPolicy() *SecurityPolicy {
	return &SecurityPolicy{
		EnablePIIDetection:    true,
		EnableTokenFiltering:  true,
		EnableURLFiltering:    true,
		EnableSQLSanitization: true,
		ReplacementText:       "[REDACTED]",
		CustomSensitiveKeys:   []string{},
		LogSecurityEvents:     false,
		AuditFailedFiltering:  false,
	}
}
