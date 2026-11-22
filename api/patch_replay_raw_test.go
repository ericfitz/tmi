package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPatchValidation_ReplayAppliesOpsRaw: a later test op must see the raw
// value an earlier op wrote, as the real apply does; normalization (owner string
// to User, metadata null to empty) applies only to the validated copy.
func TestPatchValidation_ReplayAppliesOpsRaw(t *testing.T) {
	raw := `{"name":"T","owner":{"principal_type":"user","provider":"tmi","provider_id":"alice","display_name":"Alice","email":"alice@x.com"},"metadata":[]}`
	t.Run("replace owner then test owner", func(t *testing.T) {
		_, err := ApplyPatchOperations(storeShaped[ThreatModel](t, raw), []PatchOperation{
			pop("replace", "/owner", "bob@x.com"), pop("test", "/owner", "bob@x.com"),
		})
		require.NoError(t, err)
	})
	t.Run("replace metadata null then test", func(t *testing.T) {
		_, err := ApplyPatchOperations(storeShaped[ThreatModel](t, raw), []PatchOperation{
			pop("replace", "/metadata", nil), pop("test", "/metadata", nil),
		})
		require.NoError(t, err)
	})
}
