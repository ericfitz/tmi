package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func classifiedPg(code string) error {
	return fmt.Errorf("wrapped: %w", dberrors.Classify(&pgconn.PgError{Code: code, Message: "driver text", ColumnName: "secret_col"}))
}

// Every classified client-data class maps to the documented 400 in both
// shared mappers; append-only violations (a code bug) and unclassified errors
// stay 500; transient stays 503.
func TestStoreMappers_ClassTable(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		write      int
		storeWrite int
	}{
		{"value too long", classifiedPg("22001"), 400, 400},
		{"bad encoding", classifiedPg("22021"), 400, 400},
		{"numeric overflow", classifiedPg("22003"), 400, 400},
		{"not null", classifiedPg("23502"), 400, 400},
		{"foreign key", classifiedPg("23503"), 400, 400},
		{"check", classifiedPg("23514"), 400, 400},
		{"duplicate", classifiedPg("23505"), 400, 409},
		{"append only", dberrors.Wrap(errors.New("append-only"), dberrors.ErrAppendOnlyViolation), 500, 500},
		{"transient", dberrors.Wrap(errors.New("x"), dberrors.ErrTransient), 503, 503},
		{"unclassified", errors.New("boom"), 500, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := WriteErrorToRequestError(tc.err, "Failed")
			assert.Equal(t, tc.write, w.Status)
			assert.NotContains(t, w.Message, "secret_col")
			s := StoreErrorToRequestError(tc.err, "nf", "Failed")
			assert.Equal(t, tc.storeWrite, s.Status)
			assert.NotContains(t, s.Message, "secret_col")
		})
	}
}

func TestCreateWebhookSubscription_StoreValueTooLong_Returns400(t *testing.T) {
	origSub, origQuota, origAdmin, origDeny := GlobalWebhookSubscriptionStore, GlobalWebhookQuotaStore, GlobalGroupMemberRepository, GlobalWebhookUrlDenyListStore
	defer func() {
		GlobalWebhookSubscriptionStore, GlobalWebhookQuotaStore, GlobalGroupMemberRepository, GlobalWebhookUrlDenyListStore = origSub, origQuota, origAdmin, origDeny
	}()
	GlobalWebhookUrlDenyListStore = &mockDenyListStore{entries: []WebhookUrlDenyListEntry{}}
	subStore := newMockWebhookSubscriptionStore()
	subStore.err = classifiedPg("22001")
	GlobalWebhookSubscriptionStore = subStore
	GlobalWebhookQuotaStore = newMockWebhookQuotaStore()

	r, _ := setupWebhookRouter("admin@example.com", uuid.New().String(), true)
	body, _ := json.Marshal(map[string]any{"name": "W", "url": "https://example.com/webhook", "events": []string{"threat.created"}})
	req, _ := http.NewRequest("POST", "/admin/webhooks/subscriptions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "secret_col")
}

func TestCreateAddon_StoreValueTooLong_Returns400(t *testing.T) {
	origAddon, origAdmin := GlobalAddonStore, GlobalGroupMemberRepository
	defer restoreAddonStores(origAddon, origAdmin)
	mockStore := &MockAddonStore{}
	r := setupAddonHandlerTest(mockStore, true)
	mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Addon")).Return(classifiedPg("22001"))

	body, _ := json.Marshal(map[string]any{"name": "Scanner", "webhook_id": uuid.New().String()})
	req := httptest.NewRequest("POST", "/addons", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "secret_col")
}
