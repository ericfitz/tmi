package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// exercises buildNestedConfig: dotted keys become nested maps, values typed by setting_type
func TestBuildNestedConfig(t *testing.T) {
	rows := []exportRow{
		{Key: "operator.name", Value: "Eric", Type: "string"},
		{Key: "extraction.async_enabled", Value: "true", Type: "bool"},
		{Key: "websocket.inactivity_timeout_seconds", Value: "300", Type: "int"},
	}
	root := buildNestedConfig(rows)

	op, ok := root["operator"].(map[string]any)
	if !ok {
		t.Fatalf("operator not nested: %#v", root)
	}
	if op["name"] != "Eric" {
		t.Errorf("operator.name = %v", op["name"])
	}
	ex := root["extraction"].(map[string]any)
	if ex["async_enabled"] != true {
		t.Errorf("bool not coerced: %v", ex["async_enabled"])
	}
	ws := root["websocket"].(map[string]any)
	if ws["inactivity_timeout_seconds"] != 300 {
		t.Errorf("int not coerced: %v", ws["inactivity_timeout_seconds"])
	}
}

// exercises writeExportedConfig: file round-trips through YAML
func TestWriteExportedConfig(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "export.yml")
	rows := []exportRow{{Key: "operator.name", Value: "Eric", Type: "string"}}
	if err := writeExportedConfig(rows, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("output not valid YAML: %v", err)
	}
	if parsed["operator"].(map[string]any)["name"] != "Eric" {
		t.Errorf("round-trip failed: %#v", parsed)
	}
}
