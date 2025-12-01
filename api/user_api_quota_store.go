package api

import (
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

// UserAPIQuotaStoreInterface defines operations for user API quotas
type UserAPIQuotaStoreInterface interface {
	Get(userID string) (UserAPIQuota, error)
	GetOrDefault(userID string) UserAPIQuota
	Create(item UserAPIQuota) (UserAPIQuota, error)
	Update(userID string, item UserAPIQuota) error
	Delete(userID string) error
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
