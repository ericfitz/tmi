package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeSpecOwnership covers the operations annotated by slice 2 (#365) at a
// shape sufficient to exercise reader/writer/owner enforcement against the
// shared TestFixtures threat model.
const fakeSpecOwnership = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/threat_models": {
      "get": {
        "operationId": "listThreatModels",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none"}
      },
      "post": {
        "operationId": "createThreatModel",
        "responses": {"201": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "none"}
      }
    },
    "/threat_models/{threat_model_id}": {
      "get": {
        "operationId": "getThreatModel",
        "parameters": [{"name": "threat_model_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      },
      "put": {
        "operationId": "updateThreatModel",
        "parameters": [{"name": "threat_model_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      },
      "patch": {
        "operationId": "patchThreatModel",
        "parameters": [{"name": "threat_model_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      },
      "delete": {
        "operationId": "deleteThreatModel",
        "parameters": [{"name": "threat_model_id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "owner"}
      }
    },
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}": {
      "get": {
        "operationId": "getDiagram",
        "parameters": [
          {"name": "threat_model_id", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "diagram_id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      },
      "put": {
        "operationId": "updateDiagram",
        "parameters": [
          {"name": "threat_model_id", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "diagram_id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      },
      "delete": {
        "operationId": "deleteDiagram",
        "parameters": [
          {"name": "threat_model_id", "in": "path", "required": true, "schema": {"type": "string"}},
          {"name": "diagram_id", "in": "path", "required": true, "schema": {"type": "string"}}
        ],
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "owner"}
      }
    }
  }
}`

// authzOwnershipUser identifies which TestFixtures user a row exercises.
// "anon" means no userEmail context key is set (simulates unauthenticated).
// "stranger" means an authenticated user not in the threat model's ACL.
type authzOwnershipUser string

const (
	authzUserAnon     authzOwnershipUser = "anon"
	authzUserReader   authzOwnershipUser = "reader"
	authzUserWriter   authzOwnershipUser = "writer"
	authzUserOwner    authzOwnershipUser = "owner"
	authzUserStranger authzOwnershipUser = "stranger"
)

// applyAuthzTestUser sets the JWT-supplied context keys to simulate a logged-in
// user. The shape of the keys mirrors api/middleware.go and JWT middleware.
func applyAuthzTestUser(c *gin.Context, kind authzOwnershipUser) {
	switch kind {
	case authzUserAnon:
		// no keys — middleware sees no userEmail
	case authzUserReader:
		c.Set("userEmail", TestFixtures.ReaderUser)
		c.Set("userID", TestFixtures.ReaderUser)
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
	case authzUserWriter:
		c.Set("userEmail", TestFixtures.WriterUser)
		c.Set("userID", TestFixtures.WriterUser)
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
	case authzUserOwner:
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser)
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
	case authzUserStranger:
		c.Set("userEmail", "stranger@example.com")
		c.Set("userID", "stranger@example.com")
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
	}
}

// newOwnershipAuthzRouter builds a Gin engine with the ownership AuthzTable
// installed and stub handlers for every annotated route. The route handlers
// just return 200 so that any non-200 response we observe is from the
// AuthzMiddleware itself.
func newOwnershipAuthzRouter(t *testing.T, kind authzOwnershipUser) *gin.Engine {
	t.Helper()
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecOwnership))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		applyAuthzTestUser(c, kind)
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))

	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"path": c.Request.URL.Path}) }
	r.GET("/threat_models", ok)
	r.POST("/threat_models", ok)
	r.GET("/threat_models/:threat_model_id", ok)
	r.PUT("/threat_models/:threat_model_id", ok)
	r.PATCH("/threat_models/:threat_model_id", ok)
	r.DELETE("/threat_models/:threat_model_id", ok)
	r.GET("/threat_models/:threat_model_id/diagrams/:diagram_id", ok)
	r.PUT("/threat_models/:threat_model_id/diagrams/:diagram_id", ok)
	r.DELETE("/threat_models/:threat_model_id/diagrams/:diagram_id", ok)
	return r
}

// TestAuthzMiddleware_OwnershipEnforcement_TopLevelEndpoints exhaustively
// exercises the role matrix required by issue #365's acceptance criteria
// "Per-route table tests assert anonymous, reader, writer, owner, and
// security-reviewer outcomes for the top-level endpoints."
//
// The "security_reviewer" column is an authenticated user with no ACL entry
// on the target threat model — the slice-2 ownership rules don't grant any
// implicit role to security reviewers, so it must be denied just like any
// other stranger. Slice 4 (#367-#369) introduces the role-gate enforcement
// for security_reviewer-marked routes.
func TestAuthzMiddleware_OwnershipEnforcement_TopLevelEndpoints(t *testing.T) {
	InitTestFixtures()

	tmID := TestFixtures.ThreatModelID
	diagID := TestFixtures.DiagramID

	cases := []struct {
		name       string
		method     string
		path       string
		user       authzOwnershipUser
		wantStatus int
	}{
		// /threat_models GET (ownership=none) — any auth user passes; anon
		// passes too because slice 1 ownership=none doesn't enforce JWT
		// (JWT middleware does that in production for non-public paths).
		{"list/anon", http.MethodGet, "/threat_models", authzUserAnon, http.StatusOK},
		{"list/reader", http.MethodGet, "/threat_models", authzUserReader, http.StatusOK},

		// /threat_models POST (ownership=none) — same.
		{"create/owner", http.MethodPost, "/threat_models", authzUserOwner, http.StatusOK},

		// /threat_models/{id} GET (ownership=reader)
		{"get/anon", http.MethodGet, "/threat_models/" + tmID, authzUserAnon, http.StatusUnauthorized},
		{"get/reader", http.MethodGet, "/threat_models/" + tmID, authzUserReader, http.StatusOK},
		{"get/writer", http.MethodGet, "/threat_models/" + tmID, authzUserWriter, http.StatusOK},
		{"get/owner", http.MethodGet, "/threat_models/" + tmID, authzUserOwner, http.StatusOK},
		{"get/stranger", http.MethodGet, "/threat_models/" + tmID, authzUserStranger, http.StatusForbidden},

		// /threat_models/{id} PUT (ownership=writer)
		{"put/anon", http.MethodPut, "/threat_models/" + tmID, authzUserAnon, http.StatusUnauthorized},
		{"put/reader", http.MethodPut, "/threat_models/" + tmID, authzUserReader, http.StatusForbidden},
		{"put/writer", http.MethodPut, "/threat_models/" + tmID, authzUserWriter, http.StatusOK},
		{"put/owner", http.MethodPut, "/threat_models/" + tmID, authzUserOwner, http.StatusOK},
		{"put/stranger", http.MethodPut, "/threat_models/" + tmID, authzUserStranger, http.StatusForbidden},

		// /threat_models/{id} PATCH (ownership=writer)
		{"patch/reader", http.MethodPatch, "/threat_models/" + tmID, authzUserReader, http.StatusForbidden},
		{"patch/writer", http.MethodPatch, "/threat_models/" + tmID, authzUserWriter, http.StatusOK},

		// /threat_models/{id} DELETE (ownership=owner)
		{"delete/anon", http.MethodDelete, "/threat_models/" + tmID, authzUserAnon, http.StatusUnauthorized},
		{"delete/reader", http.MethodDelete, "/threat_models/" + tmID, authzUserReader, http.StatusForbidden},
		{"delete/writer", http.MethodDelete, "/threat_models/" + tmID, authzUserWriter, http.StatusForbidden},
		{"delete/owner", http.MethodDelete, "/threat_models/" + tmID, authzUserOwner, http.StatusOK},
		{"delete/stranger", http.MethodDelete, "/threat_models/" + tmID, authzUserStranger, http.StatusForbidden},

		// /threat_models/{id}/diagrams/{did} GET (ownership=reader)
		{"diag-get/reader", http.MethodGet, "/threat_models/" + tmID + "/diagrams/" + diagID, authzUserReader, http.StatusOK},
		{"diag-get/stranger", http.MethodGet, "/threat_models/" + tmID + "/diagrams/" + diagID, authzUserStranger, http.StatusForbidden},

		// /threat_models/{id}/diagrams/{did} PUT (ownership=writer)
		{"diag-put/reader", http.MethodPut, "/threat_models/" + tmID + "/diagrams/" + diagID, authzUserReader, http.StatusForbidden},
		{"diag-put/writer", http.MethodPut, "/threat_models/" + tmID + "/diagrams/" + diagID, authzUserWriter, http.StatusOK},

		// /threat_models/{id}/diagrams/{did} DELETE (ownership=owner)
		{"diag-delete/writer", http.MethodDelete, "/threat_models/" + tmID + "/diagrams/" + diagID, authzUserWriter, http.StatusForbidden},
		{"diag-delete/owner", http.MethodDelete, "/threat_models/" + tmID + "/diagrams/" + diagID, authzUserOwner, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newOwnershipAuthzRouter(t, tc.user)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// TestAuthzMiddleware_OwnershipEnforcement_NotFound asserts that a request
// against a missing threat model produces a 404 (not 403), so unauthenticated
// or unauthorized callers can't infer existence by error code.
func TestAuthzMiddleware_OwnershipEnforcement_NotFound(t *testing.T) {
	InitTestFixtures()

	r := newOwnershipAuthzRouter(t, authzUserOwner)
	req := httptest.NewRequest(http.MethodGet, "/threat_models/00000000-0000-0000-0000-000000000000", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestAuthzMiddleware_OwnershipEnforcement_StoreUnavailable asserts that a
// nil ThreatModelStore produces a 503 with Retry-After, not a panic.
func TestAuthzMiddleware_OwnershipEnforcement_StoreUnavailable(t *testing.T) {
	InitTestFixtures()
	original := ThreatModelStore
	defer func() { ThreatModelStore = original }()
	ThreatModelStore = nil

	r := newOwnershipAuthzRouter(t, authzUserOwner)
	req := httptest.NewRequest(http.MethodGet, "/threat_models/"+TestFixtures.ThreatModelID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503; body=%s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on 503 response")
	}
}

// TestAuthzMiddleware_OwnershipEnforcement_SetsUserRoleContext asserts that
// AuthzMiddleware writes a userRole into context on allow — handlers depend
// on that key for response shaping (e.g. the IsOwner field on threat models).
func TestAuthzMiddleware_OwnershipEnforcement_SetsUserRoleContext(t *testing.T) {
	InitTestFixtures()

	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecOwnership))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		applyAuthzTestUser(c, authzUserWriter)
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))

	var (
		gotRole    Role
		gotCovered bool
	)
	r.PUT("/threat_models/:threat_model_id", func(c *gin.Context) {
		if v, ok := c.Get("userRole"); ok {
			gotRole, _ = v.(Role)
		}
		if v, ok := c.Get("authzCovered"); ok {
			gotCovered, _ = v.(bool)
		}
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPut, "/threat_models/"+TestFixtures.ThreatModelID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if gotRole != RoleWriter {
		t.Errorf("userRole: got %q, want %q", gotRole, RoleWriter)
	}
	if !gotCovered {
		t.Error("authzCovered: got false, want true")
	}
}
