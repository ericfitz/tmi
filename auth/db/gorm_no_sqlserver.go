//go:build !sqlserver

package db

import (
	"gorm.io/gorm"
)

// getSQLServerDialector returns nil when built without the sqlserver tag.
// To enable SQL Server support, build with: go build -tags sqlserver
func getSQLServerDialector(_ GormConfig) gorm.Dialector {
	return nil
}
