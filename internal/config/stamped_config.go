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
type EmbeddingProfile struct {
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	Dimension int    `json:"dimension"`
}

// Validate returns an error if the profile is missing a required field.
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
type StampedConfig struct {
	Embedding EmbeddingProfile `json:"embedding"`
}

// StampedConfigProvider is the single read point for stamped configuration.
// Both the monolith's job-envelope builder and the monolith's own Timmy query
// path read through this interface, which is what makes the shared-invariant
// guarantee structural rather than a matter of discipline.
type StampedConfigProvider interface {
	Get(ctx context.Context) (StampedConfig, error)
}
