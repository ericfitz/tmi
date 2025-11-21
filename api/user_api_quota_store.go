package api

import (
	"time"

	"github.com/google/uuid"
)

// UserAPIQuota represents per-user API rate limits
type UserAPIQuota struct {
	UserId               uuid.UUID `json:"user_id"`
	MaxRequestsPerMinute int       `json:"max_requests_per_minute"`
	MaxRequestsPerHour   *int      `json:"max_requests_per_hour,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	ModifiedAt           time.Time `json:"modified_at"`
}

// SetCreatedAt implements WithTimestamps
func (q *UserAPIQuota) SetCreatedAt(t time.Time) {
	q.CreatedAt = t
}

// SetModifiedAt implements WithTimestamps
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
const (
	DefaultMaxRequestsPerMinute = 100
	DefaultMaxRequestsPerHour   = 6000
)
