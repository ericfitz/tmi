package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/config"
)

// settingsReader is the minimal read surface NewStampedConfigProvider needs.
// *SettingsService satisfies it.
// SEM@07385154fa2286de1a8805dbf00575c0f52ce941: interface for reading typed settings values by key from a backing store
type settingsReader interface {
	GetString(ctx context.Context, key string) (string, error)
	GetInt(ctx context.Context, key string) (int, error)
}

// stampedConfigProvider reads stamped configuration from the DB-backed
// settings service. It is the concrete config.StampedConfigProvider.
// SEM@07385154fa2286de1a8805dbf00575c0f52ce941: DB-backed config.StampedConfigProvider that reads embedding settings via settingsReader
type stampedConfigProvider struct {
	settings settingsReader
}

// NewStampedConfigProvider builds a config.StampedConfigProvider that reads
// through the given settings reader (normally *SettingsService).
// SEM@07385154fa2286de1a8805dbf00575c0f52ce941: build a StampedConfigProvider backed by the given settings reader (pure)
func NewStampedConfigProvider(settings settingsReader) config.StampedConfigProvider {
	return &stampedConfigProvider{settings: settings}
}

// Get assembles the current StampedConfig from the settings service. It is the
// single read point for stamped configuration in the monolith.
// SEM@07385154fa2286de1a8805dbf00575c0f52ce941: fetch current stamped embedding configuration from the settings service (reads DB)
func (p *stampedConfigProvider) Get(ctx context.Context) (config.StampedConfig, error) {
	model, err := p.settings.GetString(ctx, "timmy.text_embedding_model")
	if err != nil {
		return config.StampedConfig{}, fmt.Errorf("stamped config: read embedding model: %w", err)
	}
	endpoint, err := p.settings.GetString(ctx, "timmy.text_embedding_base_url")
	if err != nil {
		return config.StampedConfig{}, fmt.Errorf("stamped config: read embedding endpoint: %w", err)
	}
	dim, err := p.settings.GetInt(ctx, "timmy.embedding_dimension")
	if err != nil {
		return config.StampedConfig{}, fmt.Errorf("stamped config: read embedding dimension: %w", err)
	}
	return config.StampedConfig{
		Embedding: config.EmbeddingProfile{
			Model:     model,
			Endpoint:  endpoint,
			Dimension: dim,
		},
	}, nil
}

// Compile-time assertion that *SettingsService satisfies settingsReader.
var _ settingsReader = (*SettingsService)(nil)
