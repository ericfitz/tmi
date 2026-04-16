package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// automationNamePattern validates the short identifier for automation accounts.
// Must start with a letter and end with a letter or digit. Allows letters, digits,
// spaces, underscores, periods, at-signs, and hyphens in between.
var automationNamePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9 _.@-]*[a-zA-Z0-9]$`)

// normalizeAutomationName converts a short name to the SMTP-safe local-part form.
// Lowercases and replaces characters that are not alphanumeric or hyphens with hyphens.
// Collapses consecutive hyphens and trims leading/trailing hyphens.
func normalizeAutomationName(name string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	// Collapse consecutive hyphens
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	return result
}

// CreateAutomationAccount handles POST /admin/users/automation
// Creates an automation (service) account with TMI provider, sets automation=true,
// adds to TMI Automation group, and creates a client credential.
func (s *Server) CreateAutomationAccount(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	// Parse request body
	var req CreateAutomationAccountJSONRequestBody
	if errMsg := StrictJSONBind(c, &req); errMsg != "" {
		logger.Warn("Invalid request body: %s", errMsg)
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: errMsg,
		})
		return
	}

	// Validate name
	name := strings.TrimSpace(req.Name)
	if len(name) < 2 || len(name) > 64 {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "name must be between 2 and 64 characters",
		})
		return
	}
	if !automationNamePattern.MatchString(name) {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "name must start with a letter, end with a letter or digit, and contain only letters, digits, spaces, underscores, periods, at-signs, and hyphens",
		})
		return
	}

	// Normalize name to SMTP-safe local-part
	normalized := normalizeAutomationName(name)
	if len(normalized) < 2 {
		c.JSON(http.StatusBadRequest, Error{
			Error:            "invalid_request",
			ErrorDescription: "name normalizes to fewer than 2 characters after sanitization",
		})
		return
	}

	// Build user fields
	providerUserID := "tmi-automation-" + normalized
	email := providerUserID + "@tmi.local"
	displayName := "TMI Automation: " + name

	// Allow custom email override
	if req.Email != nil {
		email = string(*req.Email)
	}

	// Check for duplicate provider_user_id
	_, err := GlobalUserStore.GetByProviderAndID(c.Request.Context(), "tmi", providerUserID)
	if err == nil {
		c.JSON(http.StatusConflict, Error{
			Error:            "conflict",
			ErrorDescription: fmt.Sprintf("An account with provider_user_id %q already exists", providerUserID),
		})
		return
	}

	// Get auth service adapter
	authServiceAdapter, ok := s.authService.(*AuthServiceAdapter)
	if !ok || authServiceAdapter == nil {
		logger.Error("Failed to get auth service adapter")
		c.Header("Retry-After", "30")
		c.JSON(http.StatusServiceUnavailable, Error{
			Error:            "service_unavailable",
			ErrorDescription: "Authentication service temporarily unavailable - please retry",
		})
		return
	}
	authSvc := authServiceAdapter.GetService()

	// Create the user via auth service
	automationTrue := true
	nowTime := time.Now()
	newUser := auth.User{
		Provider:       "tmi",
		ProviderUserID: providerUserID,
		Email:          email,
		Name:           displayName,
		EmailVerified:  true,
		Automation:     &automationTrue,
		CreatedAt:      nowTime,
		ModifiedAt:     nowTime,
	}

	createdUser, err := authSvc.CreateUser(c.Request.Context(), newUser)
	if err != nil {
		if errors.Is(err, dberrors.ErrDuplicate) || errors.Is(err, dberrors.ErrConstraint) {
			c.JSON(http.StatusConflict, Error{
				Error:            "conflict",
				ErrorDescription: "An account with the same email or provider ID already exists",
			})
			return
		}
		logger.Error("Failed to create automation user: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create automation account",
		})
		return
	}

	userUUID, err := uuid.Parse(createdUser.InternalUUID)
	if err != nil {
		logger.Error("Created user has invalid UUID: %v", err)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Failed to create automation account",
		})
		return
	}

	// Add user to TMI Automation group
	actorUUIDStr, _ := GetUserInternalUUID(c)
	var addedBy *uuid.UUID
	if actorUUID, parseErr := uuid.Parse(actorUUIDStr); parseErr == nil {
		addedBy = &actorUUID
	}
	notes := "Added automatically during automation account creation"

	if GlobalGroupMemberRepository != nil {
		_, err = GlobalGroupMemberRepository.AddMember(c.Request.Context(), GroupTMIAutomation.UUID, userUUID, addedBy, &notes)
		if err != nil {
			logger.Warn("Failed to add automation user to TMI Automation group: %v", err)
			// Continue — user is created, group membership is best-effort
		}
	}

	// Create client credential
	ccService := NewClientCredentialService(authSvc)
	ccResp, err := ccService.Create(c.Request.Context(), userUUID, CreateClientCredentialRequest{
		Name:        name,
		Description: "Auto-created for automation account " + displayName,
	})
	if err != nil {
		logger.Error("Failed to create client credential for automation user: %v", err)
		// User is created but credential failed — return error so admin knows to retry
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "User account created but client credential creation failed. Use POST /me/client_credentials to create one manually.",
		})
		return
	}

	// Get enriched admin user for response
	adminUser, err := GlobalUserStore.Get(c.Request.Context(), userUUID)
	if err != nil {
		logger.Warn("Failed to get admin user after creation: %v", err)
	}
	if adminUser != nil {
		enriched, enrichErr := GlobalUserStore.EnrichUsers(c.Request.Context(), []AdminUser{*adminUser})
		if enrichErr == nil && len(enriched) > 0 {
			adminUser = &enriched[0]
		}
	}

	// Build response
	var userResp AdminUser
	if adminUser != nil {
		userResp = *adminUser
	}

	apiCCResp := ClientCredentialResponse{
		Id:           ccResp.ID,
		ClientId:     ccResp.ClientID,
		ClientSecret: ccResp.ClientSecret,
		Name:         ccResp.Name,
		Description:  strPtr(ccResp.Description),
		CreatedAt:    ccResp.CreatedAt,
		ExpiresAt:    timePtr(ccResp.ExpiresAt),
	}

	logger.Info("[AUDIT] Automation account created: name=%s, email=%s, provider_user_id=%s, client_id=%s",
		sanitizeForLogging(displayName), email, providerUserID, ccResp.ClientID)

	c.JSON(http.StatusCreated, CreateAutomationAccountResponse{
		User:             userResp,
		ClientCredential: apiCCResp,
	})
}
