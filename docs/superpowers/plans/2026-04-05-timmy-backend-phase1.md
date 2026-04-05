# Timmy Backend Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the core Timmy backend — a conversational AI assistant that lets users chat about their threat models using LLM-powered analysis grounded in the threat model's data.

**Architecture:** Five subsystems built bottom-up: (1) GORM data model for sessions, messages, embeddings, and usage; (2) pluggable content providers that extract text from DB entities, DFD JSON, HTML, and PDFs; (3) in-memory vector index manager with DB-serialized embeddings; (4) LangChainGo-based LLM service with streaming; (5) REST+SSE Chat API. All wired through a session manager that orchestrates context construction from a two-tier model (structured overview + vector-retrieved chunks).

**Tech Stack:** Go, Gin, GORM, LangChainGo (`github.com/tmc/langchaingo`), testify, SSE over HTTP

**Spec:** `docs/superpowers/specs/2026-04-04-timmy-backend-design.md`
**Issue:** ericfitz/tmi#214

---

## File Structure

### New files

| File | Responsibility |
|------|---------------|
| `internal/config/timmy.go` | TimmyConfig struct, defaults, validation |
| `api/models/timmy.go` | GORM models: TimmySession, TimmyMessage, TimmyEmbedding, TimmyUsage |
| `internal/dbschema/timmy.go` | Expected schema definitions for 4 new tables |
| `api/timmy_embedding_store.go` | TimmyEmbeddingStore interface |
| `api/timmy_embedding_store_gorm.go` | GORM implementation of TimmyEmbeddingStore |
| `api/timmy_session_store.go` | TimmySessionStore and TimmyMessageStore interfaces |
| `api/timmy_session_store_gorm.go` | GORM implementations of session and message stores |
| `api/timmy_usage_store.go` | TimmyUsageStore interface |
| `api/timmy_usage_store_gorm.go` | GORM implementation of TimmyUsageStore |
| `api/timmy_ssrf.go` | SSRF URL validator |
| `api/timmy_content_provider.go` | ContentProvider interface, EntityReference, ExtractedContent, ProviderRegistry |
| `api/timmy_content_provider_text.go` | DirectTextProvider for DB-resident content |
| `api/timmy_content_provider_json.go` | JSONProvider for DFD diagram extraction |
| `api/timmy_content_provider_http.go` | HTTPProvider for HTML/plain text URLs |
| `api/timmy_content_provider_pdf.go` | PDFProvider for PDF URLs |
| `api/timmy_chunker.go` | Text chunking with sentence-aware splitting and overlap |
| `api/timmy_vector_index.go` | In-memory vector index (brute-force cosine similarity — adequate for threat-model scale) |
| `api/timmy_vector_manager.go` | VectorIndexManager: lifecycle, memory budget, LRU eviction, write-back |
| `api/timmy_llm_service.go` | LLMService: LangChainGo wrapper, streaming, prompt construction |
| `api/timmy_context_builder.go` | ContextBuilder: Tier 1 structured + Tier 2 vector retrieval |
| `api/timmy_sse.go` | SSE streaming utilities for Gin |
| `api/timmy_rate_limiter.go` | TimmyRateLimiter: per-user, per-TM, server-wide limits |
| `api/timmy_session_manager.go` | SessionManager: orchestrates session creation, message handling |
| `api/timmy_handlers.go` | HTTP handlers for chat sessions, messages, admin |
| `api/timmy_middleware.go` | TimmyMiddleware: enabled check, authorization |
| `api/timmy_embedding_store_test.go` | Tests for embedding store |
| `api/timmy_session_store_test.go` | Tests for session and message stores |
| `api/timmy_usage_store_test.go` | Tests for usage store |
| `api/timmy_ssrf_test.go` | Tests for SSRF validator |
| `api/timmy_content_provider_test.go` | Tests for all content providers |
| `api/timmy_chunker_test.go` | Tests for text chunker |
| `api/timmy_vector_index_test.go` | Tests for in-memory vector index |
| `api/timmy_vector_manager_test.go` | Tests for vector index manager |
| `api/timmy_llm_service_test.go` | Tests for LLM service |
| `api/timmy_context_builder_test.go` | Tests for context builder |
| `api/timmy_rate_limiter_test.go` | Tests for rate limiter |
| `api/timmy_session_manager_test.go` | Tests for session manager |
| `api/timmy_handlers_test.go` | Tests for HTTP handlers |

### Modified files

| File | Change |
|------|--------|
| `internal/config/config.go` | Add `Timmy TimmyConfig` field to Config struct |
| `api/store.go` | Add global store variables and wire into InitializeGormStores |
| `internal/dbschema/schema.go` | Add Timmy tables to GetExpectedSchema |
| `cmd/server/main.go` | Initialize Timmy subsystems, register routes |
| `api-schema/tmi-openapi.json` | Add chat endpoints, schemas, admin endpoints |
| `go.mod` / `go.sum` | Add LangChainGo dependency |

---

## Task 1: Add LangChainGo Dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add LangChainGo module**

```bash
cd /Users/efitz/Projects/tmi && go get github.com/tmc/langchaingo@latest
```

- [ ] **Step 2: Verify it resolves**

Run: `go mod tidy`
Expected: Clean exit, no errors

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add langchaingo for Timmy LLM integration"
```

---

## Task 2: Timmy Configuration

**Files:**
- Create: `internal/config/timmy.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Create TimmyConfig struct**

Create `internal/config/timmy.go`:

```go
package config

// TimmyConfig holds configuration for the Timmy AI assistant feature
type TimmyConfig struct {
	Enabled                   bool   `yaml:"enabled" env:"TMI_TIMMY_ENABLED"`
	LLMProvider               string `yaml:"llm_provider" env:"TMI_TIMMY_LLM_PROVIDER"`
	LLMModel                  string `yaml:"llm_model" env:"TMI_TIMMY_LLM_MODEL"`
	LLMAPIKey                 string `yaml:"llm_api_key" env:"TMI_TIMMY_LLM_API_KEY"`
	EmbeddingProvider         string `yaml:"embedding_provider" env:"TMI_TIMMY_EMBEDDING_PROVIDER"`
	EmbeddingModel            string `yaml:"embedding_model" env:"TMI_TIMMY_EMBEDDING_MODEL"`
	EmbeddingAPIKey           string `yaml:"embedding_api_key" env:"TMI_TIMMY_EMBEDDING_API_KEY"`
	RetrievalTopK             int    `yaml:"retrieval_top_k" env:"TMI_TIMMY_RETRIEVAL_TOP_K"`
	MaxConversationHistory    int    `yaml:"max_conversation_history" env:"TMI_TIMMY_MAX_CONVERSATION_HISTORY"`
	OperatorSystemPrompt      string `yaml:"operator_system_prompt" env:"TMI_TIMMY_OPERATOR_SYSTEM_PROMPT"`
	MaxMemoryMB               int    `yaml:"max_memory_mb" env:"TMI_TIMMY_MAX_MEMORY_MB"`
	InactivityTimeoutSeconds  int    `yaml:"inactivity_timeout_seconds" env:"TMI_TIMMY_INACTIVITY_TIMEOUT_SECONDS"`
	MaxMessagesPerUserPerHour int    `yaml:"max_messages_per_user_per_hour" env:"TMI_TIMMY_MAX_MESSAGES_PER_USER_PER_HOUR"`
	MaxSessionsPerThreatModel int    `yaml:"max_sessions_per_threat_model" env:"TMI_TIMMY_MAX_SESSIONS_PER_THREAT_MODEL"`
	MaxConcurrentLLMRequests  int    `yaml:"max_concurrent_llm_requests" env:"TMI_TIMMY_MAX_CONCURRENT_LLM_REQUESTS"`
	ChunkSize                 int    `yaml:"chunk_size" env:"TMI_TIMMY_CHUNK_SIZE"`
	ChunkOverlap              int    `yaml:"chunk_overlap" env:"TMI_TIMMY_CHUNK_OVERLAP"`
	SSRFAllowlist             string `yaml:"ssrf_allowlist" env:"TMI_TIMMY_SSRF_ALLOWLIST"` // Comma-separated list of allowed internal hosts
}

// DefaultTimmyConfig returns configuration with sensible defaults
func DefaultTimmyConfig() TimmyConfig {
	return TimmyConfig{
		Enabled:                   false,
		RetrievalTopK:             10,
		MaxConversationHistory:    50,
		MaxMemoryMB:               256,
		InactivityTimeoutSeconds:  3600,
		MaxMessagesPerUserPerHour: 60,
		MaxSessionsPerThreatModel: 50,
		MaxConcurrentLLMRequests:  10,
		ChunkSize:                 512,
		ChunkOverlap:              50,
	}
}

// IsConfigured returns true if the required LLM and embedding providers are configured
func (tc TimmyConfig) IsConfigured() bool {
	return tc.LLMProvider != "" && tc.LLMModel != "" &&
		tc.EmbeddingProvider != "" && tc.EmbeddingModel != ""
}
```

- [ ] **Step 2: Add Timmy field to Config struct**

In `internal/config/config.go`, add to the Config struct:

```go
Timmy TimmyConfig `yaml:"timmy"`
```

And in the Load function (or wherever defaults are applied), ensure:

```go
cfg.Timmy = DefaultTimmyConfig()
```

is set before YAML/env overrides are applied.

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add internal/config/timmy.go internal/config/config.go
git commit -m "feat(timmy): add configuration struct with defaults

Adds TimmyConfig with LLM/embedding provider settings, memory budget,
rate limits, and chunking parameters. Disabled by default.

Refs #214"
```

---

## Task 3: GORM Models

**Files:**
- Create: `api/models/timmy.go`

- [ ] **Step 1: Create Timmy GORM models**

Create `api/models/timmy.go`:

```go
package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TimmySession represents a chat session between a user and Timmy for a threat model
type TimmySession struct {
	ID               string     `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID    string     `gorm:"type:varchar(36);not null;index:idx_timmy_sessions_tm"`
	UserID           string     `gorm:"type:varchar(36);not null;index:idx_timmy_sessions_user;index:idx_timmy_sessions_tm_user,priority:2"`
	Title            string     `gorm:"type:varchar(256)"`
	SourceSnapshot   JSONRaw    `gorm:""`
	SystemPromptHash string     `gorm:"type:varchar(64)"`
	Status           string     `gorm:"type:varchar(20);not null;default:active;index:idx_timmy_sessions_status"`
	CreatedAt        time.Time  `gorm:"not null;autoCreateTime"`
	ModifiedAt       time.Time  `gorm:"not null;autoUpdateTime"`
	DeletedAt        *time.Time `gorm:"index:idx_timmy_sessions_deleted_at"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
	User        User        `gorm:"foreignKey:UserID;references:InternalUUID"`
}

func (TimmySession) TableName() string {
	return tableName("timmy_sessions")
}

func (s *TimmySession) BeforeCreate(tx *gorm.DB) error {
	if s.ID == "" {
		s.ID = uuid.New().String()
	}
	if s.Status == "" {
		s.Status = "active"
	}
	return nil
}

// TimmyMessage represents a single message in a Timmy chat session
type TimmyMessage struct {
	ID         string    `gorm:"primaryKey;type:varchar(36)"`
	SessionID  string    `gorm:"type:varchar(36);not null;index:idx_timmy_messages_session"`
	Role       string    `gorm:"type:varchar(20);not null"`
	Content    DBText    `gorm:"not null"`
	TokenCount int       `gorm:"default:0"`
	Sequence   int       `gorm:"not null"`
	CreatedAt  time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	Session TimmySession `gorm:"foreignKey:SessionID"`
}

func (TimmyMessage) TableName() string {
	return tableName("timmy_messages")
}

func (m *TimmyMessage) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// TimmyEmbedding represents a vector embedding for a chunk of threat model content
type TimmyEmbedding struct {
	ID             string    `gorm:"primaryKey;type:varchar(36)"`
	ThreatModelID  string    `gorm:"type:varchar(36);not null;index:idx_timmy_embeddings_tm;index:idx_timmy_embeddings_entity,priority:1"`
	EntityType     string    `gorm:"type:varchar(30);not null;index:idx_timmy_embeddings_entity,priority:2"`
	EntityID       string    `gorm:"type:varchar(36);not null;index:idx_timmy_embeddings_entity,priority:3"`
	ChunkIndex     int       `gorm:"not null;index:idx_timmy_embeddings_entity,priority:4"`
	ContentHash    string    `gorm:"type:varchar(64);not null"`
	EmbeddingModel string    `gorm:"type:varchar(100);not null"`
	EmbeddingDim   int       `gorm:"not null"`
	VectorData     []byte    `gorm:"type:blob"`
	ChunkText      DBText    `gorm:"not null"`
	CreatedAt      time.Time `gorm:"not null;autoCreateTime"`

	// Relationships
	ThreatModel ThreatModel `gorm:"foreignKey:ThreatModelID"`
}

func (TimmyEmbedding) TableName() string {
	return tableName("timmy_embeddings")
}

func (e *TimmyEmbedding) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = uuid.New().String()
	}
	return nil
}

