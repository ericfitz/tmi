# WebSocket Ticket Authentication Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace JWT tokens in WebSocket URLs with short-lived, single-use, session-scoped tickets (AUTH-VULN-007 remediation).

**Architecture:** New TicketStore (interface + in-memory + Redis) issues 30s single-use tickets scoped to collaboration sessions. New `GET /ws/ticket` REST endpoint issues tickets. WebSocket upgrade handler validates `?ticket=` instead of `?token=`. User identity enriched via DB lookup after ticket validation.

**Tech Stack:** Go, Gin, oapi-codegen, Redis (GETDEL for atomicity), crypto/rand

**Spec:** `docs/superpowers/specs/2026-03-13-ws-ticket-auth-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `api/ticket_store.go` | TicketStore interface + InMemoryTicketStore |
| `api/ticket_store_redis.go` | RedisTicketStore using GETDEL |
| `api/ticket_store_test.go` | Unit tests for both store implementations |
| `api/ws_ticket_handler.go` | GetWsTicket REST handler |
| `api/ws_ticket_handler_test.go` | Handler unit tests |
| `api/server.go` | Add ticketStore field + setter |
| `api-schema/tmi-openapi.json` | Add GET /ws/ticket endpoint |
| `api/api.go` | Regenerated from OpenAPI spec |
| `cmd/server/main.go` | Wire up TicketStore + TicketValidator |
| `cmd/server/jwt_auth.go` | Ticket-based WebSocket auth |
| `api-schema/tmi-asyncapi.yml` | Update security scheme |
| `wstest/main.go` | Use ticket flow |

---

## Chunk 1: Ticket Store

### Task 1: TicketStore Interface and InMemoryTicketStore

**Files:**
- Create: `api/ticket_store.go`
- Create: `api/ticket_store_test.go`

- [ ] **Step 1: Write failing tests for InMemoryTicketStore**

Create `api/ticket_store_test.go`:

```go
package api

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryTicketStore_IssueAndValidate(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	ticket, err := store.IssueTicket(ctx, "user123", "tmi", "session456", 30*time.Second)
	if err != nil {
		t.Fatalf("IssueTicket failed: %v", err)
	}
	if ticket == "" {
		t.Fatal("IssueTicket returned empty ticket")
	}

	userID, provider, sessionID, err := store.ValidateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("ValidateTicket failed: %v", err)
	}
	if userID != "user123" {
		t.Errorf("expected userID 'user123', got '%s'", userID)
	}
	if provider != "tmi" {
		t.Errorf("expected provider 'tmi', got '%s'", provider)
	}
	if sessionID != "session456" {
		t.Errorf("expected sessionID 'session456', got '%s'", sessionID)
	}
}

func TestInMemoryTicketStore_SingleUse(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	ticket, _ := store.IssueTicket(ctx, "user123", "tmi", "session456", 30*time.Second)

	// First validation should succeed
	_, _, _, err := store.ValidateTicket(ctx, ticket)
	if err != nil {
		t.Fatalf("first ValidateTicket should succeed: %v", err)
	}

	// Second validation should fail (single-use)
	_, _, _, err = store.ValidateTicket(ctx, ticket)
	if err == nil {
		t.Fatal("second ValidateTicket should fail (single-use)")
	}
}

func TestInMemoryTicketStore_Expired(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	ticket, _ := store.IssueTicket(ctx, "user123", "tmi", "session456", 1*time.Millisecond)

	// Wait for expiry
	time.Sleep(10 * time.Millisecond)

	_, _, _, err := store.ValidateTicket(ctx, ticket)
	if err == nil {
		t.Fatal("ValidateTicket should fail for expired ticket")
	}
}

