package api

import (
	"errors"
	"slices"
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
			name:      "zero-width non-joiner between Latin (U+200C)",
			value:     "Test\u200CAddon",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains zero-width characters",
		},
		{
			name:      "zero-width joiner between Latin (U+200D)",
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
			name:      "single combining grave accent (U+0300) allowed",
			value:     "Test\u0300Addon",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "single combining tilde (U+0303) allowed",
			value:     "Test\u0303Addon",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "single combining diaeresis (U+0308) allowed",
			value:     "Test\u0308Addon",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "Zalgo text (excessive combining marks) rejected",
			value:     "T\u0300\u0301\u0302\u0303est",
			fieldName: "name",
			wantErr:   true,
			errMsg:    "contains excessive combining diacritical marks",
		},
		{
			name:      "ZWNJ between Indic chars allowed",
			value:     "\u0915\u200C\u0916",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "ZWJ in emoji sequence allowed",
			value:     "\U0001F468\u200D\U0001F469",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "precomposed Vietnamese text",
			value:     "Vi\u1EC7t Nam",
			fieldName: "name",
			wantErr:   false,
		},
		{
			name:      "Hindi text with virama",
			value:     "\u0928\u092E\u0938\u094D\u0924\u0947",
			fieldName: "name",
			wantErr:   false,
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
			name:    "name with single combining mark allowed",
			input:   "Security\u0300Scanner",
			wantErr: false,
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
			name:    "description with zero-width joiner between Latin chars",
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
			found := slices.Contains(TMIObjectTypes, expectedType)
			if !found {
				t.Errorf("Expected object type %q not found in TMIObjectTypes", expectedType)
			}
		}
	})
}

// Helper to create string pointer
func strP(s string) *string {
	return &s
}

// Helper to create bool pointer
func boolP(b bool) *bool {
	return &b
}

// Helper to create string slice pointer
func strSliceP(s []string) *[]string {
	return &s
}

// Helper to create float32 pointer
func float32P(f float32) *float32 {
	return &f
}

// Helper to create int pointer
func intP(i int) *int {
	return &i
}

