package config

import (
	"context"
	"fmt"
)

// EmbeddingProfile is the shared, correctness-invariant embedding configuration.
// The same profile MUST be used to embed documents at ingest (by the
// tmi-chunk-embed worker) and to embed the user's query at search time (by the
// monolith). Disagreement makes vector search silently wrong.
//
// The API key is deliberately NOT part of this struct: it is a secret,
// resolved independently on each side from its own secret source, never
// carried in a job envelope.
// SEM@f0ba616243817f3864baaeb58b887479f81abb8e: shared embedding model, endpoint, and dimension config stamped into job envelopes (pure)
type EmbeddingProfile struct {
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	Dimension int    `json:"dimension"`
}

// Validate returns an error if the profile is missing a required field.
// SEM@f0ba616243817f3864baaeb58b887479f81abb8e: validate that an embedding profile has all required non-empty fields (pure)
func (p EmbeddingProfile) Validate() error {
	if p.Model == "" {
		return fmt.Errorf("embedding profile: model is required")
	}
	if p.Endpoint == "" {
		return fmt.Errorf("embedding profile: endpoint is required")
	}
	if p.Dimension <= 0 {
		return fmt.Errorf("embedding profile: dimension must be positive, got %d", p.Dimension)
	}
	return nil
}

// StampedConfig is the subset of operational configuration the monolith stamps
// into every job envelope. It carries only non-secret values; secrets are
// resolved from mounted secret sources by each consumer.
// SEM@f0ba616243817f3864baaeb58b887479f81abb8e: non-secret operational config the monolith stamps into every job envelope (pure)
type StampedConfig struct {
	Embedding EmbeddingProfile `json:"embedding"`
}

// Validate returns an error if the stamped configuration is incomplete.
// SEM@f0ba616243817f3864baaeb58b887479f81abb8e: validate that a stamped config contains a complete embedding profile (pure)
func (s StampedConfig) Validate() error {
	return s.Embedding.Validate()
}

// StampedConfigProvider is the single read point for stamped configuration.
// Both the monolith's job-envelope builder and the monolith's own Timmy query
// path read through this interface, which is what makes the shared-invariant
// guarantee structural rather than a matter of discipline.
// SEM@f0ba616243817f3864baaeb58b887479f81abb8e: interface for fetching the current stamped config used by job envelope builders and query paths
type StampedConfigProvider interface {
	Get(ctx context.Context) (StampedConfig, error)
}
