package main

import (
	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// systemSettingReaderImpl satisfies api.SystemSettingReader by reading
// system_settings rows via GORM. It is used to supply the old-value capture
// function in the admin-audit descriptor table.
type systemSettingReaderImpl struct {
	db *gorm.DB
}

// newSystemSettingReader returns an api.SystemSettingReader backed by the
// provided *gorm.DB. Returns "" for unknown or missing keys so that the
// audit middleware records an empty old-value rather than panicking.
func newSystemSettingReader(db *gorm.DB) api.SystemSettingReader {
	return &systemSettingReaderImpl{db: db}
}

// Read fetches the current Value of the system setting identified by key.
// Returns "" if the setting does not exist or the query fails.
func (r *systemSettingReaderImpl) Read(c *gin.Context, key string) string {
	var s models.SystemSetting
	if err := r.db.WithContext(c.Request.Context()).
		Where("setting_key = ?", key).
		First(&s).Error; err != nil {
		return ""
	}
	return s.Value
}
