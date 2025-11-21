package api

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// UserAPIQuotaDatabaseStore implements UserAPIQuotaStoreInterface
type UserAPIQuotaDatabaseStore struct {
	db    *sql.DB
	mutex sync.RWMutex
}

// NewUserAPIQuotaDatabaseStore creates a new database-backed store
func NewUserAPIQuotaDatabaseStore(db *sql.DB) *UserAPIQuotaDatabaseStore {
	return &UserAPIQuotaDatabaseStore{db: db}
}

// Get retrieves a user API quota by user ID
func (s *UserAPIQuotaDatabaseStore) Get(userID string) (UserAPIQuota, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var quota UserAPIQuota
	var maxRequestsPerHour sql.NullInt64

	query := `
		SELECT user_id, max_requests_per_minute, max_requests_per_hour,
		       created_at, modified_at
		FROM user_api_quotas
		WHERE user_id = $1
	`

	err := s.db.QueryRow(query, userID).Scan(
		&quota.UserId, &quota.MaxRequestsPerMinute, &maxRequestsPerHour,
		&quota.CreatedAt, &quota.ModifiedAt,
	)

	if err == sql.ErrNoRows {
		return UserAPIQuota{}, fmt.Errorf("user API quota not found")
	}
	if err != nil {
		return UserAPIQuota{}, err
	}

	// Handle nullable max_requests_per_hour
	if maxRequestsPerHour.Valid {
		val := int(maxRequestsPerHour.Int64)
		quota.MaxRequestsPerHour = &val
	}

	return quota, nil
}

// GetOrDefault retrieves a quota or returns default values
func (s *UserAPIQuotaDatabaseStore) GetOrDefault(userID string) UserAPIQuota {
	quota, err := s.Get(userID)
	if err != nil {
		// Return default quota
		userUUID, _ := uuid.Parse(userID)
		defaultHourly := DefaultMaxRequestsPerHour
		return UserAPIQuota{
			UserId:               userUUID,
			MaxRequestsPerMinute: DefaultMaxRequestsPerMinute,
			MaxRequestsPerHour:   &defaultHourly,
		}
	}
	return quota
}

// Create creates a new user API quota
func (s *UserAPIQuotaDatabaseStore) Create(item UserAPIQuota) (UserAPIQuota, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update timestamps
	updatedItem := UpdateTimestamps(&item, true)
	item = *updatedItem

	query := `
		INSERT INTO user_api_quotas (
			user_id, max_requests_per_minute, max_requests_per_hour,
			created_at, modified_at
		) VALUES ($1, $2, $3, $4, $5)
	`

	var maxRequestsPerHour interface{}
	if item.MaxRequestsPerHour != nil {
		maxRequestsPerHour = *item.MaxRequestsPerHour
	} else {
		maxRequestsPerHour = nil
	}

	_, err := s.db.Exec(query,
		item.UserId, item.MaxRequestsPerMinute, maxRequestsPerHour,
		item.CreatedAt, item.ModifiedAt,
	)

	if err != nil {
		return UserAPIQuota{}, err
	}

	return item, nil
}

// Update updates an existing user API quota
func (s *UserAPIQuotaDatabaseStore) Update(userID string, item UserAPIQuota) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	// Update modified timestamp
	updatedItem := UpdateTimestamps(&item, false)
	item = *updatedItem

	query := `
		UPDATE user_api_quotas
		SET max_requests_per_minute = $1, max_requests_per_hour = $2,
		    modified_at = $3
		WHERE user_id = $4
	`

	var maxRequestsPerHour interface{}
	if item.MaxRequestsPerHour != nil {
		maxRequestsPerHour = *item.MaxRequestsPerHour
	} else {
		maxRequestsPerHour = nil
	}

	result, err := s.db.Exec(query,
		item.MaxRequestsPerMinute, maxRequestsPerHour,
		item.ModifiedAt, userID,
	)

	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("user API quota not found")
	}

	return nil
}

// Delete deletes a user API quota
func (s *UserAPIQuotaDatabaseStore) Delete(userID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	query := `DELETE FROM user_api_quotas WHERE user_id = $1`

	result, err := s.db.Exec(query, userID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("user API quota not found")
	}

	return nil
}
