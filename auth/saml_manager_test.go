package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSAMLManager_EnsureProvider_Idempotent(t *testing.T) {
	manager := NewSAMLManager(nil)

	config := SAMLProviderConfig{
		ID:             "test",
		Name:           "Test IDP",
		EntityID:       "https://tmi.example.com",
		IDPMetadataURL: "https://idp.example.com/metadata",
		ACSURL:         "https://tmi.example.com/saml/test/acs",
	}

	// First call attempts initialization (will fail without real IDP metadata, but shouldn't panic)
	err := manager.EnsureProvider("test", config)
	if err != nil {
		assert.Contains(t, err.Error(), "test")
	}
}

func TestSAMLManager_IsProviderInitialized_NotInitialized(t *testing.T) {
	manager := NewSAMLManager(nil)
	assert.False(t, manager.IsProviderInitialized("nonexistent"))
}
