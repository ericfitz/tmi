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
type FileProvider struct {
	dir string
}

func NewFileProvider(dir string) *FileProvider {
	return &FileProvider{dir: dir}
}

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

func (p *FileProvider) Name() string { return string(ProviderTypeFile) }

func (p *FileProvider) Close() error { return nil }
