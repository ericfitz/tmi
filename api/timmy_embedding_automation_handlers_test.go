package api

// Unit tests for the three embedding automation handlers:
//   - GetEmbeddingConfig
//   - IngestEmbeddings
//   - DeleteEmbeddings

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ──────────────────────────────────────────────────────────────────

const (
	// A known threat model UUID used across embedding automation tests
	embTestTMID = "00000000-0000-0000-0000-200000000001"
	// An unknown UUID to trigger 404 responses
	embTestMissingTMID = "00000000-0000-0000-0000-200000000099"

	// embedding provider / model strings shared across tests
	embTestProvider  = "openai"
	embTestTextModel = "text-embedding-3-small"
	embTestCodeModel = "text-embedding-3-large"
)

// setupEmbeddingAutomationTest wires up global stores, creates a test server,
// and pre-populates ThreatModelStore with one known threat model.
// It returns the server and a cleanup function.
func setupEmbeddingAutomationTest(t *testing.T) (*Server, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := setupTimmyTestDB(t)

	// Save old globals
	oldEmbeddingStore := GlobalTimmyEmbeddingStore
	oldThreatModelStore := ThreatModelStore

	// Install fresh embedding store backed by the in-memory SQLite DB
	GlobalTimmyEmbeddingStore = NewGormTimmyEmbeddingStore(db)

	// Install a mock threat model store with one known TM
	mockTMStore := &MockThreatModelStore{data: make(map[string]ThreatModel)}
	knownTMID := mustParseTimmyUUID(embTestTMID)
	mockTMStore.data[embTestTMID] = ThreatModel{
		Id:   &knownTMID,
		Name: "Embedding Test TM",
	}
	ThreatModelStore = mockTMStore

	server := NewServerForTests()

	cleanup := func() {
		GlobalTimmyEmbeddingStore = oldEmbeddingStore
		ThreatModelStore = oldThreatModelStore
	}

	return server, cleanup
}

// embTestContext builds a minimal gin.Context (no auth set — not needed for
// these handlers which don't check userInternalUUID).
func embTestContext(method, path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	c.Request = req
	return c, w
}

// mustMarshal panics on marshal failure (safe for test helpers).
func mustMarshalEmbedding(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return data
}

// makeEmbeddingItem builds a minimal valid EmbeddingIngestionItem.
// entityType controls which index it belongs to; dim is both embedding_dim and len(vector).
func makeEmbeddingItem(entityType string, dim int) EmbeddingIngestionItem {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32(i) * 0.1
	}
	return EmbeddingIngestionItem{
		ChunkIndex:     0,
		ChunkText:      "sample chunk",
		ContentHash:    "abc123",
		EmbeddingDim:   dim,
		EmbeddingModel: "test-model",
		EntityId:       mustParseTimmyUUID("aaaaaaaa-0000-0000-0000-000000000001"),
		EntityType:     entityType,
		Vector:         vec,
	}
}

// ── GetEmbeddingConfig tests ──────────────────────────────────────────────────

func TestGetEmbeddingConfig_TextOnly(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	// Configure a session manager with text embedding only (no code)
	cfg := config.DefaultTimmyConfig()
	cfg.TextEmbeddingProvider = embTestProvider
	cfg.TextEmbeddingModel = embTestTextModel
	cfg.TextEmbeddingAPIKey = "sk-test"
	cfg.CodeEmbeddingProvider = "" // not configured
	cfg.CodeEmbeddingModel = ""
	server.timmySessionManager = &TimmySessionManager{config: cfg}

	c, w := embTestContext("GET", "/", nil)

	server.GetEmbeddingConfig(c, mustParseTimmyUUID(embTestTMID))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp EmbeddingConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, embTestProvider, resp.TextEmbedding.Provider)
	assert.Equal(t, embTestTextModel, resp.TextEmbedding.Model)
	require.NotNil(t, resp.TextEmbedding.ApiKey)
	assert.Equal(t, "sk-test", *resp.TextEmbedding.ApiKey)
	// No code embedding when not configured
	assert.Nil(t, resp.CodeEmbedding)
}

