package api

import (
	"context"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// RequireSurveyResponseAccess centralizes the existence check + access check
// for survey-response sub-resources. It is the single enforcement point for
// the four-question authorization decision on /intake/survey_responses/* and
// /triage/survey_responses/* paths.
//
// On success, it returns the loaded survey response. On any failure
// (not found, access denied, internal error) it writes the appropriate error
// to the Gin context and returns (nil, false). Callers must return immediately
// when the second return is false.
//
// Confidentiality / existence-disclosure rule (T5, #357):
// To avoid leaking the existence of a survey response that the caller cannot
// read, both "not found" and "access denied" surface as 404. A leaking 403
// would let an attacker probe UUIDs to learn which survey responses exist.
//
// Authorization model:
// Survey responses have their own per-row ACL stored in
// `survey_response_access`. The store's HasAccess(...) method evaluates the
// caller's direct grants AND group memberships against the required role
// (reader / writer / owner) using the standard role hierarchy
// (owner > writer > reader). This helper is a thin wrapper that adds the
// existence check and unifies the response shape so handlers do not
// duplicate the load + check + 403/404 dance and do not accidentally diverge
// on the disclosure question.
func RequireSurveyResponseAccess(
	c *gin.Context,
	surveyResponseID SurveyResponseId,
	requiredRole AuthorizationRole,
) (*SurveyResponse, bool) {
	logger := slogging.Get().WithContext(c)
	ctx := c.Request.Context()

	userUUID, ok := getUserUUID(c)
	if !ok {
		return nil, false
	}

	response, err := GlobalSurveyResponseStore.Get(ctx, surveyResponseID)
	if err != nil {
		logger.Error("RequireSurveyResponseAccess: failed to get survey response %s: %v",
			surveyResponseID.String(), err)
		HandleRequestError(c, ServerError("Failed to get survey response"))
		return nil, false
	}

	if response == nil {
		HandleRequestError(c, NotFoundError("Survey response not found"))
		return nil, false
	}

	hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, surveyResponseID, userUUID, requiredRole)
	if err != nil {
		logger.Error("RequireSurveyResponseAccess: HasAccess failed for response %s user %s: %v",
			surveyResponseID.String(), userUUID, err)
		HandleRequestError(c, ServerError("Failed to check access"))
		return nil, false
	}

	if !hasAccess {
		// Return 404 (not 403) to avoid leaking existence of a resource the
		// caller cannot read. See T5 (#357) — non-member reading a confidential
		// parent's data must not be distinguishable from "no such resource".
		logger.Debug("RequireSurveyResponseAccess: access denied for user %s on response %s (required=%s) — returning 404",
			userUUID, surveyResponseID.String(), requiredRole)
		HandleRequestError(c, NotFoundError("Survey response not found"))
		return nil, false
	}

	return response, true
}

// FilterSurveyResponseListItemsByAccess narrows a list of survey-response list
// items to only those the caller can read. Used by triage/intake list endpoints
// where the underlying store query does not enforce per-row ACL (e.g.
// ListTriageSurveyResponses, which intentionally does not filter by owner).
//
// This is an in-memory filter against the same HasAccess(...) the per-row
// handlers use, so list and per-row decisions stay in sync. The returned
// total reflects the filtered count, so pagination metadata never leaks the
// existence of confidential responses the caller cannot read (matching the
// list-leak acceptance criterion in #357).
func FilterSurveyResponseListItemsByAccess(
	ctx context.Context,
	userInternalUUID string,
	items []SurveyResponseListItem,
	requiredRole AuthorizationRole,
) []SurveyResponseListItem {
	if len(items) == 0 || GlobalSurveyResponseStore == nil {
		return items
	}
	out := make([]SurveyResponseListItem, 0, len(items))
	logger := slogging.Get()
	for _, item := range items {
		if item.Id == nil {
			continue
		}
		hasAccess, err := GlobalSurveyResponseStore.HasAccess(ctx, *item.Id, userInternalUUID, requiredRole)
		if err != nil {
			logger.Warn("FilterSurveyResponseListItemsByAccess: HasAccess error for response %s user %s: %v — excluding from list",
				item.Id.String(), userInternalUUID, err)
			continue
		}
		if hasAccess {
			out = append(out, item)
		}
	}
	return out
}