func TestValidateAddonParameters(t *testing.T) {
	tests := []struct {
		name    string
		params  []AddonParameter
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil parameters (allowed)",
			params:  nil,
			wantErr: false,
		},
		{
			name:    "empty parameters (allowed)",
			params:  []AddonParameter{},
			wantErr: false,
		},
		{
			name: "valid enum parameter",
			params: []AddonParameter{
				{
					Name:       "model",
					Type:       AddonParameterTypeEnum,
					EnumValues: strSliceP([]string{"gpt-4", "claude-3"}),
				},
			},
			wantErr: false,
		},
		{
			name: "valid enum with default",
			params: []AddonParameter{
				{
					Name:         "model",
					Type:         AddonParameterTypeEnum,
					EnumValues:   strSliceP([]string{"gpt-4", "claude-3"}),
					DefaultValue: strP("claude-3"),
				},
			},
			wantErr: false,
		},
		{
			name: "valid boolean parameter",
			params: []AddonParameter{
				{
					Name: "skip-threats",
					Type: AddonParameterTypeBoolean,
				},
			},
			wantErr: false,
		},
		{
			name: "valid boolean with default",
			params: []AddonParameter{
				{
					Name:         "skip-threats",
					Type:         AddonParameterTypeBoolean,
					DefaultValue: strP("true"),
				},
			},
			wantErr: false,
		},
		{
			name: "valid string parameter",
			params: []AddonParameter{
				{
					Name:        "comment",
					Type:        AddonParameterTypeString,
					Description: strP("A user comment"),
				},
			},
			wantErr: false,
		},
		{
			name: "valid number parameter",
			params: []AddonParameter{
				{
					Name:         "threshold",
					Type:         AddonParameterTypeNumber,
					DefaultValue: strP("0.75"),
				},
			},
			wantErr: false,
		},
		{
			name: "valid metadata_key parameter",
			params: []AddonParameter{
				{
					Name:        "tenancy",
					Type:        AddonParameterTypeMetadataKey,
					MetadataKey: strP("tenancy_ocid"),
				},
			},
			wantErr: false,
		},
		{
			name: "valid multiple parameters",
			params: []AddonParameter{
				{
					Name:       "model",
					Type:       AddonParameterTypeEnum,
					EnumValues: strSliceP([]string{"gpt-4", "claude-3"}),
					Required:   boolP(true),
				},
				{
					Name: "skip-threats",
					Type: AddonParameterTypeBoolean,
				},
				{
					Name: "comment",
					Type: AddonParameterTypeString,
				},
			},
			wantErr: false,
		},
		// Invalid cases
		{
			name:    "exceeds max parameters",
			params:  make([]AddonParameter, MaxAddonParameters+1),
			wantErr: true,
			errMsg:  "exceeds maximum size",
		},
		{
			name: "duplicate parameter names",
			params: []AddonParameter{
				{Name: "model", Type: AddonParameterTypeString},
				{Name: "Model", Type: AddonParameterTypeString},
			},
			wantErr: true,
			errMsg:  "Duplicate parameter name",
		},
		{
			name: "invalid parameter name - starts with digit",
			params: []AddonParameter{
				{Name: "1model", Type: AddonParameterTypeString},
			},
			wantErr: true,
			errMsg:  "must start with a letter",
		},
		{
			name: "invalid parameter name - special chars",
			params: []AddonParameter{
				{Name: "model@v2", Type: AddonParameterTypeString},
			},
			wantErr: true,
			errMsg:  "must start with a letter",
		},
		{
			name: "enum without enum_values",
			params: []AddonParameter{
				{Name: "model", Type: AddonParameterTypeEnum},
			},
			wantErr: true,
			errMsg:  "must have enum_values",
		},
		{
			name: "enum with empty enum_values",
			params: []AddonParameter{
				{Name: "model", Type: AddonParameterTypeEnum, EnumValues: strSliceP([]string{})},
			},
			wantErr: true,
			errMsg:  "must have enum_values",
		},
		{
			name: "enum default not in values",
			params: []AddonParameter{
				{
					Name:         "model",
					Type:         AddonParameterTypeEnum,
					EnumValues:   strSliceP([]string{"gpt-4", "claude-3"}),
					DefaultValue: strP("llama-2"),
				},
			},
			wantErr: true,
			errMsg:  "not in enum_values",
		},
		{
			name: "enum with metadata_key",
			params: []AddonParameter{
				{
					Name:        "model",
					Type:        AddonParameterTypeEnum,
					EnumValues:  strSliceP([]string{"gpt-4"}),
					MetadataKey: strP("some_key"),
				},
			},
			wantErr: true,
			errMsg:  "must not have metadata_key",
		},
		{
			name: "boolean with enum_values",
			params: []AddonParameter{
				{Name: "flag", Type: AddonParameterTypeBoolean, EnumValues: strSliceP([]string{"yes", "no"})},
			},
			wantErr: true,
			errMsg:  "must not have enum_values",
		},
		{
			name: "boolean with invalid default",
			params: []AddonParameter{
				{Name: "flag", Type: AddonParameterTypeBoolean, DefaultValue: strP("yes")},
			},
			wantErr: true,
			errMsg:  "must be 'true' or 'false'",
		},
		{
			name: "boolean with metadata_key",
			params: []AddonParameter{
				{Name: "flag", Type: AddonParameterTypeBoolean, MetadataKey: strP("some_key")},
			},
			wantErr: true,
			errMsg:  "must not have metadata_key",
		},
		{
			name: "string with enum_values",
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, EnumValues: strSliceP([]string{"a", "b"})},
			},
			wantErr: true,
			errMsg:  "must not have enum_values",
		},
		{
			name: "string with metadata_key",
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, MetadataKey: strP("some_key")},
			},
			wantErr: true,
			errMsg:  "must not have metadata_key",
		},
		{
			name: "number with non-numeric default",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, DefaultValue: strP("abc")},
			},
			wantErr: true,
			errMsg:  "must be a valid number",
		},
		{
			name: "number with enum_values",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, EnumValues: strSliceP([]string{"1", "2"})},
			},
			wantErr: true,
			errMsg:  "must not have enum_values",
		},
		{
			name: "number with metadata_key",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, MetadataKey: strP("some_key")},
			},
			wantErr: true,
			errMsg:  "must not have metadata_key",
		},
		{
			name: "metadata_key without key",
			params: []AddonParameter{
				{Name: "tenancy", Type: AddonParameterTypeMetadataKey},
			},
			wantErr: true,
			errMsg:  "must have metadata_key field set",
		},
		{
			name: "metadata_key with invalid pattern",
			params: []AddonParameter{
				{Name: "tenancy", Type: AddonParameterTypeMetadataKey, MetadataKey: strP("invalid key!")},
			},
			wantErr: true,
			errMsg:  "must match pattern",
		},
		{
			name: "metadata_key with enum_values",
			params: []AddonParameter{
				{Name: "tenancy", Type: AddonParameterTypeMetadataKey, MetadataKey: strP("ocid"), EnumValues: strSliceP([]string{"a"})},
			},
			wantErr: true,
			errMsg:  "must not have enum_values",
		},
		// Constraint field tests - number_min/number_max
		{
			name: "valid number with min and max",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMin: float32P(0), NumberMax: float32P(100)},
			},
			wantErr: false,
		},
		{
			name: "valid number with only min",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMin: float32P(0)},
			},
			wantErr: false,
		},
		{
			name: "valid number with only max",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMax: float32P(100)},
			},
			wantErr: false,
		},
		{
			name: "number min exceeds max",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMin: float32P(100), NumberMax: float32P(10)},
			},
			wantErr: true,
			errMsg:  "number_min",
		},
		{
			name: "number default below min",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMin: float32P(10), DefaultValue: strP("5")},
			},
			wantErr: true,
			errMsg:  "below number_min",
		},
		{
			name: "number default above max",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMax: float32P(10), DefaultValue: strP("15")},
			},
			wantErr: true,
			errMsg:  "exceeds number_max",
		},
		{
			name: "number default within range",
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMin: float32P(0), NumberMax: float32P(100), DefaultValue: strP("50")},
			},
			wantErr: false,
		},
		// Constraint field tests - string_max_length
		{
			name: "valid string with max_length",
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, StringMaxLength: intP(500)},
			},
			wantErr: false,
		},
		{
			name: "string_max_length too low",
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, StringMaxLength: intP(0)},
			},
			wantErr: true,
			errMsg:  "string_max_length must be between",
		},
		{
			name: "string_max_length too high",
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, StringMaxLength: intP(10001)},
			},
			wantErr: true,
			errMsg:  "string_max_length must be between",
		},
		{
			name: "string default exceeds max_length",
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, StringMaxLength: intP(5), DefaultValue: strP("toolong")},
			},
			wantErr: true,
			errMsg:  "exceeds string_max_length",
		},
		// Constraint field tests - string_validation_regex
		{
			name: "valid string with regex",
			params: []AddonParameter{
				{Name: "code", Type: AddonParameterTypeString, StringValidationRegex: strP("^[A-Z]{3}-[0-9]+$")},
			},
			wantErr: false,
		},
		{
			name: "string with invalid regex",
			params: []AddonParameter{
				{Name: "code", Type: AddonParameterTypeString, StringValidationRegex: strP("[invalid")},
			},
			wantErr: true,
			errMsg:  "not a valid regular expression",
		},
		{
			name: "string default not matching regex",
			params: []AddonParameter{
				{Name: "code", Type: AddonParameterTypeString, StringValidationRegex: strP("^[A-Z]{3}$"), DefaultValue: strP("abc")},
			},
			wantErr: true,
			errMsg:  "does not match string_validation_regex",
		},
		{
			name: "string default matching regex",
			params: []AddonParameter{
				{Name: "code", Type: AddonParameterTypeString, StringValidationRegex: strP("^[A-Z]{3}$"), DefaultValue: strP("ABC")},
			},
			wantErr: false,
		},
		// Cross-type constraint rejection
		{
			name: "enum with number_min rejected",
			params: []AddonParameter{
				{Name: "model", Type: AddonParameterTypeEnum, EnumValues: strSliceP([]string{"a"}), NumberMin: float32P(0)},
			},
			wantErr: true,
			errMsg:  "must not have number_min",
		},
		{
			name: "boolean with string_max_length rejected",
			params: []AddonParameter{
				{Name: "flag", Type: AddonParameterTypeBoolean, StringMaxLength: intP(10)},
			},
			wantErr: true,
			errMsg:  "must not have string_max_length",
		},
		{
			name: "number with string_validation_regex rejected",
			params: []AddonParameter{
				{Name: "val", Type: AddonParameterTypeNumber, StringValidationRegex: strP(".*")},
			},
			wantErr: true,
			errMsg:  "must not have string_validation_regex",
		},
		{
			name: "string with number_max rejected",
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, NumberMax: float32P(100)},
			},
			wantErr: true,
			errMsg:  "must not have number_max",
		},
		{
			name: "metadata_key with number_min rejected",
			params: []AddonParameter{
				{Name: "key", Type: AddonParameterTypeMetadataKey, MetadataKey: strP("k"), NumberMin: float32P(0)},
			},
			wantErr: true,
			errMsg:  "must not have number_min",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAddonParameters(tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAddonParameters() error = %v, wantErr %v", err, tt.wantErr)
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

func TestValidateInvocationData(t *testing.T) {
	enumValues := []string{"gpt-4", "claude-3"}
	params := []AddonParameter{
		{
			Name:       "model",
			Type:       AddonParameterTypeEnum,
			Required:   boolP(true),
			EnumValues: &enumValues,
		},
		{
			Name: "skip-threats",
			Type: AddonParameterTypeBoolean,
		},
		{
			Name: "comment",
			Type: AddonParameterTypeString,
		},
		{
			Name: "threshold",
			Type: AddonParameterTypeNumber,
		},
		{
			Name:        "tenancy",
			Type:        AddonParameterTypeMetadataKey,
			MetadataKey: strP("tenancy_ocid"),
		},
	}

	tests := []struct {
		name    string
		data    map[string]interface{}
		params  []AddonParameter
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil params - always valid",
			data:    map[string]interface{}{"anything": "goes"},
			params:  nil,
			wantErr: false,
		},
		{
			name:    "empty params - always valid",
			data:    map[string]interface{}{"anything": "goes"},
			params:  []AddonParameter{},
			wantErr: false,
		},
		{
			name: "valid data with all params",
			data: map[string]interface{}{
				"model":        "claude-3",
				"skip-threats": "true",
				"comment":      "test comment",
				"threshold":    float64(0.75),
				"tenancy":      "ocid1.tenancy.oc1..abc",
			},
			params:  params,
			wantErr: false,
		},
		{
			name: "valid - only required params",
			data: map[string]interface{}{
				"model": "gpt-4",
			},
			params:  params,
			wantErr: false,
		},
		{
			name: "valid - extra keys allowed",
			data: map[string]interface{}{
				"model":        "claude-3",
				"custom_field": "custom_value",
			},
			params:  params,
			wantErr: false,
		},
		{
			name:    "missing required parameter - nil data",
			data:    nil,
			params:  params,
			wantErr: true,
			errMsg:  "Required parameter 'model' is missing",
		},
		{
			name:    "missing required parameter - empty data",
			data:    map[string]interface{}{},
			params:  params,
			wantErr: true,
			errMsg:  "Required parameter 'model' is missing",
		},
		{
			name: "invalid enum value",
			data: map[string]interface{}{
				"model": "llama-2",
			},
			params:  params,
			wantErr: true,
			errMsg:  "not in allowed values",
		},
		{
			name: "enum - wrong type",
			data: map[string]interface{}{
				"model": 123,
			},
			params:  params,
			wantErr: true,
			errMsg:  "must be a string",
		},
		{
			name: "invalid boolean value",
			data: map[string]interface{}{
				"model":        "claude-3",
				"skip-threats": "yes",
			},
			params:  params,
			wantErr: true,
			errMsg:  "must be 'true' or 'false'",
		},
		{
			name: "boolean accepts true bool",
			data: map[string]interface{}{
				"model":        "claude-3",
				"skip-threats": true,
			},
			params:  params,
			wantErr: false,
		},
		{
			name: "boolean rejects non-bool/string",
			data: map[string]interface{}{
				"model":        "claude-3",
				"skip-threats": 42,
			},
			params:  params,
			wantErr: true,
			errMsg:  "must be a boolean",
		},
		{
			name: "string param rejects non-string",
			data: map[string]interface{}{
				"model":   "claude-3",
				"comment": 42,
			},
			params:  params,
			wantErr: true,
			errMsg:  "must be a string",
		},
		{
			name: "number param accepts float64",
			data: map[string]interface{}{
				"model":     "claude-3",
				"threshold": float64(0.5),
			},
			params:  params,
			wantErr: false,
		},
		{
			name: "number param accepts string number",
			data: map[string]interface{}{
				"model":     "claude-3",
				"threshold": "3.14",
			},
			params:  params,
			wantErr: false,
		},
		{
			name: "number param rejects non-numeric string",
			data: map[string]interface{}{
				"model":     "claude-3",
				"threshold": "abc",
			},
			params:  params,
			wantErr: true,
			errMsg:  "must be a valid number",
		},
		{
			name: "number param rejects bool",
			data: map[string]interface{}{
				"model":     "claude-3",
				"threshold": true,
			},
			params:  params,
			wantErr: true,
			errMsg:  "must be a number",
		},
		{
			name: "metadata_key param rejects non-string",
			data: map[string]interface{}{
				"model":   "claude-3",
				"tenancy": 42,
			},
			params:  params,
			wantErr: true,
			errMsg:  "must be a string",
		},
		{
			name: "no required params - nil data ok",
			data: nil,
			params: []AddonParameter{
				{Name: "optional", Type: AddonParameterTypeString},
			},
			wantErr: false,
		},
		// Constraint enforcement at invocation time
		{
			name: "number below min rejected",
			data: map[string]interface{}{
				"threshold": float64(5),
			},
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMin: float32P(10)},
			},
			wantErr: true,
			errMsg:  "below minimum",
		},
		{
			name: "number above max rejected",
			data: map[string]interface{}{
				"threshold": float64(150),
			},
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMax: float32P(100)},
			},
			wantErr: true,
			errMsg:  "exceeds maximum",
		},
		{
			name: "number within range accepted",
			data: map[string]interface{}{
				"threshold": float64(50),
			},
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMin: float32P(0), NumberMax: float32P(100)},
			},
			wantErr: false,
		},
		{
			name: "number string below min rejected",
			data: map[string]interface{}{
				"threshold": "-5",
			},
			params: []AddonParameter{
				{Name: "threshold", Type: AddonParameterTypeNumber, NumberMin: float32P(0)},
			},
			wantErr: true,
			errMsg:  "below minimum",
		},
		{
			name: "string exceeds max_length rejected",
			data: map[string]interface{}{
				"comment": "this is too long",
			},
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, StringMaxLength: intP(5)},
			},
			wantErr: true,
			errMsg:  "exceeds maximum length",
		},
		{
			name: "string within max_length accepted",
			data: map[string]interface{}{
				"comment": "ok",
			},
			params: []AddonParameter{
				{Name: "comment", Type: AddonParameterTypeString, StringMaxLength: intP(5)},
			},
			wantErr: false,
		},
		{
			name: "string not matching regex rejected",
			data: map[string]interface{}{
				"code": "abc-123",
			},
			params: []AddonParameter{
				{Name: "code", Type: AddonParameterTypeString, StringValidationRegex: strP("^[A-Z]{3}-[0-9]+$")},
			},
			wantErr: true,
			errMsg:  "does not match validation pattern",
		},
		{
			name: "string matching regex accepted",
			data: map[string]interface{}{
				"code": "ABC-123",
			},
			params: []AddonParameter{
				{Name: "code", Type: AddonParameterTypeString, StringValidationRegex: strP("^[A-Z]{3}-[0-9]+$")},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateInvocationData(tt.data, tt.params)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateInvocationData() error = %v, wantErr %v", err, tt.wantErr)
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
