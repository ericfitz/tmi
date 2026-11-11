//go:build !mysql

package db

import (
	"gorm.io/gorm"
)

// getMySQLDialector returns nil when built without the mysql tag.
// To enable MySQL support, build with: go build -tags mysql
func getMySQLDialector(_ GormConfig) gorm.Dialector {
	return nil
}