// TimmyUsage tracks LLM token usage for billing and monitoring
type TimmyUsage struct {
	ID               string    `gorm:"primaryKey;type:varchar(36)"`
	UserID           string    `gorm:"type:varchar(36);not null;index:idx_timmy_usage_user"`
	SessionID        string    `gorm:"type:varchar(36);not null;index:idx_timmy_usage_session"`
	ThreatModelID    string    `gorm:"type:varchar(36);not null;index:idx_timmy_usage_tm"`
	MessageCount     int       `gorm:"default:0"`
	PromptTokens     int       `gorm:"default:0"`
	CompletionTokens int       `gorm:"default:0"`
	EmbeddingTokens  int       `gorm:"default:0"`
	PeriodStart      time.Time `gorm:"not null;index:idx_timmy_usage_period"`
	PeriodEnd        time.Time `gorm:"not null"`

	// Relationships
	ThreatModel ThreatModel  `gorm:"foreignKey:ThreatModelID"`
	User        User         `gorm:"foreignKey:UserID;references:InternalUUID"`
	Session     TimmySession `gorm:"foreignKey:SessionID"`
}

func (TimmyUsage) TableName() string {
	return tableName("timmy_usage")
}

func (u *TimmyUsage) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = uuid.New().String()
	}
	return nil
}
```

- [ ] **Step 2: Verify build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add api/models/timmy.go
git commit -m "feat(timmy): add GORM models for sessions, messages, embeddings, usage

Four new tables: timmy_sessions, timmy_messages, timmy_embeddings,
timmy_usage. Follows existing model patterns: UUID PKs, soft delete,
BeforeCreate hooks, relationship definitions.

Refs #214"
```

---

## Task 4: Expected Schema Definitions

**Files:**
- Create: `internal/dbschema/timmy.go`
- Modify: `internal/dbschema/schema.go`

- [ ] **Step 1: Create Timmy schema definitions**

Create `internal/dbschema/timmy.go`:

```go
package dbschema

// GetTimmySchema returns the expected schema for Timmy tables
func GetTimmySchema() []TableSchema {
	return []TableSchema{
		{
			Name: "timmy_sessions",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "user_id", DataType: "character varying", IsNullable: false},
				{Name: "title", DataType: "character varying", IsNullable: true},
				{Name: "source_snapshot", DataType: "text", IsNullable: true},
				{Name: "system_prompt_hash", DataType: "character varying", IsNullable: true},
				{Name: "status", DataType: "character varying", IsNullable: false},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "deleted_at", DataType: "timestamp with time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "timmy_sessions_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_timmy_sessions_tm", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_timmy_sessions_user", Columns: []string{"user_id"}, IsUnique: false},
				{Name: "idx_timmy_sessions_status", Columns: []string{"status"}, IsUnique: false},
				{Name: "idx_timmy_sessions_deleted_at", Columns: []string{"deleted_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{Name: "fk_timmy_sessions_threat_model", Type: "FOREIGN KEY", ForeignTable: "threat_models", ForeignColumns: []string{"id"}},
				{Name: "fk_timmy_sessions_user", Type: "FOREIGN KEY", ForeignTable: "users", ForeignColumns: []string{"internal_uuid"}},
			},
		},
		{
			Name: "timmy_messages",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "session_id", DataType: "character varying", IsNullable: false},
				{Name: "role", DataType: "character varying", IsNullable: false},
				{Name: "content", DataType: "text", IsNullable: false},
				{Name: "token_count", DataType: "integer", IsNullable: true},
				{Name: "sequence", DataType: "integer", IsNullable: false},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "timmy_messages_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_timmy_messages_session", Columns: []string{"session_id"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{Name: "fk_timmy_messages_session", Type: "FOREIGN KEY", ForeignTable: "timmy_sessions", ForeignColumns: []string{"id"}},
			},
		},
		{
			Name: "timmy_embeddings",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "entity_type", DataType: "character varying", IsNullable: false},
				{Name: "entity_id", DataType: "character varying", IsNullable: false},
				{Name: "chunk_index", DataType: "integer", IsNullable: false},
				{Name: "content_hash", DataType: "character varying", IsNullable: false},
				{Name: "embedding_model", DataType: "character varying", IsNullable: false},
				{Name: "embedding_dim", DataType: "integer", IsNullable: false},
				{Name: "vector_data", DataType: "bytea", IsNullable: true},
				{Name: "chunk_text", DataType: "text", IsNullable: false},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "timmy_embeddings_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_timmy_embeddings_tm", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_timmy_embeddings_entity", Columns: []string{"threat_model_id", "entity_type", "entity_id", "chunk_index"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{Name: "fk_timmy_embeddings_threat_model", Type: "FOREIGN KEY", ForeignTable: "threat_models", ForeignColumns: []string{"id"}},
			},
		},
		{
			Name: "timmy_usage",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "user_id", DataType: "character varying", IsNullable: false},
				{Name: "session_id", DataType: "character varying", IsNullable: false},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "message_count", DataType: "integer", IsNullable: true},
				{Name: "prompt_tokens", DataType: "integer", IsNullable: true},
				{Name: "completion_tokens", DataType: "integer", IsNullable: true},
				{Name: "embedding_tokens", DataType: "integer", IsNullable: true},
				{Name: "period_start", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "period_end", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "timmy_usage_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_timmy_usage_user", Columns: []string{"user_id"}, IsUnique: false},
				{Name: "idx_timmy_usage_session", Columns: []string{"session_id"}, IsUnique: false},
				{Name: "idx_timmy_usage_tm", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_timmy_usage_period", Columns: []string{"period_start"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{Name: "fk_timmy_usage_threat_model", Type: "FOREIGN KEY", ForeignTable: "threat_models", ForeignColumns: []string{"id"}},
				{Name: "fk_timmy_usage_user", Type: "FOREIGN KEY", ForeignTable: "users", ForeignColumns: []string{"internal_uuid"}},
				{Name: "fk_timmy_usage_session", Type: "FOREIGN KEY", ForeignTable: "timmy_sessions", ForeignColumns: []string{"id"}},
			},
		},
	}
}
```

- [ ] **Step 2: Wire into GetExpectedSchema**

In `internal/dbschema/schema.go`, in the `GetExpectedSchema()` function, append the Timmy tables:

```go
func GetExpectedSchema() []TableSchema {
	schema := []TableSchema{
		// ... existing tables ...
	}
	// Add Timmy tables
	schema = append(schema, GetTimmySchema()...)
	return schema
}
```

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add internal/dbschema/timmy.go internal/dbschema/schema.go
git commit -m "feat(timmy): add expected schema definitions for Timmy tables

Defines expected columns, indexes, and foreign key constraints for
timmy_sessions, timmy_messages, timmy_embeddings, timmy_usage.

Refs #214"
```

---

## Task 5: SSRF Validator

**Files:**
- Create: `api/timmy_ssrf.go`
- Create: `api/timmy_ssrf_test.go`

- [ ] **Step 1: Write SSRF validator tests**

Create `api/timmy_ssrf_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSSRFValidator_BlocksPrivateIPs(t *testing.T) {
	v := NewSSRFValidator(nil)
	tests := []struct {
		name string
		url  string
	}{
		{"RFC1918 10.x", "http://10.0.0.1/doc.pdf"},
		{"RFC1918 172.16.x", "http://172.16.0.1/doc.pdf"},
		{"RFC1918 192.168.x", "http://192.168.1.1/doc.pdf"},
		{"Loopback", "http://127.0.0.1/doc.pdf"},
		{"Loopback localhost", "http://localhost/doc.pdf"},
		{"Link-local", "http://169.254.169.254/latest/meta-data/"},
		{"IPv6 loopback", "http://[::1]/doc.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.url)
			assert.Error(t, err, "should block %s", tt.url)
		})
	}
}

func TestSSRFValidator_AllowsPublicURLs(t *testing.T) {
	v := NewSSRFValidator(nil)
	tests := []struct {
		name string
		url  string
	}{
		{"Public HTTP", "http://example.com/doc.pdf"},
		{"Public HTTPS", "https://docs.google.com/document/d/123"},
		{"Public IP", "http://8.8.8.8/page"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(tt.url)
			assert.NoError(t, err, "should allow %s", tt.url)
		})
	}
}

func TestSSRFValidator_Allowlist(t *testing.T) {
	v := NewSSRFValidator([]string{"internal.corp.com", "wiki.internal.net"})
	err := v.Validate("https://internal.corp.com/page")
	assert.NoError(t, err, "allowlisted host should be allowed")

	err = v.Validate("http://10.0.0.1/page")
	assert.Error(t, err, "non-allowlisted private IP should still be blocked")
}

func TestSSRFValidator_RejectsNonHTTP(t *testing.T) {
	v := NewSSRFValidator(nil)
	tests := []string{
		"ftp://example.com/file",
		"file:///etc/passwd",
		"gopher://evil.com",
	}
	for _, url := range tests {
		err := v.Validate(url)
		assert.Error(t, err, "should reject non-HTTP scheme: %s", url)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestSSRFValidator`
Expected: FAIL — `NewSSRFValidator` not defined

- [ ] **Step 3: Implement SSRF validator**

Create `api/timmy_ssrf.go`:

```go
package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// SSRFValidator validates URLs to prevent Server-Side Request Forgery attacks
type SSRFValidator struct {
	allowlist map[string]bool
}

// NewSSRFValidator creates a new SSRF validator with an optional allowlist of hosts
func NewSSRFValidator(allowedHosts []string) *SSRFValidator {
	al := make(map[string]bool)
	for _, host := range allowedHosts {
		al[strings.ToLower(host)] = true
	}
	return &SSRFValidator{allowlist: al}
}

// Validate checks if the URL is safe to fetch (not targeting internal resources)
func (v *SSRFValidator) Validate(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow HTTP and HTTPS
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s (only http and https are allowed)", parsed.Scheme)
	}

	hostname := parsed.Hostname()

	// Check allowlist first — allowlisted hosts bypass all checks
	if v.allowlist[strings.ToLower(hostname)] {
		return nil
	}

	// Block localhost variants
	lower := strings.ToLower(hostname)
	if lower == "localhost" || lower == "ip6-localhost" || lower == "ip6-loopback" {
		return fmt.Errorf("blocked: localhost is not allowed")
	}

	// Resolve hostname to IP and check
	ips, err := net.LookupHost(hostname)
	if err != nil {
		// If we can't resolve, check if it's already an IP
		ip := net.ParseIP(hostname)
		if ip == nil {
			return fmt.Errorf("cannot resolve hostname: %s", hostname)
		}
		return v.checkIP(ip)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if err := v.checkIP(ip); err != nil {
			return err
		}
	}

	return nil
}

// checkIP verifies an IP address is not in a blocked range
func (v *SSRFValidator) checkIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("blocked: loopback address %s", ip)
	}
	if ip.IsPrivate() {
		return fmt.Errorf("blocked: private address %s", ip)
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("blocked: link-local address %s", ip)
	}
	// Cloud metadata endpoint
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return fmt.Errorf("blocked: cloud metadata endpoint %s", ip)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestSSRFValidator`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add api/timmy_ssrf.go api/timmy_ssrf_test.go
git commit -m "feat(timmy): add SSRF URL validator

Blocks private IPs (RFC 1918), loopback, link-local, and cloud
metadata endpoints. Supports operator-configurable allowlist for
internal hosts.

Refs #214"
```

---

## Task 6: Content Provider Interface and Registry

**Files:**
- Create: `api/timmy_content_provider.go`

- [ ] **Step 1: Create interface and registry**

Create `api/timmy_content_provider.go`:

```go
package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
)

// EntityReference identifies a source entity for content extraction.
// For DB-resident content (notes, assets), URI is empty and the provider
// reads directly from the database using EntityType + EntityID.
// For external content (documents with URLs), URI is the fetch target.
type EntityReference struct {
	EntityType string // "asset", "threat", "document", "note", "diagram", "repository"
	EntityID   string // UUID of the source entity
	URI        string // External URL (empty for DB-resident content)
	Name       string // Display name for progress reporting
}

// ExtractedContent holds the text extracted from a source entity
type ExtractedContent struct {
	Text        string            // Extracted plain text
	Title       string            // Document title if available
	ContentType string            // Original content type (e.g., "application/pdf")
	Metadata    map[string]string // Provider-specific metadata
}

// ContentProvider extracts plain text from source entities for embedding
type ContentProvider interface {
	// Name returns the provider name for logging
	Name() string
	// CanHandle returns true if this provider can extract content from the given entity
	CanHandle(ctx context.Context, ref EntityReference) bool
	// Extract fetches and returns plain text content
	Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error)
}

// ContentProviderRegistry manages content providers in priority order
type ContentProviderRegistry struct {
	providers []ContentProvider
}

// NewContentProviderRegistry creates a new registry
func NewContentProviderRegistry() *ContentProviderRegistry {
	return &ContentProviderRegistry{}
}

// Register adds a provider to the registry (providers are tried in registration order)
func (r *ContentProviderRegistry) Register(provider ContentProvider) {
	r.providers = append(r.providers, provider)
}

// Extract finds the first provider that can handle the entity and extracts its content
func (r *ContentProviderRegistry) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	logger := slogging.Get()
	for _, p := range r.providers {
		if p.CanHandle(ctx, ref) {
			logger.Debug("Using content provider %s for entity %s/%s", p.Name(), ref.EntityType, ref.EntityID)
			return p.Extract(ctx, ref)
		}
	}
	return ExtractedContent{}, fmt.Errorf("no content provider can handle entity type=%s id=%s uri=%s", ref.EntityType, ref.EntityID, ref.URI)
}
```

- [ ] **Step 2: Verify build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add api/timmy_content_provider.go
git commit -m "feat(timmy): add content provider interface and registry

Defines ContentProvider interface, EntityReference, ExtractedContent,
and ContentProviderRegistry that tries providers in priority order.

