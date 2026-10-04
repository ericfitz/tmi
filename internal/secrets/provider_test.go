package secrets

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/internal/config"
)

// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: test that the secrets provider is selected from config and validated
func TestNewProvider(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "k"), []byte("v\n"), 0o600))
	tests := []struct {
		name     string
		cfg      *config.SecretsConfig
		wantName string
		wantErr  error
	}{
		{name: "nil config defaults to env", cfg: nil, wantName: "env"},
		{name: "empty provider defaults to env", cfg: &config.SecretsConfig{}, wantName: "env"},
		{name: "env", cfg: &config.SecretsConfig{Provider: "env"}, wantName: "env"},
		{name: "file", cfg: &config.SecretsConfig{Provider: "file", FileDir: dir}, wantName: "file"},
		{name: "file without dir", cfg: &config.SecretsConfig{Provider: "file"}, wantErr: ErrInvalidConfig},
		{name: "aws without region", cfg: &config.SecretsConfig{Provider: "aws"}, wantErr: ErrInvalidConfig},
		{name: "vault not implemented", cfg: &config.SecretsConfig{Provider: "vault"}, wantErr: ErrProviderNotEnabled},
		{name: "unknown", cfg: &config.SecretsConfig{Provider: "nope"}, wantErr: ErrInvalidConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewProvider(context.Background(), tt.cfg)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantName, p.Name())
			if tt.wantName == "file" {
				v, err := p.GetSecret(context.Background(), "k")
				require.NoError(t, err)
				require.Equal(t, "v", v)
			}
		})
	}
}