func TestGetEmbeddingConfig_TextAndCode(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	cfg := config.DefaultTimmyConfig()
	cfg.TextEmbeddingProvider = embTestProvider
	cfg.TextEmbeddingModel = embTestTextModel
	cfg.CodeEmbeddingProvider = embTestProvider
	cfg.CodeEmbeddingModel = embTestCodeModel
	cfg.CodeEmbeddingAPIKey = "sk-code"
	server.timmySessionManager = &TimmySessionManager{config: cfg}

	c, w := embTestContext("GET", "/", nil)

	server.GetEmbeddingConfig(c, mustParseTimmyUUID(embTestTMID))

	assert.Equal(t, http.StatusOK, w.Code)

	var resp EmbeddingConfig
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, embTestProvider, resp.TextEmbedding.Provider)
	require.NotNil(t, resp.CodeEmbedding)
	assert.Equal(t, embTestProvider, resp.CodeEmbedding.Provider)
	assert.Equal(t, embTestCodeModel, resp.CodeEmbedding.Model)
	require.NotNil(t, resp.CodeEmbedding.ApiKey)
	assert.Equal(t, "sk-code", *resp.CodeEmbedding.ApiKey)
}

func TestGetEmbeddingConfig_NotFoundThreatModel(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	cfg := config.DefaultTimmyConfig()
	cfg.TextEmbeddingProvider = embTestProvider
	cfg.TextEmbeddingModel = embTestTextModel
	server.timmySessionManager = &TimmySessionManager{config: cfg}

	c, w := embTestContext("GET", "/", nil)

	server.GetEmbeddingConfig(c, mustParseTimmyUUID(embTestMissingTMID))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetEmbeddingConfig_NilSessionManager(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	// timmySessionManager is nil by default from NewServerForTests
	assert.Nil(t, server.timmySessionManager)

	c, w := embTestContext("GET", "/", nil)

	server.GetEmbeddingConfig(c, mustParseTimmyUUID(embTestTMID))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// ── IngestEmbeddings tests ────────────────────────────────────────────────────

func TestIngestEmbeddings_ValidBatch(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	item := makeEmbeddingItem("asset", 4)
	reqBody := EmbeddingIngestionRequest{
		IndexType:  EmbeddingIngestionRequestIndexTypeText,
		Embeddings: []EmbeddingIngestionItem{item},
	}

	c, w := embTestContext("POST", "/", mustMarshalEmbedding(t, reqBody))

	server.IngestEmbeddings(c, mustParseTimmyUUID(embTestTMID))

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp EmbeddingIngestionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Ingested)
}

func TestIngestEmbeddings_InvalidIndexType(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	// Use raw map to inject an invalid index_type value
	reqBody := map[string]any{
		"index_type": "invalid-type",
		"embeddings": []any{
			map[string]any{
				"entity_type":     "asset",
				"entity_id":       "aaaaaaaa-0000-0000-0000-000000000001",
				"chunk_index":     0,
				"chunk_text":      "chunk",
				"content_hash":    "abc",
				"embedding_dim":   4,
				"embedding_model": "m",
				"vector":          []float32{0.1, 0.2, 0.3, 0.4},
			},
		},
	}

	c, w := embTestContext("POST", "/", mustMarshalEmbedding(t, reqBody))

	server.IngestEmbeddings(c, mustParseTimmyUUID(embTestTMID))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "index_type must be 'text' or 'code'")
}

func TestIngestEmbeddings_EntityIndexTypeMismatch(t *testing.T) {
	// repository belongs to "code" index, but we declare index_type "text" → 400
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	item := makeEmbeddingItem("repository", 4) // belongs to "code" index
	reqBody := EmbeddingIngestionRequest{
		IndexType:  EmbeddingIngestionRequestIndexTypeText, // declares "text"
		Embeddings: []EmbeddingIngestionItem{item},
	}

	c, w := embTestContext("POST", "/", mustMarshalEmbedding(t, reqBody))

	server.IngestEmbeddings(c, mustParseTimmyUUID(embTestTMID))

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "belongs to index")
}

func TestIngestEmbeddings_InconsistentVectorDimensions(t *testing.T) {
	// Two items with different embedding_dim values → 422
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	item1 := makeEmbeddingItem("asset", 4)
	item2 := makeEmbeddingItem("asset", 8) // different dim from item1
	reqBody := EmbeddingIngestionRequest{
		IndexType:  EmbeddingIngestionRequestIndexTypeText,
		Embeddings: []EmbeddingIngestionItem{item1, item2},
	}

	c, w := embTestContext("POST", "/", mustMarshalEmbedding(t, reqBody))

	server.IngestEmbeddings(c, mustParseTimmyUUID(embTestTMID))

	assert.Equal(t, 422, w.Code)
	assert.Contains(t, w.Body.String(), "inconsistent_dimensions")
}

