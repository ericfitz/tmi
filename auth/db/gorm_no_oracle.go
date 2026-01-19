//go:build !oracle

package db

import (
	"gorm.io/gorm"
)

// getOracleDialector returns nil when built without the oracle tag.
// To enable Oracle support, build with: go build -tags oracle
// Oracle support requires CGO and the Oracle Instant Client libraries.
func getOracleDialector(cfg GormConfig) (gorm.Dialector, string) {
	return nil, ""
}
