package workflows

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/test/integration/framework"
)

// mintCCToken exchanges a client credential for a service-account access token.
func mintCCToken(t *testing.T, serverURL, clientID, clientSecret string) string {
	t.Helper()
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	resp, err := http.Post(serverURL+"/oauth2/token",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	framework.AssertNoError(t, err, "POST /oauth2/token")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client_credentials grant: got %d: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	framework.AssertNoError(t, json.Unmarshal(body, &tok), "parse token response")
	return tok.AccessToken
}

// createAutomationAccount creates a bot user (member of tmi-automation) with
// its initial client credential, optionally flagged direct_write.
func createAutomationAccount(t *testing.T, admin *framework.IntegrationClient, name string, directWrite bool) (userUUID, provider, providerID, clientID, clientSecret string) {
	t.Helper()
	body := map[string]any{"name": name}
	if directWrite {
		body["direct_write"] = true
	}
	resp, err := admin.Do(framework.Request{Method: "POST", Path: "/admin/users/automation", Body: body})
	framework.AssertNoError(t, err, "POST /admin/users/automation")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create automation account: got %d: %s", resp.StatusCode, string(resp.Body))
	}
	var out struct {
		User struct {
			InternalUUID   string `json:"internal_uuid"`
			Provider       string `json:"provider"`
			ProviderUserID string `json:"provider_user_id"`
		} `json:"user"`
		ClientCredential struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			DirectWrite  *bool  `json:"direct_write"`
		} `json:"client_credential"`
	}
	framework.AssertNoError(t, json.Unmarshal(resp.Body, &out), "parse automation account response")
	if got := out.ClientCredential.DirectWrite; got == nil || *got != directWrite {
		t.Fatalf("client_credential.direct_write: got %v, want %t", got, directWrite)
	}
	t.Cleanup(func() {
		_, _ = admin.Do(framework.Request{Method: "DELETE", Path: "/admin/users/" + out.User.InternalUUID})
	})
	return out.User.InternalUUID, out.User.Provider, out.User.ProviderUserID, out.ClientCredential.ClientID, out.ClientCredential.ClientSecret
}

// TestClientCredentialsDirectWrite_Integration pins #856: a client_credentials
// token minted from a direct_write credential, whose (non-admin) owner holds
// writer on a threat model, can write sub-resources; the same setup without the
// flag is still rejected by the T18 invoker-only gate (#358); and an
// administrator cannot create a direct_write credential for themselves.
func TestClientCredentialsDirectWrite_Integration(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}
	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Admin authentication failed")
	admin, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create admin client")

	suffix := framework.UniqueUserID()
	dwUserUUID, dwProvider, dwProviderID, dwClientID, dwSecret := createAutomationAccount(t, admin, "dw-"+suffix, true)
	_, plainProvider, plainProviderID, plainClientID, plainSecret := createAutomationAccount(t, admin, "plain-"+suffix, false)

	// Threat model owned by the admin; both bots are writers on it.
	tmResp, err := admin.Do(framework.Request{
		Method: "POST",
		Path:   "/threat_models",
		Body: map[string]any{
			"name": "direct_write integration " + suffix,
			"authorization": []map[string]any{
				{"principal_type": "user", "provider": dwProvider, "provider_id": dwProviderID, "role": "writer"},
				{"principal_type": "user", "provider": plainProvider, "provider_id": plainProviderID, "role": "writer"},
			},
		},
	})
	framework.AssertNoError(t, err, "create threat model")
	if tmResp.StatusCode != http.StatusCreated {
		t.Fatalf("create threat model: got %d: %s", tmResp.StatusCode, string(tmResp.Body))
	}
	tmID := framework.ExtractID(t, tmResp, "id")
	t.Cleanup(func() {
		_, _ = admin.Do(framework.Request{Method: "DELETE", Path: "/threat_models/" + tmID})
	})

	dwToken := mintCCToken(t, serverURL, dwClientID, dwSecret)
	plainToken := mintCCToken(t, serverURL, plainClientID, plainSecret)

	postNote := func(t *testing.T, token string) *framework.Response {
		t.Helper()
		client, err := framework.NewClient(serverURL, &framework.OAuthTokens{AccessToken: token})
		framework.AssertNoError(t, err, "client for SA token")
		resp, err := client.Do(framework.Request{
			Method: "POST",
			Path:   "/threat_models/" + tmID + "/notes",
			Body:   map[string]any{"name": "from automation", "content": "written by a client_credentials token"},
		})
		framework.AssertNoError(t, err, "POST note")
		return resp
	}

	t.Run("DirectWriteTokenCanWriteSubResource", func(t *testing.T) {
		resp := postNote(t, dwToken)
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("direct_write SA write: got %d, want 201: %s", resp.StatusCode, string(resp.Body))
		}
		// Read back as the same token to prove reads still work too.
		client, _ := framework.NewClient(serverURL, &framework.OAuthTokens{AccessToken: dwToken})
		list, err := client.Do(framework.Request{Method: "GET", Path: "/threat_models/" + tmID + "/notes"})
		framework.AssertNoError(t, err, "GET notes")
		framework.AssertStatusOK(t, list)
	})

	t.Run("PlainTokenStillRejectedByT18", func(t *testing.T) {
		resp := postNote(t, plainToken)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("plain SA write: got %d, want 403 (T18 invoker-only): %s", resp.StatusCode, string(resp.Body))
		}
	})

	t.Run("DirectWriteDoesNotUnlockAdminRoutes", func(t *testing.T) {
		client, _ := framework.NewClient(serverURL, &framework.OAuthTokens{AccessToken: dwToken})
		resp, err := client.Do(framework.Request{Method: "GET", Path: "/admin/users"})
		framework.AssertNoError(t, err, "GET /admin/users")
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("direct_write SA on /admin/users: got %d, want 403: %s", resp.StatusCode, string(resp.Body))
		}
	})

	t.Run("AdministratorCannotCreateDirectWriteCredential", func(t *testing.T) {
		resp, err := admin.Do(framework.Request{
			Method: "POST",
			Path:   "/me/client_credentials",
			Body:   map[string]any{"name": "admin dw " + suffix, "direct_write": true},
		})
		framework.AssertNoError(t, err, "POST /me/client_credentials")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("admin direct_write credential: got %d, want 400: %s", resp.StatusCode, string(resp.Body))
		}
	})

	t.Run("ListShowsDirectWriteFlag", func(t *testing.T) {
		// The admin credential list exposes the flag (never the secret).
		resp, err := admin.Do(framework.Request{Method: "GET", Path: "/admin/users/" + dwUserUUID + "/client_credentials"})
		framework.AssertNoError(t, err, "GET admin client credentials")
		framework.AssertStatusOK(t, resp)
		var list struct {
			Credentials []struct {
				ClientID    string `json:"client_id"`
				DirectWrite *bool  `json:"direct_write"`
			} `json:"credentials"`
		}
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &list), "parse credential list")
		found := false
		for _, c := range list.Credentials {
			if c.ClientID == dwClientID {
				found = true
				if c.DirectWrite == nil || !*c.DirectWrite {
					t.Errorf("credential %s: direct_write not reported as true", c.ClientID)
				}
			}
		}
		if !found {
			t.Errorf("credential %s not in admin list", dwClientID)
		}
	})
}
