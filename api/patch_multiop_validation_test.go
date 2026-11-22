package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPatchValidation_MultiOpAndPointerEdgeCases: validation must follow each
// operation as applied, not look the path up in the final document.
func TestPatchValidation_MultiOpAndPointerEdgeCases(t *testing.T) {
	long := strings.Repeat("a", 5000)
	raw := `{"name":"T","responsible_parties":[` +
		`{"user_id":"` + rtUserID + `","role":"engineer","user":` + rtUser + `},` +
		`{"user_id":"` + rtUserID2 + `","role":"engineer"}]}`
	bad := map[string]any{"user_id": rtUserID, "custom_role": long}
	good := map[string]any{"user_id": rtUserID2, "role": "engineer"}
	tests := []struct {
		name string
		ops  []PatchOperation
	}{
		{"append bad then append good", []PatchOperation{pop("add", "/responsible_parties/-", bad), pop("add", "/responsible_parties/-", good)}},
		{"insert bad at 0 then good at 0", []PatchOperation{pop("add", "/responsible_parties/0", bad), pop("add", "/responsible_parties/0", good)}},
		{"insert bad at 1 then good at 0", []PatchOperation{pop("add", "/responsible_parties/1", bad), pop("add", "/responsible_parties/0", good)}},
		{"negative index nested replace", []PatchOperation{pop("replace", "/responsible_parties/-1/custom_role", long)}},
		{"negative index add element", []PatchOperation{pop("add", "/responsible_parties/-1", bad)}},
		{"nested null user_id", []PatchOperation{pop("replace", "/responsible_parties/0/user_id", nil)}},
		{"nested null role", []PatchOperation{pop("replace", "/responsible_parties/0/role", nil)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ApplyPatchOperations(storeShaped[Team](t, raw), tt.ops)
			requireValidationRejects(t, err)
		})
	}
	t.Run("good multi-op patch passes", func(t *testing.T) {
		_, err := ApplyPatchOperations(storeShaped[Team](t, raw), []PatchOperation{
			pop("add", "/responsible_parties/0", good), pop("replace", "/responsible_parties/1/role", "engineering_lead"),
		})
		require.NoError(t, err)
	})
	t.Run("top-level null still clears", func(t *testing.T) {
		_, err := ApplyPatchOperations(storeShaped[Team](t, raw), []PatchOperation{pop("replace", "/responsible_parties", nil)})
		require.NoError(t, err)
	})
}

// TestPatchValidation_WriteInsideOneOfElementValidatesElement: a nested write
// the schema walk cannot address is validated via its nearest ancestor.
func TestPatchValidation_WriteInsideOneOfElementValidatesElement(t *testing.T) {
	raw := `{"name":"D","type":"DFD-1.0.0","cells":[{"id":"` + rtUserID + `","shape":"process","x":1,"y":1,"width":10,"height":10}]}`
	_, err := ApplyPatchOperations(storeShaped[DfdDiagram](t, raw), []PatchOperation{
		pop("add", "/cells/0/bogus_member_xyz", "v"),
	})
	requireValidationRejects(t, err)
}

// TestPatchInputSchemas_CallSiteCount scans the non-test sources for
// ApplyPatchOperations call sites. Adding one changes the count, forcing the
// author to register the patched type in patchInputSchemas and to list it in
// TestPatchInputSchemas_RegisteredSchemasExist, then bump this number.
func TestPatchInputSchemas_CallSiteCount(t *testing.T) {
	const knownCallSites = 13
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	count := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "func ") {
				continue
			}
			if strings.Contains(line, "ApplyPatchOperations(") {
				count++
			}
		}
	}
	require.Equal(t, knownCallSites, count, "ApplyPatchOperations call sites changed: register the patched type in patchInputSchemas, add it to the list in TestPatchInputSchemas_RegisteredSchemasExist, update knownCallSites")
}
