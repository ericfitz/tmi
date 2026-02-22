package unicodecheck

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainsZeroWidthChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"normal text", "hello world", false},
		{"zero width space", "hello\u200Bworld", true},
		{"zero width non-joiner", "hello\u200Cworld", true},
		{"zero width joiner", "hello\u200Dworld", true},
		{"left-to-right mark", "hello\u200Eworld", true},
		{"right-to-left mark", "hello\u200Fworld", true},
		{"byte order mark", "hello\uFEFFworld", true},
		{"only zero width", "\u200B", true},
		{"CJK characters", "\u4e16\u754c", false},
		{"emoji", "hello \U0001F600 world", false},
		{"accented characters", "caf\u00e9", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ContainsZeroWidthChars(tt.input))
		})
	}
}

func TestContainsBidiOverrides(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"normal text", "hello world", false},
		{"LTR embedding", "hello\u202Aworld", true},
		{"RTL embedding", "hello\u202Bworld", true},
		{"pop directional", "hello\u202Cworld", true},
		{"LTR override", "hello\u202Dworld", true},
		{"RTL override", "hello\u202Eworld", true},
		{"LTR isolate", "hello\u2066world", true},
		{"RTL isolate", "hello\u2067world", true},
		{"first strong isolate", "hello\u2068world", true},
		{"pop directional isolate", "hello\u2069world", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ContainsBidiOverrides(tt.input))
		})
	}
}

func TestContainsHangulFillers(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"normal text", "hello", false},
		{"Hangul filler U+3164", "hello\u3164world", true},
		{"Hangul filler U+FFA0", "hello\uFFA0world", true},
		{"normal Hangul", "\uAC00\uB098\uB2E4", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ContainsHangulFillers(tt.input))
		})
	}
}

func TestContainsProblematicCategories(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"normal text", "hello", false},
		{"private use area", "hello\uE000world", true},
		{"non-character FDD0", "hello\uFDD0world", true},
		{"non-character FDEF", "hello\uFDEFworld", true},
		{"non-character FFFE", "hello\uFFFEworld", true},
		{"non-character FFFF", "hello\uFFFFworld", true},
		{"normal Unicode", "caf\u00e9", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ContainsProblematicCategories(tt.input))
		})
	}
}

func TestContainsControlChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"normal text", "hello world", false},
		{"tab allowed", "hello\tworld", false},
		{"newline allowed", "hello\nworld", false},
		{"carriage return allowed", "hello\rworld", false},
		{"null byte", "hello\x00world", true},
		{"bell character", "hello\x07world", true},
		{"escape character", "hello\x1Bworld", true},
		{"delete character", "hello\x7Fworld", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ContainsControlChars(tt.input))
		})
	}
}

func TestIsCombiningMark(t *testing.T) {
	tests := []struct {
		name     string
		input    rune
		expected bool
	}{
		{"combining grave accent", '\u0300', true},
		{"combining acute accent", '\u0301', true},
		{"end of basic combining", '\u036F', true},
		{"combining Cyrillic", '\u0483', true},
		{"Hebrew combining", '\u0591', true},
		{"Arabic combining", '\u0610', true},
		{"Arabic combining cont", '\u064B', true},
		{"Thai combining", '\u0E31', true},
		{"normal letter", 'a', false},
		{"space", ' ', false},
		{"CJK character", '\u4e16', false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsCombiningMark(tt.input))
		})
	}
}

func TestIsCombiningMark_IndicRanges(t *testing.T) {
	tests := []struct {
		name     string
		input    rune
		expected bool
	}{
		// Devanagari
		{"Devanagari chandrabindu", '\u0900', true},
		{"Devanagari virama", '\u094D', true},
		{"Devanagari stress mark", '\u0951', true},
		{"Devanagari consonant ka (not combining)", '\u0915', false},
		// Bengali
		{"Bengali nukta", '\u09BC', true},
		{"Bengali vowel sign aa", '\u09BE', true},
		{"Bengali virama", '\u09CD', true},
		{"Bengali consonant ka (not combining)", '\u0995', false},
		// Tamil
		{"Tamil virama", '\u0BCD', true},
		{"Tamil vowel sign aa", '\u0BBE', true},
		{"Tamil consonant ka (not combining)", '\u0B95', false},
		// Telugu
		{"Telugu vowel sign", '\u0C3E', true},
		{"Telugu virama", '\u0C4D', true},
		// Kannada
		{"Kannada nukta", '\u0CBC', true},
		{"Kannada virama", '\u0CCD', true},
		// Malayalam
		{"Malayalam virama", '\u0D4D', true},
		// Sinhala
		{"Sinhala virama", '\u0DCA', true},
		// Non-Indic should still work
		{"Latin combining grave", '\u0300', true},
		{"ASCII letter (not combining)", 'a', false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsCombiningMark(tt.input))
		})
	}
}