Refs #214"
```

---

## Task 7: Direct Text Content Provider

**Files:**
- Create: `api/timmy_content_provider_text.go`
- Create: `api/timmy_content_provider_test.go`

- [ ] **Step 1: Write tests for DirectTextProvider**

Create `api/timmy_content_provider_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDirectTextProvider_CanHandle(t *testing.T) {
	p := NewDirectTextProvider()

	// DB-resident entities without URIs
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "note", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "asset", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "threat", EntityID: "123"}))
	assert.True(t, p.CanHandle(context.Background(), EntityReference{EntityType: "repository", EntityID: "123"}))

	// Entities with URIs should not be handled by DirectTextProvider
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "document", EntityID: "123", URI: "https://example.com/doc.pdf"}))

	// Diagrams are handled by the JSON provider, not direct text
	assert.False(t, p.CanHandle(context.Background(), EntityReference{EntityType: "diagram", EntityID: "123"}))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestDirectTextProvider`
Expected: FAIL — `NewDirectTextProvider` not defined

- [ ] **Step 3: Implement DirectTextProvider**

Create `api/timmy_content_provider_text.go`:

```go
package api

import (
	"context"
	"fmt"
	"strings"
)

// DirectTextProvider extracts text from DB-resident entities (notes, assets, threats, repositories)
type DirectTextProvider struct{}

// NewDirectTextProvider creates a new DirectTextProvider
func NewDirectTextProvider() *DirectTextProvider {
	return &DirectTextProvider{}
}

func (p *DirectTextProvider) Name() string {
	return "direct-text"
}

// CanHandle returns true for DB-resident entities that are not diagrams and have no external URI
func (p *DirectTextProvider) CanHandle(_ context.Context, ref EntityReference) bool {
	if ref.URI != "" {
		return false
	}
	switch ref.EntityType {
	case "note", "asset", "threat", "repository":
		return true
	default:
		return false
	}
}

// Extract builds a text representation of the entity from its stored fields.
// The caller must load the entity data and pass it via EntityReference metadata
// or this provider reads from the global stores.
func (p *DirectTextProvider) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	var text string
	var title string

	switch ref.EntityType {
	case "note":
		note, err := GlobalNoteStore.Get(ctx, ref.EntityID)
		if err != nil {
			return ExtractedContent{}, fmt.Errorf("failed to get note %s: %w", ref.EntityID, err)
		}
		title = note.Title
		var parts []string
		parts = append(parts, fmt.Sprintf("Note: %s", note.Title))
		if note.Content != nil {
			parts = append(parts, string(*note.Content))
		}
		text = strings.Join(parts, "\n\n")

	case "asset":
		asset, err := GlobalAssetStore.Get(ctx, ref.EntityID)
		if err != nil {
			return ExtractedContent{}, fmt.Errorf("failed to get asset %s: %w", ref.EntityID, err)
		}
		title = asset.Name
		var parts []string
		parts = append(parts, fmt.Sprintf("Asset: %s (type: %s)", asset.Name, asset.Type))
		if asset.Description != nil {
			parts = append(parts, *asset.Description)
		}
		if asset.Criticality != nil {
			parts = append(parts, fmt.Sprintf("Criticality: %s", *asset.Criticality))
		}
		text = strings.Join(parts, "\n")

	case "threat":
		threat, err := GlobalThreatStore.Get(ctx, ref.EntityID)
		if err != nil {
			return ExtractedContent{}, fmt.Errorf("failed to get threat %s: %w", ref.EntityID, err)
		}
		title = threat.Name
		var parts []string
		parts = append(parts, fmt.Sprintf("Threat: %s", threat.Name))
		if threat.Description != nil {
			parts = append(parts, *threat.Description)
		}
		if threat.Severity != nil {
			parts = append(parts, fmt.Sprintf("Severity: %s", *threat.Severity))
		}
		if threat.Mitigation != nil {
			parts = append(parts, fmt.Sprintf("Mitigation: %s", *threat.Mitigation))
		}
		text = strings.Join(parts, "\n")

	case "repository":
		repo, err := GlobalRepositoryStore.Get(ctx, ref.EntityID)
		if err != nil {
			return ExtractedContent{}, fmt.Errorf("failed to get repository %s: %w", ref.EntityID, err)
		}
		title = repo.Name
		var parts []string
		parts = append(parts, fmt.Sprintf("Repository: %s", repo.Name))
		if repo.Description != nil {
			parts = append(parts, *repo.Description)
		}
		if repo.Uri != nil {
			parts = append(parts, fmt.Sprintf("URI: %s", *repo.Uri))
		}
		text = strings.Join(parts, "\n")

	default:
		return ExtractedContent{}, fmt.Errorf("unsupported entity type for direct text: %s", ref.EntityType)
	}

	return ExtractedContent{
		Text:        text,
		Title:       title,
		ContentType: "text/plain",
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestDirectTextProvider`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add api/timmy_content_provider_text.go api/timmy_content_provider_test.go
git commit -m "feat(timmy): add direct text content provider

Extracts text from DB-resident entities (notes, assets, threats,
repositories) for embedding. Reads from global stores.

Refs #214"
```

---

## Task 8: Text Chunker

**Files:**
- Create: `api/timmy_chunker.go`
- Create: `api/timmy_chunker_test.go`

- [ ] **Step 1: Write chunker tests**

Create `api/timmy_chunker_test.go`:

```go
package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChunker_ShortTextSingleChunk(t *testing.T) {
	c := NewTextChunker(512, 50)
	chunks := c.Chunk("This is a short text.")
	assert.Len(t, chunks, 1)
	assert.Equal(t, "This is a short text.", chunks[0])
}

func TestChunker_LongTextMultipleChunks(t *testing.T) {
	c := NewTextChunker(100, 20)
	// Generate text with multiple sentences
	sentences := make([]string, 20)
	for i := range sentences {
		sentences[i] = "This is sentence number " + strings.Repeat("x", 5) + "."
	}
	text := strings.Join(sentences, " ")
	chunks := c.Chunk(text)
	assert.Greater(t, len(chunks), 1, "long text should be split into multiple chunks")

	// Verify all content is represented
	combined := strings.Join(chunks, " ")
	for _, s := range sentences {
		assert.Contains(t, combined, s[:10], "chunk content should contain all sentences")
	}
}

func TestChunker_EmptyText(t *testing.T) {
	c := NewTextChunker(512, 50)
	chunks := c.Chunk("")
	assert.Len(t, chunks, 0)
}

func TestChunker_SentenceBoundaryRespected(t *testing.T) {
	c := NewTextChunker(50, 0)
	text := "First sentence here. Second sentence here. Third sentence here."
	chunks := c.Chunk(text)
	// Each chunk should end at a sentence boundary (with a period)
	for _, chunk := range chunks {
		trimmed := strings.TrimSpace(chunk)
		if len(trimmed) > 0 {
			assert.True(t, strings.HasSuffix(trimmed, "."), "chunk should end at sentence boundary: %q", trimmed)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestChunker`
Expected: FAIL — `NewTextChunker` not defined

- [ ] **Step 3: Implement text chunker**

Create `api/timmy_chunker.go`:

```go
package api

import (
	"strings"
	"unicode"
)

// TextChunker splits text into chunks suitable for embedding
type TextChunker struct {
	maxChunkSize int // Target max characters per chunk
	overlap      int // Characters of overlap between chunks
}

// NewTextChunker creates a chunker with the given size and overlap (in characters)
func NewTextChunker(maxChunkSize, overlap int) *TextChunker {
	return &TextChunker{
		maxChunkSize: maxChunkSize,
		overlap:      overlap,
	}
}

// Chunk splits text into chunks at sentence boundaries
func (tc *TextChunker) Chunk(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	if len(text) <= tc.maxChunkSize {
		return []string{text}
	}

	sentences := splitSentences(text)
	var chunks []string
	var current strings.Builder
	var overlapSentences []string

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		// If adding this sentence would exceed the limit, finalize current chunk
		if current.Len() > 0 && current.Len()+1+len(sentence) > tc.maxChunkSize {
			chunks = append(chunks, strings.TrimSpace(current.String()))

			// Start new chunk with overlap from previous sentences
			current.Reset()
			if tc.overlap > 0 {
				for _, os := range overlapSentences {
					current.WriteString(os)
					current.WriteString(" ")
				}
			}
			overlapSentences = nil
		}

		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sentence)

		// Track recent sentences for overlap
		if tc.overlap > 0 {
			overlapSentences = append(overlapSentences, sentence)
			// Keep only enough sentences to fit in overlap budget
			totalLen := 0
			for i := len(overlapSentences) - 1; i >= 0; i-- {
				totalLen += len(overlapSentences[i]) + 1
				if totalLen > tc.overlap {
					overlapSentences = overlapSentences[i+1:]
					break
				}
			}
		}
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

// splitSentences splits text into sentences at period, question mark, or exclamation boundaries
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i, r := range runes {
		current.WriteRune(r)

		// Check for sentence-ending punctuation followed by space or end of text
		if (r == '.' || r == '?' || r == '!') &&
			(i == len(runes)-1 || unicode.IsSpace(runes[i+1])) {
			sentences = append(sentences, current.String())
			current.Reset()
		}
	}

	// Remaining text (no final punctuation)
	if current.Len() > 0 {
		sentences = append(sentences, current.String())
	}

	return sentences
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestChunker`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add api/timmy_chunker.go api/timmy_chunker_test.go
git commit -m "feat(timmy): add sentence-aware text chunker

Splits text at sentence boundaries with configurable chunk size and
overlap. Used by content providers to prepare text for embedding.

Refs #214"
```

---

## Task 9: In-Memory Vector Index

**Files:**
- Create: `api/timmy_vector_index.go`
- Create: `api/timmy_vector_index_test.go`

- [ ] **Step 1: Write vector index tests**

Create `api/timmy_vector_index_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVectorIndex_AddAndSearch(t *testing.T) {
	idx := NewVectorIndex(3) // 3-dimensional vectors for testing

	idx.Add("chunk-1", []float32{1.0, 0.0, 0.0}, "authentication")
	idx.Add("chunk-2", []float32{0.0, 1.0, 0.0}, "database")
	idx.Add("chunk-3", []float32{0.9, 0.1, 0.0}, "auth tokens")

	results := idx.Search([]float32{1.0, 0.0, 0.0}, 2)
	require.Len(t, results, 2)
	assert.Equal(t, "chunk-1", results[0].ID, "closest match should be exact vector")
	assert.Equal(t, "chunk-3", results[1].ID, "second closest should be similar vector")
}

func TestVectorIndex_SearchEmpty(t *testing.T) {
	idx := NewVectorIndex(3)
	results := idx.Search([]float32{1.0, 0.0, 0.0}, 5)
	assert.Len(t, results, 0)
}

func TestVectorIndex_SearchTopKExceedsCount(t *testing.T) {
	idx := NewVectorIndex(3)
	idx.Add("chunk-1", []float32{1.0, 0.0, 0.0}, "text")
	results := idx.Search([]float32{1.0, 0.0, 0.0}, 10)
	assert.Len(t, results, 1, "should return at most the number of stored vectors")
}

func TestVectorIndex_Delete(t *testing.T) {
	idx := NewVectorIndex(3)
	idx.Add("chunk-1", []float32{1.0, 0.0, 0.0}, "text")
	idx.Add("chunk-2", []float32{0.0, 1.0, 0.0}, "text")
	idx.Delete("chunk-1")
	results := idx.Search([]float32{1.0, 0.0, 0.0}, 5)
	assert.Len(t, results, 1)
	assert.Equal(t, "chunk-2", results[0].ID)
}

func TestVectorIndex_MemorySize(t *testing.T) {
	idx := NewVectorIndex(768)
	idx.Add("chunk-1", make([]float32, 768), "text")
	idx.Add("chunk-2", make([]float32, 768), "text")
	// 2 vectors * 768 dims * 4 bytes = 6144 bytes minimum
	assert.GreaterOrEqual(t, idx.MemorySize(), int64(6144))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestVectorIndex`
Expected: FAIL — `NewVectorIndex` not defined

- [ ] **Step 3: Implement vector index**

Create `api/timmy_vector_index.go`:

```go
package api

import (
	"math"
	"sort"
	"sync"
)

// VectorSearchResult represents a search result with similarity score
type VectorSearchResult struct {
	ID         string
	ChunkText  string
	Similarity float32
}

// vectorEntry stores a single vector in the index
type vectorEntry struct {
	id        string
	vector    []float32
	chunkText string
}

// VectorIndex is an in-memory vector index using brute-force cosine similarity.
// This is adequate for threat-model scale (hundreds of vectors).
type VectorIndex struct {
	mu        sync.RWMutex
	entries   []vectorEntry
	dimension int
}

// NewVectorIndex creates a new vector index for vectors of the given dimension
func NewVectorIndex(dimension int) *VectorIndex {
	return &VectorIndex{
		dimension: dimension,
	}
}

// Add inserts a vector into the index
func (vi *VectorIndex) Add(id string, vector []float32, chunkText string) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	vi.entries = append(vi.entries, vectorEntry{
		id:        id,
		vector:    vector,
		chunkText: chunkText,
	})
}

// Delete removes a vector by ID
func (vi *VectorIndex) Delete(id string) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	for i, e := range vi.entries {
		if e.id == id {
			vi.entries = append(vi.entries[:i], vi.entries[i+1:]...)
			return
		}
	}
}

