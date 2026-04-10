package api

import (
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AutomationMiddleware creates a middleware that requires the user to be a member of
// either the tmi-automation or embedding-automation group. This is the outer gate
// for all /automation/* routes.
func AutomationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only intercept /automation/ paths
		if !strings.HasPrefix(c.Request.URL.Path, "/automation/") {
			c.Next()
			return
		}

		logger := slogging.Get().WithContext(c)

		mc, err := ResolveMembershipContext(c)
		if err != nil {
			logger.Warn("AutomationMiddleware: failed to resolve membership context: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Authentication required",
			})
			c.Abort()
			return
		}

		ctx := c.Request.Context()

		isTMIAutomation, err := IsGroupMember(ctx, mc, GroupTMIAutomation)
		if err != nil {
			logger.Error("AutomationMiddleware: failed to check tmi-automation membership for email=%s: %v", mc.Email, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to verify automation group membership",
			})
			c.Abort()
			return
		}

		isEmbeddingAutomation, err := IsGroupMember(ctx, mc, GroupEmbeddingAutomation)
		if err != nil {
			logger.Error("AutomationMiddleware: failed to check embedding-automation membership for email=%s: %v", mc.Email, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to verify automation group membership",
			})
			c.Abort()
			return
		}

		if !isTMIAutomation && !isEmbeddingAutomation {
			logger.Warn("AutomationMiddleware: access denied for email=%s, provider=%s, groups=%v", mc.Email, mc.Provider, mc.GroupNames)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "forbidden",
				"message": "automation group membership required",
			})
			return
		}

		logger.Debug("AutomationMiddleware: access granted for email=%s (tmi-automation=%v, embedding-automation=%v)", mc.Email, isTMIAutomation, isEmbeddingAutomation)
		c.Next()
	}
}

// EmbeddingAutomationMiddleware creates a middleware that requires the user to be a member
// of the embedding-automation group specifically. This is the inner gate for
// /automation/embeddings/* routes, layered inside AutomationMiddleware.
func EmbeddingAutomationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only intercept /automation/embeddings/ paths
		if !strings.Contains(c.Request.URL.Path, "/automation/embeddings/") {
			c.Next()
			return
		}

		logger := slogging.Get().WithContext(c)

		mc, err := ResolveMembershipContext(c)
		if err != nil {
			logger.Warn("EmbeddingAutomationMiddleware: failed to resolve membership context: %v", err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusUnauthorized,
				Code:    "unauthorized",
				Message: "Authentication required",
			})
			c.Abort()
			return
		}

		isEmbeddingAutomation, err := IsGroupMember(c.Request.Context(), mc, GroupEmbeddingAutomation)
		if err != nil {
			logger.Error("EmbeddingAutomationMiddleware: failed to check embedding-automation membership for email=%s: %v", mc.Email, err)
			HandleRequestError(c, &RequestError{
				Status:  http.StatusInternalServerError,
				Code:    "server_error",
				Message: "Failed to verify embedding automation group membership",
			})
			c.Abort()
			return
		}

		if !isEmbeddingAutomation {
			logger.Warn("EmbeddingAutomationMiddleware: access denied for email=%s, provider=%s, groups=%v", mc.Email, mc.Provider, mc.GroupNames)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "forbidden",
				"message": "automation group membership required",
			})
			return
		}

		logger.Debug("EmbeddingAutomationMiddleware: access granted for email=%s", mc.Email)
		c.Next()
	}
}
