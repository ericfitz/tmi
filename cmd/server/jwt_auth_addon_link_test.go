package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

// A service-account token carrying tmi_addon_id tags the request context with
// the source addon so the webhook consumer suppresses self-deliveries (#883).
func TestSetServiceAccountContext_AddonLinkTagsSourceAddon(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func(claims jwt.MapClaims) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		setServiceAccountContext(c, slogging.Get(), "sa:cred-1:owner-1", claims)
		return c
	}

	c := newCtx(jwt.MapClaims{"tmi_direct_write": true, "tmi_addon_id": "fed11e3a-0000-4000-8000-000000000000"})
	assert.Equal(t, "fed11e3a-0000-4000-8000-000000000000", api.SourceAddonIDFromContext(c.Request.Context()))

	c = newCtx(jwt.MapClaims{"tmi_direct_write": true})
	assert.Empty(t, api.SourceAddonIDFromContext(c.Request.Context()), "no claim, no tag")
}
