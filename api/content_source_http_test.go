package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPSource_CanHandle(t *testing.T) {
	s := NewHTTPSource(NewURIValidator(nil, nil))
	assert.True(t, s.CanHandle(context.Background(), "https://example.com/doc"))
	assert.True(t, s.CanHandle(context.Background(), "http://example.com/doc"))
	assert.False(t, s.CanHandle(context.Background(), "ftp://example.com/doc"))
	assert.False(t, s.CanHandle(context.Background(), ""))
}

func TestHTTPSource_Fetch_PlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	s := NewHTTPSource(NewURIValidator([]string{"127.0.0.1"}, []string{"https", "http"}))
	data, ct, err := s.Fetch(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
	assert.Contains(t, ct, "text/plain")
}

func TestHTTPSource_Fetch_SSRFBlocked(t *testing.T) {
	s := NewHTTPSource(NewURIValidator(nil, nil))
	_, _, err := s.Fetch(context.Background(), "http://localhost/secret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SSRF")
}

func TestHTTPSource_Name(t *testing.T) {
	s := NewHTTPSource(NewURIValidator(nil, nil))
	assert.Equal(t, "http", s.Name())
}
