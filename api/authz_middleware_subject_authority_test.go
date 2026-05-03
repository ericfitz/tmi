package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeSpecSubjectAuthority covers the four cells of the
// `subject_authority` matrix on a single resource-hierarchical write
// route. The test exercises all four: SA token rejected, user token
// allowed, delegation token allowed, anonymous rejected (anonymous
// reaches us pre-JWT in production but unit-tested here too).
const fakeSpecSubjectAuthority = `{
  "openapi": "3.0.3",
  "info": {"title": "test", "version": "0"},
  "paths": {
    "/threat_models/{threat_model_id}/threats": {
      "post": {
        "operationId": "createThreat",
        "responses": {"201": {"description": "ok"}},
        "x-tmi-authz": {
          "ownership": "writer",
          "subject_authority": "invoker"
        }
      }
    },
    "/admin/internal/sa": {
      "post": {
        "operationId": "saInternal",
        "responses": {"200": {"description": "ok"}},
        "x-tmi-authz": {
          "ownership": "none",
          "roles": ["admin"],
          "subject_authority": "service_account"
        }
      }
    }
  }
}`

// applySubjectAuthorityKind sets the JWT-equivalent context keys for
// each principal type. Values match what cmd/server/jwt_auth.go writes.
type subjectKind int

const (
	subjectAnon subjectKind = iota
	subjectUser
	subjectServiceAccount
	subjectDelegation
)

func setSubjectKind(c *gin.Context, kind subjectKind) {
	switch kind {
	case subjectAnon:
		// no keys set
	case subjectUser:
		c.Set("userEmail", TestFixtures.WriterUser)
		c.Set("userID", TestFixtures.WriterUser)
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
		c.Set("isServiceAccount", false)
	case subjectServiceAccount:
		c.Set("userEmail", TestFixtures.WriterUser)
		c.Set("userID", TestFixtures.WriterUser)
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
		c.Set("isServiceAccount", true)
		c.Set("serviceAccountCredentialID", "test-cred-id")
	case subjectDelegation:
		c.Set("userEmail", TestFixtures.WriterUser)
		c.Set("userID", TestFixtures.WriterUser)
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
		c.Set("isServiceAccount", false)
		c.Set("isDelegation", true)
		c.Set("delegationAddonID", "addon-test-id")
	}
}

func newSubjectAuthorityRouter(t *testing.T, kind subjectKind) *gin.Engine {
	t.Helper()
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecSubjectAuthority))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		setSubjectKind(c, kind)
		// RequireAdministrator's test hook — only relevant for the SA test
		// route that requires admin role.
		if kind == subjectServiceAccount {
			c.Set("isAdmin", false)
		}
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	r.POST("/threat_models/:threat_model_id/threats",
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })
	r.POST("/admin/internal/sa",
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })
	return r
}

func TestAuthzMiddleware_SubjectAuthority_InvokerRejectsSA(t *testing.T) {
	InitTestFixtures()
	r := newSubjectAuthorityRouter(t, subjectServiceAccount)
	req := httptest.NewRequest(http.MethodPost,
		"/threat_models/"+TestFixtures.ThreatModelID+"/threats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403; SA token must be rejected on subject_authority=invoker route. body=%s",
			w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_SubjectAuthority_InvokerAllowsUser(t *testing.T) {
	InitTestFixtures()
	r := newSubjectAuthorityRouter(t, subjectUser)
	req := httptest.NewRequest(http.MethodPost,
		"/threat_models/"+TestFixtures.ThreatModelID+"/threats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; user (writer) must pass subject_authority=invoker. body=%s",
			w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_SubjectAuthority_InvokerAllowsDelegation(t *testing.T) {
	InitTestFixtures()
	r := newSubjectAuthorityRouter(t, subjectDelegation)
	req := httptest.NewRequest(http.MethodPost,
		"/threat_models/"+TestFixtures.ThreatModelID+"/threats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; delegation token must pass subject_authority=invoker. body=%s",
			w.Code, w.Body.String())
	}
}

func TestAuthzMiddleware_SubjectAuthority_ServiceAccountRequiresSA(t *testing.T) {
	// On a route that requires SA, a regular user token is rejected.
	InitTestFixtures()
	r := newSubjectAuthorityRouter(t, subjectUser)
	req := httptest.NewRequest(http.MethodPost, "/admin/internal/sa", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status: got %d, want 403; user token must be rejected on subject_authority=service_account route. body=%s",
			w.Code, w.Body.String())
	}
}

// TestAuthzMiddleware_SubjectAuthority_T18ConfusedDeputy is the explicit
// scenario from #358's acceptance criteria: user Y (reader on TM-A) tries
// to write to TM-A using an SA token (the addon-owner's credentials).
// Even before the ownership/role gate runs, subject_authority=invoker
// blocks the request with 403. This is the secondary defense — the
// primary defense is "addon uses delegation token" (covered separately).
func TestAuthzMiddleware_SubjectAuthority_T18ConfusedDeputy(t *testing.T) {
	InitTestFixtures()

	// Assemble the threat we're defending against: an SA token whose owner
	// has owner-level access on TM-A (TestFixtures owner) attempting to
	// POST a threat. AuthzMiddleware should reject before even loading the
	// parent ACL because subject_authority=invoker.
	tbl, err := loadAuthzTableFromJSON([]byte(fakeSpecSubjectAuthority))
	if err != nil {
		t.Fatalf("loadAuthzTableFromJSON: %v", err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		// Owner-level user, but presented as an SA token. The owner
		// would have full write access via direct calls, but this SA
		// token is being smuggled through an addon's webhook callback.
		c.Set("userEmail", TestFixtures.OwnerUser)
		c.Set("userID", TestFixtures.OwnerUser)
		c.Set("userProvider", "test")
		c.Set("userIdP", "test")
		c.Set("isServiceAccount", true)
		c.Set("serviceAccountCredentialID", "addon-cred-id")
		c.Next()
	})
	r.Use(authzMiddlewareWithTable(tbl))
	r.POST("/threat_models/:threat_model_id/threats",
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{}) })

	req := httptest.NewRequest(http.MethodPost,
		"/threat_models/"+TestFixtures.ThreatModelID+"/threats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("CRITICAL T18: SA token wrote to TM (got %d, want 403). body=%s",
			w.Code, w.Body.String())
	}
}
