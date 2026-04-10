package api

// Timmy embedding automation handlers.
// These implement the ServerInterface methods generated from the OpenAPI spec
// for the /automation/embeddings endpoints.

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
)

// GetEmbeddingConfig returns the embedding provider configuration for a threat model.
// GET /automation/embeddings/{threat_model_id}/config
func (s *Server) GetEmbeddingConfig(c *gin.Context, threatModelId ThreatModelId) {
	tmID := threatModelId.String()

	// Verify threat model exists
	if _, err := ThreatModelStore.Get(tmID); err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Timmy must be configured
	if s.timmySessionManager == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is not configured"))
		return
	}

	cfg := s.timmySessionManager.config

	// Build text embedding config (always present when Timmy is configured)
	textCfg := EmbeddingProviderConfig{
		Provider: cfg.TextEmbeddingProvider,
		Model:    cfg.TextEmbeddingModel,
	}
	if cfg.TextEmbeddingAPIKey != "" {
		textCfg.ApiKey = &cfg.TextEmbeddingAPIKey
	}
	if cfg.TextEmbeddingBaseURL != "" {
		textCfg.BaseUrl = &cfg.TextEmbeddingBaseURL
	}

	resp := EmbeddingConfig{
		TextEmbedding: textCfg,
	}

	// Build optional code embedding config
	if cfg.IsCodeIndexConfigured() {
		codeCfg := EmbeddingProviderConfig{
			Provider: cfg.CodeEmbeddingProvider,
			Model:    cfg.CodeEmbeddingModel,
		}
		if cfg.CodeEmbeddingAPIKey != "" {
			codeCfg.ApiKey = &cfg.CodeEmbeddingAPIKey
		}
		if cfg.CodeEmbeddingBaseURL != "" {
			codeCfg.BaseUrl = &cfg.CodeEmbeddingBaseURL
		}
		resp.CodeEmbedding = &codeCfg
	}

	c.JSON(http.StatusOK, resp)
}

// IngestEmbeddings accepts a batch of pre-computed embeddings for a threat model.
// POST /automation/embeddings/{threat_model_id}
func (s *Server) IngestEmbeddings(c *gin.Context, threatModelId ThreatModelId) {
	logger := slogging.Get().WithContext(c)
	tmID := threatModelId.String()

	// Verify threat model exists
	if _, err := ThreatModelStore.Get(tmID); err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	if GlobalTimmyEmbeddingStore == nil {
		HandleRequestError(c, ServiceUnavailableError("Embedding store is not configured"))
		return
	}

	// Parse request body
	var req IngestEmbeddingsJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, InvalidInputError("invalid request body: "+err.Error()))
		return
	}

	if len(req.Embeddings) == 0 {
		HandleRequestError(c, InvalidInputError("embeddings list must not be empty"))
		return
	}

	// Validate index_type
	indexType := string(req.IndexType)
	if indexType != IndexTypeText && indexType != IndexTypeCode {
		HandleRequestError(c, InvalidInputError("index_type must be 'text' or 'code'"))
		return
	}

	// Validate entity type / index type consistency and vector dimensions
	for i, item := range req.Embeddings {
		// Validate that each entity type maps to the declared index type
		expectedIndex := EntityTypeToIndexType(item.EntityType)
		if expectedIndex != indexType {
			HandleRequestError(c, InvalidInputError(
				"item at index "+itoa(i)+": entity_type '"+item.EntityType+
					"' belongs to index '"+expectedIndex+
					"' but request declares index_type '"+indexType+"'",
			))
			return
		}

		// Validate vector length matches embedding_dim
		if len(item.Vector) == 0 {
			HandleRequestError(c, InvalidInputError(
				"item at index "+itoa(i)+": vector must not be empty",
			))
			return
		}
		if item.EmbeddingDim <= 0 {
			HandleRequestError(c, InvalidInputError(
				"item at index "+itoa(i)+": embedding_dim must be positive",
			))
			return
		}
		if len(item.Vector) != item.EmbeddingDim {
			HandleRequestError(c, &RequestError{
				Status:  422,
				Code:    "dimension_mismatch",
				Message: "item at index " + itoa(i) + ": vector length does not match embedding_dim",
			})
			return
		}
	}

	// Validate all vectors have the same dimension
	firstDim := req.Embeddings[0].EmbeddingDim
	for i, item := range req.Embeddings {
		if item.EmbeddingDim != firstDim {
			HandleRequestError(c, &RequestError{
				Status:  422,
				Code:    "inconsistent_dimensions",
				Message: "item at index " + itoa(i) + ": all embeddings must have the same dimension",
			})
			return
		}
	}

	// Convert to model records
	records := make([]models.TimmyEmbedding, 0, len(req.Embeddings))
	for _, item := range req.Embeddings {
		records = append(records, models.TimmyEmbedding{
			ThreatModelID:  tmID,
			EntityType:     item.EntityType,
			EntityID:       item.EntityId.String(),
			ChunkIndex:     item.ChunkIndex,
			IndexType:      indexType,
			ContentHash:    item.ContentHash,
			EmbeddingModel: item.EmbeddingModel,
			EmbeddingDim:   item.EmbeddingDim,
			VectorData:     models.DBBytes(float32ToBytes(item.Vector)),
			ChunkText:      models.DBText(item.ChunkText),
		})
	}

	// Persist the batch
	if err := GlobalTimmyEmbeddingStore.CreateBatch(c.Request.Context(), records); err != nil {
		logger.Error("Failed to ingest embeddings for threat model %s: %v", tmID, err)
		HandleRequestError(c, ServerError("Failed to ingest embeddings"))
		return
	}

	// Invalidate the in-memory index so the next query reloads from DB
	if s.vectorManager != nil {
		s.vectorManager.InvalidateIndex(tmID, indexType)
	}

	c.JSON(http.StatusCreated, EmbeddingIngestionResponse{Ingested: len(records)})
}

