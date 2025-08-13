package config

import (
	"testing"
)

func TestAdminConfig_IsUserAdmin(t *testing.T) {
	tests := []struct {
		name   string
		config AdminConfig
		email  string
		want   bool
	}{
		{
			name: "primary email is admin",
			config: AdminConfig{
				Users: AdminUsersConfig{
					PrimaryEmail: "admin@example.com",
				},
			},
			email: "admin@example.com",
			want:  true,
		},
		{
			name: "additional email is admin",
			config: AdminConfig{
				Users: AdminUsersConfig{
					PrimaryEmail:     "admin@example.com",
					AdditionalEmails: []string{"admin2@example.com", "admin3@example.com"},
				},
			},
			email: "admin2@example.com",
			want:  true,
		},
		{
			name: "non-admin email",
			config: AdminConfig{
				Users: AdminUsersConfig{
					PrimaryEmail:     "admin@example.com",
					AdditionalEmails: []string{"admin2@example.com"},
				},
			},
			email: "user@example.com",
			want:  false,
		},
		{
			name: "empty primary email",
			config: AdminConfig{
				Users: AdminUsersConfig{
					PrimaryEmail: "",
				},
			},
			email: "admin@example.com",
			want:  false,
		},
		{
			name: "case sensitivity",
			config: AdminConfig{
				Users: AdminUsersConfig{
					PrimaryEmail: "admin@example.com",
				},
			},
			email: "Admin@Example.com",
			want:  false, // Should be case sensitive
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Admin: tt.config}
			if got := cfg.IsUserAdmin(tt.email); got != tt.want {
				t.Errorf("Config.IsUserAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAdminConfig_Validation(t *testing.T) {
	tests := []struct {
		name    string
		config  AdminConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "admin disabled - no validation required",
			config: AdminConfig{
				Enabled: false,
				Users: AdminUsersConfig{
					PrimaryEmail: "", // Empty is ok when disabled
				},
			},
			wantErr: false,
		},
		{
			name: "admin enabled with valid primary email",
			config: AdminConfig{
				Enabled: true,
				Users: AdminUsersConfig{
					PrimaryEmail: "admin@example.com",
				},
				Session: AdminSessionConfig{
					TimeoutMinutes: 240,
				},
			},
			wantErr: false,
		},
		{
			name: "admin enabled without primary email",
			config: AdminConfig{
				Enabled: true,
				Users: AdminUsersConfig{
					PrimaryEmail: "",
				},
			},
			wantErr: true,
			errMsg:  "admin primary email is required when admin interface is enabled",
		},
		{
			name: "invalid primary email format",
			config: AdminConfig{
				Enabled: true,
				Users: AdminUsersConfig{
					PrimaryEmail: "invalid-email",
				},
				Session: AdminSessionConfig{
					TimeoutMinutes: 240,
				},
			},
			wantErr: true,
			errMsg:  "invalid primary admin email format",
		},
		{
			name: "invalid additional email format",
			config: AdminConfig{
				Enabled: true,
				Users: AdminUsersConfig{
					PrimaryEmail:     "admin@example.com",
					AdditionalEmails: []string{"valid@example.com", "invalid-email"},
				},
				Session: AdminSessionConfig{
					TimeoutMinutes: 240,
				},
			},
			wantErr: true,
			errMsg:  "invalid additional admin email format",
		},
		{
			name: "session timeout too low",
			config: AdminConfig{
				Enabled: true,
				Users: AdminUsersConfig{
					PrimaryEmail: "admin@example.com",
				},
				Session: AdminSessionConfig{
					TimeoutMinutes: 2, // Below minimum of 5
				},
			},
			wantErr: true,
			errMsg:  "admin session timeout must be at least 5 minutes",
		},
		{
			name: "invalid IP in allowlist",
			config: AdminConfig{
				Enabled: true,
				Users: AdminUsersConfig{
					PrimaryEmail: "admin@example.com",
				},
				Session: AdminSessionConfig{
					TimeoutMinutes: 240,
				},
				Security: AdminSecurityConfig{
					IPAllowlist: []string{"192.168.1.1", "invalid-ip"},
				},
			},
			wantErr: true,
			errMsg:  "invalid IP/CIDR in admin allowlist",
		},
		{
			name: "valid CIDR in allowlist",
			config: AdminConfig{
				Enabled: true,
				Users: AdminUsersConfig{
					PrimaryEmail: "admin@example.com",
				},
				Session: AdminSessionConfig{
					TimeoutMinutes: 240,
				},
				Security: AdminSecurityConfig{
					IPAllowlist: []string{"192.168.1.0/24", "10.0.0.0/8"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Admin: tt.config}
			err := cfg.validateAdminConfig()

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateAdminConfig() expected error but got none")
				} else if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateAdminConfig() error = %v, want error containing %v", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateAdminConfig() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		email string
		want  bool
	}{
		{"admin@example.com", true},
		{"test.email+tag@domain.co.uk", true},
		{"user123@test-domain.com", true},
		{"invalid-email", false},
		{"@domain.com", false},
		{"user@", false},
		{"user@domain", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			if got := isValidEmail(tt.email); got != tt.want {
				t.Errorf("isValidEmail(%s) = %v, want %v", tt.email, got, tt.want)
			}
		})
	}
}

func TestIsValidIPOrCIDR(t *testing.T) {
	tests := []struct {
		ipStr string
		want  bool
	}{
		{"192.168.1.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"127.0.0.1", true},
		{"192.168.1.0/24", true},
		{"10.0.0.0/8", true},
		{"172.16.0.0/12", true},
		{"invalid-ip", false},
		{"999.999.999.999", false},
		{"192.168.1.0/33", false}, // Invalid CIDR
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.ipStr, func(t *testing.T) {
			if got := isValidIPOrCIDR(tt.ipStr); got != tt.want {
				t.Errorf("isValidIPOrCIDR(%s) = %v, want %v", tt.ipStr, got, tt.want)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || (len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
