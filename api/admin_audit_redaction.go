// Package api provides storage and HTTP handlers for the TMI service.
package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"
)

// Redactor classifies a (fieldPath, value) pair into one of three tiers and
// returns a redaction-applied string suitable for storage in
// system_audit_entries.OldValueRedacted / NewValueRedacted (#355).
//
// Tier 1 (total redaction): passwords/passphrases — {"redacted":true}.
// Tier 2 (hash + optional tail): API keys, secrets, tokens, signing keys,
// credentials, encryption-* fields. Output:
// {"redacted":true,"sha256_prefix":"...","tail":"..."} (tail only when
// value length >= 24 chars).
// Tier 3 (verbatim): everything else — value as-is (with empty-string
// sentinel "<empty>" so Oracle CLOB and PostgreSQL TEXT round-trip
// identically; Oracle treats "" as NULL on CLOB insert).
type Redactor interface {
	Redact(fieldPath, value string) string
}

type redactorImpl struct {
	tier1Patterns []string
	tier2Patterns []string
}

// NewRedactor returns a Redactor configured with the deny-list from #355's
// design spec.
func NewRedactor() Redactor {
	return &redactorImpl{
		tier1Patterns: []string{
			"*.password",
			"*.passphrase",
		},
		tier2Patterns: []string{
			"*.api_key",
			"*.client_secret",
			"*.signing_key",
			"*.private_key",
			"*.public_key",
			"*.bearer_token",
			"*.access_token",
			"*.refresh_token",
			"*.token",
			"*.secret",
			"*.credential",
			"*encryption*",
		},
	}
}

const emptyValueSentinel = "<empty>"

func (r *redactorImpl) Redact(fieldPath, value string) string {
	if matchesAny(fieldPath, r.tier1Patterns) {
		return mustJSON(map[string]any{"redacted": true})
	}
	if matchesAny(fieldPath, r.tier2Patterns) {
		out := map[string]any{
			"redacted":      true,
			"sha256_prefix": sha256Prefix8(value),
		}
		if len(value) >= 24 {
			out["tail"] = lastN(value, 6)
		}
		return mustJSON(out)
	}
	if value == "" {
		return emptyValueSentinel
	}
	return value
}

func matchesAny(fieldPath string, patterns []string) bool {
	lower := strings.ToLower(fieldPath)
	for _, p := range patterns {
		pl := strings.ToLower(p)
		// "*foo*" patterns: contains-check on the inner needle.
		if strings.HasPrefix(pl, "*") && strings.HasSuffix(pl, "*") {
			needle := strings.Trim(pl, "*")
			if strings.Contains(lower, needle) {
				return true
			}
			continue
		}
		// "*.suffix" patterns: glob match.
		ok, _ := path.Match(pl, lower)
		if ok {
			return true
		}
	}
	return false
}

func sha256Prefix8(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:4]) // 4 bytes = 8 hex chars
}

func lastN(v string, n int) string {
	if len(v) <= n {
		return v
	}
	return v[len(v)-n:]
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Should be impossible for the shapes we pass.
		return `{"redacted":true,"err":"marshal_failed"}`
	}
	return string(b)
}
