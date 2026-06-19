package api

import (
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ThreatModelInternal is the internal representation used by stores
// It stores diagram/threat/document IDs instead of full objects for single source of truth
// SEM@e28c0cfc627a2162c9550e53fb320facb734179e: internal threat model representation storing related entity IDs instead of full objects (pure)
type ThreatModelInternal struct {
	// Core fields
	Id                   *openapi_types.UUID `json:"id,omitempty"`
	Name                 string              `json:"name"`
	Description          *string             `json:"description,omitempty"`
	Owner                User                `json:"owner"`
	ThreatModelFramework string              `json:"threat_model_framework"`
	CreatedAt            *time.Time          `json:"created_at,omitempty"`
	ModifiedAt           *time.Time          `json:"modified_at,omitempty"`
	CreatedBy            *User               `json:"created_by,omitempty"`
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
// SEM@d48970168f241f7cb359d0cfdb00f3e26abb59da: convert the internal threat model to the API DTO, loading related diagrams from stores (reads DB)
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
		Authorization:        &tm.Authorization,
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
// SEM@d48970168f241f7cb359d0cfdb00f3e26abb59da: populate the internal threat model from an API DTO, extracting related entity IDs (pure)
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
	if external.Authorization != nil {
		tm.Authorization = *external.Authorization
	} else {
		tm.Authorization = nil
	}

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
