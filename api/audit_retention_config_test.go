package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSystemAuditRetentionDays_Default(t *testing.T) {
	// t.Setenv with empty registers a cleanup AND unsets for this test
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "")
	assert.Equal(t, 365, SystemAuditRetentionDays())
}

func TestSystemAuditRetentionDays_Configured(t *testing.T) {
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "180")
	assert.Equal(t, 180, SystemAuditRetentionDays())
}

func TestSystemAuditRetentionDays_ClampsToMinimum(t *testing.T) {
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "30")
	assert.Equal(t, 90, SystemAuditRetentionDays(),
		"system audit retention must clamp to the 90-day evidence minimum")
}