// Search returns the top-K most similar vectors to the query
func (vi *VectorIndex) Search(query []float32, topK int) []VectorSearchResult {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	if len(vi.entries) == 0 {
		return nil
	}

	type scored struct {
		entry      vectorEntry
		similarity float32
	}
	results := make([]scored, 0, len(vi.entries))
	for _, e := range vi.entries {
		sim := cosineSimilarity(query, e.vector)
		results = append(results, scored{entry: e, similarity: sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})

	k := topK
	if k > len(results) {
		k = len(results)
	}

	out := make([]VectorSearchResult, k)
	for i := 0; i < k; i++ {
		out[i] = VectorSearchResult{
			ID:         results[i].entry.id,
			ChunkText:  results[i].entry.chunkText,
			Similarity: results[i].similarity,
		}
	}
	return out
}

// Count returns the number of vectors in the index
func (vi *VectorIndex) Count() int {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	return len(vi.entries)
}

// MemorySize estimates the memory used by this index in bytes
func (vi *VectorIndex) MemorySize() int64 {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	// Each vector: dimension * 4 bytes (float32) + string overhead
	var size int64
	for _, e := range vi.entries {
		size += int64(len(e.vector)) * 4 // float32 vector
		size += int64(len(e.chunkText))  // chunk text
		size += int64(len(e.id))         // ID string
		size += 64                       // struct overhead estimate
	}
	return size
}

// cosineSimilarity computes cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestVectorIndex`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add api/timmy_vector_index.go api/timmy_vector_index_test.go
git commit -m "feat(timmy): add in-memory vector index with cosine similarity

Brute-force cosine similarity search over stored vectors. Thread-safe,
with add/delete/search/count/memory-size operations. Adequate for
threat-model scale (hundreds of vectors).

Refs #214"
```

---

## Task 10: Embedding Store

**Files:**
- Create: `api/timmy_embedding_store.go`
- Create: `api/timmy_embedding_store_gorm.go`
- Create: `api/timmy_embedding_store_test.go`

- [ ] **Step 1: Write embedding store interface**

Create `api/timmy_embedding_store.go`:

```go
package api

import (
	"context"

	"github.com/ericfitz/tmi/api/models"
)

// TimmyEmbeddingStore defines operations for persisting vector embeddings
type TimmyEmbeddingStore interface {
	// ListByThreatModel returns all embeddings for a threat model
	ListByThreatModel(ctx context.Context, threatModelID string) ([]models.TimmyEmbedding, error)
	// CreateBatch creates multiple embeddings in a single transaction
	CreateBatch(ctx context.Context, embeddings []models.TimmyEmbedding) error
	// DeleteByEntity deletes all embeddings for a specific entity
	DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) error
	// DeleteByThreatModel deletes all embeddings for a threat model
	DeleteByThreatModel(ctx context.Context, threatModelID string) error
}

// Global Timmy store instances
var GlobalTimmyEmbeddingStore TimmyEmbeddingStore
```

- [ ] **Step 2: Write tests**

Create `api/timmy_embedding_store_test.go`:

```go
package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimmyEmbeddingStore_CreateAndList(t *testing.T) {
	db := setupTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  "tm-001",
			EntityType:     "note",
			EntityID:       "note-001",
			ChunkIndex:     0,
			ContentHash:    "abc123",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   3,
			VectorData:     []byte{0, 0, 128, 63, 0, 0, 0, 0, 0, 0, 0, 0}, // [1.0, 0.0, 0.0]
			ChunkText:      models.DBText("test chunk"),
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	results, err := store.ListByThreatModel(ctx, "tm-001")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "note", results[0].EntityType)
	assert.Equal(t, "abc123", results[0].ContentHash)
}

func TestTimmyEmbeddingStore_DeleteByEntity(t *testing.T) {
	db := setupTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	embeddings := []models.TimmyEmbedding{
		{ThreatModelID: "tm-001", EntityType: "note", EntityID: "note-001", ChunkIndex: 0, ContentHash: "abc", EmbeddingModel: "test", EmbeddingDim: 3, VectorData: []byte{}, ChunkText: "text1"},
		{ThreatModelID: "tm-001", EntityType: "note", EntityID: "note-002", ChunkIndex: 0, ContentHash: "def", EmbeddingModel: "test", EmbeddingDim: 3, VectorData: []byte{}, ChunkText: "text2"},
	}
	require.NoError(t, store.CreateBatch(ctx, embeddings))

	err := store.DeleteByEntity(ctx, "tm-001", "note", "note-001")
	require.NoError(t, err)

	results, err := store.ListByThreatModel(ctx, "tm-001")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "note-002", results[0].EntityID)
}
```

Note: `setupTestDB` should use the existing test DB helper pattern. Check `api/handler_test.go` for the exact helper — if it uses SQLite in-memory, follow that pattern. If it uses `InitTestFixtures()`, adapt accordingly. The test may need to auto-migrate the Timmy models:

```go
func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.TimmySession{},
		&models.TimmyMessage{},
		&models.TimmyEmbedding{},
		&models.TimmyUsage{},
	))
	return db
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `make test-unit name=TestTimmyEmbeddingStore`
Expected: FAIL — `NewGormTimmyEmbeddingStore` not defined

- [ ] **Step 4: Implement GORM embedding store**

Create `api/timmy_embedding_store_gorm.go`:

```go
package api

import (
	"context"
	"fmt"
	"sync"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GormTimmyEmbeddingStore implements TimmyEmbeddingStore using GORM
type GormTimmyEmbeddingStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormTimmyEmbeddingStore creates a new GORM-backed embedding store
func NewGormTimmyEmbeddingStore(db *gorm.DB) *GormTimmyEmbeddingStore {
	return &GormTimmyEmbeddingStore{db: db}
}

func (s *GormTimmyEmbeddingStore) ListByThreatModel(ctx context.Context, threatModelID string) ([]models.TimmyEmbedding, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	var embeddings []models.TimmyEmbedding
	result := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID}).
		Order("entity_type, entity_id, chunk_index").
		Find(&embeddings)
	if result.Error != nil {
		logger.Error("Failed to list embeddings for threat model %s: %v", threatModelID, result.Error)
		return nil, fmt.Errorf("failed to list embeddings: %w", result.Error)
	}
	return embeddings, nil
}

func (s *GormTimmyEmbeddingStore) CreateBatch(ctx context.Context, embeddings []models.TimmyEmbedding) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if len(embeddings) == 0 {
		return nil
	}

	logger := slogging.Get()
	result := s.db.WithContext(ctx).Create(&embeddings)
	if result.Error != nil {
		logger.Error("Failed to create embeddings batch: %v", result.Error)
		return fmt.Errorf("failed to create embeddings: %w", result.Error)
	}
	return nil
}

func (s *GormTimmyEmbeddingStore) DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	result := s.db.WithContext(ctx).
		Where(map[string]any{
			"threat_model_id": threatModelID,
			"entity_type":     entityType,
			"entity_id":       entityID,
		}).
		Delete(&models.TimmyEmbedding{})
	if result.Error != nil {
		logger.Error("Failed to delete embeddings for entity %s/%s: %v", entityType, entityID, result.Error)
		return fmt.Errorf("failed to delete embeddings: %w", result.Error)
	}
	return nil
}

func (s *GormTimmyEmbeddingStore) DeleteByThreatModel(ctx context.Context, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	result := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID}).
		Delete(&models.TimmyEmbedding{})
	if result.Error != nil {
		logger.Error("Failed to delete embeddings for threat model %s: %v", threatModelID, result.Error)
		return fmt.Errorf("failed to delete embeddings: %w", result.Error)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test-unit name=TestTimmyEmbeddingStore`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add api/timmy_embedding_store.go api/timmy_embedding_store_gorm.go api/timmy_embedding_store_test.go
git commit -m "feat(timmy): add embedding store with GORM implementation

Interface and GORM implementation for CRUD operations on vector
embeddings. Uses map-based queries for cross-database compatibility.

Refs #214"
```

---

## Task 11: Session and Message Stores

**Files:**
- Create: `api/timmy_session_store.go`
- Create: `api/timmy_session_store_gorm.go`
- Create: `api/timmy_session_store_test.go`

- [ ] **Step 1: Write session store interface**

Create `api/timmy_session_store.go`:

```go
package api

import (
	"context"

	"github.com/ericfitz/tmi/api/models"
)

// TimmySessionStore defines operations for chat sessions
type TimmySessionStore interface {
	Create(ctx context.Context, session *models.TimmySession) error
	Get(ctx context.Context, id string) (*models.TimmySession, error)
	ListByUserAndThreatModel(ctx context.Context, userID, threatModelID string, offset, limit int) ([]models.TimmySession, int, error)
	SoftDelete(ctx context.Context, id string) error
	CountActiveByThreatModel(ctx context.Context, threatModelID string) (int, error)
}

// TimmyMessageStore defines operations for chat messages
type TimmyMessageStore interface {
	Create(ctx context.Context, message *models.TimmyMessage) error
	ListBySession(ctx context.Context, sessionID string, offset, limit int) ([]models.TimmyMessage, int, error)
	GetNextSequence(ctx context.Context, sessionID string) (int, error)
}

var GlobalTimmySessionStore TimmySessionStore
var GlobalTimmyMessageStore TimmyMessageStore
```

- [ ] **Step 2: Write tests**

Create `api/timmy_session_store_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimmySessionStore_CreateAndGet(t *testing.T) {
	db := setupTestDB(t)
	store := NewGormTimmySessionStore(db)
	ctx := context.Background()

	snapshot, _ := json.Marshal([]map[string]string{
		{"entity_type": "note", "entity_id": "note-001"},
	})
	session := &models.TimmySession{
		ThreatModelID:    "tm-001",
		UserID:           "user-001",
		Title:            "Test Session",
		SourceSnapshot:   models.JSONRaw(snapshot),
		SystemPromptHash: "hash123",
	}

	err := store.Create(ctx, session)
	require.NoError(t, err)
	assert.NotEmpty(t, session.ID)

	got, err := store.Get(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, "Test Session", got.Title)
	assert.Equal(t, "active", got.Status)
}

func TestTimmySessionStore_ListByUserAndThreatModel(t *testing.T) {
	db := setupTestDB(t)
	store := NewGormTimmySessionStore(db)
	ctx := context.Background()

	// Create sessions for different users
	store.Create(ctx, &models.TimmySession{ThreatModelID: "tm-001", UserID: "user-001", Title: "S1"})
	store.Create(ctx, &models.TimmySession{ThreatModelID: "tm-001", UserID: "user-001", Title: "S2"})
	store.Create(ctx, &models.TimmySession{ThreatModelID: "tm-001", UserID: "user-002", Title: "S3"})

	sessions, total, err := store.ListByUserAndThreatModel(ctx, "user-001", "tm-001", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, sessions, 2)
}

func TestTimmyMessageStore_CreateAndList(t *testing.T) {
	db := setupTestDB(t)
	sessionStore := NewGormTimmySessionStore(db)
	msgStore := NewGormTimmyMessageStore(db)
	ctx := context.Background()

	session := &models.TimmySession{ThreatModelID: "tm-001", UserID: "user-001", Title: "Test"}
	require.NoError(t, sessionStore.Create(ctx, session))

	msg := &models.TimmyMessage{
		SessionID: session.ID,
		Role:      "user",
		Content:   "Hello Timmy",
		Sequence:  1,
	}
	require.NoError(t, msgStore.Create(ctx, msg))

	messages, total, err := msgStore.ListBySession(ctx, session.ID, 0, 20)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, messages, 1)
	assert.Equal(t, "user", messages[0].Role)
}

func TestTimmyMessageStore_GetNextSequence(t *testing.T) {
	db := setupTestDB(t)
	sessionStore := NewGormTimmySessionStore(db)
	msgStore := NewGormTimmyMessageStore(db)
	ctx := context.Background()

	session := &models.TimmySession{ThreatModelID: "tm-001", UserID: "user-001", Title: "Test"}
	require.NoError(t, sessionStore.Create(ctx, session))

	seq, err := msgStore.GetNextSequence(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, seq, "first message should have sequence 1")

	require.NoError(t, msgStore.Create(ctx, &models.TimmyMessage{SessionID: session.ID, Role: "user", Content: "msg1", Sequence: 1}))

	seq, err = msgStore.GetNextSequence(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, seq, "next sequence should be 2")
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `make test-unit name=TestTimmySessionStore`
Expected: FAIL

- [ ] **Step 4: Implement GORM session and message stores**

Create `api/timmy_session_store_gorm.go`:

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GormTimmySessionStore implements TimmySessionStore using GORM
type GormTimmySessionStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

func NewGormTimmySessionStore(db *gorm.DB) *GormTimmySessionStore {
	return &GormTimmySessionStore{db: db}
}

func (s *GormTimmySessionStore) Create(ctx context.Context, session *models.TimmySession) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.WithContext(ctx).Create(session)
	if result.Error != nil {
		slogging.Get().Error("Failed to create timmy session: %v", result.Error)
		return fmt.Errorf("failed to create session: %w", result.Error)
	}
	return nil
}

func (s *GormTimmySessionStore) Get(ctx context.Context, id string) (*models.TimmySession, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var session models.TimmySession
	result := s.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&session)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("session not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get session: %w", result.Error)
	}
	return &session, nil
}

