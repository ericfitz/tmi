package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// microsoftPickerGrantHandlerInterface is the contract Task 10's handler implements.
type microsoftPickerGrantHandlerInterface interface {
	Handle(c *gin.Context)
}

// GrantMicrosoftFilePermission implements ServerInterface.GrantMicrosoftFilePermission by
// delegating to the attached handler. When the handler is not wired the Microsoft
// picker-grant subsystem is unavailable and a 503 is returned.
func (s *Server) GrantMicrosoftFilePermission(c *gin.Context) {
	if s.microsoftPickerGrant == nil {
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	s.microsoftPickerGrant.Handle(c)
}

// SetMicrosoftPickerGrantHandler attaches the Microsoft picker-grant handler that
// services POST /me/microsoft/picker_grants. Called from cmd/server/main.go after
// the handler is constructed. Passing nil leaves the subsystem disabled —
// GrantMicrosoftFilePermission will return 503.
func (s *Server) SetMicrosoftPickerGrantHandler(h microsoftPickerGrantHandlerInterface) {
	s.microsoftPickerGrant = h
}
