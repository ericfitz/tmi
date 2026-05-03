package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

const (
	maxVerbatimBytes      = 2048
	maxUserAgentDataBytes = 4096
)

var (
	surfaceRegex     = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,31}$`)
	clientIDRegex    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	clientBuildRegex = regexp.MustCompile(`^[0-9a-f]{7,12}$`)
	viewportRegex    = regexp.MustCompile(`^\d{1,5}x\d{1,5}$`)
)

// UsabilityFeedbackHandler bundles the three endpoints for /usability_feedback*.
type UsabilityFeedbackHandler struct {
	repo UsabilityFeedbackRepository
}

// NewUsabilityFeedbackHandler constructs the handler.
func NewUsabilityFeedbackHandler(repo UsabilityFeedbackRepository) *UsabilityFeedbackHandler {
	return &UsabilityFeedbackHandler{repo: repo}
}

// Create handles POST /usability_feedback.
func (h *UsabilityFeedbackHandler) Create(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if user.InternalUUID == "" {
		HandleRequestError(c, ServerError("authenticated user has no internal UUID"))
		return
	}

	var input UsabilityFeedbackInput
	if err := c.ShouldBindJSON(&input); err != nil {
		HandleRequestError(c, InvalidInputError("invalid request body: "+err.Error()))
		return
	}

	if err := validateUsabilityFeedbackInput(&input); err != nil {
		HandleRequestError(c, err)
		return
	}

	row := buildUsabilityFeedbackModel(&input, user.InternalUUID)
	if err := h.repo.Create(c.Request.Context(), row); err != nil {
		logger.Error("UsabilityFeedback create failed: %v", err)
		HandleRequestError(c, mapDBError(err))
		return
	}

	c.JSON(http.StatusCreated, modelToUsabilityFeedback(row))
}

// Get handles GET /usability_feedback/{id}.
func (h *UsabilityFeedbackHandler) Get(c *gin.Context) {
	id := c.Param("id")
	if _, err := uuid.Parse(id); err != nil {
		HandleRequestError(c, InvalidIDError("id must be a UUID"))
		return
	}
	row, err := h.repo.Get(c.Request.Context(), id)
	if err != nil {
		HandleRequestError(c, mapDBError(err))
		return
	}
	c.JSON(http.StatusOK, modelToUsabilityFeedback(row))
}

// List handles GET /usability_feedback with filters and pagination.
func (h *UsabilityFeedbackHandler) List(c *gin.Context) {
	limit := parseIntParam(c.DefaultQuery("limit", "50"), 50)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)
	if limit < 1 || limit > 1000 {
		HandleRequestError(c, InvalidInputError("limit must be 1..1000"))
		return
	}
	if offset < 0 {
		HandleRequestError(c, InvalidInputError("offset must be >= 0"))
		return
	}

	filter := UsabilityFeedbackListFilter{
		Sentiment: c.Query("sentiment"),
		ClientID:  c.Query("client_id"),
		Surface:   c.Query("surface"),
	}
	if v := c.Query("created_after"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			HandleRequestError(c, InvalidInputError("created_after must be RFC3339"))
			return
		}
		filter.CreatedAfter = t
	}
	if v := c.Query("created_before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			HandleRequestError(c, InvalidInputError("created_before must be RFC3339"))
			return
		}
		filter.CreatedBefore = t
	}

	rows, err := h.repo.List(c.Request.Context(), filter, offset, limit)
	if err != nil {
		HandleRequestError(c, mapDBError(err))
		return
	}
	total, err := h.repo.Count(c.Request.Context(), filter)
	if err != nil {
		HandleRequestError(c, mapDBError(err))
		return
	}

	items := make([]UsabilityFeedback, 0, len(rows))
	for i := range rows {
		items = append(items, modelToUsabilityFeedback(&rows[i]))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

// validateUsabilityFeedbackInput enforces the field rules from the design.
func validateUsabilityFeedbackInput(in *UsabilityFeedbackInput) error {
	if in.Sentiment != UsabilityFeedbackInputSentimentUp && in.Sentiment != UsabilityFeedbackInputSentimentDown {
		return InvalidInputError("sentiment must be 'up' or 'down'")
	}
	if !surfaceRegex.MatchString(in.Surface) {
		return InvalidInputError("surface does not match required pattern")
	}
	if !clientIDRegex.MatchString(in.ClientId) {
		return InvalidInputError("client_id does not match required pattern")
	}
	if in.ClientVersion != nil && len(*in.ClientVersion) > 32 {
		return InvalidInputError("client_version too long")
	}
	if in.ClientBuild != nil && !clientBuildRegex.MatchString(*in.ClientBuild) {
		return InvalidInputError("client_build does not match required pattern")
	}
	if in.UserAgent != nil && len(*in.UserAgent) > 512 {
		return InvalidInputError("user_agent too long")
	}
	if in.Viewport != nil && !viewportRegex.MatchString(*in.Viewport) {
		return InvalidInputError("viewport must match \\d{1,5}x\\d{1,5}")
	}
	if in.Verbatim != nil && len(*in.Verbatim) > maxVerbatimBytes {
		return PayloadTooLargeError("verbatim exceeds " + strconv.Itoa(maxVerbatimBytes) + " bytes")
	}
	if in.UserAgentData != nil {
		buf, err := json.Marshal(in.UserAgentData)
		if err != nil {
			return InvalidInputError("user_agent_data must be a JSON object")
		}
		if len(buf) > maxUserAgentDataBytes {
			return PayloadTooLargeError("user_agent_data exceeds " + strconv.Itoa(maxUserAgentDataBytes) + " bytes")
		}
	}
	return nil
}

func buildUsabilityFeedbackModel(in *UsabilityFeedbackInput, userInternalUUID string) *models.UsabilityFeedback {
	row := &models.UsabilityFeedback{
		Sentiment:     string(in.Sentiment),
		Surface:       in.Surface,
		ClientID:      in.ClientId,
		Verbatim:      in.Verbatim,
		ClientVersion: in.ClientVersion,
		ClientBuild:   in.ClientBuild,
		UserAgent:     in.UserAgent,
		Viewport:      in.Viewport,
		CreatedByUUID: userInternalUUID,
	}
	if in.UserAgentData != nil {
		buf, _ := json.Marshal(in.UserAgentData)
		row.UserAgentData = models.JSONRaw(buf)
	}
	return row
}

func modelToUsabilityFeedback(row *models.UsabilityFeedback) UsabilityFeedback {
	out := UsabilityFeedback{
		Id:            uuidMustParse(row.ID),
		Sentiment:     UsabilityFeedbackSentiment(row.Sentiment),
		Surface:       row.Surface,
		ClientId:      row.ClientID,
		ClientVersion: row.ClientVersion,
		ClientBuild:   row.ClientBuild,
		UserAgent:     row.UserAgent,
		Verbatim:      row.Verbatim,
		Viewport:      row.Viewport,
		CreatedBy:     uuidMustParse(row.CreatedByUUID),
		CreatedAt:     row.CreatedAt,
	}
	if len(row.UserAgentData) > 0 {
		var v map[string]any
		if err := json.Unmarshal(row.UserAgentData, &v); err == nil {
			out.UserAgentData = &v
		}
	}
	return out
}

// mapDBError converts a typed DB error to a RequestError.
func mapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, dberrors.ErrNotFound) {
		return NotFoundError("not found")
	}
	if errors.Is(err, dberrors.ErrTransient) {
		return &RequestError{Status: http.StatusServiceUnavailable, Code: "service_unavailable", Message: "transient database error, retry"}
	}
	return ServerError("database error")
}

// PayloadTooLargeError returns a 413 RequestError.
func PayloadTooLargeError(msg string) error {
	return &RequestError{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: msg}
}

// uuidMustParse parses a UUID string to openapi_types.UUID. The string is
// trusted (it comes from our own DB rows). On parse failure returns the zero
// UUID and logs.
func uuidMustParse(s string) openapi_types.UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		slogging.Get().Error("uuidMustParse: invalid UUID %q: %v", s, err)
		return openapi_types.UUID{}
	}
	return u
}

// avoid unused "context" import when context is referenced through repo.
var _ = context.Background