func TestIsIndicScript(t *testing.T) {
	tests := []struct {
		name     string
		input    rune
		expected bool
	}{
		{"Devanagari ka", '\u0915', true},
		{"Devanagari aa", '\u0906', true},
		{"Bengali ka", '\u0995', true},
		{"Gurmukhi ka", '\u0A15', true},
		{"Gujarati ka", '\u0A95', true},
		{"Oriya ka", '\u0B15', true},
		{"Tamil ka", '\u0B95', true},
		{"Telugu ka", '\u0C15', true},
		{"Kannada ka", '\u0C95', true},
		{"Malayalam ka", '\u0D15', true},
		{"Sinhala ka", '\u0D9A', true},
		{"Myanmar ka", '\u1000', true},
		{"Khmer ka", '\u1780', true},
		{"Latin a (not Indic)", 'a', false},
		{"CJK character (not Indic)", '\u4e16', false},
		{"Arabic (not Indic)", '\u0627', false},
		{"Space (not Indic)", ' ', false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isIndicScript(tt.input))
		})
	}
}

func TestIsEmojiCodepoint(t *testing.T) {
	tests := []struct {
		name     string
		input    rune
		expected bool
	}{
		{"grinning face", '\U0001F600', true},
		{"rocket", '\U0001F680', true},
		{"globe", '\U0001F30D', true},
		{"sun symbol", '\u2600', true},
		{"check mark", '\u2714', true},
		{"variation selector", '\uFE0F', true},
		{"Latin a (not emoji)", 'a', false},
		{"CJK character (not emoji)", '\u4e16', false},
		{"space (not emoji)", ' ', false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isEmojiCodepoint(tt.input))
		})
	}
}

func TestContainsDangerousZeroWidthChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Always blocked
		{"empty string", "", false},
		{"normal text", "hello world", false},
		{"ZWS always blocked", "hello\u200Bworld", true},
		{"BOM always blocked", "\uFEFFhello", true},
		{"BOM mid-string blocked", "hello\uFEFFworld", true},
		{"LRM blocked", "hello\u200Eworld", true},
		{"RLM blocked", "hello\u200Fworld", true},

		// ZWNJ context-aware
		{"ZWNJ between Indic chars allowed", "\u0915\u200C\u0916", false},           // Devanagari ka + ZWNJ + kha
		{"ZWNJ between Bengali chars allowed", "\u0995\u200C\u0996", false},         // Bengali ka + ZWNJ + kha
		{"ZWNJ between Tamil chars allowed", "\u0B95\u200C\u0B95", false},           // Tamil ka + ZWNJ + ka
		{"ZWNJ between Latin chars blocked", "test\u200Cvalue", true},               // Not Indic context
		{"ZWNJ at start of string blocked", "\u200C\u0915", true},                   // No preceding char
		{"ZWNJ at end of string blocked", "\u0915\u200C", true},                     // No following char
		{"ZWNJ between mixed scripts blocked", "a\u200C\u0915", true},               // Latin + ZWNJ + Devanagari
		{"ZWNJ Indic then Latin blocked", "\u0915\u200Ca", true},                    // Devanagari + ZWNJ + Latin
		{"multiple ZWNJ in Indic allowed", "\u0915\u200C\u0916\u200C\u0917", false}, // ka-ZWNJ-kha-ZWNJ-ga

		// ZWJ context-aware
		{"ZWJ between emoji allowed", "\U0001F468\u200D\U0001F469", false},                // man + ZWJ + woman
		{"ZWJ family emoji allowed", "\U0001F468\u200D\U0001F469\u200D\U0001F467", false}, // family
		{"ZWJ between Latin chars blocked", "test\u200Dvalue", true},                      // Not emoji context
		{"ZWJ at start blocked", "\u200D\U0001F600", true},                                // No preceding char
		{"ZWJ at end blocked", "\U0001F600\u200D", true},                                  // No following char
		{"ZWJ with one emoji side allowed", "\U0001F468\u200D\u2764", false},              // emoji + ZWJ + misc symbol (emoji range)

		// Normal text passes
		{"precomposed accent", "caf\u00e9", false},
		{"emoji without ZWJ", "Hello \U0001F600 World", false},
		{"CJK characters", "\u4e16\u754c", false},
		{"Hindi text", "\u0928\u092E\u0938\u094D\u0924\u0947", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ContainsDangerousZeroWidthChars(tt.input),
				"ContainsDangerousZeroWidthChars(%q)", tt.input)
		})
	}
}

