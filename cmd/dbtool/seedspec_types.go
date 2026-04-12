package main

import "encoding/json"

// SeedSpecFile is the top-level envelope for the seed-spec format.
// This format is the contract between tmi-ux (E2E tests) and tmi-dbtool (seeding).
type SeedSpecFile struct {
	Version         string                  `json:"version"`
	Description     string                  `json:"description,omitempty"`
	Output          *SeedSpecOutput         `json:"output,omitempty"`
	Users           []SeedSpecUser          `json:"users,omitempty"`
	Teams           []SeedSpecTeam          `json:"teams,omitempty"`
	Projects        []SeedSpecProject       `json:"projects,omitempty"`
	ThreatModels    []SeedSpecThreatModel   `json:"threat_models,omitempty"`
	Surveys         []SeedSpecSurvey        `json:"surveys,omitempty"`
	SurveyResponses []SeedSpecSurveyResp    `json:"survey_responses,omitempty"`
	AdminEntities   *SeedSpecAdmin          `json:"admin_entities,omitempty"`
	Metadata        []SeedSpecMetadataEntry `json:"metadata,omitempty"`
}

// SeedSpecOutput configures reference file generation after seeding.
type SeedSpecOutput struct {
	ReferenceFile string `json:"reference_file,omitempty"`
	ReferenceYAML string `json:"reference_yaml,omitempty"`
}

// SeedSpecUser defines a test user to seed.
type SeedSpecUser struct {
	ID            string            `json:"id"`
	Email         string            `json:"email,omitempty"`
	DisplayName   string            `json:"display_name,omitempty"`
	Roles         SeedSpecUserRoles `json:"roles,omitempty"`
	OAuthProvider string            `json:"oauth_provider,omitempty"`
	APIQuota      *SeedSpecQuota    `json:"api_quota,omitempty"`
}

// SeedSpecUserRoles defines role flags for a user.
type SeedSpecUserRoles struct {
	IsAdmin            bool `json:"is_admin,omitempty"`
	IsSecurityReviewer bool `json:"is_security_reviewer,omitempty"`
}

// SeedSpecQuota defines API rate limits for a user.
type SeedSpecQuota struct {
	RPM int `json:"rpm,omitempty"`
	RPH int `json:"rph,omitempty"`
}

// SeedSpecTeam defines a team to seed.
type SeedSpecTeam struct {
	Name     string               `json:"name"`
	Status   string               `json:"status,omitempty"`
	Members  []SeedSpecTeamMember `json:"members,omitempty"`
	Metadata []SeedSpecKV         `json:"metadata,omitempty"`
}

// SeedSpecTeamMember defines a member within a team.
type SeedSpecTeamMember struct {
	UserID string `json:"user_id"`
	Role   string `json:"role,omitempty"`
}

// SeedSpecProject defines a project to seed.
type SeedSpecProject struct {
	Name     string       `json:"name"`
	Team     string       `json:"team,omitempty"`
	Status   string       `json:"status,omitempty"`
	Metadata []SeedSpecKV `json:"metadata,omitempty"`
}

// SeedSpecThreatModel defines a threat model with all nested children.
type SeedSpecThreatModel struct {
	Name                 string               `json:"name"`
	Description          string               `json:"description,omitempty"`
	Owner                string               `json:"owner,omitempty"`
	ThreatModelFramework string               `json:"threat_model_framework,omitempty"`
	Status               string               `json:"status,omitempty"`
	IsConfidential       bool                 `json:"is_confidential,omitempty"`
	ProjectID            string               `json:"project_id,omitempty"`
	SecurityReviewer     string               `json:"security_reviewer,omitempty"`
	IssueURI             string               `json:"issue_uri,omitempty"`
	Alias                []string             `json:"alias,omitempty"`
	Metadata             []SeedSpecKV         `json:"metadata,omitempty"`
	Authorization        []SeedSpecAuthz      `json:"authorization,omitempty"`
	Threats              []SeedSpecThreat     `json:"threats,omitempty"`
	Assets               []SeedSpecAsset      `json:"assets,omitempty"`
	Documents            []SeedSpecDocument   `json:"documents,omitempty"`
	Repositories         []SeedSpecRepository `json:"repositories,omitempty"`
	Notes                []SeedSpecNote       `json:"notes,omitempty"`
	Diagrams             []SeedSpecDiagram    `json:"diagrams,omitempty"`
}

// SeedSpecAuthz defines an authorization entry on a threat model.
type SeedSpecAuthz struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

// SeedSpecThreat defines a threat nested within a threat model.
type SeedSpecThreat struct {
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	ThreatType      []string       `json:"threat_type,omitempty"`
	Severity        string         `json:"severity,omitempty"`
	Score           float64        `json:"score,omitempty"`
	Priority        string         `json:"priority,omitempty"`
	Status          string         `json:"status,omitempty"`
	Mitigated       bool           `json:"mitigated,omitempty"`
	Mitigation      string         `json:"mitigation,omitempty"`
	CWEID           []string       `json:"cwe_id,omitempty"`
	CVSS            []SeedSpecCVSS `json:"cvss,omitempty"`
	IssueURI        string         `json:"issue_uri,omitempty"`
	IncludeInReport bool           `json:"include_in_report,omitempty"`
}

