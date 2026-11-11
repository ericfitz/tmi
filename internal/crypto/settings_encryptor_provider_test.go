package crypto

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/internal/secrets"
)

// mapProvider is a secrets.Provider over a map; keys absent from it are not found.
type mapProvider map[string]string

func (m mapProvider) GetSecret(_ context.Context, key string) (string, error) {
	if v, ok := m[key]; ok {
		return v, nil
	}
	return "", secrets.ErrSecretNotFound
}

func (m mapProvider) ListSecrets(context.Context) ([]string, error) { return nil, nil }

func (m mapProvider) Name() string { return "map" }

func (m mapProvider) Close() error { return nil }

// errProvider fails every lookup with a non-NotFound error.
type errProvider struct{ mapProvider }

func (errProvider) GetSecret(context.Context, string) (string, error) {
	return "", errors.New("backend down")
}

func TestNewSettingsEncryptor_ProviderPath(t *testing.T) {
	k1, k2 := hex.EncodeToString(generateTestKey(t)), hex.EncodeToString(generateTestKey(t))
	sk := secrets.SecretKeys
	tests := []struct {
		name         string
		p            secrets.Provider
		wantErr      string
		wantEnabled  bool
		wantPrev     bool
		wantContext  int
		wantPrevious int
	}{
		{name: "no key disables encryption", p: mapProvider{}},
		{name: "valid current key defaults id to 1", p: mapProvider{sk.SettingsEncryptionKey: k1}, wantEnabled: true, wantContext: 1},
		{name: "valid current id", p: mapProvider{sk.SettingsEncryptionKey: k1, sk.SettingsEncryptionContextID: "3"}, wantEnabled: true, wantContext: 3},
		{name: "invalid id falls back to 1", p: mapProvider{sk.SettingsEncryptionKey: k1, sk.SettingsEncryptionContextID: "zero"}, wantEnabled: true, wantContext: 1},
		{name: "valid previous pair", p: mapProvider{sk.SettingsEncryptionKey: k2, sk.SettingsEncryptionContextID: "2", sk.SettingsEncryptionPreviousKey: k1, sk.SettingsEncryptionPreviousContextID: "1"}, wantEnabled: true, wantPrev: true, wantContext: 2, wantPrevious: 1},
		{name: "previous key with invalid previous id still loads", p: mapProvider{sk.SettingsEncryptionKey: k2, sk.SettingsEncryptionPreviousKey: k1, sk.SettingsEncryptionPreviousContextID: "x"}, wantEnabled: true, wantPrev: true, wantContext: 1},
		{name: "orphan previous id without previous key is ignored", p: mapProvider{sk.SettingsEncryptionKey: k1, sk.SettingsEncryptionPreviousContextID: "1"}, wantEnabled: true, wantContext: 1},
		{name: "invalid current key", p: mapProvider{sk.SettingsEncryptionKey: "zz"}, wantErr: "invalid settings encryption key"},
		{name: "short current key", p: mapProvider{sk.SettingsEncryptionKey: "abcd"}, wantErr: "invalid settings encryption key"},
		{name: "invalid previous key", p: mapProvider{sk.SettingsEncryptionKey: k1, sk.SettingsEncryptionPreviousKey: "zz"}, wantErr: "invalid settings encryption previous key"},
		{name: "backend error", p: errProvider{}, wantErr: "failed to retrieve settings encryption key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, err := NewSettingsEncryptor(context.Background(), tt.p)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantEnabled, enc.IsEnabled())
			require.Equal(t, tt.wantPrev, enc.HasPreviousKey())
			if tt.wantEnabled {
				require.Equal(t, tt.wantContext, enc.GetContext().ContextID)
				require.Equal(t, tt.wantPrevious, enc.previousID)
			}
		})
	}
}
