package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

var (
	cachedSpec     *openapi3.T
	specLoadMutex  sync.Mutex
	specLoadError  error
)

// LoadOpenAPISpec loads and caches the OpenAPI specification
// Returns cached spec on subsequent calls
func LoadOpenAPISpec() (*openapi3.T, error) {
	specLoadMutex.Lock()
	defer specLoadMutex.Unlock()

	// Return cached spec if available
	if cachedSpec != nil {
		return cachedSpec, nil
	}
	if specLoadError != nil {
		return nil, specLoadError
	}

	// Find project root (look for go.mod)
	projectRoot, err := findProjectRoot()
	if err != nil {
		specLoadError = fmt.Errorf("failed to find project root: %w", err)
		return nil, specLoadError
	}

	specPath := filepath.Join(projectRoot, "api-schema", "tmi-openapi.json")

	// Load OpenAPI spec
	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true

	spec, err := loader.LoadFromFile(specPath)
	if err != nil {
		specLoadError = fmt.Errorf("failed to load OpenAPI spec from %s: %w", specPath, err)
		return nil, specLoadError
	}

	// Validate spec
	if err := spec.Validate(loader.Context); err != nil {
		specLoadError = fmt.Errorf("OpenAPI spec validation failed: %w", err)
		return nil, specLoadError
	}

	cachedSpec = spec
	return cachedSpec, nil
}

// findProjectRoot walks up the directory tree looking for api-schema directory
// This finds the main TMI project root, not the integration test module root
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "api-schema", "tmi-openapi.json")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find project root (api-schema/tmi-openapi.json not found)")
		}
		dir = parent
	}
}

// ClearCache clears the cached OpenAPI spec (useful for testing)
func ClearCache() {
	specLoadMutex.Lock()
	defer specLoadMutex.Unlock()
	cachedSpec = nil
	specLoadError = nil
}
