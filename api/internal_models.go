package api

import (
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ThreatModelInternal is the internal representation used by stores
// It stores diagram/threat/document IDs instead of full objects for single source of truth
type ThreatModelInternal struct {
	// Core fields
	Id                   *openapi_types.UUID `json:"id,omitempty"`
	Name                 string              `json:"name"`
	Description          *string             `json:"description,omitempty"`
	Owner                string              `json:"owner"`
	ThreatModelFramework string              `json:"threat_model_framework"`
	CreatedAt            *time.Time          `json:"created_at,omitempty"`
	ModifiedAt           *time.Time          `json:"modified_at,omitempty"`
	CreatedBy            *string             `json:"created_by,omitempty"`
	IssueUri             *string             `json:"issue_uri,omitempty"`

	// Authorization (stored directly since it's small)
	Authorization []Authorization `json:"authorization"`

	// References to related entities (IDs only)
	DiagramIds  []string `json:"diagram_ids,omitempty"`
	ThreatIds   []string `json:"threat_ids,omitempty"`
	DocumentIds []string `json:"document_ids,omitempty"`
	SourceIds   []string `json:"source_ids,omitempty"`

	// Metadata stored separately in metadata table
}

// ToThreatModel converts internal representation to external API model
// This function dynamically loads related entities from their respective stores
func (tm *ThreatModelInternal) ToThreatModel() (*ThreatModel, error) {
	result := &ThreatModel{
		Id:                   tm.Id,
		Name:                 tm.Name,
		Description:          tm.Description,
		Owner:                tm.Owner,
		ThreatModelFramework: tm.ThreatModelFramework,
		CreatedAt:            tm.CreatedAt,
		ModifiedAt:           tm.ModifiedAt,
		CreatedBy:            tm.CreatedBy,
		IssueUri:             tm.IssueUri,
		Authorization:        tm.Authorization,
	}

	// Load diagrams
	if len(tm.DiagramIds) > 0 {
		diagrams := make([]Diagram, 0, len(tm.DiagramIds))
		for _, diagramId := range tm.DiagramIds {
			diagram, err := DiagramStore.Get(diagramId)
			if err != nil {
				// Skip missing diagrams but log the error
				continue
			}

			// Ensure backward compatibility for existing diagrams
			if diagram.Image == nil {
				diagram.Image = &struct {
					Svg          *[]byte `json:"svg,omitempty"`
					UpdateVector *int64  `json:"update_vector,omitempty"`
				}{}
			}

			var diagramUnion Diagram
			if err := diagramUnion.FromDfdDiagram(diagram); err != nil {
				continue
			}
			diagrams = append(diagrams, diagramUnion)
		}
		if len(diagrams) > 0 {
			result.Diagrams = &diagrams
		}
	}

	// TODO: Load threats, documents, sources similarly when needed
	// For now, we'll implement diagrams first and add others later

	return result, nil
}

// FromThreatModel converts external API model to internal representation
func (tm *ThreatModelInternal) FromThreatModel(external *ThreatModel) {
	tm.Id = external.Id
	tm.Name = external.Name
	tm.Description = external.Description
	tm.Owner = external.Owner
	tm.ThreatModelFramework = external.ThreatModelFramework
	tm.CreatedAt = external.CreatedAt
	tm.ModifiedAt = external.ModifiedAt
	tm.CreatedBy = external.CreatedBy
	tm.IssueUri = external.IssueUri
	tm.Authorization = external.Authorization

	// Extract diagram IDs
	tm.DiagramIds = []string{}
	if external.Diagrams != nil {
		for _, diagramUnion := range *external.Diagrams {
			if dfdDiag, err := diagramUnion.AsDfdDiagram(); err == nil && dfdDiag.Id != nil {
				tm.DiagramIds = append(tm.DiagramIds, dfdDiag.Id.String())
			}
		}
	}

	// TODO: Extract threat, document, source IDs when needed
}
