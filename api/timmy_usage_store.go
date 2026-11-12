package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
)

// UsageAggregation holds aggregated usage totals across multiple records
type UsageAggregation struct {
	TotalMessages         int `json:"total_messages"`
	TotalPromptTokens     int `json:"total_prompt_tokens"`
	TotalCompletionTokens int `json:"total_completion_tokens"`
	TotalEmbeddingTokens  int `json:"total_embedding_tokens"`
	SessionCount          int `json:"session_count"`
}

// TimmyUsageStore defines operations for recording and querying LLM usage
type TimmyUsageStore interface {
	Record(ctx context.Context, usage *models.TimmyUsage) error
	GetByUser(ctx context.Context, userID string, start, end time.Time) ([]models.TimmyUsage, error)
	GetByThreatModel(ctx context.Context, threatModelID string, start, end time.Time) ([]models.TimmyUsage, error)
	GetAggregated(ctx context.Context, userID, threatModelID string, start, end time.Time) (*UsageAggregation, error)
}

// GlobalTimmyUsageStore is the global usage store instance
var GlobalTimmyUsageStore TimmyUsageStore
