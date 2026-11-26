package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetApiInfo_ResponseConformsToSchema covers #1232: the root endpoint body,
// for every build-string form and with a nanosecond-precision clock, must
// validate against the ApiInfo schema in the embedded spec.
func TestGetApiInfo_ResponseConformsToSchema(t *testing.T) {
	origPre, origCommit := VersionPreRelease, GitCommit
	origNow := apiInfoNow
	defer func() {
		VersionPreRelease, GitCommit = origPre, origCommit
		apiInfoNow = origNow
	}()
	apiInfoNow = func() time.Time {
		return time.Date(2026, 11, 25, 12, 34, 56, 123456789, time.UTC)
	}

	swagger, err := GetSwagger()
	require.NoError(t, err)
	ref := swagger.Components.Schemas["ApiInfo"]
	require.NotNil(t, ref)
	require.NotNil(t, ref.Value)

	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		preRelease string
		commit     string
		isDev      bool
	}{
		{"prerelease_with_commit", "rc.1", "abc1234d", false},
		{"prerelease_no_commit", "rc.1", "development", false},
		{"commit_dev", "", "abc1234d", true},
		{"commit_production", "", "abc1234d", false},
		{"development", "", "development", true},
		{"plain", "", "development", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			VersionPreRelease, GitCommit = tt.preRelease, tt.commit
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("isDev", tt.isDev)
				c.Next()
			})
			router.GET("/", NewApiInfoHandler(NewServerForTests()).GetApiInfo)
			req, err := http.NewRequest("GET", "/", nil)
			require.NoError(t, err)
			req.Header.Set("Accept", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code)

			var body any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.NoError(t, ref.Value.VisitJSON(body), "body: %s", w.Body.String())
		})
	}
}

// TestWebhookTimestampsTruncatedToMicroseconds covers the sibling instances of
// #1232: delivery and subscription timestamps carry at most six fractional digits.
func TestWebhookTimestampsTruncatedToMicroseconds(t *testing.T) {
	ns := time.Date(2026, 11, 25, 12, 34, 56, 123456789, time.UTC)
	want := time.Date(2026, 11, 25, 12, 34, 56, 123456000, time.UTC)
	delivered, retry := ns, ns

	record := &WebhookDeliveryRecord{
		CreatedAt:      ns,
		DeliveredAt:    &delivered,
		LastActivityAt: ns,
		NextRetryAt:    &retry,
	}
	d := deliveryRecordToWebhookDelivery(record, nil)
	assert.True(t, want.Equal(d.CreatedAt))
	require.NotNil(t, d.DeliveredAt)
	assert.True(t, want.Equal(*d.DeliveredAt))
	require.NotNil(t, d.LastActivityAt)
	assert.True(t, want.Equal(*d.LastActivityAt))
	require.NotNil(t, d.NextRetryAt)
	assert.True(t, want.Equal(*d.NextRetryAt))
	// Input record is not mutated.
	assert.True(t, ns.Equal(*record.DeliveredAt))
	assert.True(t, ns.Equal(record.LastActivityAt))

	use := ns
	sub := dbWebhookSubscriptionToAPI(DBWebhookSubscription{CreatedAt: ns, ModifiedAt: ns, LastSuccessfulUse: &use}, false)
	assert.True(t, want.Equal(sub.CreatedAt))
	assert.True(t, want.Equal(sub.ModifiedAt))
	require.NotNil(t, sub.LastSuccessfulUse)
	assert.True(t, want.Equal(*sub.LastSuccessfulUse))

	// JSON form has <= 6 fractional digits.
	b, err := json.Marshal(d)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "123456789")
}
