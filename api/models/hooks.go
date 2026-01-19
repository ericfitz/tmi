// Package models - hooks.go contains GORM lifecycle hooks for validation.
// These hooks replace PostgreSQL CHECK constraints and triggers to enable
// consistent validation across all supported databases.
package models

import (
	"errors"

	"github.com/ericfitz/tmi/api/validation"
	"gorm.io/gorm"
)

// --- ThreatModel Hooks ---

// BeforeUpdate validates ThreatModel before update
func (t *ThreatModel) BeforeUpdate(tx *gorm.DB) error {
	// Only validate framework if it's being updated (non-empty)
	// Empty framework means the field wasn't included in the update
	if t.ThreatModelFramework != "" {
		if err := validation.ValidateThreatModelFramework(t.ThreatModelFramework); err != nil {
			return err
		}
	}
	if err := validation.ValidateStatusLength(t.Status); err != nil {
		return err
	}
	return nil
}

// --- Diagram Hooks ---

// BeforeUpdate validates Diagram before update
func (d *Diagram) BeforeUpdate(tx *gorm.DB) error {
	if d.Type != nil {
		if err := validation.ValidateDiagramType(*d.Type); err != nil {
			return err
		}
	}
	return nil
}

// --- Asset Hooks ---

// BeforeSave validates Asset before create or update
func (a *Asset) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateNonEmpty("name", a.Name); err != nil {
		return err
	}
	if err := validation.ValidateAssetType(a.Type); err != nil {
		return err
	}
	return nil
}

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
	if err := validation.ValidateSubjectType(t.SubjectType); err != nil {
		return err
	}
	if err := validation.ValidateRole(t.Role); err != nil {
		return err
	}
	if err := validation.ValidateSubjectXOR(t.SubjectType, t.UserInternalUUID, t.GroupInternalUUID); err != nil {
		return err
	}
	return nil
}

// --- Document Hooks ---

// BeforeSave validates Document before create or update
func (d *Document) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateNonEmpty("name", d.Name); err != nil {
		return err
	}
	if err := validation.ValidateURI("uri", d.URI); err != nil {
		return err
	}
	return nil
}

// --- Note Hooks ---

// BeforeSave validates Note before create or update
func (n *Note) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateNonEmpty("name", n.Name); err != nil {
		return err
	}
	if err := validation.ValidateNonEmpty("content", string(n.Content)); err != nil {
		return err
	}
	return nil
}

// --- Repository Hooks ---

// BeforeSave validates Repository before create or update
func (r *Repository) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateURI("uri", r.URI); err != nil {
		return err
	}
	if r.Type != nil {
		if err := validation.ValidateRepositoryType(*r.Type); err != nil {
			return err
		}
	}
	return nil
}

// --- Metadata Hooks ---

// BeforeSave validates Metadata before create or update
func (m *Metadata) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateEntityType(m.EntityType); err != nil {
		return err
	}
	if err := validation.ValidateMetadataKey(m.Key); err != nil {
		return err
	}
	if err := validation.ValidateMetadataValue(m.Value); err != nil {
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
	if err := validation.ValidateWebhookStatus(w.Status); err != nil {
		return err
	}
	return nil
}

// --- WebhookDelivery Hooks ---

// BeforeSave validates WebhookDelivery before create or update
func (w *WebhookDelivery) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateWebhookDeliveryStatus(w.Status); err != nil {
		return err
	}
	return nil
}

// --- WebhookURLDenyList Hooks ---

// BeforeSave validates WebhookURLDenyList before create or update
func (w *WebhookURLDenyList) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateWebhookPatternType(w.PatternType); err != nil {
		return err
	}
	return nil
}

// --- Administrator Hooks ---

// BeforeSave validates Administrator before create or update
func (a *Administrator) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateSubjectType(a.SubjectType); err != nil {
		return err
	}
	if err := validation.ValidateSubjectXOR(a.SubjectType, a.UserInternalUUID, a.GroupInternalUUID); err != nil {
		return err
	}
	return nil
}

// --- Group Protection Hooks ---

// BeforeDelete prevents deletion of the "everyone" pseudo-group
func (g *Group) BeforeDelete(tx *gorm.DB) error {
	if g.InternalUUID == validation.EveryonePseudoGroupUUID {
		return errors.New("cannot delete the 'everyone' pseudo-group")
	}
	return nil
}

// --- GroupMember Protection Hooks ---

// BeforeSave validates GroupMember and prevents adding to "everyone" group
func (gm *GroupMember) BeforeSave(tx *gorm.DB) error {
	if err := validation.ValidateNotEveryoneGroupMember(gm.GroupInternalUUID); err != nil {
		return err
	}
	return nil
}