func TestIngestEmbeddings_VectorLengthMismatchEmbeddingDim(t *testing.T) {
	// Vector has 3 floats but embedding_dim says 4 → 422
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	item := makeEmbeddingItem("asset", 4)
	item.Vector = []float32{0.1, 0.2, 0.3} // length 3 != embedding_dim 4
	reqBody := EmbeddingIngestionRequest{
		IndexType:  EmbeddingIngestionRequestIndexTypeText,
		Embeddings: []EmbeddingIngestionItem{item},
	}

	c, w := embTestContext("POST", "/", mustMarshalEmbedding(t, reqBody))

	server.IngestEmbeddings(c, mustParseTimmyUUID(embTestTMID))

	assert.Equal(t, 422, w.Code)
	assert.Contains(t, w.Body.String(), "dimension_mismatch")
}

func TestIngestEmbeddings_NotFoundThreatModel(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	item := makeEmbeddingItem("asset", 4)
	reqBody := EmbeddingIngestionRequest{
		IndexType:  EmbeddingIngestionRequestIndexTypeText,
		Embeddings: []EmbeddingIngestionItem{item},
	}

	c, w := embTestContext("POST", "/", mustMarshalEmbedding(t, reqBody))

	server.IngestEmbeddings(c, mustParseTimmyUUID(embTestMissingTMID))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ── DeleteEmbeddings tests ────────────────────────────────────────────────────

// seedEmbedding inserts one embedding for embTestTMID using the given entityType.
func seedEmbedding(t *testing.T, entityType string, entityID string) {
	t.Helper()
	item := makeEmbeddingItem(entityType, 4)
	entityUUID := mustParseTimmyUUID(entityID)
	item.EntityId = entityUUID
	reqBody := EmbeddingIngestionRequest{
		Embeddings: []EmbeddingIngestionItem{item},
	}
	if entityType == "repository" {
		reqBody.IndexType = EmbeddingIngestionRequestIndexTypeCode
	} else {
		reqBody.IndexType = EmbeddingIngestionRequestIndexTypeText
	}

	// Ingest via a separate context to populate the store
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)
	c.Request = httptest.NewRequest("POST", "/", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	// Use a fresh server (same globals already set up in test)
	srv := NewServerForTests()
	srv.IngestEmbeddings(c, mustParseTimmyUUID(embTestTMID))
	require.Equal(t, http.StatusCreated, w.Code, "seed ingest should succeed; body: %s", w.Body.String())
}

func TestDeleteEmbeddings_ByEntityTypeAndID(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	entityID := "bbbbbbbb-0000-0000-0000-000000000001"
	seedEmbedding(t, "asset", entityID)

	entityType := "asset"
	entityUUID := mustParseTimmyUUID(entityID)
	params := DeleteEmbeddingsParams{
		EntityType: &entityType,
		EntityId:   &entityUUID,
	}

	c, w := embTestContext("DELETE", "/", nil)

	server.DeleteEmbeddings(c, mustParseTimmyUUID(embTestTMID), params)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp EmbeddingDeleteResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Deleted)
}

func TestDeleteEmbeddings_ByIndexType(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	// Seed two text-index embeddings with distinct entity IDs
	seedEmbedding(t, "asset", "cccccccc-0000-0000-0000-000000000001")
	seedEmbedding(t, "asset", "cccccccc-0000-0000-0000-000000000002")

	indexType := DeleteEmbeddingsParamsIndexTypeText
	params := DeleteEmbeddingsParams{
		IndexType: &indexType,
	}

	c, w := embTestContext("DELETE", "/", nil)

	server.DeleteEmbeddings(c, mustParseTimmyUUID(embTestTMID), params)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp EmbeddingDeleteResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Deleted, 2)
}

func TestDeleteEmbeddings_NoFilters(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	// No params at all — should be rejected
	params := DeleteEmbeddingsParams{}

	c, w := embTestContext("DELETE", "/", nil)

	server.DeleteEmbeddings(c, mustParseTimmyUUID(embTestTMID), params)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "at least one filter parameter")
}

func TestDeleteEmbeddings_EntityIDWithoutEntityType(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	entityUUID := mustParseTimmyUUID("dddddddd-0000-0000-0000-000000000001")
	params := DeleteEmbeddingsParams{
		EntityId: &entityUUID, // entity_type is nil
	}

	c, w := embTestContext("DELETE", "/", nil)

	server.DeleteEmbeddings(c, mustParseTimmyUUID(embTestTMID), params)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "entity_id filter requires entity_type")
}

func TestDeleteEmbeddings_NotFoundThreatModel(t *testing.T) {
	server, cleanup := setupEmbeddingAutomationTest(t)
	defer cleanup()

	indexType := DeleteEmbeddingsParamsIndexTypeText
	params := DeleteEmbeddingsParams{
		IndexType: &indexType,
	}

	c, w := embTestContext("DELETE", "/", nil)

	server.DeleteEmbeddings(c, mustParseTimmyUUID(embTestMissingTMID), params)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
