package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"gopkg.in/yaml.v3"
)

// stripOperationalKeys rewrites the YAML file at path in place, removing
// every leaf key whose classification is CategoryOperational. The resulting
// file is a valid input for config.Load() containing only bootstrap keys.
//
// Types are preserved verbatim — numbers stay numbers, strings stay
// strings, no password redaction is applied. Comments and key ordering on
// surviving keys are preserved by virtue of operating on the yaml.Node
// tree rather than round-tripping through map[string]any.
//
// Returns the size of the rewritten file in bytes for the operator log.
// SEM@e7880ae29f527fb2d814f6d7b7c13280082fa033: rewrite a YAML config file in place, removing all operational-category keys (reads DB)
func stripOperationalKeys(path string) (int64, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied config path
	if err != nil {
		return 0, fmt.Errorf("read config: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return 0, fmt.Errorf("parse config: %w", err)
	}

	// The document node wraps a mapping at the top level.
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return 0, fmt.Errorf("config file %s is not a YAML document", path)
	}
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return 0, fmt.Errorf("config file %s top-level is not a mapping", path)
	}

	pruneOperationalFromMapping(top, "")

	out, err := yaml.Marshal(&root)
	if err != nil {
		return 0, fmt.Errorf("marshal stripped config: %w", err)
	}

	if err := os.WriteFile(path, out, 0o600); err != nil {
		return 0, fmt.Errorf("write stripped config: %w", err)
	}
	return int64(len(out)), nil
}

// pruneOperationalFromMapping walks a YAML mapping node and removes every
// (key, value) pair whose dotted path is classified as CategoryOperational.
// Sub-mappings are recursed; if a sub-mapping becomes empty after pruning,
// the parent pair is also removed.
//
// The mapping node's Content alternates: [key0, val0, key1, val1, ...].
// SEM@e7880ae29f527fb2d814f6d7b7c13280082fa033: recursively remove operational-category entries from a YAML mapping node (pure)
func pruneOperationalFromMapping(m *yaml.Node, prefix string) {
	if m.Kind != yaml.MappingNode {
		return
	}
	kept := make([]*yaml.Node, 0, len(m.Content))
	for i := 0; i+1 < len(m.Content); i += 2 {
		key := m.Content[i]
		val := m.Content[i+1]
		dotted := key.Value
		if prefix != "" {
			dotted = prefix + "." + key.Value
		}

		switch val.Kind {
		case yaml.MappingNode:
			pruneOperationalFromMapping(val, dotted)
			// Drop now-empty sub-mappings so the file doesn't accumulate
			// empty parent stubs (e.g. `auth: {}`).
			if len(val.Content) == 0 {
				continue
			}
			kept = append(kept, key, val)
		default:
			if config.ClassificationCategoryFor(dotted) == config.CategoryOperational {
				continue
			}
			kept = append(kept, key, val)
		}
	}
	m.Content = kept
}

// backupConfigFile writes a timestamped copy of path alongside it, returning
// the backup path. Format: <path>.<YYYYMMDD-HHMMSS>.bak.
// SEM@e7880ae29f527fb2d814f6d7b7c13280082fa033: write a timestamped backup copy of a config file alongside the original (reads DB)
func backupConfigFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied config path
	if err != nil {
		return "", fmt.Errorf("read for backup: %w", err)
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	bak := path + "." + stamp + ".bak"
	// Refuse to clobber an existing backup at the exact same timestamp —
	// vanishingly unlikely outside test races, but worth guarding.
	if _, err := os.Stat(bak); err == nil {
		return "", fmt.Errorf("backup path %s already exists", bak)
	}
	if err := os.WriteFile(bak, data, 0o600); err != nil { //nolint:gosec // backup path derived from operator-supplied config path
		return "", fmt.Errorf("write backup: %w", err)
	}
	return bak, nil
}

// looksLikeYAMLPath returns true when the given path has a YAML extension.
// The in-place rewrite path is only safe for YAML; the legacy --output
// fallback handles JSON-extension inputs.
// SEM@e7880ae29f527fb2d814f6d7b7c13280082fa033: report whether a file path has a YAML extension (pure)
func looksLikeYAMLPath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".yml") || strings.HasSuffix(lower, ".yaml")
}