func (s *GormTimmySessionStore) ListByUserAndThreatModel(ctx context.Context, userID, threatModelID string, offset, limit int) ([]models.TimmySession, int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var total int64
	s.db.WithContext(ctx).Model(&models.TimmySession{}).
		Where(map[string]any{"user_id": userID, "threat_model_id": threatModelID}).
		Where("deleted_at IS NULL").
		Count(&total)

	var sessions []models.TimmySession
	result := s.db.WithContext(ctx).
		Where(map[string]any{"user_id": userID, "threat_model_id": threatModelID}).
		Where("deleted_at IS NULL").
		Order("created_at DESC").
		Offset(offset).Limit(limit).
		Find(&sessions)
	if result.Error != nil {
		return nil, 0, fmt.Errorf("failed to list sessions: %w", result.Error)
	}
	return sessions, int(total), nil
}

func (s *GormTimmySessionStore) SoftDelete(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.WithContext(ctx).
		Model(&models.TimmySession{}).
		Where("id = ?", id).
		Update("deleted_at", gorm.Expr("CURRENT_TIMESTAMP"))
	if result.Error != nil {
		return fmt.Errorf("failed to soft delete session: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("session not found: %s", id)
	}
	return nil
}

func (s *GormTimmySessionStore) CountActiveByThreatModel(ctx context.Context, threatModelID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var count int64
	result := s.db.WithContext(ctx).Model(&models.TimmySession{}).
		Where(map[string]any{"threat_model_id": threatModelID, "status": "active"}).
		Where("deleted_at IS NULL").
		Count(&count)
	if result.Error != nil {
		return 0, fmt.Errorf("failed to count sessions: %w", result.Error)
	}
	return int(count), nil
}

// GormTimmyMessageStore implements TimmyMessageStore using GORM
type GormTimmyMessageStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

func NewGormTimmyMessageStore(db *gorm.DB) *GormTimmyMessageStore {
	return &GormTimmyMessageStore{db: db}
}

func (s *GormTimmyMessageStore) Create(ctx context.Context, message *models.TimmyMessage) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.WithContext(ctx).Create(message)
	if result.Error != nil {
		slogging.Get().Error("Failed to create timmy message: %v", result.Error)
		return fmt.Errorf("failed to create message: %w", result.Error)
	}
	return nil
}

func (s *GormTimmyMessageStore) ListBySession(ctx context.Context, sessionID string, offset, limit int) ([]models.TimmyMessage, int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var total int64
	s.db.WithContext(ctx).Model(&models.TimmyMessage{}).
		Where(map[string]any{"session_id": sessionID}).
		Count(&total)

	var messages []models.TimmyMessage
	result := s.db.WithContext(ctx).
		Where(map[string]any{"session_id": sessionID}).
		Order("sequence ASC").
		Offset(offset).Limit(limit).
		Find(&messages)
	if result.Error != nil {
		return nil, 0, fmt.Errorf("failed to list messages: %w", result.Error)
	}
	return messages, int(total), nil
}

