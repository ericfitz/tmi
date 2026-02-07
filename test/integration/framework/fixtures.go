package framework

import (
	"fmt"

	"github.com/google/uuid"
)

// ThreatModelFixture creates a test threat model
type ThreatModelFixture struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IssueURI    string `json:"issue_uri,omitempty"`
}

// NewThreatModelFixture creates a basic threat model fixture
func NewThreatModelFixture() *ThreatModelFixture {
	id := uuid.New().String()[:8]
	return &ThreatModelFixture{
		Name:        fmt.Sprintf("Test Threat Model %s", id),
		Description: "Created by integration test framework",
	}
}

// WithName sets a custom name
func (f *ThreatModelFixture) WithName(name string) *ThreatModelFixture {
	f.Name = name
	return f
}

// WithDescription sets a custom description
func (f *ThreatModelFixture) WithDescription(desc string) *ThreatModelFixture {
	f.Description = desc
	return f
}

// WithIssueURI sets an issue tracker URI
func (f *ThreatModelFixture) WithIssueURI(uri string) *ThreatModelFixture {
	f.IssueURI = uri
	return f
}

// DiagramFixture creates a test diagram
type DiagramFixture struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// NewDiagramFixture creates a basic diagram fixture
func NewDiagramFixture() *DiagramFixture {
	id := uuid.New().String()[:8]
	return &DiagramFixture{
		Name: fmt.Sprintf("Test Diagram %s", id),
		Type: "data_flow",
	}
}

// WithName sets a custom name
func (f *DiagramFixture) WithName(name string) *DiagramFixture {
	f.Name = name
	return f
}

// WithType sets diagram type
func (f *DiagramFixture) WithType(diagramType string) *DiagramFixture {
	f.Type = diagramType
	return f
}

// ThreatFixture creates a test threat
type ThreatFixture struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Status      string `json:"status,omitempty"`
}

// NewThreatFixture creates a basic threat fixture
func NewThreatFixture() *ThreatFixture {
	id := uuid.New().String()[:8]
	return &ThreatFixture{
		Name:        fmt.Sprintf("Test Threat %s", id),
		Description: "Created by integration test",
		Severity:    "Medium",
		Status:      "Open",
	}
}

// WithName sets a custom name
func (f *ThreatFixture) WithName(name string) *ThreatFixture {
	f.Name = name
	return f
}

// WithSeverity sets threat severity
func (f *ThreatFixture) WithSeverity(severity string) *ThreatFixture {
	f.Severity = severity
	return f
}

// WithStatus sets threat status
func (f *ThreatFixture) WithStatus(status string) *ThreatFixture {
	f.Status = status
	return f
}

// AssetFixture creates a test asset
type AssetFixture struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
}

// NewAssetFixture creates a basic asset fixture
func NewAssetFixture() *AssetFixture {
	id := uuid.New().String()[:8]
	return &AssetFixture{
		Name:        fmt.Sprintf("Test Asset %s", id),
		Description: "Created by integration test",
		Type:        "data",
	}
}

// WithName sets a custom name
func (f *AssetFixture) WithName(name string) *AssetFixture {
	f.Name = name
	return f
}

// WithType sets asset type
func (f *AssetFixture) WithType(assetType string) *AssetFixture {
	f.Type = assetType
	return f
}

// DocumentFixture creates a test document
type DocumentFixture struct {
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Content     string `json:"content"`
}

// NewDocumentFixture creates a basic document fixture
func NewDocumentFixture() *DocumentFixture {
	id := uuid.New().String()[:8]
	return &DocumentFixture{
		Name:        fmt.Sprintf("Test Document %s", id),
		ContentType: "text/plain",
		Content:     "This is test document content",
	}
}

// WithName sets a custom name
func (f *DocumentFixture) WithName(name string) *DocumentFixture {
	f.Name = name
	return f
}

// WithContent sets document content
func (f *DocumentFixture) WithContent(content string) *DocumentFixture {
	f.Content = content
	return f
}

// WithContentType sets content type
func (f *DocumentFixture) WithContentType(contentType string) *DocumentFixture {
	f.ContentType = contentType
	return f
}

// RepositoryFixture creates a test repository reference
type RepositoryFixture struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

// NewRepositoryFixture creates a basic repository fixture
func NewRepositoryFixture() *RepositoryFixture {
	id := uuid.New().String()[:8]
	return &RepositoryFixture{
		Name:        fmt.Sprintf("Test Repository %s", id),
		URL:         fmt.Sprintf("https://github.com/test/repo-%s", id),
		Type:        "git",
		Description: "Created by integration test",
	}
}

// WithName sets a custom name
func (f *RepositoryFixture) WithName(name string) *RepositoryFixture {
	f.Name = name
	return f
}

