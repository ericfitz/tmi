package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCamelToSnake(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"oneSide", "one_side"},
		{"Bearer", "bearer"},
		{"Critical", "critical"},
		{"OK", "ok"},
		{"DEGRADED", "degraded"},
		{"low", "low"},
		{"snake_case", "snake_case"},
		{"", ""},
		{"a", "a"},
		{"A", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := camelToSnake(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizeEnumValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Severity values
		{"Critical", "critical"},
		{"critical", "critical"},
		{"CRITICAL", "critical"},
		{"High", "high"},
		{"high", "high"},
		{"Medium", "medium"},
		{"Low", "low"},
		{"Unknown", "unknown"},
		// Edge router
		{"oneSide", "one_side"},
		{"one_side", "one_side"},
		// Token type
		{"Bearer", "bearer"},
		{"bearer", "bearer"},
		// Already snake_case
		{"pending_delete", "pending_delete"},
		{"in_progress", "in_progress"},
		// Whitespace trimming
		{" Critical ", "critical"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeEnumValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEnumNormalizerMiddleware_QueryParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		queryString   string
		expectedQuery map[string]string
	}{
		{
			name:        "severity PascalCase normalized",
			queryString: "severity=Critical",
			expectedQuery: map[string]string{
				"severity": "critical",
			},
		},
		{
			name:        "severity already lowercase",
			queryString: "severity=high",
			expectedQuery: map[string]string{
				"severity": "high",
			},
		},
		{
			name:        "format normalized",
			queryString: "format=JSON",
			expectedQuery: map[string]string{
				"format": "json",
			},
		},
		{
			name:        "non-enum param untouched",
			queryString: "limit=10&severity=High",
			expectedQuery: map[string]string{
				"severity": "high",
			},
		},
		{
			name:        "multiple enum params",
			queryString: "severity=Medium&sort_order=DESC",
			expectedQuery: map[string]string{
				"severity":   "medium",
				"sort_order": "desc",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedQuery url.Values

			r := gin.New()
			r.Use(EnumNormalizerMiddleware())
			r.GET("/test", func(c *gin.Context) {
				capturedQuery = c.Request.URL.Query()
				c.Status(http.StatusOK)
			})

			req := httptest.NewRequest("GET", "/test?"+tt.queryString, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			for param, expected := range tt.expectedQuery {
				assert.Equal(t, expected, capturedQuery.Get(param), "param %s", param)
			}
		})
	}
}

func TestEnumNormalizerMiddleware_JSONBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		body     map[string]any
		expected map[string]any
	}{
		{
			name: "severity in body normalized",
			body: map[string]any{
				"title":    "Test Threat",
				"severity": "Critical",
			},
			expected: map[string]any{
				"title":    "Test Threat",
				"severity": "critical",
			},
		},
		{
			name: "role normalized",
			body: map[string]any{
				"role": "Owner",
			},
			expected: map[string]any{
				"role": "owner",
			},
		},
		{
			name: "nested non-normalized fields untouched",
			body: map[string]any{
				"outer": map[string]any{
					"type": "DFD-1.0.0",
				},
			},
			expected: map[string]any{
				"outer": map[string]any{
					"type": "DFD-1.0.0",
				},
			},
		},
		{
			name: "non-enum fields untouched",
			body: map[string]any{
				"title":       "My Title With Caps",
				"description": "Some Description",
			},
			expected: map[string]any{
				"title":       "My Title With Caps",
				"description": "Some Description",
			},
		},
		{
			name: "token_type normalized",
			body: map[string]any{
				"grant_type": "authorization_code",
				"token_type": "Bearer",
			},
			expected: map[string]any{
				"grant_type": "authorization_code",
				"token_type": "bearer",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody map[string]any

			r := gin.New()
			r.Use(EnumNormalizerMiddleware())
			r.POST("/test", func(c *gin.Context) {
				bodyBytes, _ := io.ReadAll(c.Request.Body)
				_ = json.Unmarshal(bodyBytes, &capturedBody)
				c.Status(http.StatusOK)
			})

			bodyBytes, _ := json.Marshal(tt.body)
			req := httptest.NewRequest("POST", "/test", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.NotNil(t, capturedBody)
			for key, expectedVal := range tt.expected {
				switch ev := expectedVal.(type) {
				case string:
					assert.Equal(t, ev, capturedBody[key], "field %s", key)
				case map[string]any:
					nested, ok := capturedBody[key].(map[string]any)
					require.True(t, ok, "field %s should be a map", key)
					for nk, nv := range ev {
						assert.Equal(t, nv, nested[nk], "nested field %s.%s", key, nk)
					}
				}
			}
		})
	}
}

func TestNormalizeEnumFields(t *testing.T) {
	body := map[string]any{
		"severity": "Critical",
		"title":    "Not An Enum",
		"nested": map[string]any{
			"role": "Writer",
		},
	}

	modified := normalizeEnumFields(body)
	assert.True(t, modified)
	assert.Equal(t, "critical", body["severity"])
	assert.Equal(t, "Not An Enum", body["title"])

	nested := body["nested"].(map[string]any)
	assert.Equal(t, "writer", nested["role"])
}
