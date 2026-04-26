package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// MicrosoftPickerGrantHandler handles POST /me/microsoft/picker_grants.
//
// After a user picks a file via the Microsoft File Picker, tmi-ux POSTs the
// (drive_id, item_id) here. The server then uses the user's stored
// delegated token (with Files.ReadWrite scope) to call Graph's
//
//	POST /drives/{driveId}/items/{itemId}/permissions
//
// granting the configured TMI Entra app's identity the `read` role on this
// specific file. Subsequent fetches via DelegatedMicrosoftSource read the
// file under Files.SelectedOperations.Selected (the user's read scope).
//
// Required wiring (see cmd/server/main.go):
//   - tokens: ContentTokenRepository for the user's Microsoft delegated token
//   - registry: ContentOAuthProviderRegistry containing the "microsoft" provider
//   - applicationObjectID: the TMI Entra app's object id (used as Graph
//     permission grantee)
//   - graphBaseURL: defaults to graphV1Base when ""
//   - userLookup: extracts the authenticated user id from the Gin context
type MicrosoftPickerGrantHandler struct {
	tokens              ContentTokenRepository
	registry            *ContentOAuthProviderRegistry
	applicationObjectID string
	graphBaseURL        string
	userLookup          func(c *gin.Context) (string, bool)
	httpClient          *http.Client
}

// NewMicrosoftPickerGrantHandler creates the handler with a 30-second HTTP
// timeout. graphBaseURL defaults to https://graph.microsoft.com/v1.0 when "".
func NewMicrosoftPickerGrantHandler(
	tokens ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
	applicationObjectID string,
	graphBaseURL string,
	userLookup func(c *gin.Context) (string, bool),
) *MicrosoftPickerGrantHandler {
	if graphBaseURL == "" {
		graphBaseURL = graphV1Base
	}
	return &MicrosoftPickerGrantHandler{
		tokens:              tokens,
		registry:            registry,
		applicationObjectID: applicationObjectID,
		graphBaseURL:        graphBaseURL,
		userLookup:          userLookup,
		httpClient:          &http.Client{Timeout: 30 * time.Second},
	}
}

// Handle processes POST /me/microsoft/picker_grants.
//
// Flow:
//  1. Verify applicationObjectID is configured → else 422 picker_grants_not_configured.
//  2. Authenticate the caller → else 401 unauthenticated.
//  3. Parse body; require non-empty drive_id and item_id → else 400 invalid_request.
//  4. Look up the user's Microsoft token → else 404 not_linked.
//  5. Short-circuit on failed_refresh → 401 token_refresh_failed.
//  6. Refresh if expired → 401 (permanent) or 503 (transient) on failure.
//  7. Call Graph permissions API → 200 / 422 grant_failed / 503 transient_failure.
func (h *MicrosoftPickerGrantHandler) Handle(c *gin.Context) {
	log := slogging.Get().WithContext(c)

	if h.applicationObjectID == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":    "picker_grants_not_configured",
			"message": "Microsoft picker grants are not configured (application_object_id is empty)",
		})
		return
	}

	userID, ok := h.userLookup(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    "unauthenticated",
			"message": "authentication required",
		})
		return
	}

	var req MicrosoftPickerGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.DriveId == "" || req.ItemId == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "invalid_request",
			"message": "drive_id and item_id are required",
		})
		return
	}

	tok, err := h.tokens.GetByUserAndProvider(c.Request.Context(), userID, ProviderMicrosoft)
	if err != nil {
		if errors.Is(err, ErrContentTokenNotFound) {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    "not_linked",
				"message": "no linked Microsoft token",
			})
			return
		}
		log.Error("microsoft_picker_grant: token lookup user=%s: %v", userID, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	if tok.Status == ContentTokenStatusFailedRefresh {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    "token_refresh_failed",
			"message": "re-link Microsoft account",
		})
		return
	}

	accessToken, _, err := h.refreshIfNeeded(c, tok)
	if err != nil {
		if errors.Is(err, ErrAuthRequired) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    "token_refresh_failed",
				"message": "re-link Microsoft account",
			})
			return
		}
		if errors.Is(err, ErrTransient) {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"code":    "transient_failure",
				"message": "retry later",
			})
			return
		}
		log.Error("microsoft_picker_grant: refresh user=%s: %v", userID, err)
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	permissionID, status, gerr := h.callGrantAPI(c.Request.Context(), accessToken, req.DriveId, req.ItemId)
	if gerr != nil {
		switch {
		case status >= 400 && status < 500:
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"code":         "grant_failed",
				"message":      "Graph rejected the permission grant",
				"graph_status": status,
			})
		case status >= 500:
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"code":    "transient_failure",
				"message": "Graph 5xx",
			})
		default:
			log.Error("microsoft_picker_grant: graph user=%s: %v", userID, gerr)
			c.AbortWithStatus(http.StatusInternalServerError)
		}
		return
	}

	log.Info("microsoft_picker_grant_success user_id=%s drive_id=%s item_id=%s permission_id=%s",
		userID, req.DriveId, req.ItemId, permissionID)
	c.JSON(http.StatusOK, MicrosoftPickerGrantResponse{
		PermissionId: permissionID,
		DriveId:      req.DriveId,
		ItemId:       req.ItemId,
	})
}

