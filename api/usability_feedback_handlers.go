package api

import (
	"context"
	"encoding/base64"
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
	// maxScreenshotBytes mirrors the OpenAPI screenshot.maxLength. Headroom over
	// the ~150–400 KB the tmi-ux client actually produces (ericfitz/tmi-ux@aec93072).
	maxScreenshotBytes = 2_000_000
)

var (
	surfaceRegex     = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,31}$`)
	clientIDRegex    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	clientBuildRegex = regexp.MustCompile(`^[0-9a-f]{7,12}$`)
	viewportRegex    = regexp.MustCompile(`^\d{1,5}x\d{1,5}$`)
	// screenshotPrefixRegex matches the MIME-prefix portion only. The base64 body
	// is validated separately with encoding/base64 so we don't pay for a
	// 2 MB regex backtrack.
	screenshotPrefixRegex = regexp.MustCompile(`^data:image/(jpeg|png|webp);base64,`)
)

// UsabilityFeedbackHandler bundles the three endpoints for /usability_feedback*.
// SEM@72f2ef0deaad62ae1c2054ae42a059a253d123b7: handle HTTP endpoints for the usability feedback resource (reads DB)
type UsabilityFeedbackHandler struct {
	repo UsabilityFeedbackRepository
}

// NewUsabilityFeedbackHandler constructs the handler.
// SEM@72f2ef0deaad62ae1c2054ae42a059a253d123b7: build a UsabilityFeedbackHandler wired to a given repository (pure)
func NewUsabilityFeedbackHandler(repo UsabilityFeedbackRepository) *UsabilityFeedbackHandler {
	return &UsabilityFeedbackHandler{repo: repo}
}

// Create handles POST /usability_feedback.
// SEM@72f2ef0deaad62ae1c2054ae42a059a253d123b7: store a new usability feedback record for the authenticated user (reads DB)
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
// SEM@72f2ef0deaad62ae1c2054ae42a059a253d123b7: fetch a single usability feedback record by UUID (reads DB)
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
// SEM@72f2ef0deaad62ae1c2054ae42a059a253d123b7: list paginated usability feedback records with optional filters (reads DB)
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
// SEM@7a37ede92fcea149df69a3f3e95d1b6f9c58d526: validate all fields of a usability feedback submission against format and size rules (pure)
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
	if err := validateScreenshot(in.Screenshot); err != nil {
		return err
	}
	return nil
}

// validateScreenshot enforces the data-URL contract on the screenshot field:
// recognised image MIME prefix, valid base64 body, total length within the cap.
// Nil pointer (omitted field) is always valid.
// SEM@7a37ede92fcea149df69a3f3e95d1b6f9c58d526: validate a screenshot data URL has an allowed MIME prefix, valid base64 body, and fits the size cap (pure)
func validateScreenshot(s *string) error {
	if s == nil {
		return nil
	}
	if len(*s) > maxScreenshotBytes {
		return PayloadTooLargeError("screenshot exceeds " + strconv.Itoa(maxScreenshotBytes) + " bytes")
	}
	loc := screenshotPrefixRegex.FindStringIndex(*s)
	if loc == nil {
		return InvalidInputError("screenshot must be a data URL of the form data:image/(jpeg|png|webp);base64,…")
	}
	body := (*s)[loc[1]:]
	if body == "" {
		return InvalidInputError("screenshot data URL has empty base64 body")
	}
	if _, err := base64.StdEncoding.DecodeString(body); err != nil {
		return InvalidInputError("screenshot base64 body is not valid: " + err.Error())
	}
	return nil
}

// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: convert a usability feedback input DTO to a DB model (pure)
func buildUsabilityFeedbackModel(in *UsabilityFeedbackInput, userInternalUUID string) *models.UsabilityFeedback {
	row := &models.UsabilityFeedback{
		Sentiment:     models.DBVarchar(string(in.Sentiment)),
		Surface:       models.DBVarchar(in.Surface),
		ClientID:      models.DBVarchar(in.ClientId),
		Verbatim:      models.NewNullableDBText(in.Verbatim),
		ClientVersion: models.NewNullableDBVarchar(in.ClientVersion),
		ClientBuild:   models.NewNullableDBVarchar(in.ClientBuild),
		UserAgent:     models.NewNullableDBVarchar(in.UserAgent),
		Viewport:      models.NewNullableDBVarchar(in.Viewport),
		Screenshot:    models.NewNullableDBText(in.Screenshot),
		CreatedByUUID: models.DBVarchar(userInternalUUID),
	}
	if in.UserAgentData != nil {
		buf, _ := json.Marshal(in.UserAgentData)
		row.UserAgentData = models.JSONRaw(buf)
	}
	return row
}

// SEM@5dfa9dcf64aa0662920dbbab3bca200db1b22c73: convert a DB usability feedback model to an API response DTO (pure)
func modelToUsabilityFeedback(row *models.UsabilityFeedback) UsabilityFeedback {
	out := UsabilityFeedback{
		Id:            uuidMustParse(string(row.ID)),
		Sentiment:     UsabilityFeedbackSentiment(string(row.Sentiment)),
		Surface:       string(row.Surface),
		ClientId:      string(row.ClientID),
		ClientVersion: row.ClientVersion.Ptr(),
		ClientBuild:   row.ClientBuild.Ptr(),
		UserAgent:     row.UserAgent.Ptr(),
		Verbatim:      row.Verbatim.Ptr(),
		Viewport:      row.Viewport.Ptr(),
		Screenshot:    row.Screenshot.Ptr(),
		CreatedBy:     uuidMustParse(string(row.CreatedByUUID)),
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
// SEM@72f2ef0deaad62ae1c2054ae42a059a253d123b7: convert a typed DB error to the appropriate HTTP request error (pure)
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
// SEM@72f2ef0deaad62ae1c2054ae42a059a253d123b7: build a 413 Payload Too Large request error (pure)
func PayloadTooLargeError(msg string) error {
	return &RequestError{Status: http.StatusRequestEntityTooLarge, Code: "payload_too_large", Message: msg}
}

// uuidMustParse parses a UUID string to openapi_types.UUID. The string is
// trusted (it comes from our own DB rows). On parse failure returns the zero
// UUID and logs.
// SEM@72f2ef0deaad62ae1c2054ae42a059a253d123b7: parse a trusted UUID string to openapi_types.UUID, returning zero UUID on failure (pure)
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
