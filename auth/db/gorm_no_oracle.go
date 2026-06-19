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
// SEM@2ee8f019dfbe33d87f3059a36defca778b721453: return nil Oracle GORM dialector when built without oracle tag (pure)
func getOracleDialector(_ GormConfig) (gorm.Dialector, string) {
	return nil, ""
}
