package api

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// Survey Metadata Methods

// GetAdminSurveyMetadata gets survey metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin request to list all survey metadata entries (reads DB)
func (s *Server) GetAdminSurveyMetadata(c *gin.Context, surveyId SurveyId) {
	s.surveyMetadata.List(c)
}

// CreateAdminSurveyMetadata creates survey metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin request to store a new survey metadata entry (mutates shared state)
func (s *Server) CreateAdminSurveyMetadata(c *gin.Context, surveyId SurveyId) {
	s.surveyMetadata.Create(c)
}

// BulkCreateAdminSurveyMetadata bulk creates survey metadata
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin request to bulk-store multiple survey metadata entries (mutates shared state)
func (s *Server) BulkCreateAdminSurveyMetadata(c *gin.Context, surveyId SurveyId) {
	s.surveyMetadata.BulkCreate(c)
}

// BulkReplaceAdminSurveyMetadata replaces all survey metadata (PUT)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin request to replace all survey metadata entries atomically (mutates shared state)
func (s *Server) BulkReplaceAdminSurveyMetadata(c *gin.Context, surveyId SurveyId) {
	s.surveyMetadata.BulkReplace(c)
}

// BulkUpsertAdminSurveyMetadata upserts survey metadata (PATCH)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin request to upsert multiple survey metadata entries (mutates shared state)
func (s *Server) BulkUpsertAdminSurveyMetadata(c *gin.Context, surveyId SurveyId) {
	s.surveyMetadata.BulkUpsert(c)
}

// DeleteAdminSurveyMetadataByKey deletes survey metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin request to delete a survey metadata entry by key (mutates shared state)
func (s *Server) DeleteAdminSurveyMetadataByKey(c *gin.Context, surveyId SurveyId, key MetadataKey) {
	s.surveyMetadata.Delete(c)
}

// GetAdminSurveyMetadataByKey gets survey metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin request to fetch a single survey metadata entry by key (reads DB)
func (s *Server) GetAdminSurveyMetadataByKey(c *gin.Context, surveyId SurveyId, key MetadataKey) {
	s.surveyMetadata.GetByKey(c)
}

// UpdateAdminSurveyMetadataByKey updates survey metadata by key
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: route admin request to update a survey metadata entry by key (mutates shared state)
func (s *Server) UpdateAdminSurveyMetadataByKey(c *gin.Context, surveyId SurveyId, key MetadataKey) {
	s.surveyMetadata.Update(c)
}

// Survey Response Metadata Methods - Intake (full CRUD)
//
// All survey-response-metadata sub-resources gate on the parent survey
// response's ACL via RequireSurveyResponseAccess. Without this check, the
// generic metadata handler only verifies the parent exists — which would
// allow any authenticated user to read/write metadata on confidential or
// other-user survey responses (T5, #357). Read paths require reader; write
// paths require writer. Existence-disclosure is collapsed into 404 by the
// helper.

// GetIntakeSurveyResponseMetadata gets intake survey response metadata
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize reader access then list all metadata for a survey response (reads DB)
func (s *Server) GetIntakeSurveyResponseMetadata(c *gin.Context, surveyResponseId SurveyResponseId) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleReader); !ok {
		return
	}
	s.surveyResponseMetadata.List(c)
}

// CreateIntakeSurveyResponseMetadata creates intake survey response metadata
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize writer access then store a new survey response metadata entry (mutates shared state)
func (s *Server) CreateIntakeSurveyResponseMetadata(c *gin.Context, surveyResponseId SurveyResponseId) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleWriter); !ok {
		return
	}
	s.surveyResponseMetadata.Create(c)
}

// BulkCreateIntakeSurveyResponseMetadata bulk creates intake survey response metadata
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize writer access then bulk-store intake survey response metadata entries (mutates shared state)
func (s *Server) BulkCreateIntakeSurveyResponseMetadata(c *gin.Context, surveyResponseId SurveyResponseId) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleWriter); !ok {
		return
	}
	s.surveyResponseMetadata.BulkCreate(c)
}

// BulkReplaceIntakeSurveyResponseMetadata replaces all survey response metadata (PUT)
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize writer access then replace all intake survey response metadata entries atomically (mutates shared state)
func (s *Server) BulkReplaceIntakeSurveyResponseMetadata(c *gin.Context, surveyResponseId SurveyResponseId) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleWriter); !ok {
		return
	}
	s.surveyResponseMetadata.BulkReplace(c)
}

// BulkUpsertIntakeSurveyResponseMetadata upserts survey response metadata (PATCH)
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize writer access then upsert intake survey response metadata entries (mutates shared state)
func (s *Server) BulkUpsertIntakeSurveyResponseMetadata(c *gin.Context, surveyResponseId SurveyResponseId) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleWriter); !ok {
		return
	}
	s.surveyResponseMetadata.BulkUpsert(c)
}

