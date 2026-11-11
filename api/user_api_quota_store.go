package api

import (
	"context"
	"time"
)

// SetCreatedAt implements WithTimestamps for UserAPIQuota
func (q *UserAPIQuota) SetCreatedAt(t time.Time) {
	q.CreatedAt = t
}

// SetModifiedAt implements WithTimestamps for UserAPIQuota
func (q *UserAPIQuota) SetModifiedAt(t time.Time) {
	q.ModifiedAt = t
}

// UserAPIQuotaStoreInterface defines operations for user API quotas.
// All methods accept a context.Context so the underlying GORM transaction
// retry wrapper can use the caller-supplied context for cancellation
// instead of falling back to context.Background().
type UserAPIQuotaStoreInterface interface {
	Get(ctx context.Context, userID string) (UserAPIQuota, error)
	GetOrDefault(ctx context.Context, userID string) UserAPIQuota
	List(ctx context.Context, offset, limit int) ([]UserAPIQuota, error)
	Count(ctx context.Context) (int, error)
	Create(ctx context.Context, item UserAPIQuota) (UserAPIQuota, error)
	Update(ctx context.Context, userID string, item UserAPIQuota) error
	Delete(ctx context.Context, userID string) error
	Upsert(ctx context.Context, item UserAPIQuota) (UserAPIQuota, error)
}

// Global user API quota store instance
var GlobalUserAPIQuotaStore UserAPIQuotaStoreInterface

// Default user API quota values
// Note: These are set high for development and fuzz testing. In production,
// consider lowering these values and implementing tiered quotas per user role.
const (
	DefaultMaxRequestsPerMinute = 1000  // Increased from 100 for fuzz testing
	DefaultMaxRequestsPerHour   = 60000 // Increased from 6000 for fuzz testing
)
