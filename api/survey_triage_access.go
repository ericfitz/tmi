package api

import (
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// Triage access model (#1100, Human decision Eric 2026-10-17):
// triage endpoints are open only to members of Security Reviewers (for
// non-confidential responses) or Confidential Project Reviewers (for
// confidential responses), regardless of response ownership or the row ACL.
// Membership is resolved per request via IsGroupMemberFromContext, so
// IdP-mapped and nested group membership counts. There is no admin bypass.

// triageAccessVerifiedKey marks a Gin context whose triage route has already
// passed RequireTriageAccess, so shared sub-resource handlers skip the
// intake-oriented ACL check.
const triageAccessVerifiedKey = "triageAccessVerified"

// triageReviewerGroup returns the reviewer group whose members may triage a
// survey response with the given confidentiality.
// SEM@49259ca96d8bf22bf9c7a384c528d9a39bebe16d: select the reviewer group authorized to triage a response by confidentiality (pure)
func triageReviewerGroup(isConfidential bool) BuiltInGroup {
	if isConfidential {
		return GroupConfidentialProjectReviewers
	}
	return GroupSecurityReviewers
}

// RequireTriageAccess loads a survey response and authorizes the caller to
// triage it: the caller must belong to the reviewer group matching the
// response's confidentiality. A missing response and a denied caller both
// surface as 404 (non-disclosure, T5 #357). On failure it writes the error
// and returns (nil, false).
// SEM@c200ebeeb27cdc988a6ca70a79561a67add75afb: authorize a caller by reviewer-group membership for a survey response's confidentiality, 404 on denial (reads DB)
func RequireTriageAccess(c *gin.Context, responseID SurveyResponseId) (*SurveyResponse, bool) {
	logger := slogging.Get().WithContext(c)

	if _, ok := getUserUUID(c); !ok {
		return nil, false
	}

	response, err := GlobalSurveyResponseStore.Get(c.Request.Context(), responseID)
	if err != nil {
		logger.Error("RequireTriageAccess: failed to get survey response %s: %v", responseID.String(), err)
		HandleRequestError(c, ServerError("Failed to get survey response"))
		return nil, false
	}
	if response == nil {
		HandleRequestError(c, NotFoundError("Survey response not found"))
		return nil, false
	}

	// A NULL confidentiality is unknown, so it fails closed as confidential,
	// matching the list filter, which excludes NULL rows from the plain scope.
	isConfidential := response.IsConfidential == nil || *response.IsConfidential
	isReviewer, err := IsGroupMemberFromContext(c, triageReviewerGroup(isConfidential))
	if err != nil {
		logger.Error("RequireTriageAccess: membership check failed for response %s: %v", responseID.String(), err)
		HandleRequestError(c, ServerError("Failed to check access"))
		return nil, false
	}
	if !isReviewer {
		logger.Debug("RequireTriageAccess: caller is not a triage reviewer for response %s (confidential=%t) - returning 404",
			responseID.String(), isConfidential)
		HandleRequestError(c, NotFoundError("Survey response not found"))
		return nil, false
	}

	c.Set(triageAccessVerifiedKey, true)
	return response, true
}

// triageReviewerScope resolves which response confidentiality values the
// caller may triage. hasScope is false when the caller belongs to neither reviewer
// group; otherwise confidential, when non-nil, is the single permitted value
// and nil means both.
// SEM@fea6b3be63afa09c0668f3eaa2f51f933c5e05f1: resolve the confidentiality scope a caller may triage from reviewer group membership (reads DB)
func triageReviewerScope(c *gin.Context) (confidential *bool, hasScope bool, err error) {
	mc, err := ResolveMembershipContext(c)
	if err != nil {
		return nil, false, err
	}
	isSR, err := IsGroupMember(c.Request.Context(), mc, GroupSecurityReviewers)
	if err != nil {
		return nil, false, err
	}
	isCPR, err := IsGroupMember(c.Request.Context(), mc, GroupConfidentialProjectReviewers)
	if err != nil {
		return nil, false, err
	}
	switch {
	case isSR && isCPR:
		return nil, true, nil
	case isSR:
		v := false
		return &v, true, nil
	case isCPR:
		v := true
		return &v, true, nil
	default:
		return nil, false, nil
	}
}
