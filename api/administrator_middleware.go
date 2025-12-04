package api

import (
	"github.com/gin-gonic/gin"
)

// AdministratorMiddleware creates a middleware that requires the user to be an administrator
func AdministratorMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Use the consolidated RequireAdministrator helper
		if _, err := RequireAdministrator(c); err != nil {
			c.Abort()
			return
		}

		// User is an administrator, proceed
		c.Next()
	}
}