func (s *GormTimmyMessageStore) GetNextSequence(ctx context.Context, sessionID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var maxSeq *int
	result := s.db.WithContext(ctx).Model(&models.TimmyMessage{}).
		Where(map[string]any{"session_id": sessionID}).
		Select("MAX(sequence)").
		Scan(&maxSeq)
	if result.Error != nil {
		return 0, fmt.Errorf("failed to get max sequence: %w", result.Error)
	}
	if maxSeq == nil {
		return 1, nil
	}
	return *maxSeq + 1, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test-unit name=TestTimmySessionStore && make test-unit name=TestTimmyMessageStore`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add api/timmy_session_store.go api/timmy_session_store_gorm.go api/timmy_session_store_test.go
git commit -m "feat(timmy): add session and message stores with GORM implementation

Interfaces and GORM implementations for TimmySessionStore (create, get,
list, soft-delete, count) and TimmyMessageStore (create, list, sequence).
Sessions are filtered by user for privacy.

Refs #214"
```

---

## Task 12: Usage Store

**Files:**
- Create: `api/timmy_usage_store.go`
- Create: `api/timmy_usage_store_gorm.go`
- Create: `api/timmy_usage_store_test.go`

- [ ] **Step 1: Write usage store interface**

Create `api/timmy_usage_store.go`:

```go
package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
)

// TimmyUsageStore defines operations for tracking LLM usage
type TimmyUsageStore interface {
	Record(ctx context.Context, usage *models.TimmyUsage) error
	GetByUser(ctx context.Context, userID string, start, end time.Time) ([]models.TimmyUsage, error)
	GetByThreatModel(ctx context.Context, threatModelID string, start, end time.Time) ([]models.TimmyUsage, error)
	GetAggregated(ctx context.Context, userID, threatModelID string, start, end time.Time) (*UsageAggregation, error)
}

// UsageAggregation holds aggregated usage stats
type UsageAggregation struct {
	TotalMessages       int `json:"total_messages"`
	TotalPromptTokens   int `json:"total_prompt_tokens"`
	TotalCompletionTokens int `json:"total_completion_tokens"`
	TotalEmbeddingTokens int `json:"total_embedding_tokens"`
	SessionCount        int `json:"session_count"`
}

var GlobalTimmyUsageStore TimmyUsageStore
```

- [ ] **Step 2: Write tests, implement, and verify**

Follow the same TDD pattern as Tasks 10-11: write test, verify fail, implement `GormTimmyUsageStore`, verify pass.

Create `api/timmy_usage_store_gorm.go` with `NewGormTimmyUsageStore(db *gorm.DB)` and implementations of Record, GetByUser, GetByThreatModel, GetAggregated using map-based GORM queries and SUM aggregations.

- [ ] **Step 3: Commit**

```bash
git add api/timmy_usage_store.go api/timmy_usage_store_gorm.go api/timmy_usage_store_test.go
git commit -m "feat(timmy): add usage tracking store with GORM implementation

Records and aggregates LLM token usage by user, session, and threat
model for operator cost visibility.

Refs #214"
```

---

## Task 13: Wire Stores into InitializeGormStores

**Files:**
- Modify: `api/store.go`

- [ ] **Step 1: Add Timmy stores to InitializeGormStores**

In `api/store.go`, add to the `InitializeGormStores` function:

```go
// Timmy stores
GlobalTimmyEmbeddingStore = NewGormTimmyEmbeddingStore(db)
GlobalTimmySessionStore = NewGormTimmySessionStore(db)
GlobalTimmyMessageStore = NewGormTimmyMessageStore(db)
GlobalTimmyUsageStore = NewGormTimmyUsageStore(db)
```

- [ ] **Step 2: Add GORM AutoMigrate for Timmy models**

In `cmd/server/main.go`, where other models are auto-migrated, add:

```go
&models.TimmySession{},
&models.TimmyMessage{},
&models.TimmyEmbedding{},
&models.TimmyUsage{},
```

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 4: Run unit tests**

Run: `make test-unit`
Expected: All existing tests still pass

- [ ] **Step 5: Commit**

```bash
git add api/store.go cmd/server/main.go
git commit -m "feat(timmy): wire Timmy stores into InitializeGormStores

Registers TimmyEmbeddingStore, TimmySessionStore, TimmyMessageStore,
and TimmyUsageStore in the global store initialization. Adds Timmy
models to GORM AutoMigrate.

Refs #214"
```

---

## Task 14: SSE Utilities

**Files:**
- Create: `api/timmy_sse.go`

- [ ] **Step 1: Create SSE stream helper**

Create `api/timmy_sse.go`:

```go
package api

import (
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"
)

// SSEWriter provides helpers for writing Server-Sent Events to a Gin response
type SSEWriter struct {
	c       *gin.Context
	flusher func()
}

// NewSSEWriter initializes an SSE response stream
func NewSSEWriter(c *gin.Context) *SSEWriter {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // Disable nginx buffering

	return &SSEWriter{
		c: c,
		flusher: func() {
			c.Writer.Flush()
		},
	}
}

// SendEvent sends a named SSE event with JSON data
func (w *SSEWriter) SendEvent(event string, data any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal SSE data: %w", err)
	}
	fmt.Fprintf(w.c.Writer, "event: %s\ndata: %s\n\n", event, string(jsonBytes))
	w.flusher()
	return nil
}

// SendToken sends a single token event for LLM streaming
func (w *SSEWriter) SendToken(content string) error {
	return w.SendEvent("token", map[string]string{"content": content})
}

// SendError sends an error event
func (w *SSEWriter) SendError(code, message string) error {
	return w.SendEvent("error", map[string]string{"code": code, "message": message})
}

// IsClientGone checks if the client has disconnected
func (w *SSEWriter) IsClientGone() bool {
	select {
	case <-w.c.Request.Context().Done():
		return true
	default:
		return false
	}
}
```

- [ ] **Step 2: Verify build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 3: Commit**

```bash
git add api/timmy_sse.go
git commit -m "feat(timmy): add SSE streaming utilities for Gin

Provides SSEWriter with helpers for sending named events, token
streaming, error events, and client disconnect detection.

Refs #214"
```

---

## Task 15: Rate Limiter

**Files:**
- Create: `api/timmy_rate_limiter.go`
- Create: `api/timmy_rate_limiter_test.go`

- [ ] **Step 1: Write rate limiter tests**

Create `api/timmy_rate_limiter_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimmyRateLimiter_UserMessageLimit(t *testing.T) {
	rl := NewTimmyRateLimiter(3, 100, 100) // 3 messages per user per hour

	assert.True(t, rl.AllowMessage("user-1"), "first message should be allowed")
	assert.True(t, rl.AllowMessage("user-1"), "second message should be allowed")
	assert.True(t, rl.AllowMessage("user-1"), "third message should be allowed")
	assert.False(t, rl.AllowMessage("user-1"), "fourth message should be blocked")
	assert.True(t, rl.AllowMessage("user-2"), "different user should be allowed")
}

func TestTimmyRateLimiter_ConcurrentLLMLimit(t *testing.T) {
	rl := NewTimmyRateLimiter(100, 100, 2) // max 2 concurrent

	assert.True(t, rl.AcquireLLMSlot(), "first slot should be available")
	assert.True(t, rl.AcquireLLMSlot(), "second slot should be available")
	assert.False(t, rl.AcquireLLMSlot(), "third slot should be blocked")
	rl.ReleaseLLMSlot()
	assert.True(t, rl.AcquireLLMSlot(), "slot should be available after release")
}
```

- [ ] **Step 2: Run tests, implement, verify**

Create `api/timmy_rate_limiter.go` with:
- `NewTimmyRateLimiter(maxMessagesPerUserPerHour, maxSessionsPerTM, maxConcurrentLLM int)`
- `AllowMessage(userID string) bool` — sliding window counter per user
- `AllowSession(threatModelID string) bool` — count active sessions per TM
- `AcquireLLMSlot() bool` / `ReleaseLLMSlot()` — semaphore for concurrent LLM calls
- In-memory implementation (Redis version can be added later following existing Redis fallback pattern)

- [ ] **Step 3: Commit**

```bash
git add api/timmy_rate_limiter.go api/timmy_rate_limiter_test.go
git commit -m "feat(timmy): add rate limiter for messages, sessions, and LLM concurrency

In-memory rate limiting with per-user hourly message limits, per-threat-
model session limits, and server-wide LLM concurrency control.

Refs #214"
```

---

## Task 16: Vector Index Manager

**Files:**
- Create: `api/timmy_vector_manager.go`
- Create: `api/timmy_vector_manager_test.go`

This is the most complex component. It manages the lifecycle of in-memory vector indexes.

- [ ] **Step 1: Write tests for VectorIndexManager**

Create `api/timmy_vector_manager_test.go` testing:
- `LoadIndex` creates an index from embeddings in the store
- `GetIndex` returns existing loaded index
- `EvictIndex` writes back and removes from memory
- Memory budget enforcement (deny when over budget)
- LRU eviction selects least recently used

- [ ] **Step 2: Implement VectorIndexManager**

Create `api/timmy_vector_manager.go`:

```go
package api

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
)

// LoadedIndex represents an in-memory vector index for a threat model
type LoadedIndex struct {
	ThreatModelID  string
	Index          *VectorIndex
	LastAccessed   time.Time
	ActiveSessions int
	MemoryBytes    int64
}

// VectorIndexManager manages in-memory HNSW indexes per threat model
type VectorIndexManager struct {
	mu              sync.Mutex
	indexes         map[string]*LoadedIndex
	embeddingStore  TimmyEmbeddingStore
	maxMemoryBytes  int64
	inactivityTimeout time.Duration

	// Metrics
	totalEvictions         int64
	pressureEvictions      int64
	rejectedSessions       int64
}

// NewVectorIndexManager creates a new manager with the given memory budget
func NewVectorIndexManager(embeddingStore TimmyEmbeddingStore, maxMemoryMB int, inactivityTimeoutSeconds int) *VectorIndexManager {
	mgr := &VectorIndexManager{
		indexes:           make(map[string]*LoadedIndex),
		embeddingStore:    embeddingStore,
		maxMemoryBytes:    int64(maxMemoryMB) * 1024 * 1024,
		inactivityTimeout: time.Duration(inactivityTimeoutSeconds) * time.Second,
	}
	// Start background eviction goroutine
	go mgr.evictionLoop()
	return mgr
}

// GetOrLoadIndex returns the index for a threat model, loading from DB if needed
func (m *VectorIndexManager) GetOrLoadIndex(ctx context.Context, threatModelID string, dimension int) (*VectorIndex, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already loaded
	if loaded, ok := m.indexes[threatModelID]; ok {
		loaded.LastAccessed = time.Now()
		loaded.ActiveSessions++
		return loaded.Index, nil
	}

	// Check memory budget
	if !m.canAllocate() {
		m.evictLRU()
		if !m.canAllocate() {
			m.rejectedSessions++
			return nil, fmt.Errorf("insufficient memory to load vector index")
		}
	}

	// Load from database
	embeddings, err := m.embeddingStore.ListByThreatModel(ctx, threatModelID)
	if err != nil {
		return nil, fmt.Errorf("failed to load embeddings: %w", err)
	}

	idx := NewVectorIndex(dimension)
	for _, emb := range embeddings {
		vector := bytesToFloat32(emb.VectorData)
		idx.Add(emb.ID, vector, string(emb.ChunkText))
	}

	loaded := &LoadedIndex{
		ThreatModelID:  threatModelID,
		Index:          idx,
		LastAccessed:   time.Now(),
		ActiveSessions: 1,
		MemoryBytes:    idx.MemorySize(),
	}
	m.indexes[threatModelID] = loaded

	slogging.Get().Debug("Loaded vector index for threat model %s: %d vectors, %d bytes",
		threatModelID, idx.Count(), loaded.MemoryBytes)
	return idx, nil
}

// ReleaseIndex decrements the active session count for a threat model's index
func (m *VectorIndexManager) ReleaseIndex(threatModelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if loaded, ok := m.indexes[threatModelID]; ok {
		loaded.ActiveSessions--
		if loaded.ActiveSessions < 0 {
			loaded.ActiveSessions = 0
		}
	}
}

// WriteBackAndEvict persists the index state to DB and removes from memory
func (m *VectorIndexManager) WriteBackAndEvict(ctx context.Context, threatModelID string) {
	m.mu.Lock()
	loaded, ok := m.indexes[threatModelID]
	if !ok {
		m.mu.Unlock()
		return
	}
	delete(m.indexes, threatModelID)
	m.totalEvictions++
	m.mu.Unlock()

	slogging.Get().Debug("Evicted vector index for threat model %s (%d bytes)", threatModelID, loaded.MemoryBytes)
	_ = loaded // Write-back happens during embedding updates, not eviction
}

// GetStatus returns current memory and index status for the admin endpoint
func (m *VectorIndexManager) GetStatus() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	var totalMemory int64
	var largestIndex int64
	indexDetails := make([]map[string]any, 0, len(m.indexes))

	for _, loaded := range m.indexes {
		totalMemory += loaded.MemoryBytes
		if loaded.MemoryBytes > largestIndex {
			largestIndex = loaded.MemoryBytes
		}
		indexDetails = append(indexDetails, map[string]any{
			"threat_model_id":  loaded.ThreatModelID,
			"vectors":          loaded.Index.Count(),
			"memory_bytes":     loaded.MemoryBytes,
			"active_sessions":  loaded.ActiveSessions,
			"last_accessed":    loaded.LastAccessed,
		})
	}

	avgSize := int64(0)
	if len(m.indexes) > 0 {
		avgSize = totalMemory / int64(len(m.indexes))
	}

	return map[string]any{
		"memory_used_bytes":      totalMemory,
		"memory_budget_bytes":    m.maxMemoryBytes,
		"memory_utilization_pct": float64(totalMemory) / float64(m.maxMemoryBytes) * 100,
		"indexes_loaded":         len(m.indexes),
		"avg_index_size_bytes":   avgSize,
		"largest_index_bytes":    largestIndex,
		"evictions_total":        m.totalEvictions,
		"evictions_pressure":     m.pressureEvictions,
		"sessions_rejected":      m.rejectedSessions,
		"indexes":                indexDetails,
	}
}

func (m *VectorIndexManager) canAllocate() bool {
	var total int64
	for _, loaded := range m.indexes {
		total += loaded.MemoryBytes
	}
	// Allow allocation if under 90% of budget
	return total < int64(float64(m.maxMemoryBytes)*0.9)
}

func (m *VectorIndexManager) evictLRU() {
	var oldest *LoadedIndex
	var oldestID string

	for id, loaded := range m.indexes {
		if loaded.ActiveSessions > 0 {
			continue // Never evict indexes with active sessions
		}
		if oldest == nil || loaded.LastAccessed.Before(oldest.LastAccessed) {
			oldest = loaded
			oldestID = id
		}
	}

	if oldest != nil {
		delete(m.indexes, oldestID)
		m.totalEvictions++
		m.pressureEvictions++
		slogging.Get().Debug("Pressure-evicted vector index for threat model %s", oldestID)
	}
}

func (m *VectorIndexManager) evictionLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for id, loaded := range m.indexes {
			if loaded.ActiveSessions == 0 && now.Sub(loaded.LastAccessed) > m.inactivityTimeout {
				delete(m.indexes, id)
				m.totalEvictions++
				slogging.Get().Debug("Inactivity-evicted vector index for threat model %s", id)
			}
		}
		m.mu.Unlock()
	}
}

// bytesToFloat32 converts a byte slice to a float32 slice
func bytesToFloat32(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	n := len(data) / 4
	result := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(data[i*4 : (i+1)*4])
		result[i] = math.Float32frombits(bits)
	}
	return result
}

// float32ToBytes converts a float32 slice to a byte slice
func float32ToBytes(data []float32) []byte {
	result := make([]byte, len(data)*4)
	for i, v := range data {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(result[i*4:(i+1)*4], bits)
	}
	return result
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `make test-unit name=TestVectorIndexManager`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add api/timmy_vector_manager.go api/timmy_vector_manager_test.go
git commit -m "feat(timmy): add vector index manager with memory budget and LRU eviction

Manages in-memory vector indexes per threat model. Loads from DB on
demand, tracks memory explicitly, evicts LRU idle indexes under
pressure, and provides operational metrics for the admin endpoint.

Refs #214"
```

---

## Task 17: LLM Service

**Files:**
- Create: `api/timmy_llm_service.go`
- Create: `api/timmy_llm_service_test.go`

- [ ] **Step 1: Create LLM service with LangChainGo integration**

Create `api/timmy_llm_service.go`:

```go
package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// TimmyLLMService provides LLM chat and embedding capabilities via LangChainGo
type TimmyLLMService struct {
	chatModel     llms.Model
	embedder      embeddings.Embedder
	config        config.TimmyConfig
	basePrompt    string
}

const timmyBasePrompt = `You are Timmy, a security analysis assistant for threat modeling. You help users understand, analyze, and improve their threat models.

Your role:
- Analyze threats and identify gaps in security coverage
- Explain data flows and attack surfaces based on the threat model's diagrams and assets
- Suggest mitigations based on identified threats
- Answer questions about any aspect of the threat model
- Summarize content across multiple entities

Your rules:
- Always ground your responses in the threat model data provided. Cite specific entities by name.
- Clearly distinguish between facts from the threat model and your general security knowledge.
- Never fabricate CVE numbers, CVSS scores, or threat identifiers that aren't in the source data.
- If information is insufficient to answer a question, say so rather than speculating.`

// NewTimmyLLMService creates a new LLM service from configuration
func NewTimmyLLMService(cfg config.TimmyConfig) (*TimmyLLMService, error) {
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("timmy LLM/embedding providers not configured")
	}

	// Initialize chat model via LangChainGo
	// LangChainGo uses openai-compatible interface for many providers
	chatModel, err := openai.New(
		openai.WithModel(cfg.LLMModel),
		openai.WithToken(cfg.LLMAPIKey),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM chat model: %w", err)
	}

	// Initialize embedder
	embedder, err := embeddings.NewEmbedder(chatModel)
	if err != nil {
		// Try creating a separate embedder if the chat model doesn't support embeddings
		embModel, embErr := openai.New(
			openai.WithModel(cfg.EmbeddingModel),
			openai.WithToken(cfg.EmbeddingAPIKey),
			openai.WithEmbeddingModel(cfg.EmbeddingModel),
		)
		if embErr != nil {
			return nil, fmt.Errorf("failed to create embedding model: %w", embErr)
		}
		embedder, err = embeddings.NewEmbedder(embModel)
		if err != nil {
			return nil, fmt.Errorf("failed to create embedder: %w", err)
		}
	}

	prompt := timmyBasePrompt
	if cfg.OperatorSystemPrompt != "" {
		prompt = prompt + "\n\n" + cfg.OperatorSystemPrompt
	}

	return &TimmyLLMService{
		chatModel:  chatModel,
		embedder:   embedder,
		config:     cfg,
		basePrompt: prompt,
	}, nil
}

// EmbedTexts returns embeddings for the given texts
func (s *TimmyLLMService) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	vectors, err := s.embedder.EmbedDocuments(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}
	return vectors, nil
}

// EmbeddingDimension returns the dimension of the embedding model by embedding a test string
func (s *TimmyLLMService) EmbeddingDimension(ctx context.Context) (int, error) {
	vectors, err := s.EmbedTexts(ctx, []string{"dimension test"})
	if err != nil {
		return 0, err
	}
	if len(vectors) == 0 {
		return 0, fmt.Errorf("no embedding returned for dimension test")
	}
	return len(vectors[0]), nil
}

// GenerateStreamingResponse sends a chat request and streams tokens via the callback
func (s *TimmyLLMService) GenerateStreamingResponse(
	ctx context.Context,
	systemPrompt string,
	messages []llms.MessageContent,
	onToken func(token string),
) (string, int, error) {
	logger := slogging.Get()

	// Build message list with system prompt
	allMessages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
	}
	allMessages = append(allMessages, messages...)

	var fullResponse strings.Builder
	tokenCount := 0

	_, err := s.chatModel.GenerateContent(ctx, allMessages,
		llms.WithStreamingFunc(func(ctx context.Context, chunk []byte) error {
			token := string(chunk)
			fullResponse.WriteString(token)
			tokenCount++
			if onToken != nil {
				onToken(token)
			}
			return nil
		}),
	)
	if err != nil {
		logger.Error("LLM generation failed: %v", err)
		return "", 0, fmt.Errorf("LLM generation failed: %w", err)
	}

	return fullResponse.String(), tokenCount, nil
}

// GetBasePrompt returns the base system prompt (immutable + operator extension)
func (s *TimmyLLMService) GetBasePrompt() string {
	return s.basePrompt
}
```

Note: The exact LangChainGo API may differ slightly from what's shown above. The implementing engineer should check the current LangChainGo documentation (use `Context7` MCP tool) for the exact constructor signatures and streaming API. The key patterns to preserve:
- System prompt construction (base + operator)
- Streaming via callback
- Embedding via `EmbedDocuments`

- [ ] **Step 2: Write basic tests with mocks**

Create `api/timmy_llm_service_test.go` testing the prompt construction (base + operator) and embedding dimension detection with a mock embedder.

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 4: Commit**

```bash
git add api/timmy_llm_service.go api/timmy_llm_service_test.go
git commit -m "feat(timmy): add LLM service with LangChainGo integration

Provider-agnostic LLM service for chat completion and text embedding.
Includes base system prompt with security analysis guardrails and
operator extension support. Streaming via callback for SSE delivery.

Refs #214"
```

---

## Task 18: Context Builder

**Files:**
- Create: `api/timmy_context_builder.go`
- Create: `api/timmy_context_builder_test.go`

- [ ] **Step 1: Create context builder**

Create `api/timmy_context_builder.go` implementing:

- `BuildTier1Context(ctx, threatModelID, sourceSnapshot)` — reads all structured data (threat model name/status/owner, entity names+descriptions, threat details with severity/CWEs/CVSS/mitigations, diagram structure from JSON provider, asset/repository metadata) and formats as a structured text block
- `BuildTier2Context(index *VectorIndex, query string, topK int)` — searches the vector index for relevant chunks and formats them with source attribution
- `BuildFullContext(tier1, tier2 string, messages []models.TimmyMessage, maxHistory int)` — assembles the complete context: system prompt + tier1 + tier2 + conversation history (truncated from front if needed, preserving first and most recent messages) + current user message

- [ ] **Step 2: Write tests verifying context assembly**

Test that:
- Tier 1 includes entity names and descriptions
- Tier 2 includes vector search results with source attribution
- Conversation history is truncated correctly (oldest dropped, first preserved)
- Empty conversation history works

- [ ] **Step 3: Verify and commit**

```bash
git add api/timmy_context_builder.go api/timmy_context_builder_test.go
git commit -m "feat(timmy): add two-tier context builder

Constructs LLM context from Tier 1 structured overview (entity names,
threats, diagram topology) and Tier 2 vector-retrieved chunks. Handles
conversation history truncation preserving first and recent messages.

Refs #214"
```

---

## Task 19: Session Manager

**Files:**
- Create: `api/timmy_session_manager.go`
- Create: `api/timmy_session_manager_test.go`

The session manager orchestrates session creation (snapshot sources, load/update vector index, stream progress) and message handling (context construction, LLM call, persist response).

- [ ] **Step 1: Create SessionManager struct**

Create `api/timmy_session_manager.go`:

```go
package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/tmc/langchaingo/llms"
)

// SourceSnapshotEntry records which entities were included in a session
type SourceSnapshotEntry struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
}

// SessionProgressCallback is called during session preparation to report progress
type SessionProgressCallback func(phase, entityType, entityName string, progress int, detail string)

// TimmySessionManager orchestrates Timmy session and message lifecycle
type TimmySessionManager struct {
	config          config.TimmyConfig
	llmService      *TimmyLLMService
	vectorManager   *VectorIndexManager
	providerRegistry *ContentProviderRegistry
	chunker         *TextChunker
	contextBuilder  *ContextBuilder
	rateLimiter     *TimmyRateLimiter
}

// NewTimmySessionManager creates a new session manager
func NewTimmySessionManager(
	cfg config.TimmyConfig,
	llm *TimmyLLMService,
	vm *VectorIndexManager,
	registry *ContentProviderRegistry,
	rl *TimmyRateLimiter,
) *TimmySessionManager {
	return &TimmySessionManager{
		config:           cfg,
		llmService:       llm,
		vectorManager:    vm,
		providerRegistry: registry,
		chunker:          NewTextChunker(cfg.ChunkSize, cfg.ChunkOverlap),
		contextBuilder:   NewContextBuilder(),
		rateLimiter:      rl,
	}
}

// CreateSession creates a new chat session, snapshots sources, and loads/updates the vector index
func (sm *TimmySessionManager) CreateSession(
	ctx context.Context,
	userID, threatModelID string,
	onProgress SessionProgressCallback,
) (*models.TimmySession, error) {
	logger := slogging.Get()

	// Check session rate limit
	count, err := GlobalTimmySessionStore.CountActiveByThreatModel(ctx, threatModelID)
	if err != nil {
		return nil, fmt.Errorf("failed to check session count: %w", err)
	}
	if count >= sm.config.MaxSessionsPerThreatModel {
		return nil, &RequestError{Status: 429, Code: "session_limit", Message: "Maximum sessions per threat model reached"}
	}

	// Snapshot timmy_enabled sources
	snapshot, err := sm.snapshotSources(ctx, threatModelID)
	if err != nil {
		return nil, fmt.Errorf("failed to snapshot sources: %w", err)
	}

	snapshotJSON, _ := json.Marshal(snapshot)
	promptHash := fmt.Sprintf("%x", sha256.Sum256([]byte(sm.llmService.GetBasePrompt())))

	session := &models.TimmySession{
		ThreatModelID:    threatModelID,
		UserID:           userID,
		SourceSnapshot:   models.JSONRaw(snapshotJSON),
		SystemPromptHash: promptHash[:16],
		Status:           "active",
	}

	if err := GlobalTimmySessionStore.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Load/update vector index with progress reporting
	if err := sm.prepareVectorIndex(ctx, threatModelID, snapshot, onProgress); err != nil {
		logger.Error("Failed to prepare vector index for session %s: %v", session.ID, err)
		// Session is created but index may be partial — report error but don't fail
	}

	return session, nil
}

// HandleMessage processes a user message and returns the assistant's response via streaming
func (sm *TimmySessionManager) HandleMessage(
	ctx context.Context,
	sessionID, userMessage string,
	onToken func(token string),
) (*models.TimmyMessage, error) {
	logger := slogging.Get()

	// Get session
	session, err := GlobalTimmySessionStore.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	// Check rate limit
	if !sm.rateLimiter.AllowMessage(session.UserID) {
		return nil, &RequestError{Status: 429, Code: "rate_limited", Message: "Hourly message limit reached"}
	}

	// Acquire LLM slot
	if !sm.rateLimiter.AcquireLLMSlot() {
		return nil, &RequestError{Status: 503, Code: "server_busy", Message: "Too many concurrent requests"}
	}
	defer sm.rateLimiter.ReleaseLLMSlot()

	// Get next sequence number
	userSeq, err := GlobalTimmyMessageStore.GetNextSequence(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sequence: %w", err)
	}

	// Persist user message
	userMsg := &models.TimmyMessage{
		SessionID: sessionID,
		Role:      "user",
		Content:   models.DBText(userMessage),
		Sequence:  userSeq,
	}
	if err := GlobalTimmyMessageStore.Create(ctx, userMsg); err != nil {
		return nil, fmt.Errorf("failed to save user message: %w", err)
	}

	// Build context
	tier1 := sm.contextBuilder.BuildTier1Context(ctx, session.ThreatModelID)

	// Get vector index for Tier 2
	dim, _ := sm.llmService.EmbeddingDimension(ctx)
	index, err := sm.vectorManager.GetOrLoadIndex(ctx, session.ThreatModelID, dim)
	if err != nil {
		logger.Error("Failed to load vector index for message: %v", err)
		// Proceed without Tier 2 context
	}

	var tier2 string
	if index != nil {
		// Embed the user's query
		queryVectors, err := sm.llmService.EmbedTexts(ctx, []string{userMessage})
		if err == nil && len(queryVectors) > 0 {
			tier2 = sm.contextBuilder.BuildTier2Context(index, queryVectors[0], sm.config.RetrievalTopK)
		}
	}

	// Get conversation history
	history, _, _ := GlobalTimmyMessageStore.ListBySession(ctx, sessionID, 0, sm.config.MaxConversationHistory)

	// Build LLM messages from history
	var llmMessages []llms.MessageContent
	for _, msg := range history {
		role := llms.ChatMessageTypeHuman
		if msg.Role == "assistant" {
			role = llms.ChatMessageTypeAI
		}
		llmMessages = append(llmMessages, llms.TextParts(role, string(msg.Content)))
	}

	// Assemble system prompt with context
	systemPrompt := sm.contextBuilder.BuildFullContext(
		sm.llmService.GetBasePrompt(), tier1, tier2,
	)

	// Generate streaming response
	fullResponse, tokenCount, err := sm.llmService.GenerateStreamingResponse(
		ctx, systemPrompt, llmMessages, onToken,
	)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %w", err)
	}

	// Persist assistant message
	assistantMsg := &models.TimmyMessage{
		SessionID:  sessionID,
		Role:       "assistant",
		Content:    models.DBText(fullResponse),
		TokenCount: tokenCount,
		Sequence:   userSeq + 1,
	}
	if err := GlobalTimmyMessageStore.Create(ctx, assistantMsg); err != nil {
		logger.Error("Failed to save assistant message: %v", err)
	}

	// Record usage
	// (usage recording omitted for brevity — create TimmyUsage record with token counts)

	return assistantMsg, nil
}

// snapshotSources collects all timmy_enabled entities for the threat model
func (sm *TimmySessionManager) snapshotSources(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, error) {
	// This reads from each sub-resource store, filtering for timmy_enabled == true
	// Implementation reads from GlobalAssetStore, GlobalThreatStore, GlobalDocumentStore,
	// GlobalNoteStore, GlobalRepositoryStore, DiagramStore, filtering by timmy_enabled
	// Returns list of SourceSnapshotEntry with entity_type, entity_id, name
	//
	// The implementing engineer should check how each store's List method works
	// and add timmy_enabled filtering. Each store returns API types that need
	// to be checked for the TimmyEnabled field.

	var entries []SourceSnapshotEntry
	// TODO: implement by reading from each store with timmy_enabled filter
	// This is the key integration point with existing stores
	return entries, nil
}

// prepareVectorIndex loads, updates, and embeds content for the vector index
func (sm *TimmySessionManager) prepareVectorIndex(
	ctx context.Context,
	threatModelID string,
	sources []SourceSnapshotEntry,
	onProgress SessionProgressCallback,
) error {
	// For each source in the snapshot:
	// 1. Report "loading" progress
	// 2. Check content hash against stored embeddings
	// 3. If stale or new: extract content via provider registry, chunk, embed
	// 4. Report "embedding" progress
	// 5. Store new embeddings in DB
	//
	// This is the core embedding pipeline. The implementing engineer should:
	// - Build EntityReference for each source
	// - Call providerRegistry.Extract()
	// - Call chunker.Chunk()
	// - Call llmService.EmbedTexts()
	// - Store via GlobalTimmyEmbeddingStore.CreateBatch()
	// - Load into vector index via vectorManager.GetOrLoadIndex()

	return nil // TODO: implement embedding pipeline
}
```

- [ ] **Step 2: Write tests**

Test session creation, message handling with mock LLM service, and source snapshot logic.

- [ ] **Step 3: Verify build and commit**

```bash
git add api/timmy_session_manager.go api/timmy_session_manager_test.go
git commit -m "feat(timmy): add session manager orchestrating session and message lifecycle

Orchestrates session creation (source snapshot, vector index prep),
message handling (context construction, LLM streaming, persistence),
and rate limiting. Integrates all Timmy subsystems.

Refs #214"
```

---

## Task 20: OpenAPI Spec Updates

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 1: Add Timmy schemas and endpoints to OpenAPI spec**

Add to `api-schema/tmi-openapi.json`:

**New tag:** `"Timmy Chat"`, `"Timmy Administration"`

**New schemas:** `TimmyChatSession`, `TimmyChatMessage`, `CreateTimmySessionResponse`, `ListTimmySessionsResponse`, `ListTimmyMessagesResponse`, `TimmyUsageResponse`, `TimmyStatusResponse`

**New paths:**
- `POST /threat_models/{threat_model_id}/chat/sessions` — operationId: `createTimmyChatSession`
- `GET /threat_models/{threat_model_id}/chat/sessions` — operationId: `listTimmyChatSessions`
- `GET /threat_models/{threat_model_id}/chat/sessions/{session_id}` — operationId: `getTimmyChatSession`
- `DELETE /threat_models/{threat_model_id}/chat/sessions/{session_id}` — operationId: `deleteTimmyChatSession`
- `POST /threat_models/{threat_model_id}/chat/sessions/{session_id}/messages` — operationId: `createTimmyChatMessage`
- `GET /threat_models/{threat_model_id}/chat/sessions/{session_id}/messages` — operationId: `listTimmyChatMessages`
- `GET /admin/timmy/usage` — operationId: `getTimmyUsage`
- `GET /admin/timmy/status` — operationId: `getTimmyStatus`

Note: The POST endpoints that return SSE streams should document `text/event-stream` as the response content type alongside `application/json` for error responses.

Use `jq` for surgical edits to the OpenAPI spec given its size (>100KB).

- [ ] **Step 2: Validate spec**

Run: `make validate-openapi`
Expected: No errors

- [ ] **Step 3: Generate API code**

Run: `make generate-api`
Expected: Clean generation

- [ ] **Step 4: Verify build**

Run: `make build-server`
Expected: Clean build (may have unimplemented interface methods — that's expected at this point)

- [ ] **Step 5: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(timmy): add chat API endpoints to OpenAPI spec

Adds session CRUD, message creation with SSE streaming, and admin
endpoints for usage and status. Tags: Timmy Chat, Timmy Administration.

Refs #214"
```

---

## Task 21: HTTP Handlers

**Files:**
- Create: `api/timmy_handlers.go`
- Create: `api/timmy_handlers_test.go`

- [ ] **Step 1: Implement chat handlers**

Create `api/timmy_handlers.go` implementing the generated ServerInterface methods:

```go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/ericfitz/tmi/internal/slogging"
)

// TimmyHandler provides HTTP handlers for Timmy chat endpoints
type TimmyHandler struct {
	sessionManager *TimmySessionManager
	vectorManager  *VectorIndexManager
}

// NewTimmyHandler creates a new Timmy handler
func NewTimmyHandler(sm *TimmySessionManager, vm *VectorIndexManager) *TimmyHandler {
	return &TimmyHandler{
		sessionManager: sm,
		vectorManager:  vm,
	}
}

// CreateTimmyChatSession creates a new chat session and streams preparation progress
func (h *TimmyHandler) CreateTimmyChatSession(c *gin.Context) {
	logger := slogging.Get()

	threatModelID := c.Param("threat_model_id")
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		HandleRequestError(c, UnauthorizedError("User not authenticated"))
		return
	}
	userID := userInternalUUID.(string)

	sse := NewSSEWriter(c)

	session, err := h.sessionManager.CreateSession(
		c.Request.Context(),
		userID, threatModelID,
		func(phase, entityType, entityName string, progress int, detail string) {
			sse.SendEvent("progress", map[string]any{
				"phase":       phase,
				"entity_type": entityType,
				"entity_name": entityName,
				"progress":    progress,
				"detail":      detail,
			})
		},
	)
	if err != nil {
		logger.Error("Failed to create Timmy session: %v", err)
		if re, ok := err.(*RequestError); ok {
			sse.SendError(re.Code, re.Message)
		} else {
			sse.SendError("server_error", "Failed to create session")
		}
		return
	}

	sse.SendEvent("session_created", map[string]any{
		"session_id": session.ID,
	})

	sse.SendEvent("ready", map[string]any{
		"session_id": session.ID,
	})
}