// DeleteIntakeSurveyResponseMetadataByKey deletes intake survey response metadata by key
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize writer access then delete intake survey response metadata by key (mutates shared state)
func (s *Server) DeleteIntakeSurveyResponseMetadataByKey(c *gin.Context, surveyResponseId SurveyResponseId, key MetadataKey) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleWriter); !ok {
		return
	}
	s.surveyResponseMetadata.Delete(c)
}

// GetIntakeSurveyResponseMetadataByKey gets intake survey response metadata by key
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize reader access then fetch intake survey response metadata by key (reads DB)
func (s *Server) GetIntakeSurveyResponseMetadataByKey(c *gin.Context, surveyResponseId SurveyResponseId, key MetadataKey) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleReader); !ok {
		return
	}
	s.surveyResponseMetadata.GetByKey(c)
}

// UpdateIntakeSurveyResponseMetadataByKey updates intake survey response metadata by key
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize writer access then update intake survey response metadata by key (mutates shared state)
func (s *Server) UpdateIntakeSurveyResponseMetadataByKey(c *gin.Context, surveyResponseId SurveyResponseId, key MetadataKey) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleWriter); !ok {
		return
	}
	s.surveyResponseMetadata.Update(c)
}

// Survey Response Metadata Methods - Triage (read-only)

// GetTriageSurveyResponseMetadata gets triage survey response metadata
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize reader access then list all triage survey response metadata entries (reads DB)
func (s *Server) GetTriageSurveyResponseMetadata(c *gin.Context, surveyResponseId SurveyResponseId) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleReader); !ok {
		return
	}
	s.surveyResponseMetadata.List(c)
}

// GetTriageSurveyResponseMetadataByKey gets triage survey response metadata by key
// SEM@368e91d91cb110162c64b6ea10d49562a59bf3f0: authorize reader access then fetch triage survey response metadata by key (reads DB)
func (s *Server) GetTriageSurveyResponseMetadataByKey(c *gin.Context, surveyResponseId SurveyResponseId, key MetadataKey) {
	if _, ok := RequireSurveyResponseAccess(c, surveyResponseId, AuthorizationRoleReader); !ok {
		return
	}
	s.surveyResponseMetadata.GetByKey(c)
}

// Survey Response Triage Notes Methods - Triage (create + read)

// ListTriageSurveyResponseTriageNotes lists triage notes for a survey response
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list triage notes for a survey response via the triage stage (reads DB)
func (s *Server) ListTriageSurveyResponseTriageNotes(c *gin.Context, surveyResponseId SurveyResponseId, params ListTriageSurveyResponseTriageNotesParams) {
	c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: surveyResponseId.String()})
	s.triageNoteHandler.ListTriageNotes(c)
}

// CreateTriageSurveyResponseTriageNote creates a triage note
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: store a new triage note for a survey response via the triage stage (mutates shared state)
func (s *Server) CreateTriageSurveyResponseTriageNote(c *gin.Context, surveyResponseId SurveyResponseId) {
	c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: surveyResponseId.String()})
	s.triageNoteHandler.CreateTriageNote(c)
}

// GetTriageSurveyResponseTriageNote gets a specific triage note
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch a single triage note for a survey response via the triage stage (reads DB)
func (s *Server) GetTriageSurveyResponseTriageNote(c *gin.Context, surveyResponseId SurveyResponseId, triageNoteId TriageNoteId) {
	c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: surveyResponseId.String()})
	c.Params = append(c.Params, gin.Param{Key: "triage_note_id", Value: strconv.Itoa(triageNoteId)})
	s.triageNoteHandler.GetTriageNote(c)
}

// Survey Response Triage Notes Methods - Intake (read-only)

// ListIntakeSurveyResponseTriageNotes lists triage notes for submitter (read-only)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: list triage notes for a survey response via the intake stage (read-only) (reads DB)
func (s *Server) ListIntakeSurveyResponseTriageNotes(c *gin.Context, surveyResponseId SurveyResponseId, params ListIntakeSurveyResponseTriageNotesParams) {
	c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: surveyResponseId.String()})
	s.triageNoteHandler.ListTriageNotes(c)
}

// GetIntakeSurveyResponseTriageNote gets a specific triage note for submitter (read-only)
// SEM@28792aa3991e394010e49c040d3db2d5f14a6eff: fetch a single triage note for a survey response via the intake stage (read-only) (reads DB)
func (s *Server) GetIntakeSurveyResponseTriageNote(c *gin.Context, surveyResponseId SurveyResponseId, triageNoteId TriageNoteId) {
	c.Params = append(c.Params, gin.Param{Key: "survey_response_id", Value: surveyResponseId.String()})
	c.Params = append(c.Params, gin.Param{Key: "triage_note_id", Value: strconv.Itoa(triageNoteId)})
	s.triageNoteHandler.GetTriageNote(c)
}