// DeleteEmbeddings deletes embeddings for a threat model, optionally filtered.
// DELETE /automation/embeddings/{threat_model_id}
func (s *Server) DeleteEmbeddings(c *gin.Context, threatModelId ThreatModelId, params DeleteEmbeddingsParams) {
	logger := slogging.Get().WithContext(c)
	tmID := threatModelId.String()

	// Verify threat model exists
	if _, err := ThreatModelStore.Get(tmID); err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	if GlobalTimmyEmbeddingStore == nil {
		HandleRequestError(c, ServiceUnavailableError("Embedding store is not configured"))
		return
	}

	// Require at least one filter
	if params.EntityType == nil && params.EntityId == nil && params.IndexType == nil {
		HandleRequestError(c, InvalidInputError("at least one filter parameter (entity_type, entity_id, or index_type) is required"))
		return
	}

	// entity_id requires entity_type
	if params.EntityId != nil && params.EntityType == nil {
		HandleRequestError(c, InvalidInputError("entity_id filter requires entity_type"))
		return
	}

	// entity_type without entity_id is not supported — direct to use index_type instead
	if params.EntityType != nil && params.EntityId == nil {
		HandleRequestError(c, InvalidInputError(
			"entity_type filter requires entity_id; use index_type filter to delete all embeddings of a type",
		))
		return
	}

	// Validate index_type if set
	var indexType string
	if params.IndexType != nil {
		indexType = string(*params.IndexType)
		if indexType != IndexTypeText && indexType != IndexTypeCode {
			HandleRequestError(c, InvalidInputError("index_type must be 'text' or 'code'"))
			return
		}
	}

	ctx := c.Request.Context()
	var deleted int64
	var err error

	switch {
	case params.EntityType != nil && params.EntityId != nil:
		// Delete by specific entity
		entityType := *params.EntityType
		entityID := params.EntityId.String()
		deleted, err = GlobalTimmyEmbeddingStore.DeleteByEntity(ctx, tmID, entityType, entityID)
		if err != nil {
			logger.Error("Failed to delete embeddings by entity for threat model %s: %v", tmID, err)
			HandleRequestError(c, ServerError("Failed to delete embeddings"))
			return
		}
		// Invalidate the index that corresponds to this entity type
		affectedIndex := EntityTypeToIndexType(entityType)
		if s.vectorManager != nil {
			s.vectorManager.InvalidateIndex(tmID, affectedIndex)
		}

	case params.IndexType != nil:
		// Delete all embeddings for this threat model + index type
		deleted, err = GlobalTimmyEmbeddingStore.DeleteByThreatModelAndIndexType(ctx, tmID, indexType)
		if err != nil {
			logger.Error("Failed to delete embeddings by index type for threat model %s: %v", tmID, err)
			HandleRequestError(c, ServerError("Failed to delete embeddings"))
			return
		}
		if s.vectorManager != nil {
			s.vectorManager.InvalidateIndex(tmID, indexType)
		}

	default:
		// This path should not be reachable given the validation above
		HandleRequestError(c, InvalidInputError("at least one filter parameter is required"))
		return
	}

	c.JSON(http.StatusOK, EmbeddingDeleteResponse{Deleted: int(deleted)})
}

// itoa converts an int to a decimal string without importing strconv in this file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
