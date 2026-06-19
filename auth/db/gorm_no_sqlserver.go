//go:build !sqlserver

package db

import (
	"gorm.io/gorm"
)

// getSQLServerDialector returns nil when built without the sqlserver tag.
// To enable SQL Server support, build with: go build -tags sqlserver
// SEM@f494d0d545837596afcc5bccc1deb2ee4bf3e336: return nil to signal SQL Server is not compiled in this build (pure)
func getSQLServerDialector(_ GormConfig) gorm.Dialector {
	return nil
}
