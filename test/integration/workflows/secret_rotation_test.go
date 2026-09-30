package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ericfitz/tmi/internal/rotator"
	"github.com/ericfitz/tmi/test/integration/framework"
)

// apiLoop calls GET /me every 50ms until stop is closed, counting 5xx/401 responses.
func apiLoop(client *framework.IntegrationClient, stop <-chan struct{}) *int32 {
	var bad int32
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			resp, err := client.Do(framework.Request{Method: http.MethodGet, Path: "/me"})
			if err != nil || resp.StatusCode >= 500 || resp.StatusCode == http.StatusUnauthorized {
				atomic.AddInt32(&bad, 1)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return &bad
}

func rotationTestClient(t *testing.T, user string) *framework.IntegrationClient {
	t.Helper()
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
	var tokens *framework.OAuthTokens
	var err error
	if user == "" {
		tokens, err = framework.AuthenticateAdmin()
	} else {
		tokens, err = framework.AuthenticateUser(user)
	}
	framework.AssertNoError(t, err, "authenticate")
	client, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "client")
	return client
}

func TestSecretRotationIntegration_RedisPassword(t *testing.T) {
	client := rotationTestClient(t, "alice")

	opts, err := framework.RedisOptions()
	framework.AssertNoError(t, err, "redis options")
	rdb := redis.NewClient(opts)
	defer func() { _ = rdb.Close() }()
	oldPassword := opts.Password

	ctx := context.Background()
	st := rotator.NewMemorySecretStore(&rotator.Secret{Name: "tmi-secrets", Data: map[string]string{rotator.RedisPasswordKey: oldPassword}, Annotations: map[string]string{}})
	env := &rotator.Env{Secrets: st, Rollouts: rotator.NewFakeRolloutWaiter(st, "tmi-server"), SecretName: "tmi-secrets", ServerDeployment: "tmi-server", RolloutTimeout: time.Second, Now: time.Now}

	// withPassword returns an ACL handle authenticated with pw.
	withPassword := func(pw string) (*rotator.GoRedisACL, func()) {
		c := *opts
		c.Password = pw
		rc := redis.NewClient(&c)
		return rotator.NewGoRedisACL(rc), func() { _ = rc.Close() }
	}

	// Registered BEFORE the rotation: a failure mid-way must not leave the
	// harness Redis on a password the rest of the suite does not know. Also
	// removes the rotated-in password so the harness ends as it started.
	t.Cleanup(func() {
		s, err := st.Get(ctx, "tmi-secrets")
		if err != nil {
			return
		}
		if pw := s.Data[rotator.RedisPasswordKey]; pw != oldPassword {
			acl, closeFn := withPassword(pw)
			defer closeFn()
			_ = acl.AddPassword(ctx, oldPassword)
			_ = acl.RemovePasswordHash(ctx, sha256HexString(pw))
		}
	})

	stop := make(chan struct{})
	bad := apiLoop(client, stop)
	rot := rotator.NewRedisPasswordRotation(rotator.NewGoRedisACL(rdb))
	err = rot.Run(ctx, env)
	if err != nil {
		close(stop)
	}
	framework.AssertNoError(t, err, "rotation")

	s, err := st.Get(ctx, "tmi-secrets")
	framework.AssertNoError(t, err, "read rotated secret")
	newPassword := s.Data[rotator.RedisPasswordKey]
	framework.AssertNotEqual(t, oldPassword, newPassword, "password must change")

	newOpts := *opts
	newOpts.Password = newPassword
	newClient := redis.NewClient(&newOpts)
	newErr := newClient.Ping(ctx).Err()
	_ = newClient.Close()
	oldClient := redis.NewClient(opts)
	oldErr := oldClient.Ping(ctx).Err()
	_ = oldClient.Close()

	// Restore OLD inline (the Docker harness has no Reloader, so the running
	// server still holds OLD and would get WRONGPASS on new pooled connections).
	acl, closeFn := withPassword(newPassword)
	restoreErr := acl.AddPassword(ctx, oldPassword)
	closeFn()

	close(stop)
	if newErr != nil {
		t.Fatalf("new password rejected: %v", newErr)
	}
	if oldErr == nil {
		t.Fatal("old password still accepted after rotation")
	}
	framework.AssertNoError(t, restoreErr, "restore old password")
	if n := atomic.LoadInt32(bad); n != 0 {
		t.Fatalf("%d failed API calls during Redis password rotation", n)
	}

	resp, err := client.Do(framework.Request{Method: http.MethodGet, Path: "/me"})
	framework.AssertNoError(t, err, "post-rotation call")
	framework.AssertStatusOK(t, resp)
}

func TestSecretRotationIntegration_ReencryptUnderLoad(t *testing.T) {
	adminClient := rotationTestClient(t, "")
	userClient := rotationTestClient(t, "bob")

	stop := make(chan struct{})
	bad := apiLoop(userClient, stop)
	var last *framework.Response
	var err error
	for i := 0; i < 3; i++ { // idempotent: a second and third pass are no-ops
		last, err = adminClient.Do(framework.Request{Method: http.MethodPost, Path: "/admin/settings/reencrypt"})
		if err != nil {
			break
		}
		framework.AssertStatusOK(t, last)
	}
	close(stop)
	framework.AssertNoError(t, err, fmt.Sprintf("reencrypt (last status %v)", last != nil))
	if n := atomic.LoadInt32(bad); n != 0 {
		t.Fatalf("%d failed API calls during re-encryption", n)
	}
}

func sha256HexString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
