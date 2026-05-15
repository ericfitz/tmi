// Package models - hooks.go contains GORM lifecycle hooks for validation.
// These hooks replace PostgreSQL CHECK constraints and triggers to enable
// consistent validation across all supported databases.
package models

import (
	"fmt"

	"github.com/ericfitz/tmi/api/validation"
	"gorm.io/gorm"
)

// --- ThreatModel Hooks ---

// BeforeUpdate validates ThreatModel before update
func (t *ThreatModel) BeforeUpdate(tx *gorm.DB) error {
	// Only validate framework if it's being updated (non-empty)
	// Empty framework means the field wasn't included in the update
	if t.ThreatModelFramework != "" {
		if err := validation.ValidateThreatModelFramework(string(t.ThreatModelFramework)); err != nil {
			return err
		}
	}
	s := string(t.Status)
	if err := validation.ValidateStatusLength(&s); err != nil {
		return err
	}
	return nil
}

// --- Diagram Hooks ---

// BeforeUpdate validates Diagram before update
func (d *Diagram) BeforeUpdate(tx *gorm.DB) error {
	if d.Type.Valid {
		if err := validation.ValidateDiagramType(d.Type.String); err != nil {
			return err
		}
	}
	return nil
}

// --- Asset Hooks ---

// Note: Asset validation (name, type) is in models.go BeforeCreate hook,
// not here as BeforeSave, because GORM map-based updates (.Model(&Asset{}).Updates(map))
// trigger BeforeSave on the empty model struct, causing false "cannot be empty" errors.
// Update-time validation is handled by the API layer.

// --- Threat Hooks ---

// BeforeSave validates Threat before create or update
func (t *Threat) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateScore(t.Score); err != nil {
		return err
	}
	return nil
}

// --- ThreatModelAccess Hooks ---

// BeforeSave validates ThreatModelAccess before create or update
func (t *ThreatModelAccess) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateSubjectType(string(t.SubjectType)); err != nil {
		return err
	}
	if err := validation.ValidateRole(string(t.Role)); err != nil {
		return err
	}
	if err := validation.ValidateSubjectXOR(string(t.SubjectType), t.UserInternalUUID.Ptr(), t.GroupInternalUUID.Ptr()); err != nil {
		return err
	}
	return nil
}

// --- Document Hooks ---

// BeforeSave validates Document before create or update
func (d *Document) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateNonEmpty("name", string(d.Name)); err != nil {
		return err
	}
	if err := validation.ValidateURI("uri", d.URI); err != nil {
		return err
	}
	return nil
}

// --- Note Hooks ---
// Note: Required field validation (name, content) is in models.go BeforeCreate,
// not here, because the Update path uses map-based GORM Updates() on an empty
// model struct. A BeforeSave hook would validate the empty struct's zero-value
// fields, causing false "cannot be empty" errors.

// --- Repository Hooks ---

// BeforeSave validates Repository before create or update
func (r *Repository) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateURI("uri", r.URI); err != nil {
		return err
	}
	if r.Type.Valid {
		if err := validation.ValidateRepositoryType(r.Type.String); err != nil {
			return err
		}
	}
	return nil
}

// --- Metadata Hooks ---

// BeforeSave validates Metadata before create or update
func (m *Metadata) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateEntityType(string(m.EntityType)); err != nil {
		return err
	}
	if err := validation.ValidateMetadataKey(string(m.Key)); err != nil {
		return err
	}
	if err := validation.ValidateMetadataValue(string(m.Value)); err != nil {
		return err
	}
	return nil
}

// --- CollaborationSession Hooks ---

// BeforeSave validates CollaborationSession before create or update
func (c *CollaborationSession) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateWebSocketURL(c.WebsocketURL); err != nil {
		return err
	}
	return nil
}

// --- WebhookSubscription Hooks ---

// BeforeSave validates WebhookSubscription before create or update
func (w *WebhookSubscription) BeforeSave(tx *gorm.DB) error {
	// Only validate status if it's non-empty (allows partial updates via map-based Updates)
	if w.Status != "" {
		if err := validation.ValidateWebhookStatus(string(w.Status)); err != nil {
			return err
		}
	}
	return nil
}

// --- WebhookURLDenyList Hooks ---

// BeforeSave validates WebhookURLDenyList before create or update
func (w *WebhookURLDenyList) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateWebhookPatternType(string(w.PatternType)); err != nil {
		return err
	}
	return nil
}

// --- Group Protection Hooks ---

// BeforeDelete prevents deletion of built-in groups (everyone, security-reviewers, administrators)
func (g *Group) BeforeDelete(tx *gorm.DB) error {
	if validation.IsBuiltInGroup(string(g.InternalUUID)) {
		return fmt.Errorf("cannot delete built-in group %q: %w", g.GroupName, ErrBuiltInGroupProtected)
	}
	return nil
}

// BeforeUpdate prevents renaming or changing the description of built-in groups
func (g *Group) BeforeUpdate(tx *gorm.DB) error {
	if !validation.IsBuiltInGroup(string(g.InternalUUID)) {
		return nil
	}

	// Load the current record to compare fields
	var existing Group
	if err := tx.Session(&gorm.Session{NewDB: true}).First(&existing, "internal_uuid = ?", g.InternalUUID).Error; err != nil {
		return fmt.Errorf("failed to verify built-in group: %w", err)
	}

	if g.GroupName != existing.GroupName {
		return fmt.Errorf("cannot rename built-in group %q: %w", existing.GroupName, ErrBuiltInGroupProtected)
	}

	// Check Name pointer changes
	switch {
	case !g.Name.Valid && existing.Name.Valid:
		return fmt.Errorf("cannot clear the display name of built-in group %q: %w", existing.GroupName, ErrBuiltInGroupProtected)
	case g.Name.Valid && !existing.Name.Valid:
		// Setting a name for the first time is OK (shouldn't happen for seeded groups, but safe)
	case g.Name.Valid && existing.Name.Valid && g.Name.String != existing.Name.String:
		return fmt.Errorf("cannot rename built-in group %q: %w", existing.GroupName, ErrBuiltInGroupProtected)
	}

	// Check Description changes
	switch {
	case !g.Description.Valid && existing.Description.Valid:
		return fmt.Errorf("cannot clear the description of built-in group %q: %w", existing.GroupName, ErrBuiltInGroupProtected)
	case g.Description.Valid && !existing.Description.Valid:
		// Setting a description for the first time is OK
	case g.Description.Valid && existing.Description.Valid && g.Description.String != existing.Description.String:
		return fmt.Errorf("cannot change the description of built-in group %q: %w", existing.GroupName, ErrBuiltInGroupProtected)
	}

	return nil
}

// --- GroupMember Protection Hooks ---

// BeforeSave validates GroupMember and prevents adding to "everyone" group
func (gm *GroupMember) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateNotEveryoneGroupMember(string(gm.GroupInternalUUID)); err != nil {
		return err
	}
	// Default SubjectType to "user" for backward compatibility
	if gm.SubjectType == "" {
		gm.SubjectType = "user"
	}
	if err := validation.ValidateSubjectType(string(gm.SubjectType)); err != nil {
		return err
	}
	if err := validation.ValidateSubjectXOR(string(gm.SubjectType), gm.UserInternalUUID.Ptr(), gm.MemberGroupInternalUUID.Ptr()); err != nil {
		return err
	}
	return nil
}
