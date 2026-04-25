package api

import (
	"github.com/gin-gonic/gin"
)

// MintPickerToken implements ServerInterface.MintPickerToken by delegating
// to the attached *PickerTokenHandler. When the handler is not wired the
// picker subsystem is unavailable and a 503 is returned.
func (s *Server) MintPickerToken(c *gin.Context, providerId string) {
	if s.pickerToken == nil {
		contentOAuthUnavailable(c)
		return
	}
	setProviderIDParam(c, providerId)
	s.pickerToken.Handle(c)
}
