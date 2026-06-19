package api

import "github.com/gin-gonic/gin"

// microsoftPickerGrantHandlerInterface is the contract Task 10's handler implements.
// SEM@381a47a882e241c075287ba07805b46c737fcc0e: contract for the Microsoft file picker permission-grant handler
type microsoftPickerGrantHandlerInterface interface {
	Handle(c *gin.Context)
}

// GrantMicrosoftFilePermission implements ServerInterface.GrantMicrosoftFilePermission.
// Stub until Task 10 attaches the real handler. When unwired, returns a structured
// 404 ("feature_not_available") via the shared contentOAuthUnavailable helper.
// SEM@8fd7f29e24352406748fc94d4c81959f59d433fd: delegate Microsoft file permission grant to the registered handler, or 404 if unregistered
func (s *Server) GrantMicrosoftFilePermission(c *gin.Context) {
	if s.microsoftPickerGrant == nil {
		contentOAuthUnavailable(c)
		return
	}
	s.microsoftPickerGrant.Handle(c)
}

// SetMicrosoftPickerGrantHandler attaches the Microsoft picker-grant handler.
// When unset, GrantMicrosoftFilePermission returns 503.
// SEM@381a47a882e241c075287ba07805b46c737fcc0e: register the Microsoft file picker grant handler on the server (mutates shared state)
func (s *Server) SetMicrosoftPickerGrantHandler(h microsoftPickerGrantHandlerInterface) {
	s.microsoftPickerGrant = h
}
