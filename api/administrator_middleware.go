package api

import (
	"github.com/gin-gonic/gin"
)

// AdministratorMiddleware creates a middleware that requires the user to be an administrator
// SEM@a5548be4c61d9f98ed2f3edd998abd909cd5f4ab: authorize requests by rejecting non-administrator users before the handler runs
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
