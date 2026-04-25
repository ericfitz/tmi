package auth

import (
	"fmt"

	"github.com/gin-gonic/gin"
)

// emptySubjectError returns the gin.H body to send when claim extraction
// yields an empty user_id, and the matching log message for operators.
// Returned separately from the handler so the diagnostic format can be
// unit-tested without building a full handler stack.
func emptySubjectError(providerID, email string) (gin.H, string) {
	body := gin.H{
		"error":             "provider_response_invalid",
		"error_description": "Authentication provider returned incomplete profile data. Please contact the administrator.",
	}
	msg := fmt.Sprintf(
		"Runtime backstop triggered: claim extraction produced empty user_id (provider_id=%s, user_email=%s, subject_claim_path_default=sub). Likely cause: missing OAUTH_PROVIDERS_%s_USERINFO_CLAIMS_SUBJECT_CLAIM mapping. See issue #288.",
		providerID, email, providerIDToEnvKey(providerID),
	)
	return body, msg
}
