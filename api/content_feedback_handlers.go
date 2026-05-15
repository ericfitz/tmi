package api

import (
	"errors"
	"net/http"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// validFalsePositiveSubreasons maps each reason to its allowed subreasons.
// A reason missing from the map (or with an empty slice) means "no subreason allowed."
var validFalsePositiveSubreasons = map[string][]string{
	"detection_misfired":    {"code_does_not_exist", "trigger_conditions_not_met"},
	"out_of_scope":          {"component_outside_threat_model"},
	"intended_behavior":     {"sanctioned_by_design"},
	"detection_rule_flawed": {"not_a_real_risk", "needs_tuning"},
	// real_but_mitigated, real_but_not_exploitable, duplicate, already_remediated: no subreasons
}

// ContentFeedbackHandler bundles the three /threat_models/{id}/feedback endpoints.
type ContentFeedbackHandler struct {
	repo ContentFeedbackRepository
	db   *gorm.DB // used for target-existence checks
}

// NewContentFeedbackHandler constructs the handler.
func NewContentFeedbackHandler(repo ContentFeedbackRepository, db *gorm.DB) *ContentFeedbackHandler {
	return &ContentFeedbackHandler{repo: repo, db: db}
}

// Create handles POST /threat_models/{threat_model_id}/feedback.
func (h *ContentFeedbackHandler) Create(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	tmID := c.Param("threat_model_id")
	if _, err := uuid.Parse(tmID); err != nil {
		HandleRequestError(c, InvalidIDError("threat_model_id must be a UUID"))
		return
	}

	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if user.InternalUUID == "" {
		HandleRequestError(c, ServerError("authenticated user has no internal UUID"))
		return
	}

	var input ContentFeedbackInput
	if err := c.ShouldBindJSON(&input); err != nil {
		HandleRequestError(c, InvalidInputError("invalid request body: "+err.Error()))
		return
	}

	if err := validateContentFeedbackInput(&input); err != nil {
		HandleRequestError(c, err)
		return
	}

	target, err := resolveContentFeedbackTarget(tmID, &input)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	row := buildContentFeedbackModel(&input, tmID, user.InternalUUID)
	if err := h.repo.CreateWithTargetCheck(c.Request.Context(), row, target); err != nil {
		if errors.Is(err, ErrContentFeedbackTargetNotFound) {
			HandleRequestError(c, InvalidInputError("target_id not found in this threat model"))
			return
		}
		logger.Error("ContentFeedback create failed: %v", err)
		HandleRequestError(c, mapDBError(err))
		return
	}

	c.JSON(http.StatusCreated, modelToContentFeedback(row))
}

// Get handles GET /threat_models/{threat_model_id}/feedback/{feedback_id}.
func (h *ContentFeedbackHandler) Get(c *gin.Context) {
	tmID := c.Param("threat_model_id")
	if _, err := uuid.Parse(tmID); err != nil {
		HandleRequestError(c, InvalidIDError("threat_model_id must be a UUID"))
		return
	}
	fbID := c.Param("feedback_id")
	if _, err := uuid.Parse(fbID); err != nil {
		HandleRequestError(c, InvalidIDError("feedback_id must be a UUID"))
		return
	}

	row, err := h.repo.Get(c.Request.Context(), fbID)
	if err != nil {
		HandleRequestError(c, mapDBError(err))
		return
	}
	if string(row.ThreatModelID) != tmID {
		// Don't leak existence across TMs — return 404.
		HandleRequestError(c, NotFoundError("feedback not found"))
		return
	}
	c.JSON(http.StatusOK, modelToContentFeedback(row))
}

// List handles GET /threat_models/{threat_model_id}/feedback.
func (h *ContentFeedbackHandler) List(c *gin.Context) {
	tmID := c.Param("threat_model_id")
	if _, err := uuid.Parse(tmID); err != nil {
		HandleRequestError(c, InvalidIDError("threat_model_id must be a UUID"))
		return
	}

	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)
	if limit < 1 || limit > 100 {
		HandleRequestError(c, InvalidInputError("limit must be 1..100"))
		return
	}
	if offset < 0 {
		HandleRequestError(c, InvalidInputError("offset must be >= 0"))
		return
	}

	filter := ContentFeedbackListFilter{
		TargetType:          c.Query("target_type"),
		TargetID:            c.Query("target_id"),
		Sentiment:           c.Query("sentiment"),
		FalsePositiveReason: c.Query("false_positive_reason"),
	}

	rows, err := h.repo.List(c.Request.Context(), tmID, filter, offset, limit)
	if err != nil {
		HandleRequestError(c, mapDBError(err))
		return
	}
	total, err := h.repo.Count(c.Request.Context(), tmID, filter)
	if err != nil {
		HandleRequestError(c, mapDBError(err))
		return
	}

	items := make([]ContentFeedback, 0, len(rows))
	for i := range rows {
		items = append(items, modelToContentFeedback(&rows[i]))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func validateContentFeedbackInput(in *ContentFeedbackInput) error {
	if in.Sentiment != ContentFeedbackInputSentimentUp && in.Sentiment != ContentFeedbackInputSentimentDown {
		return InvalidInputError("sentiment must be 'up' or 'down'")
	}

	switch in.TargetType {
	case ContentFeedbackInputTargetTypeNote, ContentFeedbackInputTargetTypeDiagram, ContentFeedbackInputTargetTypeThreat:
		if in.TargetField != nil {
			return InvalidInputError("target_field is allowed only for target_type=threat_classification")
		}
	case ContentFeedbackInputTargetTypeThreatClassification:
		if in.TargetField == nil || *in.TargetField == "" {
			return InvalidInputError("target_field is required for target_type=threat_classification")
		}
		if len(*in.TargetField) > 64 {
			return InvalidInputError("target_field exceeds 64 chars")
		}
	default:
		return InvalidInputError("target_type must be note|diagram|threat|threat_classification")
	}

	if !clientIDRegex.MatchString(in.ClientId) {
		return InvalidInputError("client_id does not match required pattern")
	}
	if in.ClientVersion != nil && len(*in.ClientVersion) > 32 {
		return InvalidInputError("client_version too long")
	}
	if in.Verbatim != nil && len(*in.Verbatim) > maxVerbatimBytes {
		return PayloadTooLargeError("verbatim exceeds max length")
	}
	if err := validateScreenshot(in.Screenshot); err != nil {
		return err
	}

	// false_positive_reason allowed only when sentiment=down AND target_type=threat.
	if in.FalsePositiveReason != nil {
		if in.Sentiment != ContentFeedbackInputSentimentDown {
			return InvalidInputError("false_positive_reason allowed only when sentiment=down")
		}
		if in.TargetType != ContentFeedbackInputTargetTypeThreat {
			return InvalidInputError("false_positive_reason allowed only when target_type=threat")
		}
		reason := string(*in.FalsePositiveReason)
		allowedSubs, isReason := validFalsePositiveSubreasons[reason]
		// Confirm reason is in the enum (oapi-codegen already validates the enum but we double-check).
		if !isReason && !isReasonWithoutSubreasons(reason) {
			return InvalidInputError("false_positive_reason is not a valid value")
		}

		if in.FalsePositiveSubreason != nil {
			if !isReason {
				return InvalidInputError("false_positive_subreason not allowed for this reason")
			}
			sub := string(*in.FalsePositiveSubreason)
			if !containsStr(allowedSubs, sub) {
				return InvalidInputError("false_positive_subreason not valid for chosen reason")
			}
		}
	} else if in.FalsePositiveSubreason != nil {
		return InvalidInputError("false_positive_subreason requires false_positive_reason")
	}

	return nil
}

func isReasonWithoutSubreasons(reason string) bool {
	switch reason {
	case "real_but_mitigated", "real_but_not_exploitable", "duplicate", "already_remediated":
		return true
	}
	return false
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// resolveContentFeedbackTarget maps the input's target_type to the GORM table
// name and returns the ref the repository uses to perform the locked
// existence check inside the create transaction.
func resolveContentFeedbackTarget(tmID string, in *ContentFeedbackInput) (ContentFeedbackTargetRef, error) {
	var table string
	switch in.TargetType {
	case ContentFeedbackInputTargetTypeNote:
		table = models.Note{}.TableName()
	case ContentFeedbackInputTargetTypeDiagram:
		table = models.Diagram{}.TableName()
	case ContentFeedbackInputTargetTypeThreat, ContentFeedbackInputTargetTypeThreatClassification:
		table = models.Threat{}.TableName()
	}
	if table == "" {
		return ContentFeedbackTargetRef{}, InvalidInputError("invalid target_type")
	}
	return ContentFeedbackTargetRef{
		Table:         table,
		TargetID:      in.TargetId.String(),
		ThreatModelID: tmID,
	}, nil
}

func buildContentFeedbackModel(in *ContentFeedbackInput, tmID, userInternalUUID string) *models.ContentFeedback {
	row := &models.ContentFeedback{
		ThreatModelID: models.DBVarchar(tmID),
		TargetType:    models.DBVarchar(string(in.TargetType)),
		TargetID:      models.DBVarchar(in.TargetId.String()),
		TargetField:   models.NewNullableDBVarchar(in.TargetField),
		Sentiment:     models.DBVarchar(string(in.Sentiment)),
		Verbatim:      models.NewNullableDBText(in.Verbatim),
		ClientID:      models.DBVarchar(in.ClientId),
		ClientVersion: models.NewNullableDBVarchar(in.ClientVersion),
		Screenshot:    models.NewNullableDBText(in.Screenshot),
		CreatedByUUID: models.DBVarchar(userInternalUUID),
	}
	if in.FalsePositiveReason != nil {
		s := string(*in.FalsePositiveReason)
		row.FalsePositiveReason = models.NewNullableDBVarchar(&s)
	}
	if in.FalsePositiveSubreason != nil {
		s := string(*in.FalsePositiveSubreason)
		row.FalsePositiveSubreason = models.NewNullableDBVarchar(&s)
	}
	return row
}

func modelToContentFeedback(row *models.ContentFeedback) ContentFeedback {
	out := ContentFeedback{
		Id:            uuidMustParse(string(row.ID)),
		ThreatModelId: uuidMustParse(string(row.ThreatModelID)),
		TargetType:    ContentFeedbackTargetType(string(row.TargetType)),
		TargetId:      uuidMustParse(string(row.TargetID)),
		TargetField:   row.TargetField.Ptr(),
		Sentiment:     ContentFeedbackSentiment(string(row.Sentiment)),
		Verbatim:      row.Verbatim.Ptr(),
		ClientId:      string(row.ClientID),
		ClientVersion: row.ClientVersion.Ptr(),
		Screenshot:    row.Screenshot.Ptr(),
		CreatedBy:     uuidMustParse(string(row.CreatedByUUID)),
		CreatedAt:     row.CreatedAt,
	}
	if row.FalsePositiveReason.Valid {
		v := ContentFeedbackFalsePositiveReason(row.FalsePositiveReason.String)
		out.FalsePositiveReason = &v
	}
	if row.FalsePositiveSubreason.Valid {
		v := ContentFeedbackFalsePositiveSubreason(row.FalsePositiveSubreason.String)
		out.FalsePositiveSubreason = &v
	}
	return out
}
