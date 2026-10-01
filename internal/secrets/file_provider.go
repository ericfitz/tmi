package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileProvider reads one secret per file from a directory, the shape of a
// Kubernetes Secret volume or a umask-077 staging directory.
// SEM@3b682947: secrets provider that reads each secret from <dir>/<key> (reads files)
type FileProvider struct {
	dir string
}

// SEM@3b682947: build a directory-backed secrets provider (pure)
func NewFileProvider(dir string) *FileProvider {
	return &FileProvider{dir: dir}
}

// SEM@3b682947: fetch a secret from the file named after its key, rejecting path traversal (reads files)
func (p *FileProvider) GetSecret(_ context.Context, key string) (string, error) {
	if key == "" || key == "." || key == ".." || key != filepath.Base(key) {
		return "", fmt.Errorf("%w: invalid secret key", ErrInvalidConfig)
	}
	data, err := os.ReadFile(filepath.Join(p.dir, key)) // #nosec G304 -- key is a validated single path element
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrSecretNotFound
		}
		return "", fmt.Errorf("failed to read secret file: %w", err)
	}
	return strings.TrimSuffix(string(data), "\n"), nil
}

// SEM@3b682947: list secret keys as the regular file names in the directory (reads files)
func (p *FileProvider) ListSecrets(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		return nil, fmt.Errorf("failed to list secrets directory: %w", err)
	}
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Type().IsRegular() {
			keys = append(keys, e.Name())
		}
	}
	return keys, nil
}

// SEM@3b682947: return the provider type name (pure)
func (p *FileProvider) Name() string { return string(ProviderTypeFile) }

// SEM@3b682947: no-op close of the secrets provider (pure)
func (p *FileProvider) Close() error { return nil }
