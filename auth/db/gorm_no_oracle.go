//go:build !oracle

package db

import (
	"gorm.io/gorm"
)

// getOracleDialector returns nil when built without the oracle tag.
// To enable Oracle support, build with: go build -tags oracle
// Oracle support requires CGO and the Oracle Instant Client libraries.
//
// The cfg parameter is unused in this build variant but must remain in the
// signature: the oracle-tagged variant in gorm_oracle.go consumes it, and the
// shared caller in gorm.go passes it. The blank name documents that it is
// intentionally unused here.
func getOracleDialector(_ GormConfig) (gorm.Dialector, string) {
	return nil, ""
}
