package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContextBuilder_BuildTier1Context(t *testing.T) {
	cb := NewContextBuilder()

	summaries := []EntitySummary{
		{EntityType: "asset", Name: "User Database", Description: "PostgreSQL database storing user credentials"},
		{EntityType: "asset", Name: "API Gateway", Description: "Public-facing API endpoint"},
		{EntityType: "threat", Name: "SQL Injection", Description: "Attacker injects SQL via user input"},
	}

	result := cb.BuildTier1Context(summaries)
	assert.Contains(t, result, "User Database")
	assert.Contains(t, result, "SQL Injection")
	assert.Contains(t, result, "### Assets")
	assert.Contains(t, result, "### Threats")
}

func TestContextBuilder_BuildTier1Context_Empty(t *testing.T) {
	cb := NewContextBuilder()
	result := cb.BuildTier1Context(nil)
	assert.Equal(t, "", result)
}

func TestContextBuilder_BuildTier2Context(t *testing.T) {
	cb := NewContextBuilder()
	idx := NewVectorIndex(3)
	idx.Add("chunk-1", []float32{1.0, 0.0, 0.0}, "Authentication uses JWT tokens with 1-hour expiry.")
	idx.Add("chunk-2", []float32{0.0, 1.0, 0.0}, "Database uses AES-256 encryption at rest.")

	result := cb.BuildTier2Context(idx, []float32{1.0, 0.0, 0.0}, 2)
	assert.Contains(t, result, "JWT tokens")
	assert.Contains(t, result, "Source 1")
	assert.Contains(t, result, "relevance:")
}

func TestContextBuilder_BuildTier2Context_NilIndex(t *testing.T) {
	cb := NewContextBuilder()
	result := cb.BuildTier2Context(nil, []float32{1.0, 0.0, 0.0}, 5)
	assert.Equal(t, "", result)
}

func TestContextBuilder_BuildFullContext(t *testing.T) {
	cb := NewContextBuilder()
	result := cb.BuildFullContext("You are Timmy.", "## Overview\nAssets...", "## Sources\nChunk 1...")
	assert.Contains(t, result, "You are Timmy.")
	assert.Contains(t, result, "## Overview")
	assert.Contains(t, result, "## Sources")
}

func TestContextBuilder_BuildFullContext_EmptyTiers(t *testing.T) {
	cb := NewContextBuilder()
	result := cb.BuildFullContext("Base prompt only.", "", "")
	assert.Equal(t, "Base prompt only.", result)
}
