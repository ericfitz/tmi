package api

import "github.com/gin-gonic/gin"

// microsoftPickerGrantHandlerInterface is the contract Task 10's handler implements.
type microsoftPickerGrantHandlerInterface interface {
	Handle(c *gin.Context)
}

// GrantMicrosoftFilePermission implements ServerInterface.GrantMicrosoftFilePermission.
// Stub until Task 10 attaches the real handler. When unwired, returns a structured
// 404 ("feature_not_available") via the shared contentOAuthUnavailable helper.
func (s *Server) GrantMicrosoftFilePermission(c *gin.Context) {
	if s.microsoftPickerGrant == nil {
		contentOAuthUnavailable(c)
		return
	}
	s.microsoftPickerGrant.Handle(c)
}

// SetMicrosoftPickerGrantHandler attaches the Microsoft picker-grant handler.
// When unset, GrantMicrosoftFilePermission returns 503.
func (s *Server) SetMicrosoftPickerGrantHandler(h microsoftPickerGrantHandlerInterface) {
	s.microsoftPickerGrant = h
}
