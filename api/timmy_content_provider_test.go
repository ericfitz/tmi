package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDirectTextProvider_CanHandle(t *testing.T) {
	p := NewDirectTextProvider()

	// DB-resident entities without URIs
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "note", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "asset", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "threat", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "repository", EntityID: "123"}))

	// Entities with URIs should not be handled by DirectTextProvider
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", EntityID: "123", URI: "https://example.com/doc.pdf"}))

	// Diagrams are handled by the JSON provider, not direct text
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "diagram", EntityID: "123"}))
}
