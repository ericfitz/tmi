// Package main implements the tmi-dbtool CLI for TMI database administration.
// It supports schema migration, config import, and test data import.
package main

import "time"

// ToolInfo holds build-time metadata for the startup banner.
type ToolInfo struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuiltAt      string `json:"built_at"`
	SchemaModels int    `json:"schema_models"`
}

// ExitSummary is the JSON structure printed at exit.
type ExitSummary struct {
	Tool         string         `json:"tool"`
	Version      string         `json:"version"`
	Commit       string         `json:"commit"`
	BuiltAt      string         `json:"built_at"`
	SchemaModels int            `json:"schema_models"`
	Arguments    map[string]any `json:"arguments"`
	Status       string         `json:"status"`
	Error        string         `json:"error"`
}

// SeedFile is the top-level envelope for a seed data file.
type SeedFile struct {
	FormatVersion string      `json:"format_version" yaml:"format_version"`
	Description   string      `json:"description" yaml:"description"`
	CreatedAt     time.Time   `json:"created_at" yaml:"created_at"`
	Output        *SeedOutput `json:"output,omitempty" yaml:"output,omitempty"`
	Seeds         []SeedEntry `json:"seeds" yaml:"seeds"`
}

// SeedOutput configures reference file generation after seeding.
type SeedOutput struct {
	ReferenceFile string `json:"reference_file,omitempty" yaml:"reference_file,omitempty"`
	ReferenceYAML string `json:"reference_yaml,omitempty" yaml:"reference_yaml,omitempty"`
}

// SeedEntry is a single entity to seed.
type SeedEntry struct {
	Kind string         `json:"kind" yaml:"kind"`
	Ref  string         `json:"ref,omitempty" yaml:"ref,omitempty"`
	Data map[string]any `json:"data" yaml:"data"`
}

// SeedResult tracks the result of seeding a single entry.
type SeedResult struct {
	Ref  string
	Kind string
	ID   string
	// Extra holds additional fields needed for reference file generation
	// (e.g., threat_model_id for child resources, provider info for users).
	Extra map[string]string
}

// RefMap tracks ref names to their created IDs for cross-referencing.
type RefMap map[string]*SeedResult
