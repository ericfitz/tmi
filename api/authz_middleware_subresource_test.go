package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeSpecSubResources covers a representative slice of the operations
// annotated by slice 3 (#366) — one example per sub-resource family plus
// the two special cases (POST /restore = owner, POST /rollback = owner).
//
// The middleware extracts parts[1] as the parent threat-model ID for any
// path under /threat_models/{tm_id}/..., so testing one path per family
// proves the ID extractor and the role mapping work for the whole family.
const fakeSpecSubResources = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/threat_models/{threat_model_id}/threats": {
      "get": {
        "operationId": "listThreats",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      },
      "post": {
        "operationId": "createThreat",
        "responses": {"201": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    },
    "/threat_models/{threat_model_id}/threats/{threat_id}": {
      "get": {
        "operationId": "getThreat",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      },
      "patch": {
        "operationId": "patchThreat",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      },
      "delete": {
        "operationId": "deleteThreat",
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    },
    "/threat_models/{threat_model_id}/threats/{threat_id}/restore": {
      "post": {
        "operationId": "restoreThreat",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "owner"}
      }
    },
    "/threat_models/{threat_model_id}/documents/{document_id}": {
      "get": {
        "operationId": "getDocument",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      },
      "put": {
        "operationId": "putDocument",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    },
    "/threat_models/{threat_model_id}/notes/{note_id}": {
      "get": {
        "operationId": "getNote",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      }
    },
    "/threat_models/{threat_model_id}/assets/{asset_id}": {
      "patch": {
        "operationId": "patchAsset",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    },
    "/threat_models/{threat_model_id}/repositories/{repository_id}": {
      "delete": {
        "operationId": "deleteRepository",
        "responses": {"204": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    },
    "/threat_models/{threat_model_id}/audit_trail": {
      "get": {
        "operationId": "listAuditTrail",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      }
    },
    "/threat_models/{threat_model_id}/audit_trail/{entry_id}/rollback": {
      "post": {
        "operationId": "rollbackAuditEntry",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "owner"}
      }
    },
    "/threat_models/{threat_model_id}/metadata/{key}": {
      "get": {
        "operationId": "getMetadataKey",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      },
      "put": {
        "operationId": "putMetadataKey",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    },
    "/threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata": {
      "post": {
        "operationId": "createDiagramMetadata",
        "responses": {"201": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    },
    "/threat_models/{threat_model_id}/chat/sessions": {
      "get": {
        "operationId": "listChatSessions",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "reader"}
      },
      "post": {
        "operationId": "createChatSession",
        "responses": {"201": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    },
    "/threat_models/{threat_model_id}/chat/sessions/{session_id}/messages": {
      "post": {
        "operationId": "createChatMessage",
        "responses": {"201": {"description": "ok"}},
        "x-tmi-authz": {"ownership": "writer"}
      }
    }
  }
}`

// newSubResourceAuthzRouter builds a Gin engine wiring AuthzMiddleware against
// the sub-resource fake spec. Stub handlers return 200 so any non-OK response
// observable in tests is from the middleware.
func newSubResourceAuthzRouter(t *testing.T, kind authzOwnershipUser) *gin.Engine {
	t.Helper()
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecSubResources))
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
	r.GET("/threat_models/:threat_model_id/threats", ok)
	r.POST("/threat_models/:threat_model_id/threats", ok)
	r.GET("/threat_models/:threat_model_id/threats/:threat_id", ok)
	r.PATCH("/threat_models/:threat_model_id/threats/:threat_id", ok)
	r.DELETE("/threat_models/:threat_model_id/threats/:threat_id", ok)
	r.POST("/threat_models/:threat_model_id/threats/:threat_id/restore", ok)
	r.GET("/threat_models/:threat_model_id/documents/:document_id", ok)
	r.PUT("/threat_models/:threat_model_id/documents/:document_id", ok)
	r.GET("/threat_models/:threat_model_id/notes/:note_id", ok)
	r.PATCH("/threat_models/:threat_model_id/assets/:asset_id", ok)
	r.DELETE("/threat_models/:threat_model_id/repositories/:repository_id", ok)
	r.GET("/threat_models/:threat_model_id/audit_trail", ok)
	r.POST("/threat_models/:threat_model_id/audit_trail/:entry_id/rollback", ok)
	r.GET("/threat_models/:threat_model_id/metadata/:key", ok)
	r.PUT("/threat_models/:threat_model_id/metadata/:key", ok)
	r.POST("/threat_models/:threat_model_id/diagrams/:diagram_id/metadata", ok)
	r.GET("/threat_models/:threat_model_id/chat/sessions", ok)
	r.POST("/threat_models/:threat_model_id/chat/sessions", ok)
	r.POST("/threat_models/:threat_model_id/chat/sessions/:session_id/messages", ok)
	return r
}

// TestAuthzMiddleware_SubResources_RoleMatrix exercises the issue #366
// acceptance criterion "Per-route tests cover at least one example per
// sub-resource type for reader/writer/owner outcomes" against the shared
// TestFixtures threat model. Sub-resources inherit the parent threat
// model's ACL, so the same reader/writer/owner users from #365 apply.
func TestAuthzMiddleware_SubResources_RoleMatrix(t *testing.T) {
	InitTestFixtures()

	tmID := TestFixtures.ThreatModelID
	diagID := TestFixtures.DiagramID
	const subID = "11111111-1111-1111-1111-111111111111" // child IDs aren't checked by middleware

	cases := []struct {
		name       string
		method     string
		path       string
		user       authzOwnershipUser
		wantStatus int
	}{
		// /threat_models/{id}/threats — reader/writer
		{"threats-list/reader", http.MethodGet, "/threat_models/" + tmID + "/threats", authzUserReader, http.StatusOK},
		{"threats-list/stranger", http.MethodGet, "/threat_models/" + tmID + "/threats", authzUserStranger, http.StatusForbidden},
		{"threats-create/reader", http.MethodPost, "/threat_models/" + tmID + "/threats", authzUserReader, http.StatusForbidden},
		{"threats-create/writer", http.MethodPost, "/threat_models/" + tmID + "/threats", authzUserWriter, http.StatusOK},

		// /threat_models/{id}/threats/{tid} — reader/writer/writer (DELETE
		// relaxed to writer per issue body's "mechanical writer for write")
		{"threat-get/reader", http.MethodGet, "/threat_models/" + tmID + "/threats/" + subID, authzUserReader, http.StatusOK},
		{"threat-patch/reader", http.MethodPatch, "/threat_models/" + tmID + "/threats/" + subID, authzUserReader, http.StatusForbidden},
		{"threat-patch/writer", http.MethodPatch, "/threat_models/" + tmID + "/threats/" + subID, authzUserWriter, http.StatusOK},
		{"threat-delete/reader", http.MethodDelete, "/threat_models/" + tmID + "/threats/" + subID, authzUserReader, http.StatusForbidden},
		{"threat-delete/writer", http.MethodDelete, "/threat_models/" + tmID + "/threats/" + subID, authzUserWriter, http.StatusOK},
		{"threat-delete/owner", http.MethodDelete, "/threat_models/" + tmID + "/threats/" + subID, authzUserOwner, http.StatusOK},

		// POST /threat_models/{id}/threats/{tid}/restore — owner only
		{"threat-restore/writer", http.MethodPost, "/threat_models/" + tmID + "/threats/" + subID + "/restore", authzUserWriter, http.StatusForbidden},
		{"threat-restore/owner", http.MethodPost, "/threat_models/" + tmID + "/threats/" + subID + "/restore", authzUserOwner, http.StatusOK},

		// /threat_models/{id}/documents/{did} — reader/writer
		{"document-get/reader", http.MethodGet, "/threat_models/" + tmID + "/documents/" + subID, authzUserReader, http.StatusOK},
		{"document-put/writer", http.MethodPut, "/threat_models/" + tmID + "/documents/" + subID, authzUserWriter, http.StatusOK},
		{"document-put/reader", http.MethodPut, "/threat_models/" + tmID + "/documents/" + subID, authzUserReader, http.StatusForbidden},

		// /threat_models/{id}/notes/{nid} — reader
		{"note-get/reader", http.MethodGet, "/threat_models/" + tmID + "/notes/" + subID, authzUserReader, http.StatusOK},
		{"note-get/anon", http.MethodGet, "/threat_models/" + tmID + "/notes/" + subID, authzUserAnon, http.StatusUnauthorized},

		// /threat_models/{id}/assets/{aid} — writer (PATCH)
		{"asset-patch/reader", http.MethodPatch, "/threat_models/" + tmID + "/assets/" + subID, authzUserReader, http.StatusForbidden},
		{"asset-patch/writer", http.MethodPatch, "/threat_models/" + tmID + "/assets/" + subID, authzUserWriter, http.StatusOK},

		// /threat_models/{id}/repositories/{rid} — writer (DELETE)
		{"repo-delete/reader", http.MethodDelete, "/threat_models/" + tmID + "/repositories/" + subID, authzUserReader, http.StatusForbidden},
		{"repo-delete/writer", http.MethodDelete, "/threat_models/" + tmID + "/repositories/" + subID, authzUserWriter, http.StatusOK},

		// /threat_models/{id}/audit_trail (GET) — reader
		{"audit-list/reader", http.MethodGet, "/threat_models/" + tmID + "/audit_trail", authzUserReader, http.StatusOK},
		{"audit-list/stranger", http.MethodGet, "/threat_models/" + tmID + "/audit_trail", authzUserStranger, http.StatusForbidden},

		// POST /audit_trail/{eid}/rollback — owner only
		{"audit-rollback/writer", http.MethodPost, "/threat_models/" + tmID + "/audit_trail/" + subID + "/rollback", authzUserWriter, http.StatusForbidden},
		{"audit-rollback/owner", http.MethodPost, "/threat_models/" + tmID + "/audit_trail/" + subID + "/rollback", authzUserOwner, http.StatusOK},

		// /threat_models/{id}/metadata/{key} — reader/writer
		{"meta-get/reader", http.MethodGet, "/threat_models/" + tmID + "/metadata/priority", authzUserReader, http.StatusOK},
		{"meta-put/writer", http.MethodPut, "/threat_models/" + tmID + "/metadata/priority", authzUserWriter, http.StatusOK},
		{"meta-put/reader", http.MethodPut, "/threat_models/" + tmID + "/metadata/priority", authzUserReader, http.StatusForbidden},

		// /threat_models/{id}/diagrams/{did}/metadata POST — writer (slice-3
		// covers diagram-nested sub-resources alongside the TM-nested ones)
		{"diag-meta-post/reader", http.MethodPost, "/threat_models/" + tmID + "/diagrams/" + diagID + "/metadata", authzUserReader, http.StatusForbidden},
		{"diag-meta-post/writer", http.MethodPost, "/threat_models/" + tmID + "/diagrams/" + diagID + "/metadata", authzUserWriter, http.StatusOK},

		// /threat_models/{id}/chat/sessions* — slice 6 (#369). Reader can
		// list and view; only writer can start a session or send messages.
		{"chat-list/reader", http.MethodGet, "/threat_models/" + tmID + "/chat/sessions", authzUserReader, http.StatusOK},
		{"chat-list/stranger", http.MethodGet, "/threat_models/" + tmID + "/chat/sessions", authzUserStranger, http.StatusForbidden},
		{"chat-create/reader", http.MethodPost, "/threat_models/" + tmID + "/chat/sessions", authzUserReader, http.StatusForbidden},
		{"chat-create/writer", http.MethodPost, "/threat_models/" + tmID + "/chat/sessions", authzUserWriter, http.StatusOK},
		{"chat-message/reader", http.MethodPost, "/threat_models/" + tmID + "/chat/sessions/" + subID + "/messages", authzUserReader, http.StatusForbidden},
		{"chat-message/writer", http.MethodPost, "/threat_models/" + tmID + "/chat/sessions/" + subID + "/messages", authzUserWriter, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newSubResourceAuthzRouter(t, tc.user)
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Errorf("status: got %d, want %d; body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

// TestAuthzMiddleware_SubResources_RestoreUsesIncludingDeleted asserts that
// POST .../restore loads the parent-threat-model auth via the
// "including-deleted" path, so a soft-deleted parent is still reachable for
// owner-driven recovery. This mirrors the legacy ThreatModelMiddleware
// branch (lines 345-359 pre-#365) which is now expressed through the
// `isRestoreRoute` flag inside enforceOwnership.
func TestAuthzMiddleware_SubResources_RestoreDetectsTrailingSegment(t *testing.T) {
	InitTestFixtures()

	// We can't easily exercise the soft-deleted path inside a unit test
	// without rewiring the mock store, but we can at least verify that the
	// restore detection (parts[len-1] == "restore") fires for nested paths
	// — the owner gate must apply, not writer.
	r := newSubResourceAuthzRouter(t, authzUserWriter)
	req := httptest.NewRequest(http.MethodPost,
		"/threat_models/"+TestFixtures.ThreatModelID+"/threats/11111111-1111-1111-1111-111111111111/restore",
		nil,
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("writer attempting nested restore: got %d, want 403; body=%s", w.Code, w.Body.String())
	}
}
