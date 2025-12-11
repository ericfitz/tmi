package api

import (
	"testing"
)

func TestValidateUnicodeContent(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		fieldName string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid ASCII text",
			value:     "Hello World",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "valid Unicode text (emoji)",
			value:     "Test 🚀 Addon",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "empty string",
			value:     "",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "zero-width space (U+200B)",
			value:     "Test\u200BAddon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains zero-width characters",
		},
		{
			name:      "zero-width non-joiner (U+200C)",
			value:     "Test\u200CAddon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains zero-width characters",
		},
		{
			name:      "zero-width joiner (U+200D)",
			value:     "Test\u200DAddon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains zero-width characters",
		},
		{
			name:      "zero-width no-break space/BOM (U+FEFF)",
			value:     "Test\uFEFFAddon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains zero-width characters",
		},
		{
			name:      "left-to-right embedding (U+202A)",
			value:     "Test\u202AAddon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "right-to-left embedding (U+202B)",
			value:     "Test\u202BAddon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "pop directional formatting (U+202C)",
			value:     "Test\u202CAddon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "left-to-right override (U+202D)",
			value:     "Test\u202DAddon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "right-to-left override (U+202E)",
			value:     "Test\u202EAddon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "left-to-right isolate (U+2066)",
			value:     "Test\u2066Addon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "right-to-left isolate (U+2067)",
			value:     "Test\u2067Addon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "first strong isolate (U+2068)",
			value:     "Test\u2068Addon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "pop directional isolate (U+2069)",
			value:     "Test\u2069Addon",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "contains bidirectional text control characters",
		},
		{
			name:      "Hangul filler (U+3164)",
			value:     "Test\u3164Addon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains Hangul filler characters",
		},
		{
			name:      "combining grave accent (U+0300)",
			value:     "Test\u0300Addon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains excessive combining diacritical marks",
		},
		{
			name:      "combining tilde (U+0303)",
			value:     "Test\u0303Addon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains excessive combining diacritical marks",
		},
		{
			name:      "combining diaeresis (U+0308)",
			value:     "Test\u0308Addon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains excessive combining diacritical marks",
		},
		{
			name:      "Zalgo text (multiple combining marks)",
			value:     "T\u0300\u0301\u0302\u0303est",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains excessive combining diacritical marks",
		},
		{
			name:      "valid CJK text",
			value:     "测试插件",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "valid Arabic text",
			value:     "اختبار",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "valid Cyrillic text",
			value:     "Тест",
			fieldName: "name",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUnicodeContent(tt.value, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUnicodeContent() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				reqErr, ok := err.(*RequestError)
				if !ok {
					t.Errorf("Expected RequestError, got %T", err)
					return
				}
				if !containsString(reqErr.Message, tt.errMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errMsg, reqErr.Message)
				}
			}
		})
	}
}

func TestValidateAddonNameWithUnicode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid name",
			input:   "Security Scanner",
			wantErr: false,
		},
		{
			name:    "name with zero-width space",
			input:   "Security\u200BScanner",
			wantErr: true,
			errMsg:  "contains zero-width characters",
		},
		{
			name:    "name with bidirectional override",
			input:   "Security\u202EScanner",
			wantErr: true,
			errMsg:  "contains bidirectional text control characters",
		},
		{
			name:    "name with Hangul filler",
			input:   "Security\u3164Scanner",
			wantErr: true,
			errMsg:  "contains Hangul filler characters",
		},
		{
			name:    "name with combining marks",
			input:   "Security\u0300Scanner",
			wantErr: true,
			errMsg:  "contains excessive combining diacritical marks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAddonName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAddonName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				reqErr, ok := err.(*RequestError)
				if !ok {
					t.Errorf("Expected RequestError, got %T", err)
					return
				}
				if !containsString(reqErr.Message, tt.errMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errMsg, reqErr.Message)
				}
			}
		})
	}
}

func TestValidateAddonDescriptionWithUnicode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid description",
			input:   "A powerful security scanner for threat detection",
			wantErr: false,
		},
		{
			name:    "empty description (allowed)",
			input:   "",
			wantErr: false,
		},
		{
			name:    "description with zero-width joiner",
			input:   "Scanner\u200Dfor\u200Dsecurity",
			wantErr: true,
			errMsg:  "contains zero-width characters",
		},
		{
			name:    "description with bidirectional isolate",
			input:   "Security\u2066scanner\u2069",
			wantErr: true,
			errMsg:  "contains bidirectional text control characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAddonDescription(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAddonDescription() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				reqErr, ok := err.(*RequestError)
				if !ok {
					t.Errorf("Expected RequestError, got %T", err)
					return
				}
				if !containsString(reqErr.Message, tt.errMsg) {
					t.Errorf("Expected error message to contain %q, got %q", tt.errMsg, reqErr.Message)
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
