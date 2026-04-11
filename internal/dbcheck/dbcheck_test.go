package dbcheck

import (
	"errors"
	"testing"
)

func TestIsPermissionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		dbType   string
		expected bool
	}{
		// PostgreSQL
		{"pg permission denied", errors.New("ERROR: permission denied for table users (SQLSTATE 42501)"), "postgres", true},
		{"pg insufficient privilege", errors.New("insufficient_privilege"), "postgres", true},
		{"pg permission denied simple", errors.New("permission denied"), "postgres", true},
		{"pg connection error", errors.New("connection refused"), "postgres", false},
		{"pg syntax error", errors.New("syntax error at or near"), "postgres", false},

		// Oracle
		{"ora insufficient privileges", errors.New("ORA-01031: insufficient privileges"), "oracle", true},
		{"ora no privileges on tablespace", errors.New("ORA-01950: no privileges on tablespace"), "oracle", true},
		{"ora table already exists", errors.New("ORA-00955: name is already used by an existing object"), "oracle", false},
		{"ora connection error", errors.New("ORA-12541: TNS:no listener"), "oracle", false},

		// MySQL
		{"mysql command denied", errors.New("Error 1142: INSERT command denied to user"), "mysql", true},
		{"mysql access denied", errors.New("Error 1044: Access denied for user"), "mysql", true},
		{"mysql syntax error", errors.New("Error 1064: You have an error in your SQL syntax"), "mysql", false},

		// SQL Server
		{"mssql create table denied", errors.New("Error 262: CREATE TABLE permission denied in database"), "sqlserver", true},
		{"mssql connection error", errors.New("login failed for user"), "sqlserver", false},

		// SQLite (never permission errors from DB roles)
		{"sqlite readonly", errors.New("attempt to write a readonly database"), "sqlite", false},

		// Unknown database type
		{"unknown type", errors.New("permission denied"), "unknown", false},

		// Nil-safe
		{"nil error", nil, "postgres", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPermissionError(tt.err, tt.dbType)
			if got != tt.expected {
				errStr := "<nil>"
				if tt.err != nil {
					errStr = tt.err.Error()
				}
				t.Errorf("IsPermissionError(%q, %q) = %v, want %v", errStr, tt.dbType, got, tt.expected)
			}
		})
	}
}