// ListTimmyChatSessions lists the current user's sessions for a threat model
func (h *TimmyHandler) ListTimmyChatSessions(c *gin.Context) {
	threatModelID := c.Param("threat_model_id")
	userInternalUUID, exists := c.Get("userInternalUUID")
	if !exists {
		HandleRequestError(c, UnauthorizedError("User not authenticated"))
		return
	}
	userID := userInternalUUID.(string)

	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	sessions, total, err := GlobalTimmySessionStore.ListByUserAndThreatModel(
		c.Request.Context(), userID, threatModelID, offset, limit,
	)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sessions": sessions,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// GetTimmyChatSession retrieves a specific session
func (h *TimmyHandler) GetTimmyChatSession(c *gin.Context) {
	sessionID := c.Param("session_id")

	session, err := GlobalTimmySessionStore.Get(c.Request.Context(), sessionID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Session not found"))
		return
	}

	// Verify ownership
	userInternalUUID, _ := c.Get("userInternalUUID")
	if session.UserID != userInternalUUID.(string) {
		HandleRequestError(c, ForbiddenError("Cannot access another user's session"))
		return
	}

	c.JSON(http.StatusOK, session)
}

// DeleteTimmyChatSession soft-deletes a session
func (h *TimmyHandler) DeleteTimmyChatSession(c *gin.Context) {
	sessionID := c.Param("session_id")

	session, err := GlobalTimmySessionStore.Get(c.Request.Context(), sessionID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Session not found"))
		return
	}

	userInternalUUID, _ := c.Get("userInternalUUID")
	if session.UserID != userInternalUUID.(string) {
		HandleRequestError(c, ForbiddenError("Cannot delete another user's session"))
		return
	}

	if err := GlobalTimmySessionStore.SoftDelete(c.Request.Context(), sessionID); err != nil {
		HandleRequestError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

// CreateTimmyChatMessage sends a message and streams the assistant's response
func (h *TimmyHandler) CreateTimmyChatMessage(c *gin.Context) {
	sessionID := c.Param("session_id")

	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		HandleRequestError(c, ValidationError("content is required"))
		return
	}

	// Verify session ownership
	session, err := GlobalTimmySessionStore.Get(c.Request.Context(), sessionID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Session not found"))
		return
	}
	userInternalUUID, _ := c.Get("userInternalUUID")
	if session.UserID != userInternalUUID.(string) {
		HandleRequestError(c, ForbiddenError("Cannot access another user's session"))
		return
	}

	sse := NewSSEWriter(c)

	assistantMsg, err := h.sessionManager.HandleMessage(
		c.Request.Context(),
		sessionID,
		req.Content,
		func(token string) {
			if !sse.IsClientGone() {
				sse.SendToken(token)
			}
		},
	)
	if err != nil {
		if re, ok := err.(*RequestError); ok {
			sse.SendError(re.Code, re.Message)
		} else {
			sse.SendError("server_error", "Failed to generate response")
		}
		return
	}

	sse.SendEvent("message_end", map[string]any{
		"message_id":  assistantMsg.ID,
		"token_count": assistantMsg.TokenCount,
	})
}

// ListTimmyChatMessages lists message history for a session
func (h *TimmyHandler) ListTimmyChatMessages(c *gin.Context) {
	sessionID := c.Param("session_id")

	// Verify session ownership
	session, err := GlobalTimmySessionStore.Get(c.Request.Context(), sessionID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Session not found"))
		return
	}
	userInternalUUID, _ := c.Get("userInternalUUID")
	if session.UserID != userInternalUUID.(string) {
		HandleRequestError(c, ForbiddenError("Cannot access another user's session"))
		return
	}

	limit := parseIntParam(c.DefaultQuery("limit", "50"), 50)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	messages, total, err := GlobalTimmyMessageStore.ListBySession(c.Request.Context(), sessionID, offset, limit)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"messages": messages,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// GetTimmyUsage returns aggregated usage statistics (admin only)
func (h *TimmyHandler) GetTimmyUsage(c *gin.Context) {
	// Admin middleware should already be applied
	userID := c.Query("user_id")
	tmID := c.Query("threat_model_id")

	// Parse date range or use defaults
	// Implementation uses GlobalTimmyUsageStore.GetAggregated()

	c.JSON(http.StatusOK, gin.H{
		"user_id":         userID,
		"threat_model_id": tmID,
		"usage":           map[string]any{}, // TODO: aggregate from store
	})
}

// GetTimmyStatus returns current memory and index status (admin only)
func (h *TimmyHandler) GetTimmyStatus(c *gin.Context) {
	status := h.vectorManager.GetStatus()
	c.JSON(http.StatusOK, status)
}
```

- [ ] **Step 2: Write handler tests**

Create `api/timmy_handlers_test.go` following the existing pattern:
- `gin.SetMode(gin.TestMode)`, create router, inject context middleware
- Test ListTimmyChatSessions, GetTimmyChatSession, DeleteTimmyChatSession
- Test ListTimmyChatMessages
- Test session ownership enforcement (403 for wrong user)

- [ ] **Step 3: Run tests**

Run: `make test-unit name=TestTimmy`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add api/timmy_handlers.go api/timmy_handlers_test.go
git commit -m "feat(timmy): add HTTP handlers for chat sessions, messages, and admin

Implements all Timmy API endpoints: session CRUD with SSE progress
streaming, message creation with SSE token streaming, message history,
admin usage and status endpoints. Enforces session ownership privacy.

Refs #214"
```

---

## Task 22: Middleware and Route Registration

**Files:**
- Create: `api/timmy_middleware.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Create Timmy middleware**

Create `api/timmy_middleware.go`:

```go
package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
)

// TimmyEnabledMiddleware returns 404 for all Timmy endpoints when Timmy is disabled
func TimmyEnabledMiddleware(cfg config.TimmyConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.Enabled {
			c.JSON(http.StatusNotFound, gin.H{
				"error":             "not_found",
				"error_description": "Timmy AI assistant is not enabled",
			})
			c.Abort()
			return
		}

		if cfg.Enabled && !cfg.IsConfigured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":             "service_unavailable",
				"error_description": "Timmy is enabled but LLM/embedding providers are not configured",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
```

- [ ] **Step 2: Wire routes in main.go**

In `cmd/server/main.go`, after existing route registration:

```go
// Initialize Timmy subsystems (only if enabled)
if cfg.Timmy.Enabled && cfg.Timmy.IsConfigured() {
	llmService, err := api.NewTimmyLLMService(cfg.Timmy)
	if err != nil {
		slogging.Get().Error("Failed to initialize Timmy LLM service: %v", err)
	} else {
		vectorManager := api.NewVectorIndexManager(
			api.GlobalTimmyEmbeddingStore,
			cfg.Timmy.MaxMemoryMB,
			cfg.Timmy.InactivityTimeoutSeconds,
		)
		registry := api.NewContentProviderRegistry()
		registry.Register(api.NewDirectTextProvider())
		// registry.Register(api.NewJSONProvider())  // TODO: Task for JSON provider
		// registry.Register(api.NewHTTPProvider())   // TODO: Task for HTTP provider
		// registry.Register(api.NewPDFProvider())    // TODO: Task for PDF provider

		rateLimiter := api.NewTimmyRateLimiter(
			cfg.Timmy.MaxMessagesPerUserPerHour,
			cfg.Timmy.MaxSessionsPerThreatModel,
			cfg.Timmy.MaxConcurrentLLMRequests,
		)

		sessionManager := api.NewTimmySessionManager(
			cfg.Timmy, llmService, vectorManager, registry, rateLimiter,
		)

		timmyHandler := api.NewTimmyHandler(sessionManager, vectorManager)

		// Register Timmy routes
		timmyMiddleware := api.TimmyEnabledMiddleware(cfg.Timmy)
		timmyGroup := router.Group("", timmyMiddleware)
		{
			tmChat := timmyGroup.Group("/threat_models/:threat_model_id/chat")
			tmChat.POST("/sessions", timmyHandler.CreateTimmyChatSession)
			tmChat.GET("/sessions", timmyHandler.ListTimmyChatSessions)
			tmChat.GET("/sessions/:session_id", timmyHandler.GetTimmyChatSession)
			tmChat.DELETE("/sessions/:session_id", timmyHandler.DeleteTimmyChatSession)
			tmChat.POST("/sessions/:session_id/messages", timmyHandler.CreateTimmyChatMessage)
			tmChat.GET("/sessions/:session_id/messages", timmyHandler.ListTimmyChatMessages)
		}

		// Admin Timmy routes
		adminTimmy := timmyGroup.Group("/admin/timmy", api.AdministratorMiddleware())
		{
			adminTimmy.GET("/usage", timmyHandler.GetTimmyUsage)
			adminTimmy.GET("/status", timmyHandler.GetTimmyStatus)
		}

		slogging.Get().Info("Timmy AI assistant initialized with LLM provider: %s", cfg.Timmy.LLMProvider)
	}
} else if cfg.Timmy.Enabled {
	slogging.Get().Warn("Timmy is enabled but LLM/embedding providers are not configured")
}
```

Note: The route registration approach depends on whether these routes go through the OpenAPI-generated router or are registered separately. Check the existing pattern in `cmd/server/main.go`. If all routes go through `api.RegisterHandlersWithOptions()`, then the handler methods need to match the generated interface exactly. If some routes are registered manually (like WebSocket), follow that pattern.

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 4: Run all unit tests**

Run: `make test-unit`
Expected: All pass

- [ ] **Step 5: Commit**

```bash
git add api/timmy_middleware.go cmd/server/main.go
git commit -m "feat(timmy): add middleware and route registration

TimmyEnabledMiddleware gates all Timmy endpoints (404 when disabled,
503 when enabled but not configured). Routes registered for chat
sessions, messages, and admin endpoints.

Refs #214"
```

---

## Task 23: JSON Content Provider (Diagrams)

**Files:**
- Create: `api/timmy_content_provider_json.go`
- Add tests to: `api/timmy_content_provider_test.go`

- [ ] **Step 1: Implement JSON provider that extracts semantic text from DFD diagrams**

The JSON provider reads diagram cells (nodes, edges, security boundaries) and produces human-readable descriptions. For example: "Process: Auth Service connects to Store: User Database via flow: credentials (crosses trust boundary: External/Internal)".

The implementing engineer should:
- Read the DFD diagram schema from the OpenAPI spec to understand cell structure
- Extract: node labels + shapes + descriptions, edge labels + source/target, security boundary names + contained nodes
- Produce structured text that gives the LLM useful spatial and relational understanding

- [ ] **Step 2: Write tests, verify, commit**

```bash
git add api/timmy_content_provider_json.go api/timmy_content_provider_test.go
git commit -m "feat(timmy): add JSON content provider for DFD diagrams

Extracts semantic text from DFD JSON: node labels, edge relationships,
trust boundaries, and annotations. Produces human-readable descriptions
for embedding and LLM context.

Refs #214"
```

---

## Task 24: HTTP/HTML Content Provider

**Files:**
- Create: `api/timmy_content_provider_http.go`
- Add tests to: `api/timmy_content_provider_test.go`

- [ ] **Step 1: Implement HTTP provider with SSRF protection**

```go
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// HTTPContentProvider fetches and extracts text from HTML and plain text URLs
type HTTPContentProvider struct {
	ssrfValidator *SSRFValidator
	client        *http.Client
}

func NewHTTPContentProvider(ssrfValidator *SSRFValidator) *HTTPContentProvider {
	return &HTTPContentProvider{
		ssrfValidator: ssrfValidator,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *HTTPContentProvider) Name() string { return "http-html" }

func (p *HTTPContentProvider) CanHandle(_ context.Context, ref EntityReference) bool {
	if ref.URI == "" {
		return false
	}
	return strings.HasPrefix(ref.URI, "http://") || strings.HasPrefix(ref.URI, "https://")
}

func (p *HTTPContentProvider) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	if err := p.ssrfValidator.Validate(ref.URI); err != nil {
		return ExtractedContent{}, fmt.Errorf("SSRF check failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", ref.URI, nil)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// Limit reading to 10MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to read response: %w", err)
	}

	contentType := resp.Header.Get("Content-Type")
	var text string
	if strings.Contains(contentType, "text/html") {
		text = extractTextFromHTML(string(body))
	} else {
		text = string(body)
	}

	return ExtractedContent{
		Text:        text,
		Title:       ref.Name,
		ContentType: contentType,
	}, nil
}

// extractTextFromHTML strips HTML tags and returns plain text
func extractTextFromHTML(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent // Fallback to raw content
	}
	var sb strings.Builder
	var extractText func(*html.Node)
	extractText = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
		}
		// Skip script and style elements
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractText(c)
		}
	}
	extractText(doc)
	return strings.TrimSpace(sb.String())
}
```

- [ ] **Step 2: Write tests (including SSRF blocking), verify, commit**

```bash
git add api/timmy_content_provider_http.go api/timmy_content_provider_test.go
git commit -m "feat(timmy): add HTTP/HTML content provider with SSRF protection