func TestHasExcessiveCombiningMarks(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		maxConsec int
		expected  bool
	}{
		{"empty string", "", 3, false},
		{"normal text", "hello", 3, false},
		{"one combining mark", "e\u0301", 3, false},
		{"two consecutive", "a\u0300\u0301", 3, false},
		{"three consecutive (at threshold)", "a\u0300\u0301\u0302", 3, true},
		{"Zalgo text", "a\u0300\u0301\u0302\u0303\u0304", 3, true},
		{"threshold of 1 rejects any", "e\u0301", 1, true},
		{"separated combining marks", "a\u0300b\u0301", 3, false},
		{"mixed scripts combining", "a\u0300\u0483\u0591", 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, HasExcessiveCombiningMarks(tt.input, tt.maxConsec))
		})
	}
}

func TestContainsAnyCombiningMarks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"normal text", "hello", false},
		{"grave accent", "e\u0300", true},
		{"acute accent", "e\u0301", true},
		{"precomposed accent", "\u00e9", false},                       // e-acute is precomposed, not a combining mark
		{"Cyrillic combining (not in basic range)", "a\u0483", false}, // Outside U+0300-U+036F
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ContainsAnyCombiningMarks(tt.input))
		})
	}
}

func TestContainsFullwidthStructuralChars(t *testing.T) {
	// Note: The original middleware used strings.ContainsAny(string(r), "[]{}\":,")
	// which compares fullwidth runes against ASCII bytes. Fullwidth chars encode
	// as different UTF-8 bytes than their ASCII counterparts, so this check
	// currently does not match fullwidth structural characters. This function
	// faithfully reproduces that behavior for backward compatibility.
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", false},
		{"normal text", "hello", false},
		{"normal brackets", "{}", false},
		{"fullwidth left bracket", "\uFF5B", false},     // ｛ - no UTF-8 byte overlap with ASCII
		{"fullwidth right bracket", "\uFF5D", false},    // ｝ - no UTF-8 byte overlap with ASCII
		{"fullwidth left square", "\uFF3B", false},      // ［ - no UTF-8 byte overlap with ASCII
		{"fullwidth quotation", "\uFF02", false},        // ＂ - no UTF-8 byte overlap with ASCII
		{"fullwidth letter (allowed)", "\uFF21", false}, // Ａ (not structural)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, ContainsFullwidthStructuralChars(tt.input))
		})
	}
}

func TestIsNFCNormalized(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"empty string", "", true},
		{"ASCII text", "hello", true},
		{"precomposed e-acute (NFC)", "\u00e9", true},
		{"decomposed e-acute (NFD)", "e\u0301", false},
		{"mixed normal and decomposed", "caf\u00e9", true},
		{"decomposed mixed", "cafe\u0301", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsNFCNormalized(tt.input))
		})
	}
}

func TestSanitizeForLogging(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty string", "", ""},
		{"normal text", "hello world", "hello world"},
		{"tabs preserved", "hello\tworld", "hello\tworld"},
		{"newlines preserved", "hello\nworld", "hello\nworld"},
		{"null byte replaced", "hello\x00world", "hello[CTRL]world"},
		{"zero width space replaced", "hello\u200Bworld", "hello[ZW]world"},
		{"BOM replaced", "hello\uFEFFworld", "hello[ZW]world"},
		{"multiple replacements", "\x00\u200B\x07", "[CTRL][ZW][CTRL]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, SanitizeForLogging(tt.input))
		})
	}
}
