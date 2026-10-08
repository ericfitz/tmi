package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/errcode"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func errorBody(t *testing.T, w *httptest.ResponseRecorder) Error {
	t.Helper()
	var body Error
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestErrorVocabularyPatchFailureIsInvalidPatch(t *testing.T) {
	_, err := ApplyPatchOperations(PatchTestEntity{
		Name:  "n",
		Owner: User{PrincipalType: UserPrincipalTypeUser, Provider: "test", ProviderId: "o", DisplayName: "O", Email: "o@example.com"},
	}, []PatchOperation{
		{Op: "invalid_op", Path: "/name", Value: "value"},
	})
	require.Error(t, err)
	var reqErr *RequestError
	require.True(t, errors.As(err, &reqErr))
	assert.Equal(t, http.StatusBadRequest, reqErr.Status)
	assert.Equal(t, errcode.InvalidPatch, reqErr.Code)
}

func TestErrorVocabularyHandleRequestErrorBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name   string
		err    *RequestError
		status int
		code   string
	}{
		{"invalid id", InvalidIDError("bad id"), http.StatusBadRequest, "invalid_id"},
		{"invalid patch", InvalidPatchError("bad patch"), http.StatusBadRequest, "invalid_patch"},
		{"pagination", ValidatePaginationParams(new(-1), nil), http.StatusBadRequest, "invalid_input"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
			HandleRequestError(c, tc.err)
			assert.Equal(t, tc.status, w.Code)
			assert.Equal(t, tc.code, string(errorBody(t, w).Error))
		})
	}
}