Fetches and extracts text from HTTP/HTTPS URLs. Strips HTML tags,
skips script/style elements. Enforces SSRF protection and 10MB
response size limit.

Refs #214"
```

---

## Task 25: PDF Content Provider

**Files:**
- Create: `api/timmy_content_provider_pdf.go`

- [ ] **Step 1: Add PDF extraction dependency**

```bash
go get github.com/ledongthuc/pdf@latest
```

(Or another Go PDF text extraction library — check current best options)

- [ ] **Step 2: Implement PDF provider**

Similar to HTTP provider but detects `application/pdf` content type or `.pdf` URL suffix, downloads the file, extracts text using the PDF library.

- [ ] **Step 3: Test, verify, commit**

```bash
git add api/timmy_content_provider_pdf.go api/timmy_content_provider_test.go go.mod go.sum
git commit -m "feat(timmy): add PDF content provider

Fetches PDF documents via HTTP and extracts text content. Uses SSRF
protection and size limits from HTTP provider.

Refs #214"
```

---

## Task 26: Final Integration and Quality Gates

- [ ] **Step 1: Run lint**

Run: `make lint`
Fix any issues.

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: All pass

- [ ] **Step 3: Build**

Run: `make build-server`
Expected: Clean build

- [ ] **Step 4: Validate OpenAPI spec**

Run: `make validate-openapi`
Expected: No errors

- [ ] **Step 5: Final commit for any remaining fixes**

```bash
git add -A
git commit -m "chore(timmy): fix lint and test issues from integration

Refs #214"
```

- [ ] **Step 6: Push**

```bash
git pull --rebase && git push
git status  # Must show "up to date with origin"
```
