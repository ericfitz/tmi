package api

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDenyListStore for testing
type mockDenyListStore struct {
	entries []WebhookUrlDenyListEntry
}

func (m *mockDenyListStore) List() ([]WebhookUrlDenyListEntry, error) {
	return m.entries, nil
}

func (m *mockDenyListStore) Create(item WebhookUrlDenyListEntry) (WebhookUrlDenyListEntry, error) {
	m.entries = append(m.entries, item)
	return item, nil
}

func (m *mockDenyListStore) Delete(id string) error {
	return nil
}

func TestWebhookUrlValidator_ValidateWebhookURL(t *testing.T) {
	// Create validator with empty deny list for basic tests
	store := &mockDenyListStore{entries: []WebhookUrlDenyListEntry{}}
	validator := NewWebhookUrlValidator(store)

	tests := []struct {
		name      string
		url       string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid https url",
			url:       "https://example.com/webhook",
			wantError: false,
		},
		{
			name:      "valid https url with port",
			url:       "https://example.com:8443/webhook",
			wantError: false,
		},
		{
			name:      "valid https url with subdomain",
			url:       "https://api.example.com/webhooks/events",
			wantError: false,
		},
		{
			name:      "valid https url case insensitive",
			url:       "HTTPS://example.com/webhook",
			wantError: false,
		},
		{
			name:      "http url rejected",
			url:       "http://example.com/webhook",
			wantError: true,
			errorMsg:  "must start with https://",
		},
		{
			name:      "ftp url rejected",
			url:       "ftp://example.com/webhook",
			wantError: true,
			errorMsg:  "must start with https://",
		},
		{
			name:      "url too short",
			url:       "https",
			wantError: true,
			errorMsg:  "too short",
		},
		{
			name:      "url without hostname",
			url:       "https:///webhook",
			wantError: true,
			errorMsg:  "must contain a valid hostname",
		},
		{
			name:      "hostname too long",
			url:       "https://" + stringRepeat("a", 250) + ".com/webhook",
			wantError: true,
			errorMsg:  "too long",
		},
		{
			name:      "valid international domain name",
			url:       "https://münchen.de/webhook",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateWebhookURL(tt.url)
			if tt.wantError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWebhookUrlValidator_DenyList(t *testing.T) {
	denyList := []WebhookUrlDenyListEntry{
		{Id: uuid.New(), Pattern: "localhost", PatternType: "glob", Description: "Block localhost"},
		{Id: uuid.New(), Pattern: "127.*", PatternType: "glob", Description: "Block loopback"},
		{Id: uuid.New(), Pattern: "10.*", PatternType: "glob", Description: "Block private 10.x"},
		{Id: uuid.New(), Pattern: "192.168.*", PatternType: "glob", Description: "Block private 192.168.x"},
		{Id: uuid.New(), Pattern: ".*\\.internal", PatternType: "regex", Description: "Block .internal domains"},
	}

	store := &mockDenyListStore{entries: denyList}
	validator := NewWebhookUrlValidator(store)

	tests := []struct {
		name      string
		url       string
		wantError bool
		errorMsg  string
	}{
		{
			name:      "localhost blocked",
			url:       "https://localhost/webhook",
			wantError: true,
			errorMsg:  "URL blocked",
		},
		{
			name:      "127.0.0.1 blocked",
			url:       "https://127.0.0.1/webhook",
			wantError: true,
			errorMsg:  "URL blocked",
		},
		{
			name:      "10.0.0.1 blocked",
			url:       "https://10.0.0.1/webhook",
			wantError: true,
			errorMsg:  "URL blocked",
		},
		{
			name:      "192.168.1.1 blocked",
			url:       "https://192.168.1.1/webhook",
			wantError: true,
			errorMsg:  "URL blocked",
		},
		{
			name:      ".internal domain blocked by regex",
			url:       "https://api.internal/webhook",
			wantError: true,
			errorMsg:  "URL blocked",
		},
		{
			name:      "valid external domain allowed",
			url:       "https://api.example.com/webhook",
			wantError: false,
		},
		{
			name:      "8.8.8.8 allowed (public IP)",
			url:       "https://8.8.8.8/webhook",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateWebhookURL(tt.url)
			if tt.wantError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWebhookUrlValidator_DNSLabelValidation(t *testing.T) {
	store := &mockDenyListStore{entries: []WebhookUrlDenyListEntry{}}
	validator := NewWebhookUrlValidator(store)

	tests := []struct {
		name      string
		url       string
		wantError bool
	}{
		{
			name:      "valid labels",
			url:       "https://api-v1.example.com/webhook",
			wantError: false,
		},
		{
			name:      "label starting with hyphen invalid",
			url:       "https://-api.example.com/webhook",
			wantError: true,
		},
		{
			name:      "label ending with hyphen invalid",
			url:       "https://api-.example.com/webhook",
			wantError: true,
		},
		{
			name:      "label too long (>63 chars)",
			url:       "https://" + stringRepeat("a", 64) + ".example.com/webhook",
			wantError: true,
		},
		{
			name:      "empty label invalid",
			url:       "https://example..com/webhook",
			wantError: true,
		},
		{
			name:      "numeric TLD allowed",
			url:       "https://example.123/webhook",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateWebhookURL(tt.url)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Helper function to repeat a string n times
func stringRepeat(s string, count int) string {
	var result strings.Builder
	for range count {
		result.WriteString(s)
	}
	return result.String()
}