// WithURL sets repository URL
func (f *RepositoryFixture) WithURL(url string) *RepositoryFixture {
	f.URL = url
	return f
}

// MetadataFixture creates test metadata
type MetadataFixture struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

// NewMetadataFixture creates a basic metadata fixture
func NewMetadataFixture(key string, value interface{}) *MetadataFixture {
	return &MetadataFixture{
		Key:   key,
		Value: value,
	}
}

// WebhookSubscriptionFixture creates a test webhook subscription
type WebhookSubscriptionFixture struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
	Active bool     `json:"active"`
}

// NewWebhookSubscriptionFixture creates a basic webhook subscription
func NewWebhookSubscriptionFixture() *WebhookSubscriptionFixture {
	return &WebhookSubscriptionFixture{
		URL:    "https://example.com/webhook",
		Events: []string{"threat_model.created", "threat_model.updated"},
		Active: true,
	}
}

// WithURL sets webhook URL
func (f *WebhookSubscriptionFixture) WithURL(url string) *WebhookSubscriptionFixture {
	f.URL = url
	return f
}

// WithEvents sets webhook events
func (f *WebhookSubscriptionFixture) WithEvents(events []string) *WebhookSubscriptionFixture {
	f.Events = events
	return f
}

// AddonFixture creates a test addon
type AddonFixture struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
	Active      bool   `json:"active"`
}

// NewAddonFixture creates a basic addon fixture
func NewAddonFixture() *AddonFixture {
	id := uuid.New().String()[:8]
	return &AddonFixture{
		Name:        fmt.Sprintf("Test Addon %s", id),
		Description: "Created by integration test",
		URL:         fmt.Sprintf("https://example.com/addon-%s", id),
		Active:      true,
	}
}

// WithName sets addon name
func (f *AddonFixture) WithName(name string) *AddonFixture {
	f.Name = name
	return f
}

// WithURL sets addon URL
func (f *AddonFixture) WithURL(url string) *AddonFixture {
	f.URL = url
	return f
}

// SurveyFixture creates a test survey
type SurveyFixture struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Version     string                 `json:"version,omitempty"`
	Status      string                 `json:"status,omitempty"`
	SurveyJSON  map[string]interface{} `json:"survey_json"`
}

// NewSurveyFixture creates a basic survey fixture with valid SurveyJS JSON
func NewSurveyFixture() *SurveyFixture {
	id := uuid.New().String()[:8]
	return &SurveyFixture{
		Name:        fmt.Sprintf("Test Survey %s", id),
		Description: "Created by integration test",
		Version:     "1.0",
		Status:      "active",
		SurveyJSON: map[string]interface{}{
			"pages": []interface{}{
				map[string]interface{}{
					"name": "page1",
					"elements": []interface{}{
						map[string]interface{}{
							"type": "text",
							"name": "project_name",
							"title": "What is your project name?",
						},
						map[string]interface{}{
							"type": "comment",
							"name": "project_description",
							"title": "Describe your project",
						},
					},
				},
			},
		},
	}
}

// WithName sets a custom name
func (f *SurveyFixture) WithName(name string) *SurveyFixture {
	f.Name = name
	return f
}

// WithDescription sets a custom description
func (f *SurveyFixture) WithDescription(desc string) *SurveyFixture {
	f.Description = desc
	return f
}

// WithVersion sets survey version
func (f *SurveyFixture) WithVersion(version string) *SurveyFixture {
	f.Version = version
	return f
}

// WithStatus sets survey status
func (f *SurveyFixture) WithStatus(status string) *SurveyFixture {
	f.Status = status
	return f
}

// SurveyResponseFixture creates a test survey response
type SurveyResponseFixture struct {
	SurveyID       string                 `json:"survey_id"`
	IsConfidential bool                   `json:"is_confidential,omitempty"`
	Answers        map[string]interface{} `json:"answers,omitempty"`
}

// NewSurveyResponseFixture creates a basic survey response fixture
func NewSurveyResponseFixture(surveyID string) *SurveyResponseFixture {
	return &SurveyResponseFixture{
		SurveyID: surveyID,
		Answers: map[string]interface{}{
			"project_name":        "Test Project",
			"project_description": "A test project for integration testing",
		},
	}
}

// WithConfidential sets confidentiality flag
func (f *SurveyResponseFixture) WithConfidential(confidential bool) *SurveyResponseFixture {
	f.IsConfidential = confidential
	return f
}

// WithAnswers sets custom answers
func (f *SurveyResponseFixture) WithAnswers(answers map[string]interface{}) *SurveyResponseFixture {
	f.Answers = answers
	return f
}

// UniqueUserID generates a unique user ID for testing
func UniqueUserID() string {
	return "testuser-" + uuid.New().String()[:8]
}

// UniqueName generates a unique name with prefix
func UniqueName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, uuid.New().String()[:8])
}
