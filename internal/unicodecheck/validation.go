// Package unicodecheck provides consolidated Unicode validation for security-sensitive input.
// It detects zero-width characters, bidirectional overrides, Hangul fillers, control characters,
// excessive combining marks (Zalgo text), and other problematic Unicode that can be used in attacks.
//
// This package consolidates validation previously scattered across api and auth packages
// to ensure consistent character detection across all code paths.
package unicodecheck

import (
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Zero-width characters commonly used in spoofing attacks.
var zeroWidthChars = []rune{
	'\u200B', // Zero Width Space
	'\u200C', // Zero Width Non-Joiner
	'\u200D', // Zero Width Joiner
	'\u200E', // Left-to-Right Mark
	'\u200F', // Right-to-Left Mark
	'\uFEFF', // Byte Order Mark / Zero Width No-Break Space
}

// Bidirectional text override characters that can reorder displayed text.
var bidiOverrideChars = []rune{
	'\u202A', // Left-to-Right Embedding
	'\u202B', // Right-to-Left Embedding
	'\u202C', // Pop Directional Formatting
	'\u202D', // Left-to-Right Override
	'\u202E', // Right-to-Left Override
	'\u2066', // Left-to-Right Isolate
	'\u2067', // Right-to-Left Isolate
	'\u2068', // First Strong Isolate
	'\u2069', // Pop Directional Isolate
}

// ContainsZeroWidthChars checks for zero-width Unicode characters that can be used for spoofing.
// This includes zero-width spaces, joiners, directional marks, and the byte order mark.
func ContainsZeroWidthChars(s string) bool {
	for _, r := range s {
		if slices.Contains(zeroWidthChars, r) {
			return true
		}
	}
	return false
}

// ContainsBidiOverrides checks for bidirectional text override characters
// that can reorder displayed text to disguise malicious content.
func ContainsBidiOverrides(s string) bool {
	for _, r := range s {
		if slices.Contains(bidiOverrideChars, r) {
			return true
		}
	}
	return false
}

// ContainsHangulFillers checks for Hangul filler characters used in fuzzing attacks.
func ContainsHangulFillers(s string) bool {
	for _, r := range s {
		if r == '\u3164' || r == '\uFFA0' {
			return true
		}
	}
	return false
}

// ContainsProblematicCategories checks for characters in problematic Unicode categories:
// Private Use Area, Surrogates, and Non-character codepoints.
func ContainsProblematicCategories(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Co, r) || // Private Use
			unicode.Is(unicode.Cs, r) || // Surrogate
			(r >= 0xFDD0 && r <= 0xFDEF) || // Non-characters
			(r&0xFFFF == 0xFFFE) || (r&0xFFFF == 0xFFFF) { // Non-characters at end of planes
			return true
		}
	}
	return false
}

// ContainsControlChars checks for control characters except common whitespace (\n, \r, \t).
func ContainsControlChars(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	return false
}

// IsCombiningMark returns true if the rune is a Unicode combining character.
// Covers Combining Diacritical Marks (U+0300-U+036F) and common extended ranges
// for Cyrillic, Hebrew, Arabic, and Thai scripts.
func IsCombiningMark(r rune) bool {
	return (r >= '\u0300' && r <= '\u036F') || // Combining Diacritical Marks
		(r >= '\u0483' && r <= '\u0489') || // Combining Cyrillic
		(r >= '\u0591' && r <= '\u05BD') || // Hebrew combining marks
		(r >= '\u0610' && r <= '\u061A') || // Arabic combining marks
		(r >= '\u064B' && r <= '\u065F') || // Arabic combining marks (cont.)
		(r >= '\u0E31' && r <= '\u0E3A') || // Thai combining marks
		(r >= '\u0E47' && r <= '\u0E4E') // Thai combining marks (cont.)
}

// HasExcessiveCombiningMarks detects "Zalgo text" abuse by checking for
// sequences of consecutive combining marks exceeding the given threshold.
// A threshold of 3 is typical for middleware; use 1 to reject any combining marks
// in the basic Combining Diacritical Marks range (U+0300-U+036F).
func HasExcessiveCombiningMarks(s string, maxConsecutive int) bool {
	consecutiveCombining := 0
	for _, r := range s {
		if IsCombiningMark(r) {
			consecutiveCombining++
			if consecutiveCombining >= maxConsecutive {
				return true
			}
			continue
		}
		consecutiveCombining = 0
	}
	return false
}

// ContainsAnyCombiningMarks checks whether the string contains any combining
// diacritical marks in the basic range (U+0300-U+036F). This is stricter than
// HasExcessiveCombiningMarks and is used for fields where combining marks are
// not expected at all.
func ContainsAnyCombiningMarks(s string) bool {
	for _, r := range s {
		if r >= '\u0300' && r <= '\u036F' {
			return true
		}
	}
	return false
}

// ContainsFullwidthStructuralChars checks for fullwidth characters that could be
// used for visual spoofing of JSON structural characters (brackets, quotes, etc.).
// Fullwidth forms are legitimate in CJK text but not in JSON structure.
func ContainsFullwidthStructuralChars(s string) bool {
	for _, r := range s {
		if r >= '\uFF00' && r <= '\uFFEF' {
			if strings.ContainsAny(string(r), "[]{}\":,") {
				return true
			}
		}
	}
	return false
}

// IsNFCNormalized checks whether the string is in NFC (Canonical Composition) form.
// Non-normalized Unicode can cause storage and comparison issues.
func IsNFCNormalized(s string) bool {
	return norm.NFC.String(s) == s
}

// SanitizeForLogging removes potentially dangerous characters from strings before logging.
// Replaces control characters with [CTRL] and zero-width characters with [ZW].
func SanitizeForLogging(s string) string {
	var result strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r':
			result.WriteString("[CTRL]")
		case isZeroWidthChar(r):
			result.WriteString("[ZW]")
		default:
			result.WriteRune(r)
		}
	}
	return result.String()
}

// isZeroWidthChar checks a single rune against the zero-width character list.
func isZeroWidthChar(r rune) bool {
	return slices.Contains(zeroWidthChars, r)
}
