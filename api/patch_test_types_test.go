package api

import "reflect"

// SimpleEntity is a test-only patch target with no request schema.
type SimpleEntity struct {
	Name string `json:"name"`
}

// Test-only fixtures that are patched but have no request schema.
func init() {
	patchInputSchemas[reflect.TypeFor[PatchTestEntity]()] = nil
	patchInputSchemas[reflect.TypeFor[SimpleEntity]()] = nil
}
