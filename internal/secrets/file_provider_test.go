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
	_, err = p.GetSecret(context.Background(), "../etc/passwd")
	require.Error(t, err)
	keys, err := p.ListSecrets(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"settings_encryption_key"}, keys)
}
