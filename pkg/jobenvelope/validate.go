package jobenvelope

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Validate checks a Job against the envelope contract. It rejects a missing
// job_id, a missing content_type, an empty Input, and an Input that sets
// both the content-ref and source-locator modes at once. It does NOT
// reject source-locator alone — that mode is RESERVED but schema-valid.
// SEM@29ce3cc7a3143d57faa07f7e0071ce27b53226e0: validate a job envelope for required fields and mutually exclusive input locators (pure)
func Validate(j Job) error {
	if j.JobID == "" {
		return fmt.Errorf("jobenvelope: job_id is required")
	}
	if j.ContentType == "" {
		return fmt.Errorf("jobenvelope: content_type is required")
	}
	hasContentRef := j.Input.ObjectRef != ""
	hasSourceLocator := j.Input.SourceURL != ""
	switch {
	case hasContentRef && hasSourceLocator:
		return fmt.Errorf("jobenvelope: input sets both content-ref and source-locator")
	case !hasContentRef && !hasSourceLocator:
		return fmt.Errorf("jobenvelope: input has neither object_ref nor source_url")
	}
	return nil
}

// Field bounds for the return-path (Result) envelope. Results are published
// by workers over NATS and cross a trust boundary back into the monolith,
// which persists the reason fields and later serves them to authenticated
// clients — so the bounds are enforced here at consume time, not only at
// the database schema level.
const (
	// MaxReasonCodeLen mirrors the documents.access_reason_code column
	// width. Enforced at the boundary so an oversize code is rejected up
	// front instead of erroring inside the database write and redelivering.
	MaxReasonCodeLen = 64
	// MaxReasonDetailLen caps the free-text reason_detail a worker may
	// attach. SanitizeResult truncates rather than rejects, so an over-long
	// detail cannot wedge a result delivery into a retry loop.
	MaxReasonDetailLen = 4096
	// MaxResultRefLen bounds output.result_ref. Real refs are short
	// "<bucket>/<name>" Object Store locators.
	MaxResultRefLen = 1024
)

// ValidateResult checks a Result against the envelope contract: job_id
// present, status one of the known terminal states, reason_code within
// length and charset bounds, and output.result_ref bounded. It does NOT
// check reason_detail — free text is normalized by SanitizeResult instead,
// so a verbose-but-honest worker is truncated rather than dropped. Callers
// on the consume side should Term (not Nak) on error: an invalid envelope
// never becomes valid on redelivery.
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: validate a job result envelope for required fields, status, reason code format, and ref length (pure)
func ValidateResult(r Result) error {
	if r.JobID == "" {
		return fmt.Errorf("jobenvelope: job_id is required")
	}
	switch r.Status {
	case StatusCompleted, StatusFailed:
	default:
		return fmt.Errorf("jobenvelope: invalid status %q", r.Status)
	}
	if len(r.ReasonCode) > MaxReasonCodeLen {
		return fmt.Errorf("jobenvelope: reason_code exceeds %d bytes", MaxReasonCodeLen)
	}
	if !validReasonCode(r.ReasonCode) {
		return fmt.Errorf("jobenvelope: reason_code %q contains invalid characters", r.ReasonCode)
	}
	if len(r.Output.ResultRef) > MaxResultRefLen {
		return fmt.Errorf("jobenvelope: output.result_ref exceeds %d bytes", MaxResultRefLen)
	}
	return nil
}

// validReasonCode reports whether s is syntactically a reason code. Codes
// are machine-readable identifiers (e.g. "extraction_limit:part_size"), so
// the charset is the closed set used by every existing Reason* constant:
// lowercase ASCII letters, digits, '_', ':', '.', and '-'. Empty is valid
// (success results carry no code). The check is syntactic rather than a
// constant whitelist so a newer worker can introduce a reason code without
// a lockstep monolith deploy.
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: check that a reason code contains only lowercase alphanumeric and safe punctuation characters (pure)
func validReasonCode(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '_', c == ':', c == '.', c == '-':
		default:
			return false
		}
	}
	return true
}

// SanitizeResult returns r with its free-text fields normalized:
// reason_detail is coerced to valid UTF-8 (so the database write cannot
// fail on encoding and redeliver), stripped of control characters other
// than newline and tab (so terminal escape sequences cannot ride a stored
// detail into a log viewer or client), and truncated to MaxReasonDetailLen
// bytes on a rune boundary. Call it after ValidateResult on the consume
// side; truncation instead of rejection keeps an oversize detail from
// turning into a redelivery loop.
// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: sanitize the free-text reason detail of a job result and return the cleaned copy (pure)
func SanitizeResult(r Result) Result {
	r.ReasonDetail = sanitizeDetail(r.ReasonDetail)
	return r
}

// SEM@d056a3ea026249d40d05ab6af7f092a043f72c7a: strip control characters, coerce invalid UTF-8, and truncate the reason detail to its byte limit (pure)
func sanitizeDetail(s string) string {
	s = strings.ToValidUTF8(s, "�")
	s = strings.Map(func(c rune) rune {
		if c == '\n' || c == '\t' {
			return c
		}
		if unicode.IsControl(c) {
			return -1
		}
		return c
	}, s)
	if len(s) > MaxReasonDetailLen {
		cut := MaxReasonDetailLen
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}
