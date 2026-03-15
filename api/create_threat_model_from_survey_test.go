package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCollectionAnswer_Repositories(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "frontend", "uri": "https://github.com/org/frontend"},
		{"name": "backend", "uri": "https://github.com/org/backend"}
	]`)
	repos, fallback := parseCollectionAnswer("repositories", answer)
	assert.Len(t, repos, 2)
	assert.Empty(t, fallback)

	repo0 := repos[0].(Repository)
	assert.Equal(t, "frontend", *repo0.Name)
	assert.Equal(t, "https://github.com/org/frontend", repo0.Uri)

	repo1 := repos[1].(Repository)
	assert.Equal(t, "backend", *repo1.Name)
	assert.Equal(t, "https://github.com/org/backend", repo1.Uri)
}

func TestParseCollectionAnswer_Documents(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "Design Doc", "uri": "https://docs.example.com/design"}
	]`)
	docs, fallback := parseCollectionAnswer("documents", answer)
	assert.Len(t, docs, 1)
	assert.Empty(t, fallback)

	doc := docs[0].(Document)
	assert.Equal(t, "Design Doc", doc.Name)
	assert.Equal(t, "https://docs.example.com/design", doc.Uri)
}

func TestParseCollectionAnswer_Assets(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "Database", "type": "data-store", "description": "Main PostgreSQL DB"}
	]`)
	assets, fallback := parseCollectionAnswer("assets", answer)
	assert.Len(t, assets, 1)
	assert.Empty(t, fallback)

	asset := assets[0].(Asset)
	assert.Equal(t, "Database", asset.Name)
	assert.Equal(t, AssetType("data-store"), asset.Type)
	assert.Equal(t, "Main PostgreSQL DB", *asset.Description)
}

func TestParseCollectionAnswer_IncompleteObject(t *testing.T) {
	answer := json.RawMessage(`[
		{"name": "frontend"},
		{"name": "backend", "uri": "https://github.com/org/backend"}
	]`)
	repos, fallback := parseCollectionAnswer("repositories", answer)
	assert.Len(t, repos, 1)
	assert.Len(t, fallback, 1)

	assert.Equal(t, "repositories.name", fallback[0].Key)
	assert.Equal(t, "frontend", fallback[0].Value)
}

func TestParseCollectionAnswer_UnrecognizedCollection(t *testing.T) {
	answer := json.RawMessage(`[{"name": "test"}]`)
	items, fallback := parseCollectionAnswer("unknowns", answer)
	assert.Empty(t, items)
	assert.Len(t, fallback, 1)
}

func TestParseCollectionAnswer_InvalidJSON(t *testing.T) {
	answer := json.RawMessage(`"just a string"`)
	items, fallback := parseCollectionAnswer("repositories", answer)
	assert.Empty(t, items)
	assert.Len(t, fallback, 1)
}
