package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeVaultResolver is a test double for SecretResolver.
type fakeVaultResolver struct {
	values map[string]string
	err    error
}

func (f *fakeVaultResolver) ResolveVault(_ context.Context, path string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.values[path]
	if !ok {
		return "", os.ErrNotExist
	}
	return v, nil
}

func TestIsSecretReference(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"vault://kv/data/jwt", true},
		{"env://TMI_JWT_SECRET", true},
		{"file:///etc/tmi/jwt", true},
		{"plain-secret-string", false},
		{"postgres://user:pass@host:5432/db", false},
		{"", false},
		{"http://example.com", false},
	}
	for _, tc := range cases {
		if got := IsSecretReference(tc.value); got != tc.want {
			t.Errorf("IsSecretReference(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestResolveSecretValue_Inline(t *testing.T) {
	ctx := context.Background()
	got, err := ResolveSecretValue(ctx, "a-plain-jwt-secret", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "a-plain-jwt-secret" {
		t.Errorf("got %q, want inline pass-through", got)
	}

	// A postgres URL is inline even though it contains "://".
	got, err = ResolveSecretValue(ctx, "postgres://u:p@h:5432/db", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "postgres://u:p@h:5432/db" {
		t.Errorf("got %q, want postgres URL unchanged", got)
	}
}

func TestResolveSecretValue_Env(t *testing.T) {
	ctx := context.Background()

	t.Setenv("TMI_TEST_SECRET_REF", "resolved-from-env")
	got, err := ResolveSecretValue(ctx, "env://TMI_TEST_SECRET_REF", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "resolved-from-env" {
		t.Errorf("got %q, want resolved-from-env", got)
	}

	// Missing / empty env var is an error.
	if _, err := ResolveSecretValue(ctx, "env://TMI_TEST_MISSING_VAR_XYZ", nil); err == nil {
		t.Error("expected error for missing env var, got nil")
	}
}

func TestResolveSecretValue_File(t *testing.T) {
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(path, []byte("  file-secret-value\n"), 0o600); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	got, err := ResolveSecretValue(ctx, "file://"+path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "file-secret-value" {
		t.Errorf("got %q, want trimmed file contents", got)
	}

	// Missing file is an error.
	if _, err := ResolveSecretValue(ctx, "file://"+filepath.Join(dir, "nope.txt"), nil); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestResolveSecretValue_Vault(t *testing.T) {
	ctx := context.Background()
	vault := &fakeVaultResolver{values: map[string]string{
		"kv/data/jwt": "vault-resolved-jwt",
	}}

	got, err := ResolveSecretValue(ctx, "vault://kv/data/jwt", vault)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "vault-resolved-jwt" {
		t.Errorf("got %q, want vault-resolved-jwt", got)
	}

	// vault:// with a nil resolver is an error.
	_, err = ResolveSecretValue(ctx, "vault://kv/data/jwt", nil)
	if err == nil {
		t.Error("expected error for vault:// reference with nil resolver, got nil")
	}
	if !strings.Contains(err.Error(), "secrets provider") {
		t.Errorf("error %q should mention secrets provider", err)
	}

	// Vault path the provider does not know is an error.
	if _, err := ResolveSecretValue(ctx, "vault://kv/data/unknown", vault); err == nil {
		t.Error("expected error for unknown vault path, got nil")
	}
}

func TestResolveSecretValue_UnrecognizedSchemeIsInline(t *testing.T) {
	ctx := context.Background()
	// Only vault://, env://, and file:// are reference schemes. Any other
	// "scheme://" value is an inline literal returned unchanged — a connection
	// URL such as "redis://" must not be misinterpreted as a reference.
	for _, v := range []string{"s3://bucket/key", "redis://:p@h:6379/0"} {
		got, err := ResolveSecretValue(ctx, v, nil)
		if err != nil {
			t.Errorf("ResolveSecretValue(%q) errored: %v", v, err)
		}
		if got != v {
			t.Errorf("ResolveSecretValue(%q) = %q, want unchanged", v, got)
		}
	}
}
