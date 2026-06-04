package models

import "testing"

func TestExtractionJob_TableName(t *testing.T) {
	got := ExtractionJob{}.TableName()
	if got != tableName("extraction_jobs") {
		t.Fatalf("TableName() = %q, want %q", got, tableName("extraction_jobs"))
	}
}

func TestExtractionJob_InAllModels(t *testing.T) {
	found := false
	for _, m := range AllModels() {
		if _, ok := m.(*ExtractionJob); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ExtractionJob not registered in AllModels()")
	}
}
