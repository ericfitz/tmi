package api

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestConvertToGroup_IsBuiltin verifies convertToGroup derives IsBuiltin from the
// validation.IsBuiltInGroup UUID allowlist rather than provider or name matching -
// this is the authoritative conversion path used by List, Get, and GetByProviderAndName.
func TestConvertToGroup_IsBuiltin(t *testing.T) {
	r := &GormGroupRepository{}

	t.Run("seeded built-in UUID reports is_builtin true", func(t *testing.T) {
		gg := &models.Group{
			InternalUUID: models.DBVarchar(validation.EveryonePseudoGroupUUID),
			Provider:     models.DBVarchar(BuiltInProvider),
			GroupName:    "everyone",
		}
		g := r.convertToGroup(gg)
		assert.True(t, g.IsBuiltin)
	})

	t.Run("admin-created group with random UUID and provider tmi reports is_builtin false", func(t *testing.T) {
		gg := &models.Group{
			InternalUUID: models.DBVarchar(uuid.New().String()),
			// Admin-created groups also get provider "tmi" - IsBuiltin must NOT
			// be derived from provider matching, only from the UUID allowlist.
			Provider:  models.DBVarchar(BuiltInProvider),
			GroupName: "admin-created-group",
		}
		g := r.convertToGroup(gg)
		assert.False(t, g.IsBuiltin)
	})

	t.Run("non-builtin provider and random UUID reports is_builtin false", func(t *testing.T) {
		gg := &models.Group{
			InternalUUID: models.DBVarchar(uuid.New().String()),
			Provider:     models.DBVarchar("github"),
			GroupName:    "engineering-team",
		}
		g := r.convertToGroup(gg)
		assert.False(t, g.IsBuiltin)
	})
}
