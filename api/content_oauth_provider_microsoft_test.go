package api

import (
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestNewMicrosoftContentOAuthProvider_ID(t *testing.T) {
	cfg := config.ContentOAuthProviderConfig{
		Enabled:        true,
		ClientID:       "client",
		AuthURL:        "https://login.microsoftonline.com/contoso/oauth2/v2.0/authorize",
		TokenURL:       "https://login.microsoftonline.com/contoso/oauth2/v2.0/token",
		UserinfoURL:    "https://graph.microsoft.com/v1.0/me",
		RequiredScopes: []string{"Files.SelectedOperations.Selected", "Files.ReadWrite", "offline_access", "User.Read"},
	}
	p := NewMicrosoftContentOAuthProvider(NewBaseContentOAuthProvider(ProviderMicrosoft, cfg))
	assert.Equal(t, ProviderMicrosoft, p.ID())
	assert.Contains(t, p.RequiredScopes(), "Files.SelectedOperations.Selected")
	assert.Contains(t, p.RequiredScopes(), "Files.ReadWrite")
	assert.Contains(t, p.RequiredScopes(), "offline_access")
}
