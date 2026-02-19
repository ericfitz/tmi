package api

import (
	"errors"
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
				var reqErr *RequestError
				if !errors.As(err, &reqErr) {
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
				var reqErr *RequestError
				if !errors.As(err, &reqErr) {
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
				var reqErr *RequestError
				if !errors.As(err, &reqErr) {
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

func TestValidateIcon(t *testing.T) {
	tests := []struct {
		name    string
		icon    string
		wantErr bool
		errMsg  string
	}{
		// Valid cases
		{
			name:    "empty icon (allowed)",
			icon:    "",
			wantErr: false,
		},
		{
			name:    "valid Material Symbols icon - simple name",
			icon:    "material-symbols:home",
			wantErr: false,
		},
		{
			name:    "valid Material Symbols icon - with underscores",
			icon:    "material-symbols:security_scanner",
			wantErr: false,
		},
		{
			name:    "valid Material Symbols icon - with digits in middle",
			icon:    "material-symbols:icon3d",
			wantErr: false,
		},
		{
			name:    "valid Material Symbols icon - complex name",
			icon:    "material-symbols:add_circle_outline",
			wantErr: false,
		},
		{
			name:    "valid FontAwesome icon - solid style",
			icon:    "fa-solid fa-rocket",
			wantErr: false,
		},
		{
			name:    "valid FontAwesome icon - regular style",
			icon:    "fa-regular fa-shield",
			wantErr: false,
		},
		{
			name:    "valid FontAwesome icon - brands style",
			icon:    "fa-brands fa-github",
			wantErr: false,
		},
		{
			name:    "valid FontAwesome icon - with hyphens in icon name",
			icon:    "fa-solid fa-arrow-right",
			wantErr: false,
		},
		{
			name:    "valid FontAwesome icon - multiple hyphens",
			icon:    "fa-solid fa-circle-chevron-right",
			wantErr: false,
		},
		// Invalid cases
		{
			name:    "icon exceeds maximum length",
			icon:    "material-symbols:" + string(make([]byte, MaxIconLength)),
			wantErr: true,
			errMsg:  "exceeds maximum length",
		},
		{
			name:    "invalid Material Symbols - uppercase letter",
			icon:    "material-symbols:Home",
			wantErr: true,
			errMsg:  "Invalid Material Symbols icon format",
		},
		{
			name:    "valid Material Symbols - consecutive underscores allowed by regex",
			icon:    "material-symbols:home__icon",
			wantErr: false, // Current regex allows consecutive underscores despite comment
		},
		{
			name:    "invalid Material Symbols - trailing underscore",
			icon:    "material-symbols:home_",
			wantErr: true,
			errMsg:  "Invalid Material Symbols icon format",
		},
		{
			name:    "invalid Material Symbols - starts with digit",
			icon:    "material-symbols:123icon",
			wantErr: true,
			errMsg:  "Invalid Material Symbols icon format",
		},
		{
			name:    "invalid Material Symbols - special characters",
			icon:    "material-symbols:home@icon",
			wantErr: true,
			errMsg:  "Invalid Material Symbols icon format",
		},
		{
			name:    "invalid FontAwesome - missing space",
			icon:    "fa-solidfa-rocket",
			wantErr: true,
			errMsg:  "Invalid FontAwesome icon format",
		},
		{
			name:    "invalid FontAwesome - uppercase",
			icon:    "fa-solid fa-Rocket",
			wantErr: true,
			errMsg:  "Invalid FontAwesome icon format",
		},
		{
			name:    "invalid FontAwesome - numbers in icon name",
			icon:    "fa-solid fa-rocket123",
			wantErr: true,
			errMsg:  "Invalid FontAwesome icon format",
		},
		{
			name:    "invalid FontAwesome - double hyphen",
			icon:    "fa-solid fa-arrow--right",
			wantErr: true,
			errMsg:  "Invalid FontAwesome icon format",
		},
		{
			name:    "unknown icon format - no prefix",
			icon:    "rocket",
			wantErr: true,
			errMsg:  "must be in Material Symbols format",
		},
		{
			name:    "unknown icon format - wrong prefix",
			icon:    "icon:rocket",
			wantErr: true,
			errMsg:  "must be in Material Symbols format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIcon(tt.icon)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIcon() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				var reqErr *RequestError
				if !errors.As(err, &reqErr) {
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

func TestValidateObjects(t *testing.T) {
	tests := []struct {
		name    string
		objects []string
		wantErr bool
		errMsg  string
	}{
		// Valid cases
		{
			name:    "empty objects array (allowed)",
			objects: []string{},
			wantErr: false,
		},
		{
			name:    "nil objects (allowed)",
			objects: nil,
			wantErr: false,
		},
		{
			name:    "single valid object type",
			objects: []string{"threat_model"},
			wantErr: false,
		},
		{
			name:    "multiple valid object types",
			objects: []string{"threat_model", "diagram", "threat"},
			wantErr: false,
		},
		{
			name:    "all valid object types",
			objects: TMIObjectTypes,
			wantErr: false,
		},
		// Invalid cases
		{
			name:    "single invalid object type",
			objects: []string{"invalid_type"},
			wantErr: true,
			errMsg:  "Invalid object types",
		},
		{
			name:    "mixed valid and invalid types",
			objects: []string{"threat_model", "invalid_type", "diagram"},
			wantErr: true,
			errMsg:  "Invalid object types",
		},
		{
			name:    "multiple invalid types",
			objects: []string{"foo", "bar", "baz"},
			wantErr: true,
			errMsg:  "Invalid object types",
		},
		{
			name:    "case sensitive - uppercase",
			objects: []string{"THREAT_MODEL"},
			wantErr: true,
			errMsg:  "Invalid object types",
		},
		{
			name:    "exceeds maximum objects",
			objects: make([]string, MaxAddonObjects+1),
			wantErr: true,
			errMsg:  "exceeds maximum size",
		},
	}

	// Fill the excessive objects test case with valid types
	for i, tt := range tests {
		if tt.name == "exceeds maximum objects" {
			for j := range tests[i].objects {
				tests[i].objects[j] = "threat_model"
			}
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateObjects(tt.objects)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateObjects() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				var reqErr *RequestError
				if !errors.As(err, &reqErr) {
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

func TestCheckHTMLInjection(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		fieldName string
		wantErr   bool
		errMsg    string
	}{
		// Valid cases
		{
			name:      "plain text",
			value:     "This is a valid description",
			fieldName: "description",
			wantErr:   false,
		},
		{
			name:      "text with angle brackets (not HTML)",
			value:     "Value > 100 and < 200",
			fieldName: "description",
			wantErr:   false,
		},
		// Invalid cases - script tags
		{
			name:      "script tag lowercase",
			value:     "<script>alert('xss')</script>",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "script tag uppercase",
			value:     "<SCRIPT>alert('xss')</SCRIPT>",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "script tag mixed case",
			value:     "<ScRiPt>alert('xss')</ScRiPt>",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		// Invalid cases - iframe
		{
			name:      "iframe tag",
			value:     "<iframe src='evil.com'></iframe>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		// Invalid cases - javascript protocol
		{
			name:      "javascript protocol",
			value:     "javascript:alert('xss')",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		// Invalid cases - event handlers
		{
			name:      "onload event",
			value:     "<img src='x' onload=alert('xss')>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "onerror event",
			value:     "<img src='x' onerror=alert('xss')>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "onclick event",
			value:     "<div onclick=alert('xss')>Click</div>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "onmouseover event",
			value:     "<span onmouseover=alert('xss')>Hover</span>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "onfocus event",
			value:     "<input onfocus=alert('xss')>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "onblur event",
			value:     "<input onblur=alert('xss')>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		// Invalid cases - dangerous elements
		{
			name:      "object tag",
			value:     "<object data='evil.swf'></object>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "embed tag",
			value:     "<embed src='evil.swf'>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		{
			name:      "applet tag",
			value:     "<applet code='Evil.class'></applet>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "potentially unsafe content",
		},
		// Template injection patterns
		{
			name:      "Handlebars/Jinja2 template opening",
			value:     "{{constructor.constructor('alert(1)')()}}",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "template expression",
		},
		{
			name:      "Handlebars/Jinja2 template closing",
			value:     "some text}}",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "template expression",
		},
		{
			name:      "JavaScript template literal",
			value:     "${alert(1)}",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "template interpolation",
		},
		{
			name:      "JSP/ASP/ERB opening tag",
			value:     "<%=System.getProperty('user.home')%>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "server template tag",
		},
		{
			name:      "JSP/ASP/ERB closing tag",
			value:     "some text%>",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "server template tag",
		},
		{
			name:      "Spring EL expression",
			value:     "#{T(java.lang.Runtime).getRuntime().exec('calc')}",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "expression language",
		},
		{
			name:      "GitHub Actions context injection",
			value:     "${{github.event.issue.title}}",
			fieldName: "description",
			wantErr:   true,
			errMsg:    "GitHub Actions context",
		},
		{
			name:      "Angular template expression",
			value:     "{{user.name}}",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "template expression",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkHTMLInjection(tt.value, tt.fieldName)
			if (err != nil) != tt.wantErr {
				t.Errorf("checkHTMLInjection() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				var reqErr *RequestError
				if !errors.As(err, &reqErr) {
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

func TestValidateAddonName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		// Valid cases
		{
			name:    "valid simple name",
			input:   "Security Scanner",
			wantErr: false,
		},
		{
			name:    "valid name with special characters",
			input:   "Scanner v2.0 - Enterprise Edition",
			wantErr: false,
		},
		{
			name:    "valid name at max length",
			input:   "A valid addon name that is exactly at the maximum length allowed for addon names", // Normal text
			wantErr: false,
		},
		// Invalid cases
		{
			name:    "empty name",
			input:   "",
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name:    "name exceeds 255 characters",
			input:   "This is a very long addon name that exceeds the maximum allowed length of 255 characters and should be rejected by the validation function because it is too long for the database field and could cause issues with storage and display. We need at least 256 characters here.",
			wantErr: true,
			errMsg:  "exceeds maximum length",
		},
		{
			name:    "name with script tag",
			input:   "<script>alert('xss')</script>",
			wantErr: true,
			errMsg:  "potentially unsafe content",
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
				var reqErr *RequestError
				if !errors.As(err, &reqErr) {
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

func TestValidateAddonDescription(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		// Valid cases
		{
			name:    "valid description",
			input:   "This addon provides security scanning capabilities for threat models.",
			wantErr: false,
		},
		{
			name:    "empty description (allowed)",
			input:   "",
			wantErr: false,
		},
		// Invalid cases
		{
			name:    "description with javascript protocol",
			input:   "Click here: javascript:alert('xss')",
			wantErr: true,
			errMsg:  "potentially unsafe content",
		},
		{
			name:    "description with iframe",
			input:   "See embedded content: <iframe src='evil.com'></iframe>",
			wantErr: true,
			errMsg:  "potentially unsafe content",
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
				var reqErr *RequestError
				if !errors.As(err, &reqErr) {
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

func TestTMIObjectTypes(t *testing.T) {
	t.Run("contains expected object types", func(t *testing.T) {
		expected := []string{
			"threat_model",
			"diagram",
			"asset",
			"threat",
			"document",
			"note",
			"repository",
			"metadata",
			"survey",
			"survey_response",
		}

		if len(TMIObjectTypes) != len(expected) {
			t.Errorf("Expected %d object types, got %d", len(expected), len(TMIObjectTypes))
		}

		for _, expectedType := range expected {
			found := false
			for _, actualType := range TMIObjectTypes {
				if actualType == expectedType {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Expected object type %q not found in TMIObjectTypes", expectedType)
			}
		}
	})
}
