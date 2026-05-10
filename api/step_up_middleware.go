// Package api provides storage and HTTP handlers for the TMI service.
package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// StepUpMiddleware enforces auth_time freshness on routes resolved as
// step-up-required by the provided table. Window is read once at
// construction; changes require a server restart.
//
// Behavior (#355):
//   - Route not in table or not flagged required: pass through.
//   - userAuthTime missing or older than window: 401 with
//     WWW-Authenticate: Bearer error="insufficient_user_authentication", max_age=<seconds>.
//     (Per draft-ietf-oauth-step-up-authn-challenge.)
//   - Fresh enough: pass through.
//
// Order: this middleware MUST run after AuthzMiddleware so non-admins get
// 403 (not 401) when they hit an admin route they can't reach. See spec
// "Request flow for a gated admin write".
func StepUpMiddleware(window time.Duration, table StepUpRouteTable) gin.HandlerFunc {
	maxAgeSeconds := int(window.Seconds())
	return func(c *gin.Context) {
		// Use the matched route template (FullPath), not the request URL,
		// so /admin/settings/foo resolves to /admin/settings/:key.
		// Convert from Gin's colon form to OpenAPI's curly-brace form for the
		// route table lookup (table is keyed on OpenAPI path form).
		openAPIPath := ginPathToOpenAPI(c.FullPath())
		if !table.Required(c.Request.Method, openAPIPath) {
			c.Next()
			return
		}

		authTimeAny, _ := c.Get("userAuthTime")
		authTime, ok := authTimeAny.(*time.Time)
		if !ok || authTime == nil || time.Since(*authTime) > window {
			reason := stepUpReason(authTime, window)
			slogging.Get().WithContext(c).Info(
				"step-up required: method=%s path=%s reason=%s",
				c.Request.Method, c.FullPath(), reason)

			challenge := fmt.Sprintf(
				`Bearer error="insufficient_user_authentication", error_description="re-authentication required for this admin operation", max_age=%d`,
				maxAgeSeconds)
			c.Header("WWW-Authenticate", challenge)
			c.JSON(http.StatusUnauthorized, Error{
				Error:            "insufficient_user_authentication",
				ErrorDescription: "Recent re-authentication required",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// stepUpReason produces a small human-readable string for the audit log
// describing why a request was challenged.
func stepUpReason(authTime *time.Time, window time.Duration) string {
	if authTime == nil {
		return "auth_time_missing"
	}
	return fmt.Sprintf("auth_time_stale age=%v window=%v", time.Since(*authTime), window)
}
