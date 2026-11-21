package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// A store that reports a classified 22001 (value too long) must surface as a
// 400, not a 500, through the shared WriteErrorToRequestError fallback.
func TestCreateDocument_StoreValueTooLong_Returns400(t *testing.T) {
	r, mockStore := setupDocumentSubResourceHandler()
	threatModelID := testUUID1

	raw := &pgconn.PgError{Code: "22001", Message: "value too long for type character varying(256)", ColumnName: "name"}
	mockStore.On("Create", mock.Anything, mock.AnythingOfType("*api.Document"), threatModelID).
		Return(fmt.Errorf("transaction failed: %w", dberrors.Classify(raw)))

	body, _ := json.Marshal(map[string]any{"name": "Doc", "uri": "https://example.com/doc.pdf"})
	req := httptest.NewRequest("POST", "/threat_models/"+threatModelID+"/documents", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "varying")
	mockStore.AssertExpectations(t)
}
