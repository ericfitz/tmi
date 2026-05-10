package models

import "testing"

func TestSystemAuditEntryTableName(t *testing.T) {
	if got := (SystemAuditEntry{}).TableName(); got != "system_audit_entries" {
		t.Errorf("TableName() = %q, want %q", got, "system_audit_entries")
	}
}

func TestSystemAuditEntryTableName_Uppercase(t *testing.T) {
	UseUppercaseTableNames = true
	defer func() { UseUppercaseTableNames = false }()
	if got := (SystemAuditEntry{}).TableName(); got != "SYSTEM_AUDIT_ENTRIES" {
		t.Errorf("TableName() = %q, want %q", got, "SYSTEM_AUDIT_ENTRIES")
	}
}
