package workflows

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ericfitz/tmi/internal/rotator"
	"github.com/ericfitz/tmi/test/integration/framework"
)

// apiLoad hits GET /me every 50ms in the background and counts 5xx/401 responses.
type apiLoad struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
	bad  int32
}

// SEM@<sha>: start a background loop calling GET /me and counting server failures; stopped by t.Cleanup if not earlier
func startAPILoad(t *testing.T, client *framework.IntegrationClient) *apiLoad {
	l := &apiLoad{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(l.done)
		for {
			select {
			case <-l.stop:
				return
			default:
			}
			resp, err := client.Do(framework.Request{Method: http.MethodGet, Path: "/me"})
			if err != nil || resp.StatusCode >= 500 || resp.StatusCode == http.StatusUnauthorized {
				atomic.AddInt32(&l.bad, 1)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	t.Cleanup(func() { l.finish() }) // no goroutine leak on any t.Fatal path
	return l
}

// finish stops the loop, waits for it to exit, and returns the failure count.
// SEM@<sha>: stop the API load loop, wait for it to exit, and return the failed-call count (idempotent)
func (l *apiLoad) finish() int32 {
	l.once.Do(func() { close(l.stop) })
	<-l.done
	return atomic.LoadInt32(&l.bad)
}

// SEM@<sha>: authenticate a test user (admin when empty) and build an integration client; skip unless integration tests are enabled
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

// The Docker harness has no Reloader, so the running server never picks up the
// new password: it only sees the post-retire window (OLD rejected for new
// connections) until the inline restore below. This test therefore proves the
// rotation's Redis-side behaviour and that the server stays healthy through the
// add/swap, not a full server roll (Task 14 covers that on k3s).
// SEM@<sha>: verify Redis password rotation against the harness Redis while the server serves API traffic
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

	load := startAPILoad(t, client)
	rot := rotator.NewRedisPasswordRotation(rotator.NewGoRedisACL(rdb))
	err = rot.Run(ctx, env)
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

	badCalls := load.finish()
	if newErr != nil {
		t.Fatalf("new password rejected: %v", newErr)
	}
	if oldErr == nil {
		t.Fatal("old password still accepted after rotation")
	}
	framework.AssertNoError(t, restoreErr, "restore old password")
	if badCalls != 0 {
		t.Fatalf("%d failed API calls during Redis password rotation", badCalls)
	}

	resp, err := client.Do(framework.Request{Method: http.MethodGet, Path: "/me"})
	framework.AssertNoError(t, err, "post-rotation call")
	framework.AssertStatusOK(t, resp)
}

// SEM@<sha>: verify settings re-encryption converts a stale row once and is idempotent while the server serves API traffic
func TestSecretRotationIntegration_ReencryptUnderLoad(t *testing.T) {
	adminClient := rotationTestClient(t, "")
	userClient := rotationTestClient(t, "bob")

	// Seed one stale (plaintext) row straight into the DB so the first pass
	// has real work. The value is random and never logged.
	tdb, err := framework.NewTestDatabase()
	framework.AssertNoError(t, err, "test database")
	t.Cleanup(func() { _ = tdb.Close() }) // registered before the DELETE cleanup: LIFO runs DELETE first
	raw := make([]byte, 16)
	_, err = rand.Read(raw)
	framework.AssertNoError(t, err, "random value")
	const seedKey = "integration.rotation_test.secret_value"
	framework.AssertNoError(t, tdb.ExecSQL("DELETE FROM system_settings WHERE setting_key = '"+seedKey+"'"), "clear seed")
	framework.AssertNoError(t, tdb.ExecSQL("INSERT INTO system_settings (setting_key, value, setting_type, modified_at) VALUES ('"+seedKey+"', '"+hex.EncodeToString(raw)+"', 'string', CURRENT_TIMESTAMP)"), "seed stale setting")
	t.Cleanup(func() {
		if err := tdb.ExecSQL("DELETE FROM system_settings WHERE setting_key = '" + seedKey + "'"); err != nil {
			t.Errorf("remove seeded setting %s: %v", seedKey, err)
		}
	})

	load := startAPILoad(t, userClient)
	for i := 0; i < 3; i++ { // idempotent: later passes are no-ops
		resp, err := adminClient.Do(framework.Request{Method: http.MethodPost, Path: "/admin/settings/reencrypt"})
		framework.AssertNoError(t, err, fmt.Sprintf("reencrypt %d", i))
		framework.AssertStatusOK(t, resp)
		var body struct {
			Reencrypted int   `json:"reencrypted"`
			Errors      []any `json:"errors"`
		}
		framework.AssertNoError(t, json.Unmarshal(resp.Body, &body), "parse reencrypt response")
		if body.Errors == nil {
			t.Fatalf("pass %d: errors must be an array, got null", i)
		}
		if i == 0 && body.Reencrypted < 1 {
			t.Fatalf("first pass re-encrypted %d rows, want >= 1", body.Reencrypted)
		}
		if i > 0 && body.Reencrypted != 0 {
			t.Fatalf("pass %d re-encrypted %d rows, want 0", i, body.Reencrypted)
		}
	}
	if n := load.finish(); n != 0 {
		t.Fatalf("%d failed API calls during re-encryption", n)
	}
}

// SEM@<sha>: hex SHA-256 of a string (pure)
func sha256HexString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
