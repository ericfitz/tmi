package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileProvider_ReadsOneFilePerKey(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings_encryption_key"), []byte("abc\n"), 0o600))
	p := NewFileProvider(dir)
	v, err := p.GetSecret(context.Background(), "settings_encryption_key")
	require.NoError(t, err)
	require.Equal(t, "abc", v)
	_, err = p.GetSecret(context.Background(), "missing")
	require.True(t, errors.Is(err, ErrSecretNotFound))
	for _, k := range []string{"../etc/passwd", "a/b", "..", ".", ""} {
		_, err = p.GetSecret(context.Background(), k)
		require.ErrorIs(t, err, ErrInvalidConfig, k)
	}
	keys, err := p.ListSecrets(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"settings_encryption_key"}, keys)
}

func TestFileProvider_RejectsTraversalToRealFile(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "secrets")
	require.NoError(t, os.Mkdir(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(parent, "sibling"), []byte("leak"), 0o600))
	_, err := NewFileProvider(dir).GetSecret(context.Background(), "../sibling")
	require.ErrorIs(t, err, ErrInvalidConfig)
}
