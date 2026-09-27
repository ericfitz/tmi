package api

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestBodySizeLimitMiddleware pins #959: an oversized feedback body gets the
// documented 413 up front (declared or chunked length) instead of being
// parsed for seconds; normal bodies and other routes pass through intact.
func TestBodySizeLimitMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodySizeLimitMiddleware())
	echo := func(c *gin.Context) {
		b, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, "%d", len(b))
	}
	r.POST("/threat_models/:threat_model_id/feedback", echo)
	r.POST("/usability_feedback", echo)
	r.POST("/threat_models", echo)

	big := strings.Repeat("x", int(feedbackBodyLimit)+1)
	send := func(path string, body io.Reader, contentLength int64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, body)
		req.ContentLength = contentLength
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := send("/threat_models/abc/feedback", strings.NewReader(big), int64(len(big)))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "declared oversize")

	w = send("/usability_feedback", strings.NewReader(big), -1)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "chunked oversize")

	ok := `{"sentiment":"up"}`
	w = send("/usability_feedback", strings.NewReader(ok), int64(len(ok)))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "18", w.Body.String(), "handler must see the full buffered body")

	w = send("/threat_models", bytes.NewReader([]byte(big)), int64(len(big)))
	assert.Equal(t, http.StatusOK, w.Code, "routes without a limit are untouched")
}