// SeedSpecCVSS defines a CVSS score entry.
type SeedSpecCVSS struct {
	Version string  `json:"version"`
	Vector  string  `json:"vector"`
	Score   float64 `json:"score"`
}

// SeedSpecAsset defines an asset nested within a threat model.
type SeedSpecAsset struct {
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Type            string   `json:"type,omitempty"`
	Criticality     string   `json:"criticality,omitempty"`
	Classification  []string `json:"classification,omitempty"`
	Sensitivity     string   `json:"sensitivity,omitempty"`
	IncludeInReport bool     `json:"include_in_report,omitempty"`
}

// SeedSpecDocument defines a document nested within a threat model.
type SeedSpecDocument struct {
	Name            string `json:"name"`
	URI             string `json:"uri,omitempty"`
	Description     string `json:"description,omitempty"`
	IncludeInReport bool   `json:"include_in_report,omitempty"`
}

// SeedSpecRepository defines a repository nested within a threat model.
type SeedSpecRepository struct {
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	URI         string `json:"uri,omitempty"`
	Description string `json:"description,omitempty"`
}

// SeedSpecNote defines a note nested within a threat model.
type SeedSpecNote struct {
	Name            string `json:"name"`
	Content         string `json:"content,omitempty"`
	Description     string `json:"description,omitempty"`
	IncludeInReport bool   `json:"include_in_report,omitempty"`
}

// SeedSpecDiagram defines a diagram with simplified node/edge format.
type SeedSpecDiagram struct {
	Name        string         `json:"name"`
	Type        string         `json:"type,omitempty"`
	Description string         `json:"description,omitempty"`
	Nodes       []SeedSpecNode `json:"nodes,omitempty"`
	Edges       []SeedSpecEdge `json:"edges,omitempty"`
}

// SeedSpecNode defines a node in the simplified diagram format.
type SeedSpecNode struct {
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Label  string  `json:"label,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Parent string  `json:"parent,omitempty"`
}

// SeedSpecEdge defines an edge in the simplified diagram format.
type SeedSpecEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

// SeedSpecSurvey defines a survey to seed.
type SeedSpecSurvey struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Version     string          `json:"version,omitempty"`
	Status      string          `json:"status,omitempty"`
	SurveyJSON  json.RawMessage `json:"survey_json,omitempty"`
	Settings    json.RawMessage `json:"settings,omitempty"`
}

// SeedSpecSurveyResp defines a survey response to seed.
type SeedSpecSurveyResp struct {
	Survey    string          `json:"survey"`
	User      string          `json:"user,omitempty"`
	Status    string          `json:"status,omitempty"`
	Responses json.RawMessage `json:"responses,omitempty"`
}

// SeedSpecAdmin groups admin-only entities.
type SeedSpecAdmin struct {
	Groups                []SeedSpecGroup       `json:"groups,omitempty"`
	Quotas                []SeedSpecAdminQuota  `json:"quotas,omitempty"`
	Webhooks              []SeedSpecWebhook     `json:"webhooks,omitempty"`
	Addons                []SeedSpecAddon       `json:"addons,omitempty"`
	Settings              []SeedSpecKV          `json:"settings,omitempty"`
	ClientCredentials     []SeedSpecClientCred  `json:"client_credentials,omitempty"`
	WebhookTestDeliveries []SeedSpecWebhookTest `json:"webhook_test_deliveries,omitempty"`
}

// SeedSpecGroup defines a group with members.
type SeedSpecGroup struct {
	Name    string   `json:"name"`
	Members []string `json:"members,omitempty"`
}

// SeedSpecAdminQuota defines a quota in the admin_entities section.
type SeedSpecAdminQuota struct {
	User      string `json:"user"`
	RateLimit int    `json:"rate_limit"`
	Period    string `json:"period"`
}

// SeedSpecWebhook defines a webhook subscription.
type SeedSpecWebhook struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Events     []string `json:"events,omitempty"`
	HMACSecret string   `json:"hmac_secret,omitempty"`
}

// SeedSpecAddon defines an addon.
type SeedSpecAddon struct {
	Name        string `json:"name"`
	Webhook     string `json:"webhook,omitempty"`
	ThreatModel string `json:"threat_model,omitempty"`
}

// SeedSpecClientCred defines a client credential.
type SeedSpecClientCred struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SeedSpecWebhookTest defines a webhook test delivery trigger.
type SeedSpecWebhookTest struct {
	Webhook string `json:"webhook"`
}

// SeedSpecMetadataEntry defines a standalone metadata entry.
type SeedSpecMetadataEntry struct {
	Target     string `json:"target"`
	TargetKind string `json:"target_kind"`
	Key        string `json:"key"`
	Value      string `json:"value"`
}

// SeedSpecKV is a simple key-value pair used for metadata and settings.
type SeedSpecKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
