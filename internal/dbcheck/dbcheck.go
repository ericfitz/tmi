// Package dbcheck provides utilities for classifying database errors.
package dbcheck

import "strings"

// IsPermissionError returns true if the given error indicates insufficient
// database privileges for DDL operations (CREATE TABLE, ALTER TABLE, etc.).
//
// This is used by the server startup to distinguish "schema needs migration
// but user lacks DDL permissions" from other migration errors.
func IsPermissionError(err error, dbType string) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	switch dbType {
	case "postgres", "postgresql":
		return strings.Contains(errStr, "42501") ||
			strings.Contains(errStr, "insufficient_privilege") ||
			strings.Contains(errStr, "permission denied")

	case "oracle":
		return strings.Contains(errStr, "ora-01031") ||
			strings.Contains(errStr, "ora-01950")

	case "mysql":
		return strings.Contains(errStr, "error 1142") ||
			strings.Contains(errStr, "error 1044")

	case "sqlserver":
		return strings.Contains(errStr, "error 262") ||
			strings.Contains(errStr, "permission denied")

	default:
		return false
	}
}
