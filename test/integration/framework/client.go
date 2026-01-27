package framework

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/integration/spec"
)

// IntegrationClient is an HTTP client for integration testing with OpenAPI validation
type IntegrationClient struct {
	baseURL        string
	httpClient     *http.Client
	tokens         *OAuthTokens
	validator      *spec.OpenAPIValidator
	workflowState  map[string]interface{}
	validateSchema bool
	logRequests    bool
}

// ClientOption is a functional option for configuring IntegrationClient
type ClientOption func(*IntegrationClient)

// WithValidation enables/disables OpenAPI schema validation
func WithValidation(enable bool) ClientOption {
	return func(c *IntegrationClient) {
		c.validateSchema = enable
	}
}

// WithRequestLogging enables/disables request/response logging
func WithRequestLogging(enable bool) ClientOption {
	return func(c *IntegrationClient) {
		c.logRequests = enable
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *IntegrationClient) {
		c.httpClient = httpClient
	}
}

// NewClient creates a new integration test client
func NewClient(baseURL string, tokens *OAuthTokens, opts ...ClientOption) (*IntegrationClient, error) {
	validator, err := spec.NewValidator()
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAPI validator: %w", err)
	}

	client := &IntegrationClient{
		baseURL:        strings.TrimSuffix(baseURL, "/"),
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		tokens:         tokens,
		validator:      validator,
		workflowState:  make(map[string]interface{}),
		validateSchema: true, // Default to enabled
		logRequests:    true, // Default to enabled
	}

	// Apply options
	for _, opt := range opts {
		opt(client)
	}

	return client, nil
}

// Request represents an HTTP request configuration
type Request struct {
	Method      string
	Path        string
	Body        interface{}            // JSON body (will be marshaled)
	FormBody    map[string]string      // Form-urlencoded body (takes precedence over Body)
	Headers     map[string]string
	QueryParams map[string]string
}

// Response represents an HTTP response with validation
type Response struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	RawRequest *http.Request
}

// Do executes an HTTP request with optional OpenAPI validation
func (c *IntegrationClient) Do(req Request) (*Response, error) {
	// Build full URL
	fullURL := c.baseURL + req.Path
	if len(req.QueryParams) > 0 {
		params := url.Values{}
		for k, v := range req.QueryParams {
			params.Add(k, v)
		}
		fullURL = fullURL + "?" + params.Encode()
	}

	// Prepare request body
	var bodyReader io.Reader
	var bodyBytes []byte
	var contentType string
	if req.FormBody != nil {
		// Form-urlencoded body
		formData := url.Values{}
		for k, v := range req.FormBody {
			formData.Set(k, v)
		}
		bodyBytes = []byte(formData.Encode())
		bodyReader = bytes.NewReader(bodyBytes)
		contentType = "application/x-www-form-urlencoded"
	} else if req.Body != nil {
		var err error
		bodyBytes, err = json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
		// Use JSON Patch content type for PATCH requests, regular JSON for others
		if req.Method == "PATCH" {
			contentType = "application/json-patch+json"
		} else {
			contentType = "application/json"
		}
	}

	// Create HTTP request
	httpReq, err := http.NewRequest(req.Method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if c.tokens != nil && c.tokens.AccessToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.tokens.AccessToken)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Log request
	if c.logRequests {
		c.logRequest(httpReq, bodyBytes)
	}

	// Validate request against OpenAPI spec
	if c.validateSchema {
		if err := c.validator.ValidateRequest(httpReq, bodyBytes); err != nil {
			slogging.Get().Warn("Request validation warning (continuing anyway): %s %s - %v",
				req.Method, req.Path, err)
		}
	}

	// Execute request
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Log response
	if c.logRequests {
		c.logResponse(httpResp, respBody)
	}

	// Validate response against OpenAPI spec
	if c.validateSchema {
		if err := c.validator.ValidateResponse(httpReq, httpResp, respBody); err != nil {
			return nil, fmt.Errorf("response validation failed: %w", err)
		}
	}

	return &Response{
		StatusCode: httpResp.StatusCode,
		Body:       respBody,
		Headers:    httpResp.Header,
		RawRequest: httpReq,
	}, nil
}

// DecodeJSON decodes response body into target struct
func (r *Response) DecodeJSON(target interface{}) error {
	if err := json.Unmarshal(r.Body, target); err != nil {
		return fmt.Errorf("failed to decode JSON response: %w", err)
	}
	return nil
}

// SaveState stores a value in the workflow state
func (c *IntegrationClient) SaveState(key string, value interface{}) {
	c.workflowState[key] = value
}

// GetState retrieves a value from the workflow state
func (c *IntegrationClient) GetState(key string) (interface{}, bool) {
	val, ok := c.workflowState[key]
	return val, ok
}

// GetStateString retrieves a string value from workflow state
func (c *IntegrationClient) GetStateString(key string) (string, error) {
	val, ok := c.workflowState[key]
	if !ok {
		return "", fmt.Errorf("workflow state key '%s' not found", key)
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("workflow state key '%s' is not a string (type: %T)", key, val)
	}
	return str, nil
}

// ClearState clears all workflow state
func (c *IntegrationClient) ClearState() {
	c.workflowState = make(map[string]interface{})
}

// UpdateTokens updates the OAuth tokens (useful for token refresh scenarios)
func (c *IntegrationClient) UpdateTokens(tokens *OAuthTokens) {
	c.tokens = tokens
}

// GetTokens returns the current OAuth tokens
func (c *IntegrationClient) GetTokens() *OAuthTokens {
	return c.tokens
}

// logRequest logs HTTP request details
func (c *IntegrationClient) logRequest(req *http.Request, body []byte) {
	bodyStr := ""
	if len(body) > 0 && len(body) < 1024 {
		bodyStr = string(body)
	}
	slogging.Get().Info("HTTP Request: %s %s %s", req.Method, req.URL.String(), bodyStr)
}

// logResponse logs HTTP response details
func (c *IntegrationClient) logResponse(resp *http.Response, body []byte) {
	bodyStr := ""
	if len(body) > 0 && len(body) < 1024 {
		bodyStr = string(body)
	}
	slogging.Get().Info("HTTP Response: status=%d content_type=%s %s",
		resp.StatusCode, resp.Header.Get("Content-Type"), bodyStr)
}
