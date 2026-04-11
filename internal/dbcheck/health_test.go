package dbcheck

import (
	"testing"
)

func TestSchemaHealthResult_IsCurrent(t *testing.T) {
	tests := []struct {
		name     string
		result   SchemaHealthResult
		expected bool
	}{
		{
			"all tables present",
			SchemaHealthResult{
				ExpectedTables: 44,
				PresentTables:  44,
				MissingTables:  nil,
			},
			true,
		},
		{
			"some tables missing",
			SchemaHealthResult{
				ExpectedTables: 44,
				PresentTables:  42,
				MissingTables:  []string{"teams", "projects"},
			},
			false,
		},
		{
			"empty database",
			SchemaHealthResult{
				ExpectedTables: 44,
				PresentTables:  0,
				MissingTables:  []string{"users", "groups"},
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.IsCurrent()
			if got != tt.expected {
				t.Errorf("IsCurrent() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExpectedTableNames(t *testing.T) {
	names := ExpectedTableNames()
	if len(names) == 0 {
		t.Fatal("ExpectedTableNames() returned empty list")
	}
	// Check a few known tables exist
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	required := []string{"users", "groups", "threat_models", "diagrams", "threats"}
	for _, r := range required {
		if !nameSet[r] {
			t.Errorf("Expected table %q not in ExpectedTableNames()", r)
		}
	}
}