func TestInMemoryTicketStore_InvalidTicket(t *testing.T) {
	store := NewInMemoryTicketStore()
	defer store.Close()
	ctx := context.Background()

	_, _, _, err := store.ValidateTicket(ctx, "nonexistent-ticket")
	if err == nil {
		t.Fatal("ValidateTicket should fail for invalid ticket")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestInMemoryTicketStore`
Expected: FAIL — `NewInMemoryTicketStore` not defined

- [ ] **Step 3: Implement TicketStore interface and InMemoryTicketStore**

Create `api/ticket_store.go`. The interface stores `provider` alongside `userID` so that `fetchAndSetUserObject` in `jwt_auth.go` can look up the user by `(provider, provider_user_id)` — the same lookup path used by the JWT claims extractor (see `cmd/server/jwt_auth.go:268-274`):

```go
package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// TicketStore manages short-lived, single-use WebSocket authentication tickets.
type TicketStore interface {
	// IssueTicket creates a ticket bound to a user, provider, and session, returning the ticket string.
	IssueTicket(ctx context.Context, userID, provider, sessionID string, ttl time.Duration) (string, error)
	// ValidateTicket validates and consumes a ticket (single-use). Returns the bound userID, provider, and sessionID.
	ValidateTicket(ctx context.Context, ticket string) (userID, provider, sessionID string, err error)
}

type ticketEntry struct {
	UserID    string
	Provider  string
	SessionID string
	ExpiresAt time.Time
}

// InMemoryTicketStore implements TicketStore using in-memory storage.
type InMemoryTicketStore struct {
	mu      sync.Mutex
	tickets map[string]*ticketEntry
	cleanup *time.Ticker
	done    chan bool
}

// NewInMemoryTicketStore creates a new in-memory ticket store.
func NewInMemoryTicketStore() *InMemoryTicketStore {
	store := &InMemoryTicketStore{
		tickets: make(map[string]*ticketEntry),
		cleanup: time.NewTicker(30 * time.Second),
		done:    make(chan bool),
	}
	go store.cleanupExpired()
	return store
}

func (s *InMemoryTicketStore) IssueTicket(_ context.Context, userID, provider, sessionID string, ttl time.Duration) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(tokenBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tickets[ticket] = &ticketEntry{
		UserID:    userID,
		Provider:  provider,
		SessionID: sessionID,
		ExpiresAt: time.Now().Add(ttl),
	}

	return ticket, nil
}

func (s *InMemoryTicketStore) ValidateTicket(_ context.Context, ticket string) (string, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.tickets[ticket]
	if !exists {
		return "", "", "", fmt.Errorf("ticket not found")
	}

	// Delete immediately (single-use)
	delete(s.tickets, ticket)

	if time.Now().After(entry.ExpiresAt) {
		return "", "", "", fmt.Errorf("ticket expired")
	}

	return entry.UserID, entry.Provider, entry.SessionID, nil
}

func (s *InMemoryTicketStore) cleanupExpired() {
	for {
		select {
		case <-s.cleanup.C:
			s.mu.Lock()
			now := time.Now()
			for ticket, entry := range s.tickets {
				if now.After(entry.ExpiresAt) {
					delete(s.tickets, ticket)
				}
			}
			s.mu.Unlock()
		case <-s.done:
			s.cleanup.Stop()
			return
		}
	}
}

// Close stops the cleanup goroutine.
func (s *InMemoryTicketStore) Close() {
	close(s.done)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestInMemoryTicketStore`
Expected: PASS (all 4 tests)

- [ ] **Step 5: Lint**

Run: `make lint`
Expected: No new issues

- [ ] **Step 6: Commit**

```bash
git add api/ticket_store.go api/ticket_store_test.go
git commit -m "feat(api): add TicketStore interface and InMemoryTicketStore

Implements short-lived, single-use, session-scoped tickets for
WebSocket authentication. Stores provider alongside userID for
proper user lookup. Part of AUTH-VULN-007 remediation.

Ref #173"
```

---

### Task 2: RedisTicketStore

**Files:**
- Create: `api/ticket_store_redis.go`
- Modify: `api/ticket_store_test.go`

- [ ] **Step 1: Write failing test for RedisTicketStore**

Append to `api/ticket_store_test.go`:

```go
func TestRedisTicketStore_ImplementsInterface(t *testing.T) {
	// Compile-time check that RedisTicketStore implements TicketStore
	var _ TicketStore = (*RedisTicketStore)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestRedisTicketStore_ImplementsInterface`
Expected: FAIL — `RedisTicketStore` not defined

- [ ] **Step 3: Implement RedisTicketStore**

Create `api/ticket_store_redis.go`. Note: `IssueTicket` writes via `redis.Set()` (the `db.RedisDB` wrapper) and `ValidateTicket` reads via `redis.GetClient().GetDel()` (raw client, bypassing the wrapper). This is intentional — `ws_ticket:` keys are not in `sensitiveKeyPrefixes` so encryption is not applied. If encryption is later needed for these keys, both paths must be updated.

```go
package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
)

type ticketData struct {
	UserID    string `json:"user_id"`
	Provider  string `json:"provider"`
	SessionID string `json:"session_id"`
}

// RedisTicketStore implements TicketStore using Redis with atomic GETDEL for single-use semantics.
type RedisTicketStore struct {
	redis *db.RedisDB
}

// NewRedisTicketStore creates a new Redis-backed ticket store.
func NewRedisTicketStore(redis *db.RedisDB) *RedisTicketStore {
	return &RedisTicketStore{redis: redis}
}

func (s *RedisTicketStore) ticketKey(ticket string) string {
	return fmt.Sprintf("ws_ticket:%s", ticket)
}

func (s *RedisTicketStore) IssueTicket(ctx context.Context, userID, provider, sessionID string, ttl time.Duration) (string, error) {
	logger := slogging.Get()

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(tokenBytes)

	data, err := json.Marshal(ticketData{
		UserID:    userID,
		Provider:  provider,
		SessionID: sessionID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal ticket data: %w", err)
	}

	if err := s.redis.Set(ctx, s.ticketKey(ticket), string(data), ttl); err != nil {
		logger.Error("Failed to store ticket in Redis: %v", err)
		return "", fmt.Errorf("failed to store ticket: %w", err)
	}

	return ticket, nil
}

func (s *RedisTicketStore) ValidateTicket(ctx context.Context, ticket string) (string, string, string, error) {
	logger := slogging.Get()
	key := s.ticketKey(ticket)

	// Atomic get-and-delete to prevent race conditions (Redis 6.2+)
	result, err := s.redis.GetClient().GetDel(ctx, key).Result()
	if err != nil {
		logger.Debug("Ticket validation failed (not found or expired): %v", err)
		return "", "", "", fmt.Errorf("ticket not found or expired")
	}

	var data ticketData
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		logger.Error("Failed to unmarshal ticket data: %v", err)
		return "", "", "", fmt.Errorf("invalid ticket data")
	}

	return data.UserID, data.Provider, data.SessionID, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestRedisTicketStore_ImplementsInterface`
Expected: PASS

- [ ] **Step 5: Lint**

Run: `make lint`
Expected: No new issues

- [ ] **Step 6: Commit**

```bash
git add api/ticket_store_redis.go api/ticket_store_test.go
git commit -m "feat(api): add RedisTicketStore with atomic GETDEL

Redis-backed ticket store using GETDEL for atomic single-use
ticket validation. Part of AUTH-VULN-007 remediation.

Ref #173"
```

---

## Chunk 2: OpenAPI Spec, Code Generation, and Handler

### Task 3: Add GET /ws/ticket to OpenAPI Spec

**Files:**
- Modify: `api-schema/tmi-openapi.json`

The OpenAPI spec is large (>100KB). Use `jq` for surgical updates.

- [ ] **Step 1: Add WsTicketResponse schema to components/schemas**

Use `jq` to add the response schema:

```bash
jq '.components.schemas.WsTicketResponse = {
  "type": "object",
  "required": ["ticket"],
  "properties": {
    "ticket": {
      "type": "string",
      "description": "Short-lived, single-use authentication ticket for WebSocket connection"
    }
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Add GET /ws/ticket path**

Use `jq` to add the endpoint. The `session_id` is a required query parameter. Add standard error responses matching existing patterns. Note: the 400 case for missing/invalid `session_id` is handled automatically by the OpenAPI validation middleware (the parameter is `required: true` with `format: uuid`), but we still document it for API consumers:

```bash
jq '.paths["/ws/ticket"] = {
  "get": {
    "operationId": "getWsTicket",
    "summary": "Get a WebSocket authentication ticket",
    "description": "Issues a short-lived, single-use authentication ticket for establishing a WebSocket connection to a collaboration session. The ticket is scoped to the specified session and the authenticated user. Tickets expire after 30 seconds and can only be used once.",
    "tags": ["WebSocket"],
    "security": [{"bearerAuth": []}],
    "parameters": [
      {
        "name": "session_id",
        "in": "query",
        "required": true,
        "schema": {
          "type": "string",
          "format": "uuid"
        },
        "description": "The collaboration session ID the ticket is scoped to"
      }
    ],
    "responses": {
      "200": {
        "description": "Ticket issued successfully",
        "headers": {
          "Cache-Control": {
            "schema": {"type": "string"},
            "description": "Set to no-store to prevent caching"
          }
        },
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/WsTicketResponse"
            }
          }
        }
      },
      "400": {
        "description": "Missing or invalid session_id parameter",
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/Error"
            }
          }
        }
      },
      "401": {
        "description": "Unauthorized - missing or invalid authentication",
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/Error"
            }
          }
        }
      },
      "404": {
        "description": "Session not found or user is not a participant",
        "content": {
          "application/json": {
            "schema": {
              "$ref": "#/components/schemas/Error"
            }
          }
        }
      }
    }
  }
}' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 3: Validate the OpenAPI spec**

Run: `make validate-openapi`
Expected: Validation passes

- [ ] **Step 4: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` regenerated with `GetWsTicket` in the `ServerInterface`

- [ ] **Step 5: Build to check generated code compiles (expect failure — handler not yet implemented)**

Run: `make build-server`
Expected: FAIL — `Server` does not implement `GetWsTicket` from `ServerInterface`

- [ ] **Step 6: Commit spec and generated code**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add GET /ws/ticket to OpenAPI spec

Adds WebSocket ticket endpoint with session_id query parameter,
WsTicketResponse schema, and standard error responses.

Ref #173"
```

---

### Task 4: Implement Handler with Tests (TDD)

**Files:**
- Modify: `api/server.go` (lines 15-54 for struct, add setter after line 159)
- Create: `api/ws_ticket_handler.go`
- Create: `api/ws_ticket_handler_test.go`
- Modify: `api/websocket.go` (add FindSessionByID, IsUserInSession)

- [ ] **Step 1: Add ticketStore to Server struct and setter**

In `api/server.go`, add `ticketStore TicketStore` field to the Server struct (after `configProvider` on line 53), and add a setter method after the existing setters (around line 159):

```go
// In Server struct, add field:
ticketStore TicketStore

// Add setter method:
// SetTicketStore sets the ticket store for WebSocket authentication
func (s *Server) SetTicketStore(ticketStore TicketStore) {
	s.ticketStore = ticketStore
}
```

- [ ] **Step 2: Add FindSessionByID to WebSocketHub**

In `api/websocket.go`, add a helper method. `FindSessionByID` iterates `h.Diagrams` to find a session by its ID field:

```go
// FindSessionByID finds a collaboration session by its session ID.
func (h *WebSocketHub) FindSessionByID(sessionID string) *DiagramSession {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, session := range h.Diagrams {
		if session.ID == sessionID {
			return session
		}
	}
	return nil
}
```

Note: We do NOT add an `IsUserInSession` method because checking connected clients would fail for non-host participants requesting their first ticket (they haven't connected via WebSocket yet). Instead, the handler checks threat model read access — the same authorization used by `GetDiagramCollaborate` and `CreateDiagramCollaborate`.

- [ ] **Step 3: Write failing handler tests**

Create `api/ws_ticket_handler_test.go`. Check the generated `api/api.go` for the exact `GetWsTicketParams` struct definition before writing tests. Use patterns from existing test files in `api/` for setting up Gin test context and mocking auth context values.

Key test cases:
- Returns 200 with ticket for valid session + authenticated user with read access
- Returns 401 for unauthenticated request
- Returns 404 for nonexistent session
- Returns 403/404 when user lacks read access to the threat model
- Sets `Cache-Control: no-store` header

- [ ] **Step 4: Run tests to verify they fail**

Run: `make test-unit name=TestGetWsTicket`
Expected: FAIL — `GetWsTicket` not defined

- [ ] **Step 5: Implement GetWsTicket handler**

Create `api/ws_ticket_handler.go`. The handler uses threat model read access (via `CheckResourceAccessFromContext`) to authorize ticket issuance — the same authorization pattern used by `GetDiagramCollaborate` (see `threat_model_diagram_handlers.go:640`). This correctly handles non-host participants who haven't yet connected via WebSocket:

```go
package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ericfitz/tmi/internal/slogging"
)

const wsTicketTTL = 30 * time.Second

// GetWsTicket issues a short-lived WebSocket authentication ticket.
func (s *Server) GetWsTicket(c *gin.Context, params GetWsTicketParams) {
	logger := slogging.GetContextLogger(c)

	if s.ticketStore == nil {
		logger.Error("TicketStore not configured")
		HandleRequestError(c, ServerError("WebSocket tickets not available"))
		return
	}

	// Get authenticated user
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
	if err != nil {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Authentication required")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Authentication required to obtain a WebSocket ticket",
		})
		return
	}

	sessionID := params.SessionId.String()

	// Find the session in the WebSocket hub
	session := s.wsHub.FindSessionByID(sessionID)
	if session == nil {
		HandleRequestError(c, NotFoundError("Collaboration session not found"))
		return
	}

	// Authorize: check that the user has read access to the threat model
	// associated with this session. This is the same check used by
	// GetDiagramCollaborate (threat_model_diagram_handlers.go:640).
	// We use threat model access rather than checking connected clients
	// because the user hasn't connected via WebSocket yet — that's
	// the whole point of getting a ticket.
	tm, err := ThreatModelStore.Get(session.ThreatModelID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Collaboration session not found"))
		return
	}

	hasReadAccess, err := CheckResourceAccessFromContext(c, userEmail, tm, RoleReader)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	if !hasReadAccess {
		HandleRequestError(c, NotFoundError("Collaboration session not found"))
		return
	}

	// Get user ID and provider from context for the ticket
	userIDVal, exists := c.Get("userID")
	if !exists {
		HandleRequestError(c, ServerError("User ID not found in context"))
		return
	}
	userID, ok := userIDVal.(string)
	if !ok || userID == "" {
		HandleRequestError(c, ServerError("Invalid user ID in context"))
		return
	}

	provider := c.GetString("userProvider")

	// Issue ticket
	ticket, err := s.ticketStore.IssueTicket(c.Request.Context(), userID, provider, sessionID, wsTicketTTL)
	if err != nil {
		logger.Error("Failed to issue WebSocket ticket: %v", err)
		HandleRequestError(c, ServerError("Failed to issue ticket"))
		return
	}

	logger.Info("Issued WebSocket ticket for user %s, session %s", userEmail, sessionID)

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, WsTicketResponse{
		Ticket: ticket,
	})
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `make test-unit name=TestGetWsTicket`
Expected: PASS

- [ ] **Step 7: Build and lint**

Run: `make build-server && make lint`
Expected: Both PASS

- [ ] **Step 8: Commit**

```bash
git add api/server.go api/ws_ticket_handler.go api/ws_ticket_handler_test.go api/websocket.go
git commit -m "feat(api): implement GetWsTicket handler with tests

Issues session-scoped, single-use WebSocket tickets. Stores provider
alongside userID for proper user lookup. Adds FindSessionByID helper
to WebSocketHub. Uses threat model access check for authorization.

Ref #173"
```

---

## Chunk 3: WebSocket Auth and Server Wiring

### Task 5: Update WebSocket Auth and Wire TicketStore

This task combines the JWT middleware changes and server wiring to avoid circular dependencies — both `TicketValidator` and `TicketStore` wiring go into `cmd/server/main.go` together.

**Files:**
- Modify: `cmd/server/jwt_auth.go` (lines 24-40 for ExtractToken, add TicketValidator)
- Modify: `cmd/server/main.go` (wiring)

- [ ] **Step 1: Update TokenExtractor.ExtractToken for ticket-based WebSocket auth**

In `cmd/server/jwt_auth.go`, modify the `ExtractToken` method (lines 28-39):

1. **Exclude `/ws/ticket` from WebSocket path detection**: The `GET /ws/ticket` endpoint is a REST call that needs normal JWT auth (Bearer/cookie), not WebSocket-style ticket extraction.

2. **Replace `?token=` with `?ticket=`** for actual WebSocket paths.

The modified WebSocket detection block becomes:

```go
	// For WebSocket connections (but NOT /ws/ticket which is a REST endpoint)
	isWebSocketPath := strings.HasPrefix(c.Request.URL.Path, "/ws/") || strings.HasSuffix(c.Request.URL.Path, "/ws")
	isTicketEndpoint := c.Request.URL.Path == "/ws/ticket"

	if isWebSocketPath && !isTicketEndpoint {
		ticketStr := c.Query("ticket")
		if ticketStr != "" {
			// Ticket-based auth — return with prefix marker so JWTAuthenticator
			// knows to use TicketValidator instead of JWT validation.
			return "ticket:" + ticketStr, nil
		}
		logger.Warn("Authentication failed: Missing ticket for WebSocket path: %s", c.Request.URL.Path)
		return "", fmt.Errorf("missing ticket")
	}
```

- [ ] **Step 2: Add TicketValidator to jwt_auth.go**

Add a `TicketValidator` struct in `cmd/server/jwt_auth.go`. It validates the ticket, cross-checks session_id, and populates user context by looking up the user via `(provider, provider_user_id)` — the same lookup path used by `fetchAndSetUserObject` (see lines 268-274):

```go
// TicketValidator handles WebSocket ticket validation
type TicketValidator struct {
	ticketStore  api.TicketStore
	authHandlers *auth.Handlers
	config       *config.Config
}

// NewTicketValidator creates a new ticket validator
func NewTicketValidator(ticketStore api.TicketStore, authHandlers *auth.Handlers, cfg *config.Config) *TicketValidator {
	return &TicketValidator{
		ticketStore:  ticketStore,
		authHandlers: authHandlers,
		config:       cfg,
	}
}

// ValidateTicket validates a WebSocket ticket and populates user context.
func (v *TicketValidator) ValidateTicket(c *gin.Context, ticketStr string) error {
	logger := slogging.GetContextLogger(c)

	userID, provider, sessionID, err := v.ticketStore.ValidateTicket(c.Request.Context(), ticketStr)
	if err != nil {
		logger.Warn("WebSocket ticket validation failed: %v", err)
		return fmt.Errorf("invalid or expired ticket")
	}

	// Cross-check session_id from ticket against query param
	querySessionID := c.Query("session_id")
	if querySessionID != "" && querySessionID != sessionID {
		logger.Warn("WebSocket ticket session_id mismatch: ticket=%s, query=%s", sessionID, querySessionID)
		return fmt.Errorf("ticket session mismatch")
	}

	// Set basic context from ticket data
	c.Set("userID", userID)
	c.Set("userProvider", provider)
	c.Set("userIdP", provider)

	// Look up full user from database using (provider, provider_user_id)
	// This mirrors the fetchAndSetUserObject pattern in ClaimsExtractor
	dbManager := db.GetGlobalManager()
	if dbManager == nil {
		logger.Error("Database manager not available for ticket user lookup")
		return fmt.Errorf("database not available")
	}

	service, err := auth.NewService(dbManager, auth.ConfigFromUnified(v.config))
	if err != nil {
		logger.Error("Failed to create auth service for ticket user lookup: %v", err)
		return fmt.Errorf("auth service unavailable")
	}

	if provider != "" && userID != "" {
		user, err := service.GetUserByProviderID(c.Request.Context(), provider, userID)
		if err == nil {
			c.Set("userEmail", user.Email)
			c.Set("userDisplayName", user.DisplayName)
			c.Set(string(auth.UserContextKey), user)
			c.Set("userInternalUUID", user.InternalUUID)
			logger.Debug("WebSocket ticket validated for user %s, session %s", user.Email, sessionID)
			return nil
		}
		logger.Debug("User lookup by provider ID failed: %v", err)
	}

	// Fallback: lookup by email is not possible since ticket doesn't store email.
	// If provider lookup fails, ticket validation fails.
	return fmt.Errorf("user not found for ticket (provider=%s, userID=%s)", provider, userID)
}
```

- [ ] **Step 3: Add ticketValidator to JWTAuthenticator and update AuthenticateRequest**

Add `ticketValidator *TicketValidator` field to `JWTAuthenticator` struct (line 306-312). Update `NewJWTAuthenticator` to accept and store it. Update `AuthenticateRequest` (line 326) to branch when the extracted token starts with `"ticket:"`:

```go
// In JWTAuthenticator struct, add field:
ticketValidator *TicketValidator

// In NewJWTAuthenticator, add parameter and assignment:
func NewJWTAuthenticator(cfg *config.Config, tokenBlacklist *auth.TokenBlacklist,
    authHandlers *auth.Handlers, ticketValidator *TicketValidator) *JWTAuthenticator {
    return &JWTAuthenticator{
        // ... existing fields ...
        ticketValidator: ticketValidator,
    }
}

// In AuthenticateRequest, after ExtractToken (line 330-338), add ticket branch:
func (a *JWTAuthenticator) AuthenticateRequest(c *gin.Context) error {
    logger := slogging.GetContextLogger(c)

    tokenStr, err := a.tokenExtractor.ExtractToken(c)
    if err != nil {
        return &AuthError{
            Code:        "unauthorized",
            Description: "Authentication required",
            StatusCode:  http.StatusUnauthorized,
        }
    }

    // Check for ticket-based WebSocket auth
    if strings.HasPrefix(tokenStr, "ticket:") {
        ticketStr := strings.TrimPrefix(tokenStr, "ticket:")
        if a.ticketValidator == nil {
            logger.Error("Ticket validator not configured")
            return &AuthError{
                Code:        "server_error",
                Description: "Ticket validation not available",
                StatusCode:  http.StatusInternalServerError,
            }
        }
        if err := a.ticketValidator.ValidateTicket(c, ticketStr); err != nil {
            return &AuthError{
                Code:        "unauthorized",
                Description: "Authentication required",
                StatusCode:  http.StatusUnauthorized,
            }
        }
        return nil
    }

    // ... rest of existing JWT validation flow (ValidateToken, CheckBlacklist, ExtractAndSetClaims) ...
```

- [ ] **Step 4: Wire TicketStore and TicketValidator in main.go**

In `cmd/server/main.go`:

1. After the addon invocation store initialization (around line 769), initialize the ticket store. Check if Redis is available; fall back to in-memory if not:

```go
// Initialize ticket store for WebSocket authentication
logger.Info("Initializing WebSocket ticket store")
var ticketStore api.TicketStore
if dbManager != nil && dbManager.Redis() != nil {
    ticketStore = api.NewRedisTicketStore(dbManager.Redis())
} else {
    logger.Warn("Redis not available, using in-memory ticket store (not suitable for multi-instance deployments)")
    ticketStore = api.NewInMemoryTicketStore()
}
apiServer.SetTicketStore(ticketStore)
```

2. Update the `JWTMiddleware` function signature (line 208) to accept a `*TicketValidator`:

```go
func JWTMiddleware(cfg *config.Config, tokenBlacklist *auth.TokenBlacklist,
    authHandlers *auth.Handlers, ticketValidator *TicketValidator) gin.HandlerFunc {
    authenticator := NewJWTAuthenticator(cfg, tokenBlacklist, authHandlers, ticketValidator)
    // ... rest unchanged ...
```

3. Update the `JWTMiddleware` call site (line 826) to pass the ticket validator:

```go
ticketValidator := NewTicketValidator(ticketStore, authHandlers, cfg)
r.Use(JWTMiddleware(config, server.tokenBlacklist, authHandlers, ticketValidator))
```

The `ticketStore` variable must be initialized before this call. Since store initialization (step 1) happens around line 769 and `JWTMiddleware` is called at line 826, ordering is correct.

- [ ] **Step 5: Build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 6: Lint**

Run: `make lint`
Expected: No new issues

- [ ] **Step 7: Write tests for TicketValidator session_id mismatch**

Add tests to verify the security-critical session_id cross-check. These can go in a new file `cmd/server/jwt_auth_ticket_test.go` or be added to existing test files. Key test cases:

- Ticket with matching session_id query param succeeds
- Ticket with mismatching session_id query param returns error
- Ticket with no session_id query param succeeds (cross-check is skipped)

- [ ] **Step 8: Run tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add cmd/server/jwt_auth.go cmd/server/main.go cmd/server/jwt_auth_ticket_test.go
git commit -m "feat(auth): replace ?token= with ?ticket= for WebSocket auth

WebSocket connections now require a ticket from GET /ws/ticket.
Tickets are validated atomically via TicketStore and user context
is populated via (provider, userID) database lookup. The /ws/ticket
REST endpoint itself uses normal JWT auth (Bearer/cookie).
Falls back to in-memory ticket store if Redis is unavailable.

Ref #173"
```

---

### Task 6: Update AsyncAPI Spec

**Files:**
- Modify: `api-schema/tmi-asyncapi.yml`

- [ ] **Step 1: Update security scheme**

In `api-schema/tmi-asyncapi.yml`, replace the `jwtAuth` security scheme description that references `?token=eyJ0eXAiOiJKV1Q...` with the ticket-based flow:

```yaml
    description: |
      Ticket-based authentication for WebSocket connections.

      ## Authentication Flow
      1. Client obtains a short-lived ticket via GET /ws/ticket?session_id={uuid}
         (authenticated with Bearer token or HttpOnly cookie)
      2. Client connects to WebSocket with ticket as query parameter: ?ticket={nonce}
      3. Server validates the ticket (single-use, 30s TTL, session-scoped)
      4. Server looks up user identity from the ticket and verifies access
      5. WebSocket connection is established if all validations pass

      Tickets are:
      - Single-use (consumed on first validation)
      - Short-lived (30 second TTL)
      - Session-scoped (bound to a specific collaboration session)
      - Cryptographically random (32 bytes, base64url-encoded)
```

- [ ] **Step 2: Validate AsyncAPI spec**

Run: `make validate-asyncapi`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add api-schema/tmi-asyncapi.yml
git commit -m "docs(asyncapi): update WebSocket security scheme to ticket-based auth

Replaces ?token= JWT pattern with ?ticket= nonce pattern.
Documents ticket properties: single-use, 30s TTL, session-scoped.

Ref #173"
```

---

## Chunk 4: wstest Update and Integration

### Task 7: Update wstest CLI Tool

**Files:**
- Modify: `wstest/main.go`

- [ ] **Step 1: Add ticket acquisition function**

Add a new function `getWebSocketTicket` that calls `GET /ws/ticket?session_id={uuid}` with the Bearer token:

```go
func getWebSocketTicket(config Config, tokens *AuthTokens, sessionID string) (string, error) {
	url := fmt.Sprintf("%s/ws/ticket?session_id=%s", config.ServerURL, sessionID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create ticket request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", tokens.AccessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ticket request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ticket request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var ticketResp struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ticketResp); err != nil {
		return "", fmt.Errorf("failed to parse ticket response: %w", err)
	}

	return ticketResp.Ticket, nil
}
```

- [ ] **Step 2: Update connectToWebSocket to use ticket**

Modify `connectToWebSocket` in `wstest/main.go` (line 991). Change the function signature to accept `sessionID string`. Replace the `?token=` URL construction (lines 993-996) with ticket acquisition and `?ticket=` URL:

```go
func connectToWebSocket(ctx context.Context, config Config, tokens *AuthTokens, threatModelID, diagramID, sessionID string) error {
	// Get ticket for WebSocket auth
	ticket, err := getWebSocketTicket(config, tokens, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get WebSocket ticket: %w", err)
	}

	slogging.Get().GetSlogger().Info("Obtained WebSocket ticket", "session_id", sessionID)

	// Build WebSocket URL with ticket (replaces old ?token= pattern)
	wsURL := strings.Replace(config.ServerURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	wsURL = fmt.Sprintf("%s/threat_models/%s/diagrams/%s/ws?session_id=%s&ticket=%s",
		wsURL, threatModelID, diagramID, sessionID, ticket)

	// ... rest of existing connection code (from line 998 onwards) ...
```

- [ ] **Step 3: Update callers of connectToWebSocket**

Update `runHostMode` (line 732) and `runParticipantMode` (line 756) to pass `session.SessionID`:

In `runHostMode` (line 732):
```go
return connectToWebSocket(ctx, config, tokens, threatModel.ID, diagram.ID, session.SessionID)
```

In `runParticipantMode` (line 756):
```go
err = connectToWebSocket(ctx, config, tokens, threatModelID, diagramID, session.SessionID)
```

- [ ] **Step 4: Build wstest**

Run: `make build-wstest`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add wstest/main.go
git commit -m "feat(wstest): update to use ticket-based WebSocket auth

Replaces ?token= JWT with ticket flow: GET /ws/ticket?session_id=
then ?ticket= on WebSocket URL.

Ref #173"
```

---

### Task 8: Integration Testing and Final Verification

**Files:**
- Modify: `api/websocket_test.go` (contains `?token=` usage)
- Any other test files using `?token=` for WebSocket connections

- [ ] **Step 1: Find and update existing WebSocket tests**

Search for `?token=` usage in test files:
```bash
grep -rn 'token=' api/*_test.go wstest/ --include='*.go'
```

Key files to update:
- `api/websocket_test.go`: Tests that construct WebSocket URLs with `?token=` need to:
  1. Create an `InMemoryTicketStore` in the test setup
  2. Set the ticket store on the test server via `server.SetTicketStore(store)`
  3. Issue a ticket via `store.IssueTicket(ctx, userID, provider, sessionID, 30*time.Second)`
  4. Connect with `?ticket=<ticket>&session_id=<sessionID>` instead of `?token=<jwt>`

For each test that establishes a WebSocket connection, the pattern changes from:
```go
// Old:
wsURL := fmt.Sprintf("ws://localhost/ws?token=%s", jwtToken)

// New:
ticket, _ := ticketStore.IssueTicket(ctx, userID, provider, sessionID, 30*time.Second)
wsURL := fmt.Sprintf("ws://localhost/threat_models/%s/diagrams/%s/ws?session_id=%s&ticket=%s",
    threatModelID, diagramID, sessionID, ticket)
```

- [ ] **Step 2: Run unit tests**

Run: `make test-unit`
Expected: PASS (all tests)

- [ ] **Step 3: Run integration tests**

Run: `make test-integration`
Expected: PASS

- [ ] **Step 4: Full build verification**

Run: `make build-server && make lint`
Expected: Both PASS

- [ ] **Step 5: Commit any test updates**

```bash
git add api/*_test.go
git commit -m "test: update WebSocket tests for ticket-based auth

Replaces ?token= JWT pattern with ticket-based auth in all
WebSocket test fixtures.

Ref #173"
```

- [ ] **Step 6: Close the issue**

```bash
gh issue close 173 --repo ericfitz/tmi --comment "Implemented WebSocket ticket authentication (AUTH-VULN-007 remediation). See commits on release/1.3.0 branch."
```
