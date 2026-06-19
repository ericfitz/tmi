package main

import (
	"errors"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// systemSettingReaderImpl satisfies api.SystemSettingReader by reading
// system_settings rows via GORM. It is used to supply the old-value capture
// function in the admin-audit descriptor table.
// SEM@f44001bd271fd2fa66f717ac20086e48d444cd07: GORM-backed reader that fetches system setting values for audit middleware
type systemSettingReaderImpl struct {
	db *gorm.DB
}

// newSystemSettingReader returns an api.SystemSettingReader backed by the
// provided *gorm.DB. Returns "" for unknown or missing keys so that the
// audit middleware records an empty old-value rather than panicking.
// SEM@f44001bd271fd2fa66f717ac20086e48d444cd07: build a DB-backed system setting reader for audit old-value capture
func newSystemSettingReader(db *gorm.DB) api.SystemSettingReader {
	return &systemSettingReaderImpl{db: db}
}

// Read fetches the current Value of the system setting identified by key.
// Returns "" if the setting does not exist or the query fails. Non-ErrRecordNotFound
// errors (e.g., transient connection issues) are logged at Debug so transient
// Oracle/Postgres failures don't silently become empty old-values in audit rows.
// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: fetch the current value of a system setting by key, returning empty string on miss (reads DB)
func (r *systemSettingReaderImpl) Read(c *gin.Context, key string) string {
	var s models.SystemSetting
	if err := r.db.WithContext(c.Request.Context()).
		Where("setting_key = ?", key).
		First(&s).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			// Warn — not Debug — because a transient DB failure here produces a
			// silent empty old-value in the audit row, which is exactly the
			// evidentiary gap the audit log exists to close.
			slogging.Get().WithContext(c).Warn(
				"system setting read failed (audit will record empty old value): key=%s err=%v",
				key, err)
		}
		return ""
	}
	return string(s.Value)
}
