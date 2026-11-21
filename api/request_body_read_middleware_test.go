package api

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// shortBody yields its data and then fails like a stalled or truncated client.
type shortBody struct {
	data io.Reader
	err  error
}

func (s *shortBody) Read(p []byte) (int, error) {
	n, err := s.data.Read(p)
	if errors.Is(err, io.EOF) {
		return n, s.err
	}
	return n, err
}
func (s *shortBody) Close() error { return nil }

func runBodyReadMiddleware(t *testing.T, body io.ReadCloser, contentLength int64) (int, bool) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	reached := false
	r := gin.New()
	r.Use(RequestBodyReadMiddleware())
	r.POST("/x", func(c *gin.Context) {
		reached = true
		b, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, string(b))
	})
	req := httptest.NewRequest(http.MethodPost, "/x", body)
	req.ContentLength = contentLength
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, reached
}

func TestRequestBodyReadMiddleware_IncompleteBodyIs400(t *testing.T) {
	for name, readErr := range map[string]error{
		"unexpected EOF":   io.ErrUnexpectedEOF,
		"read timeout":     errors.New("i/o timeout"),
		"context canceled": errors.New("context canceled"),
	} {
		t.Run(name, func(t *testing.T) {
			code, reached := runBodyReadMiddleware(t, &shortBody{data: strings.NewReader("{}"), err: readErr}, 500)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.False(t, reached, "handler must not run after a failed body read")
		})
	}
}

func TestRequestBodyReadMiddleware_CompleteBodyPassesThrough(t *testing.T) {
	code, reached := runBodyReadMiddleware(t, io.NopCloser(strings.NewReader("{}")), 2)
	assert.Equal(t, http.StatusOK, code)
	assert.True(t, reached)
}
