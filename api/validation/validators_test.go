package validation

import (
	"testing"
)

func TestValidateEnum(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		value   string
		allowed []string
		wantErr bool
	}{
		{"valid value", "type", "git", []string{"git", "svn"}, false},
		{"invalid value", "type", "cvs", []string{"git", "svn"}, true},
		{"empty value", "type", "", []string{"git", "svn"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEnum(tt.field, tt.value, tt.allowed)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEnum() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateThreatModelFramework(t *testing.T) {
	tests := []struct {
		name      string
		framework string
		wantErr   bool
	}{
		{"valid CIA", "CIA", false},
		{"valid STRIDE", "STRIDE", false},
		{"valid LINDDUN", "LINDDUN", false},
		{"valid DIE", "DIE", false},
		{"valid PLOT4ai", "PLOT4ai", false},
		{"invalid framework", "INVALID", true},
		{"empty framework", "", true},
		{"lowercase", "stride", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateThreatModelFramework(tt.framework)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateThreatModelFramework() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDiagramType(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid DFD", "DFD-1.0.0", false},
		{"invalid type", "UML", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDiagramType(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDiagramType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAssetType(t *testing.T) {
	validTypes := []string{"data", "hardware", "software", "infrastructure", "service", "personnel"}
	for _, vt := range validTypes {
		t.Run("valid_"+vt, func(t *testing.T) {
			if err := ValidateAssetType(vt); err != nil {
				t.Errorf("ValidateAssetType(%q) unexpected error: %v", vt, err)
			}
		})
	}

	t.Run("invalid type", func(t *testing.T) {
		if err := ValidateAssetType("invalid"); err == nil {
			t.Error("ValidateAssetType(invalid) expected error")
		}
	})
}

func TestValidateRole(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		wantErr bool
	}{
		{"valid owner", "owner", false},
		{"valid writer", "writer", false},
		{"valid reader", "reader", false},
		{"invalid role", "admin", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRole(tt.role)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRole() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMetadataKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr bool
	}{
		{"valid alphanumeric", "test_key123", false},
		{"valid with hyphen", "test-key", false},
		{"valid simple", "key", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"too long", string(make([]byte, 129)), true},
		{"invalid chars space", "test key", true},
		{"invalid chars special", "test@key", true},
		{"invalid chars dot", "test.key", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMetadataKey(tt.key)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMetadataKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMetadataValue(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"valid value", "some value", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"too long", string(make([]byte, 65536)), true},
		{"max length", string(make([]byte, 65535)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMetadataValue(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMetadataValue() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateScore(t *testing.T) {
	tests := []struct {
		name    string
		score   *float64
		wantErr bool
	}{
		{"nil score", nil, false},
		{"zero", new(0.0), false},
		{"mid range", new(5.5), false},
		{"max", new(10.0), false},
		{"below min", new(-0.1), true},
		{"above max", new(10.1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateScore(tt.score)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScore() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSubjectXOR(t *testing.T) {
	tests := []struct {
		name        string
		subjectType string
		userUUID    *string
		groupUUID   *string
		wantErr     bool
	}{
		{"valid user", "user", new("user-uuid"), nil, false},
		{"valid user with empty group", "user", new("user-uuid"), new(""), false},
		{"valid group", "group", nil, new("group-uuid"), false},
		{"valid group with empty user", "group", new(""), new("group-uuid"), false},
		{"user without uuid", "user", nil, nil, true},
		{"user with both uuids", "user", new("user-uuid"), new("group-uuid"), true},
		{"group without uuid", "group", nil, nil, true},
		{"group with both uuids", "group", new("user-uuid"), new("group-uuid"), true},
		{"invalid subject type", "invalid", new("uuid"), nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSubjectXOR(tt.subjectType, tt.userUUID, tt.groupUUID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSubjectXOR() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateNotEveryoneGroup(t *testing.T) {
	tests := []struct {
		name      string
		groupUUID string
		wantErr   bool
	}{
		{"regular group", "123e4567-e89b-12d3-a456-426614174000", false},
		{"everyone group", EveryonePseudoGroupUUID, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNotEveryoneGroup(tt.groupUUID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNotEveryoneGroup() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateStatusLength(t *testing.T) {
	tests := []struct {
		name    string
		status  *string
		wantErr bool
	}{
		{"nil status", nil, false},
		{"empty status", new(""), false},
		{"valid status", new("active"), false},
		{"max length", new(string(make([]byte, 128))), false},
		{"too long", new(string(make([]byte, 129))), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStatusLength(tt.status)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStatusLength() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	err := NewValidationError("field_name", "error message")
	expected := "field_name: error message"
	if err.Error() != expected {
		t.Errorf("ValidationError.Error() = %q, want %q", err.Error(), expected)
	}
}

// Helper functions
//
//go:fix inline
func strPtr(s string) *string {
	return new(s)
}

//go:fix inline
func floatPtr(f float64) *float64 {
	return new(f)
}