// refreshIfNeeded returns the plaintext access token and expiry for the given
// ContentToken, refreshing it first when it is expired (or within 30s of
// expiry). The logic mirrors PickerTokenHandler.refreshIfNeeded.
//
// Error returns:
//   - ErrAuthRequired: permanent OAuth failure (token revoked/invalid) or no
//     refresh token; caller should ask user to re-authorize.
//   - ErrTransient: transient network/5xx failure; caller may retry.
//   - Other: unexpected repository or provider error.
func (h *MicrosoftPickerGrantHandler) refreshIfNeeded(c *gin.Context, tok *ContentToken) (string, *time.Time, error) {
	log := slogging.Get().WithContext(c)

	if !pickerTokenExpired(tok) {
		return tok.AccessToken, tok.ExpiresAt, nil
	}

	log.Debug("microsoft_picker_grant: token expired, refreshing user=%s", tok.UserID)

	provider, ok := h.registry.Get(ProviderMicrosoft)
	if !ok {
		log.Warn("microsoft_picker_grant: microsoft provider not registered")
		return "", nil, ErrAuthRequired
	}

	var permanentFailure bool
	var transientFailure bool

	updated, err := h.tokens.RefreshWithLock(c.Request.Context(), tok.ID, func(current *ContentToken) (*ContentToken, error) {
		// Re-check expiry inside the lock: another goroutine may have already
		// refreshed the token between our initial check and acquiring the lock.
		if !pickerTokenExpired(current) {
			log.Debug("microsoft_picker_grant: token already refreshed by peer, skipping provider call")
			return current, nil
		}

		// No refresh token — flip to failed_refresh and commit.
		if current.RefreshToken == "" {
			log.Warn("microsoft_picker_grant: no refresh token available, marking failed")
			current.Status = ContentTokenStatusFailedRefresh
			current.LastError = errNoRefreshToken
			permanentFailure = true
			return current, nil
		}

		resp, refreshErr := provider.Refresh(c.Request.Context(), current.RefreshToken)
		if refreshErr != nil {
			if IsContentOAuthPermanentFailure(refreshErr) {
				log.Warn("microsoft_picker_grant: permanent refresh failure, marking failed err=%s", refreshErr)
				current.Status = ContentTokenStatusFailedRefresh
				current.LastError = refreshErr.Error()
				permanentFailure = true
				return current, nil
			}
			log.Warn("microsoft_picker_grant: transient refresh failure err=%s", refreshErr)
			transientFailure = true
			return nil, fmt.Errorf("microsoft_picker_grant refresh transient: %w", refreshErr)
		}

		// Success: update token fields.
		now := time.Now()
		current.AccessToken = resp.AccessToken
		if resp.RefreshToken != "" {
			current.RefreshToken = resp.RefreshToken
		}
		if resp.Scope != "" {
			current.Scopes = resp.Scope
		}
		current.ExpiresAt = resp.ExpiresAt()
		current.LastRefreshAt = &now
		current.LastError = ""
		current.Status = ContentTokenStatusActive
		return current, nil
	})

	if permanentFailure {
		return "", nil, ErrAuthRequired
	}
	if transientFailure {
		return "", nil, ErrTransient
	}
	if err != nil {
		return "", nil, err
	}
	return updated.AccessToken, updated.ExpiresAt, nil
}

// callGrantAPI calls Graph's POST /drives/{driveId}/items/{itemId}/permissions.
// Returns (permissionID, statusCode, error). statusCode is set to the HTTP
// status on non-2xx responses; on network errors it is 0.
func (h *MicrosoftPickerGrantHandler) callGrantAPI(ctx context.Context, token, driveID, itemID string) (string, int, error) {
	body := map[string]any{
		"roles": []string{"read"},
		"grantedToIdentities": []map[string]any{
			{"application": map[string]any{"id": h.applicationObjectID, "displayName": "TMI"}},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", 0, fmt.Errorf("marshal grant body: %w", err)
	}

	url := fmt.Sprintf("%s/drives/%s/items/%s/permissions", h.graphBaseURL, driveID, itemID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.httpClient.Do(req) //nolint:gosec // G107 — Graph URL constructed from trusted base URL and validated drive/item ids
	if err != nil {
		return "", 0, fmt.Errorf("graph request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 400 {
		return "", resp.StatusCode, fmt.Errorf("graph %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", resp.StatusCode, fmt.Errorf("decode permission response: %w", err)
	}
	return parsed.ID, resp.StatusCode, nil
}

// compile-time assertion: MicrosoftPickerGrantHandler satisfies the interface.
var _ microsoftPickerGrantHandlerInterface = (*MicrosoftPickerGrantHandler)(nil)
