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

// isIndicScript returns true if the rune belongs to an Indic script Unicode block
// where ZWNJ has legitimate use for proper text rendering.
func isIndicScript(r rune) bool {
	return (r >= '\u0900' && r <= '\u097F') || // Devanagari (Hindi, Sanskrit, Marathi)
		(r >= '\u0980' && r <= '\u09FF') || // Bengali
		(r >= '\u0A00' && r <= '\u0A7F') || // Gurmukhi (Punjabi)
		(r >= '\u0A80' && r <= '\u0AFF') || // Gujarati
		(r >= '\u0B00' && r <= '\u0B7F') || // Oriya
		(r >= '\u0B80' && r <= '\u0BFF') || // Tamil
		(r >= '\u0C00' && r <= '\u0C7F') || // Telugu
		(r >= '\u0C80' && r <= '\u0CFF') || // Kannada
		(r >= '\u0D00' && r <= '\u0D7F') || // Malayalam
		(r >= '\u0D80' && r <= '\u0DFF') || // Sinhala
		(r >= '\u1000' && r <= '\u109F') || // Myanmar
		(r >= '\u1780' && r <= '\u17FF') // Khmer
}

// isEmojiCodepoint returns true if the rune is commonly part of emoji sequences.
// Covers the main emoji ranges per Unicode Standard Annex #51.
func isEmojiCodepoint(r rune) bool {
	return (r >= 0x1F600 && r <= 0x1F64F) || // Emoticons
		(r >= 0x1F300 && r <= 0x1F5FF) || // Misc Symbols and Pictographs
		(r >= 0x1F680 && r <= 0x1F6FF) || // Transport and Map
		(r >= 0x1F900 && r <= 0x1F9FF) || // Supplemental Symbols and Pictographs
		(r >= 0x1FA00 && r <= 0x1FA6F) || // Chess Symbols
		(r >= 0x1FA70 && r <= 0x1FAFF) || // Symbols and Pictographs Extended-A
		(r >= 0x2600 && r <= 0x26FF) || // Misc Symbols
		(r >= 0x2700 && r <= 0x27BF) || // Dingbats
		(r >= 0xFE00 && r <= 0xFE0F) || // Variation Selectors
		(r >= 0xE0020 && r <= 0xE007F) // Tags (flag sequences)
}

// ContainsDangerousZeroWidthChars performs context-aware checking of zero-width
// characters. Unlike ContainsZeroWidthChars which blocks all zero-width chars,
// this function allows:
//   - ZWNJ (U+200C) when appearing between Indic script characters
//   - ZWJ (U+200D) when appearing between emoji codepoints
//
// It always blocks:
//   - ZWS (U+200B) — truly invisible, no legitimate use in JSON string values
//   - BOM (U+FEFF) — no legitimate use mid-string
//   - LRM (U+200E) and RLM (U+200F) — directional marks
func ContainsDangerousZeroWidthChars(s string) bool {
	runes := []rune(s)
	for i, r := range runes {
		switch r {
		case '\u200B', '\uFEFF', '\u200E', '\u200F':
			// Always dangerous
			return true
		case '\u200C': // ZWNJ
			// Allow between Indic script characters
			if i == 0 || i == len(runes)-1 {
				return true // At boundary — no valid context
			}
			prev := runes[i-1]
			next := runes[i+1]
			if !isIndicScript(prev) || !isIndicScript(next) {
				return true
			}
		case '\u200D': // ZWJ
			// Allow between emoji codepoints
			if i == 0 || i == len(runes)-1 {
				return true
			}
			prev := runes[i-1]
			next := runes[i+1]
			if !isEmojiCodepoint(prev) && !isEmojiCodepoint(next) {
				return true
			}
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

// combiningMarkRanges defines Unicode ranges of combining characters across scripts.
// Used by IsCombiningMark to detect combining marks for Zalgo text prevention.
var combiningMarkRanges = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0x0300, Hi: 0x036F, Stride: 1}, // Combining Diacritical Marks
		{Lo: 0x0483, Hi: 0x0489, Stride: 1}, // Combining Cyrillic
		{Lo: 0x0591, Hi: 0x05BD, Stride: 1}, // Hebrew combining marks
		{Lo: 0x0610, Hi: 0x061A, Stride: 1}, // Arabic combining marks
		{Lo: 0x064B, Hi: 0x065F, Stride: 1}, // Arabic combining marks (cont.)
		{Lo: 0x0900, Hi: 0x0903, Stride: 1}, // Devanagari: chandrabindu through visarga
		{Lo: 0x093A, Hi: 0x094F, Stride: 1}, // Devanagari: nukta through virama
		{Lo: 0x0951, Hi: 0x0957, Stride: 1}, // Devanagari: stress/accent marks
		{Lo: 0x0962, Hi: 0x0963, Stride: 1}, // Devanagari: vowel signs
		{Lo: 0x0981, Hi: 0x0983, Stride: 1}, // Bengali: chandrabindu through visarga
		{Lo: 0x09BC, Hi: 0x09BC, Stride: 1}, // Bengali: nukta
		{Lo: 0x09BE, Hi: 0x09CD, Stride: 1}, // Bengali: vowel signs through virama
		{Lo: 0x0A01, Hi: 0x0A03, Stride: 1}, // Gurmukhi: vowel signs
		{Lo: 0x0A3C, Hi: 0x0A4D, Stride: 1}, // Gurmukhi: nukta through virama
		{Lo: 0x0A81, Hi: 0x0A83, Stride: 1}, // Gujarati: vowel signs
		{Lo: 0x0ABC, Hi: 0x0ACD, Stride: 1}, // Gujarati: nukta through virama
		{Lo: 0x0B01, Hi: 0x0B03, Stride: 1}, // Oriya: vowel signs
		{Lo: 0x0B3C, Hi: 0x0B4D, Stride: 1}, // Oriya: nukta through virama
		{Lo: 0x0B82, Hi: 0x0B83, Stride: 1}, // Tamil: anusvara/visarga
		{Lo: 0x0BBE, Hi: 0x0BCD, Stride: 1}, // Tamil: vowel signs through virama
		{Lo: 0x0C00, Hi: 0x0C03, Stride: 1}, // Telugu: vowel signs
		{Lo: 0x0C3E, Hi: 0x0C4D, Stride: 1}, // Telugu: vowel signs through virama
		{Lo: 0x0C81, Hi: 0x0C83, Stride: 1}, // Kannada: vowel signs
		{Lo: 0x0CBC, Hi: 0x0CCD, Stride: 1}, // Kannada: nukta through virama
		{Lo: 0x0D00, Hi: 0x0D03, Stride: 1}, // Malayalam: vowel signs
		{Lo: 0x0D3B, Hi: 0x0D4D, Stride: 1}, // Malayalam: virama range
		{Lo: 0x0D81, Hi: 0x0D83, Stride: 1}, // Sinhala: vowel signs
		{Lo: 0x0DCA, Hi: 0x0DDF, Stride: 1}, // Sinhala: virama through vowel signs
		{Lo: 0x0E31, Hi: 0x0E3A, Stride: 1}, // Thai combining marks
		{Lo: 0x0E47, Hi: 0x0E4E, Stride: 1}, // Thai combining marks (cont.)
	},
}

// IsCombiningMark returns true if the rune is a Unicode combining character.
// Covers Combining Diacritical Marks (U+0300-U+036F) and extended ranges
// for Cyrillic, Hebrew, Arabic, Thai, and Indic scripts.
func IsCombiningMark(r rune) bool {
	return unicode.Is(combiningMarkRanges, r)
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
