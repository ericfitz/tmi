package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeSystemAuditRepo struct {
	rows []models.SystemAuditEntry
	err  error
}

func (f *fakeSystemAuditRepo) Create(ctx context.Context, e models.SystemAuditEntry) error {
	if f.err != nil {
		return f.err
	}
	f.rows = append(f.rows, e)
	return nil
}

func (f *fakeSystemAuditRepo) ListByActor(ctx context.Context, actor string, from, to time.Time) ([]models.SystemAuditEntry, error) {
	return f.rows, nil
}

type fakeSystemSettingReader struct{ value string }

func (r *fakeSystemSettingReader) Read(c *gin.Context, key string) string { return r.value }

func newAuditTestRouter(t *testing.T, repo SystemAuditRepository, reader SystemSettingReader, handlerStatus int) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Mimic actor context shape (matches what JWT middleware sets in production).
	r.Use(func(c *gin.Context) {
		c.Set("userEmail", "alice@example.com")
		c.Set("userIdP", "google") // GetUserAuthFieldsForAccessCheck reads userIdP
		c.Set("userInternalUUID", "uuid-1")
		c.Set("userID", "google-sub-1") // GetUserAuthFieldsForAccessCheck reads userID for providerUserID
		c.Set("userDisplayName", "Alice")
		c.Next()
	})
	r.Use(AdminAuditMiddleware(repo, NewRedactor(), adminAuditDescriptors(reader)))
	r.PUT("/admin/settings/:key", func(c *gin.Context) { c.Status(handlerStatus) })
	return r
}

func TestAdminAuditMiddleware_Writes2xx(t *testing.T) {
	repo := &fakeSystemAuditRepo{}
	reader := &fakeSystemSettingReader{value: "old-value"}
	r := newAuditTestRouter(t, repo, reader, http.StatusOK)

	body, _ := json.Marshal(map[string]string{"value": "new-value"})
	req := httptest.NewRequest("PUT", "/admin/settings/foo", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, repo.rows, 1)
	row := repo.rows[0]
	assert.Equal(t, "system_settings.foo", row.FieldPath)
	assert.Equal(t, "alice@example.com", row.ActorEmail)
	assert.Equal(t, "google", row.ActorProvider)
	assert.Equal(t, "google-sub-1", row.ActorProviderID)
	assert.Equal(t, "Alice", row.ActorDisplayName)
	assert.Equal(t, "PUT", row.HTTPMethod)
	assert.Equal(t, "/admin/settings/:key", row.HTTPPath)
	// "foo" is not in the deny-list, so old/new values are verbatim.
	assert.Equal(t, "old-value", row.OldValueRedacted.String)
	assert.Equal(t, "new-value", row.NewValueRedacted.String)
}

func TestAdminAuditMiddleware_DoesNotWriteOnNon2xx(t *testing.T) {
	repo := &fakeSystemAuditRepo{}
	r := newAuditTestRouter(t, repo, &fakeSystemSettingReader{}, http.StatusBadRequest)

	body, _ := json.Marshal(map[string]string{"value": "x"})
	req := httptest.NewRequest("PUT", "/admin/settings/foo", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Len(t, repo.rows, 0, "no audit row should be written for non-2xx")
}

func TestAdminAuditMiddleware_RedactsSensitiveFields(t *testing.T) {
	repo := &fakeSystemAuditRepo{}
	reader := &fakeSystemSettingReader{value: "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAA1234abcd56"}
	r := newAuditTestRouter(t, repo, reader, http.StatusOK)

	body, _ := json.Marshal(map[string]string{"value": "ghp_BBBBBBBBBBBBBBBBBBBBBBBBBBBBwxyz9876"})
	req := httptest.NewRequest("PUT", "/admin/settings/oauth.providers.github.client_secret", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, repo.rows, 1)
	oldVal := repo.rows[0].OldValueRedacted.String
	newVal := repo.rows[0].NewValueRedacted.String
	assert.Contains(t, oldVal, `"redacted":true`)
	assert.Contains(t, newVal, `"redacted":true`)
	assert.NotContains(t, oldVal, "ghp_AAAA", "redacted value must not leak plaintext prefix")
}

func TestAdminAuditMiddleware_RepoFailureDoesNotFailRequest(t *testing.T) {
	repo := &fakeSystemAuditRepo{err: context.DeadlineExceeded}
	r := newAuditTestRouter(t, repo, &fakeSystemSettingReader{}, http.StatusOK)

	body, _ := json.Marshal(map[string]string{"value": "x"})
	req := httptest.NewRequest("PUT", "/admin/settings/foo", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "audit-write failure must not fail admin write")
}

func TestAdminAuditMiddleware_NoDescriptorPassesThrough(t *testing.T) {
	repo := &fakeSystemAuditRepo{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AdminAuditMiddleware(repo, NewRedactor(), adminAuditDescriptors(nil)))
	r.GET("/some/random/path", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest("GET", "/some/random/path", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, repo.rows, 0, "no descriptor for this route → no audit row")
}

func TestAdminAuditDescriptors_AllExpectedRoutesCovered(t *testing.T) {
	descs := adminAuditDescriptors(nil)
	covered := map[string]bool{}
	for _, d := range descs {
		covered[d.Method+" "+d.PathTpl] = true
	}
	required := []string{
		"PUT /admin/settings/{key}",
		"DELETE /admin/settings/{key}",
		"POST /admin/settings/reencrypt",
		"PATCH /admin/users/{internal_uuid}",
		"DELETE /admin/users/{internal_uuid}",
		"POST /admin/users/{internal_uuid}/transfer",
		"POST /admin/users/automation",
		"POST /admin/users/{internal_uuid}/client_credentials",
		"DELETE /admin/users/{internal_uuid}/client_credentials/{credential_id}",
		"POST /admin/groups",
		"PATCH /admin/groups/{internal_uuid}",
		"DELETE /admin/groups/{internal_uuid}",
		"POST /admin/groups/{internal_uuid}/members",
		"DELETE /admin/groups/{internal_uuid}/members/{member_uuid}",
		"PUT /admin/quotas/users/{user_id}",
		"DELETE /admin/quotas/users/{user_id}",
		"PUT /admin/quotas/webhooks/{user_id}",
		"DELETE /admin/quotas/webhooks/{user_id}",
		"PUT /admin/quotas/addons/{user_id}",
		"DELETE /admin/quotas/addons/{user_id}",
	}
	for _, r := range required {
		if !covered[r] {
			t.Errorf("descriptor missing for required gated route: %s", r)
		}
	}
}
