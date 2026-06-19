//go:build !mysql

package db

import (
	"gorm.io/gorm"
)

// getMySQLDialector returns nil when built without the mysql tag.
// To enable MySQL support, build with: go build -tags mysql
// SEM@f494d0d545837596afcc5bccc1deb2ee4bf3e336: return nil to signal MySQL is not compiled in for this build (pure)
func getMySQLDialector(_ GormConfig) gorm.Dialector {
	return nil
}
