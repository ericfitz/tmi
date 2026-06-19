// Package main implements the tmi-dbtool CLI for TMI database administration.
// It supports schema migration, config import, and test data import.
package main

import "time"

// ToolInfo holds build-time metadata for the startup banner.
// SEM@e93cc27eac1d842461899300fefcaebc977cb3db: build-time metadata displayed in the dbtool startup banner (pure)
type ToolInfo struct {
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuiltAt      string `json:"built_at"`
	SchemaModels int    `json:"schema_models"`
}

// ExitSummary is the JSON structure printed at exit.
// SEM@e93cc27eac1d842461899300fefcaebc977cb3db: structured JSON summary emitted by dbtool at exit (pure)
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
// SEM@6a2dbeba0bdf487e7efc952c5a0d6f23584d2e03: top-level envelope for a seed data file with format version and ordered seed entries (pure)
type SeedFile struct {
	FormatVersion string      `json:"format_version" yaml:"format_version"`
	Description   string      `json:"description" yaml:"description"`
	CreatedAt     time.Time   `json:"created_at" yaml:"created_at"`
	Output        *SeedOutput `json:"output,omitempty" yaml:"output,omitempty"`
	Seeds         []SeedEntry `json:"seeds" yaml:"seeds"`
}

// SeedOutput configures reference file generation after seeding.
// SEM@6a2dbeba0bdf487e7efc952c5a0d6f23584d2e03: optional seed run config controlling reference file output paths (pure)
type SeedOutput struct {
	ReferenceFile string `json:"reference_file,omitempty" yaml:"reference_file,omitempty"`
	ReferenceYAML string `json:"reference_yaml,omitempty" yaml:"reference_yaml,omitempty"`
}

// SeedEntry is a single entity to seed.
// SEM@6a2dbeba0bdf487e7efc952c5a0d6f23584d2e03: single entity descriptor in a seed data file (pure)
type SeedEntry struct {
	Kind string         `json:"kind" yaml:"kind"`
	Ref  string         `json:"ref,omitempty" yaml:"ref,omitempty"`
	Data map[string]any `json:"data" yaml:"data"`
}

// SeedResult tracks the result of seeding a single entry.
// SEM@6a2dbeba0bdf487e7efc952c5a0d6f23584d2e03: outcome of seeding a single entry, including created ID and extra fields (pure)
type SeedResult struct {
	Ref  string
	Kind string
	ID   string
	// Extra holds additional fields needed for reference file generation
	// (e.g., threat_model_id for child resources, provider info for users).
	Extra map[string]string
}

// RefMap tracks ref names to their created IDs for cross-referencing.
// SEM@6a2dbeba0bdf487e7efc952c5a0d6f23584d2e03: map of seed ref names to their created SeedResult for cross-reference resolution (pure)
type RefMap map[string]*SeedResult
