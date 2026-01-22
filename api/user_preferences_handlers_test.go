package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidatePreferences(t *testing.T) {
	t.Run("ValidPreferences", func(t *testing.T) {
		valid := []byte(`{
			"tmi-ux": {"theme": "dark", "locale": "en-US"},
			"tmi-cli": {"output": "json"}
		}`)
		err := validatePreferences(valid)
		assert.NoError(t, err)
	})

	t.Run("EmptyPreferences", func(t *testing.T) {
		valid := []byte(`{}`)
		err := validatePreferences(valid)
		assert.NoError(t, err)
	})

	t.Run("ValidClientKeys", func(t *testing.T) {
		// Test various valid client key patterns
		testCases := []string{
			`{"abc": {}}`,
			`{"ABC": {}}`,
			`{"abc-123": {}}`,
			`{"abc_123": {}}`,
			`{"tmi-ux": {}}`,
			`{"my_client-app": {}}`,
		}
		for _, tc := range testCases {
			err := validatePreferences([]byte(tc))
			assert.NoError(t, err, "Expected valid for: %s", tc)
		}
	})

	t.Run("InvalidClientKeys", func(t *testing.T) {
		// Test various invalid client key patterns
		testCases := []string{
			`{"invalid.key": {}}`, // dots not allowed
			`{"invalid key": {}}`, // spaces not allowed
			`{"invalid/key": {}}`, // slashes not allowed
			`{"": {}}`,            // empty key not allowed
			`{"@invalid": {}}`,    // special chars not allowed
		}
		for _, tc := range testCases {
			err := validatePreferences([]byte(tc))
			assert.Error(t, err, "Expected error for: %s", tc)
		}
	})

	t.Run("ClientKeyTooLong", func(t *testing.T) {
		// Client key must be 1-64 chars
		longKey := strings.Repeat("a", 65)
		invalid := []byte(`{"` + longKey + `": {}}`)
		err := validatePreferences(invalid)
		assert.Error(t, err)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		invalid := []byte(`{invalid json`)
		err := validatePreferences(invalid)
		assert.Error(t, err)
	})

	t.Run("ExceedsSizeLimit", func(t *testing.T) {
		// Create payload larger than 1KB (1024 bytes)
		largeValue := strings.Repeat("x", 2000)
		invalid := []byte(`{"tmi-ux": {"data": "` + largeValue + `"}}`)
		err := validatePreferences(invalid)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "1KB limit")
	})

	t.Run("TooManyClients", func(t *testing.T) {
		// Build preferences with 21 clients (max is 20)
		var clients []string
		for i := 0; i <= 20; i++ {
			clients = append(clients, `"client`+string(rune('a'+i))+`": {}`)
		}
		invalid := []byte(`{` + strings.Join(clients, ",") + `}`)
		err := validatePreferences(invalid)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "maximum 20")
	})

	t.Run("MaximumClientsAllowed", func(t *testing.T) {
		// Build preferences with exactly 20 clients (at the limit)
		var clients []string
		for i := 0; i < 20; i++ {
			clients = append(clients, `"c`+string(rune('a'+i))+`": {}`)
		}
		valid := []byte(`{` + strings.Join(clients, ",") + `}`)
		err := validatePreferences(valid)
		assert.NoError(t, err)
	})
}
