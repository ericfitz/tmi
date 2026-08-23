// Package dbschema: removal of system_settings rows for setting keys the
// registry no longer declares (#813).
//
// rate_limit.requests_per_minute and rate_limit.requests_per_hour were seeded
// into system_settings on every fresh database but read by no handler --
// actual rate limiting runs off server.disable_rate_limiting and
// server.ratelimit_public_rpm. #812 classified them, which cleared the live
// GET/DELETE 404 that #809 reported, and #813 removes them outright.
//
// Removing the declarations alone is not sufficient. Existing deployments
// (RDS and k3s both) already hold the seeded rows, and a row whose key has no
// registry entry resolves to VisibilityInternal -- so LIST would keep showing
// it while GET and DELETE 404, which is exactly the inconsistency #809 was
// about. Deleting the declaration without deleting the row would therefore
// re-create the bug on every existing install while fixing it only for new
// ones.
//
// Deletion is unconditional rather than restricted to seeded-origin rows. The
// keys are leaving the product: an operator-set value for a setting nothing
// reads has no effect to preserve, and leaving explicit-origin rows behind
// would reproduce the orphan case above for precisely the installs most
// likely to notice. The keys and row count are logged; values never are.
//
// Ordering matters. This must run BEFORE BackfillSystemSettingOrigin.
// expectedSeedValues (system_setting_origin_backfill.go) derives from
// models.DefaultSystemSettings(), which projects the registry -- so once these
// defs are gone, a surviving NULL-origin rate_limit row no longer matches any
// known default and the backfill would stamp it explicit moments before this
// function deletes it. Harmless, but it inflates the backfill's reported row
// count and makes upgrade logs misleading.
//
// Idempotent and cheap in steady state: after the first successful run the
// DELETE matches nothing. Safe on every boot, mirroring
// BackfillSystemSettingOrigin and DeduplicateGroups.
//
// DML, not DDL -- so the Oracle PrepareStmt hazard that makes a repeated
// byte-identical DDL string silently no-op through gorm.DB.Exec does not
// apply here.
package dbschema

import (
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// retiredSettingKeys are setting keys the registry no longer declares and
// whose system_settings rows must not survive an upgrade.
//
// Append to this list when a setting is retired; never remove an entry, since
// a deployment may upgrade across several releases at once and still be
// carrying a row retired two releases ago.
var retiredSettingKeys = []string{
	// #813: dead since forever -- seeded but read by nothing. Real rate
	// limiting is server.disable_rate_limiting / server.ratelimit_public_rpm.
	"rate_limit.requests_per_minute",
	"rate_limit.requests_per_hour",
}

// PruneRetiredSystemSettings deletes system_settings rows whose keys the
// registry no longer declares, returning the number of rows removed.
//
// SEM@24731679561a852b21b37271caffd9a597080f0b: delete system_settings rows for keys the registry no longer declares (writes DB)
func PruneRetiredSystemSettings(db *gorm.DB) (int64, error) {
	logger := slogging.Get()
	settingsTable := (&models.SystemSetting{}).TableName()

	present, err := requireMigrationTable(db, settingsTable, "retired system_settings prune (#813)")
	if err != nil {
		return 0, err
	}
	if !present {
		return 0, nil
	}

	var removed int64
	err = withMigrationRetry("retired system_settings prune", func() error {
		res := db.Where("setting_key IN ?", retiredSettingKeys).Delete(&models.SystemSetting{})
		if res.Error != nil {
			return res.Error
		}
		removed = res.RowsAffected
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("failed to delete retired system_settings rows: %w", err)
	}

	if removed > 0 {
		// Keys and counts only -- a setting's value is never logged.
		logger.Info("Removed %d retired system_settings row(s) for keys: %v", removed, retiredSettingKeys)
	}
	return removed, nil
}
