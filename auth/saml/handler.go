package saml

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"time"

	crewjamsaml "github.com/crewjam/saml"
	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SAMLHandler handles SAML authentication operations
type SAMLHandler struct {
	service      *auth.Service
	samlProvider *SAMLProvider
	stateStore   auth.StateStore
}

// NewSAMLHandler creates a new SAML handler
func NewSAMLHandler(service *auth.Service, provider *SAMLProvider, stateStore auth.StateStore) *SAMLHandler {
	return &SAMLHandler{
		service:      service,
		samlProvider: provider,
		stateStore:   stateStore,
	}
}

// Metadata returns the SP metadata XML
func (h *SAMLHandler) Metadata(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("Serving SAML metadata")

	metadataXML, err := h.samlProvider.GetMetadataXML()
	if err != nil {
		logger.Error("Failed to get SAML metadata: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate metadata",
		})
		return
	}

	c.Header("Content-Type", "application/samlmetadata+xml")
	c.Data(http.StatusOK, "application/samlmetadata+xml", metadataXML)
}

// InitiateLogin starts the SAML authentication flow
func (h *SAMLHandler) InitiateLogin(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("Initiating SAML login")

	// Generate state parameter
	state := uuid.New().String()

	// Get callback URL from query or use default
	callbackURL := c.Query("client_callback")
	if callbackURL == "" {
		callbackURL = c.Query("redirect_uri")
	}

	// Store state with callback URL
	stateData := &auth.StateData{
		State:       state,
		Provider:    "saml",
		CallbackURL: callbackURL,
		CreatedAt:   time.Now(),
	}

	if err := h.stateStore.StoreState(c.Request.Context(), state, stateData); err != nil {
		logger.Error("Failed to store state: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to initiate login",
		})
		return
	}

	// Get SAML authentication URL
	authURL, err := h.samlProvider.GetAuthorizationURL(state)
	if err != nil {
		logger.Error("Failed to get SAML auth URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate authentication URL",
		})
		return
	}

	// Redirect to IdP
	c.Redirect(http.StatusFound, authURL)
}

// ACS handles the SAML Assertion Consumer Service endpoint
func (h *SAMLHandler) ACS(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("Processing SAML ACS callback")

	// Get SAML response from POST data
	samlResponse := c.PostForm("SAMLResponse")
	if samlResponse == "" {
		logger.Error("No SAMLResponse in POST data")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing SAML response",
		})
		return
	}

	// Get relay state (our state parameter)
	relayState := c.PostForm("RelayState")
	if relayState == "" {
		logger.Warn("No RelayState in SAML response")
	}

	// Decode base64 SAML response
	decodedResponse, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		logger.Error("Failed to decode SAML response: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid SAML response encoding",
		})
		return
	}

	// Parse and validate SAML response
	assertion, err := h.samlProvider.ParseResponse(string(decodedResponse))
	if err != nil {
		logger.Error("Failed to parse/validate SAML response: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid SAML response",
		})
		return
	}

	// Extract user info from assertion
	userInfo, err := h.samlProvider.ExtractUserInfoFromAssertion(assertion)
	if err != nil {
		logger.Error("Failed to extract user info from assertion: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to process user information",
		})
		return
	}

	// Get state data if relay state is present
	var callbackURL string
	if relayState != "" {
		stateData, err := h.stateStore.GetState(c.Request.Context(), relayState)
		if err != nil {
			logger.Warn("Failed to retrieve state data: %v", err)
			// Continue without callback URL
		} else {
			callbackURL = stateData.CallbackURL
			// Clean up state
			_ = h.stateStore.DeleteState(c.Request.Context(), relayState)
		}
	}

	// Process user in our system
	user, err := h.processSAMLUser(c.Request.Context(), userInfo, assertion)
	if err != nil {
		logger.Error("Failed to process SAML user: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to process authentication",
		})
		return
	}

	// Extract groups from assertion
	groups := h.extractGroups(assertion)

	// Cache groups if present
	if len(groups) > 0 {
		if err := h.service.CacheUserGroups(c.Request.Context(), user.ID, groups); err != nil {
			logger.Warn("Failed to cache user groups: %v", err)
			// Continue - groups can be retrieved later
		}
	}

	// Generate JWT token
	claims := auth.JWTClaims{
		UserID:           user.ID,
		Email:            user.Email,
		Name:             user.Name,
		IdentityProvider: "saml",
		Groups:           groups,
		ExpiresAt:        time.Now().Add(24 * time.Hour).Unix(),
		IssuedAt:         time.Now().Unix(),
	}

	token, err := h.service.GenerateToken(claims)
	if err != nil {
		logger.Error("Failed to generate JWT token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to generate authentication token",
		})
		return
	}

	// If we have a callback URL, redirect with token
	if callbackURL != "" {
		// Parse callback URL and add token
		parsedURL, err := url.Parse(callbackURL)
		if err != nil {
			logger.Error("Failed to parse callback URL: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Invalid callback URL",
			})
			return
		}

		// Add token to query parameters
		q := parsedURL.Query()
		q.Set("access_token", token)
		q.Set("token_type", "Bearer")
		if relayState != "" {
			q.Set("state", relayState)
		}
		parsedURL.RawQuery = q.Encode()

		c.Redirect(http.StatusFound, parsedURL.String())
		return
	}

	// Return token as JSON
	c.JSON(http.StatusOK, gin.H{
		"access_token": token,
		"token_type":   "Bearer",
		"user": gin.H{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
			"idp":   "saml",
			"groups": groups,
		},
	})
}

// SLO handles Single Logout
func (h *SAMLHandler) SLO(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("Processing SAML SLO request")

	// Get SAML request from query or POST
	samlRequest := c.Query("SAMLRequest")
	if samlRequest == "" {
		samlRequest = c.PostForm("SAMLRequest")
	}

	if samlRequest == "" {
		logger.Error("No SAMLRequest in request")
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Missing SAML logout request",
		})
		return
	}

	// Decode base64 SAML request
	decodedRequest, err := base64.StdEncoding.DecodeString(samlRequest)
	if err != nil {
		logger.Error("Failed to decode SAML logout request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid SAML logout request encoding",
		})
		return
	}

	// Parse logout request
	var logoutRequest crewjamsaml.LogoutRequest
	if err := xml.Unmarshal(decodedRequest, &logoutRequest); err != nil {
		logger.Error("Failed to parse SAML logout request: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid SAML logout request",
		})
		return
	}

	// TODO: Validate logout request signature
	// TODO: Invalidate user session

	// For now, just acknowledge the logout
	logger.Info("SAML logout processed for user: %s", logoutRequest.NameID.Value)

	// Create logout response
	// TODO: Implement proper logout response
	c.JSON(http.StatusOK, gin.H{
		"message": "Logout successful",
	})
}

// processSAMLUser creates or updates a user from SAML assertion
func (h *SAMLHandler) processSAMLUser(ctx context.Context, userInfo *auth.UserInfo, assertion *crewjamsaml.Assertion) (*auth.User, error) {
	logger := slogging.Get()

	// Check if user exists
	existingUser, err := h.service.GetUserByEmail(ctx, userInfo.Email)
	if err == nil {
		// User exists, update their info
		existingUser.Name = userInfo.Name
		existingUser.IdentityProvider = "saml"
		existingUser.ModifiedAt = time.Now()

		if err := h.service.UpdateUser(ctx, existingUser); err != nil {
			return nil, fmt.Errorf("failed to update user: %w", err)
		}

		logger.Debug("Updated existing SAML user: %s", existingUser.Email)
		return existingUser, nil
	}

	// Create new user
	newUser := &auth.User{
		ID:               uuid.New().String(),
		Email:            userInfo.Email,
		Name:             userInfo.Name,
		IdentityProvider: "saml",
		CreatedAt:        time.Now(),
		ModifiedAt:       time.Now(),
	}

	if err := h.service.CreateUser(ctx, newUser); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	logger.Info("Created new SAML user: %s", newUser.Email)
	return newUser, nil
}

// extractGroups extracts group memberships from SAML assertion
func (h *SAMLHandler) extractGroups(assertion *crewjamsaml.Assertion) []string {
	logger := slogging.Get()

	var groups []string
	config := h.samlProvider.GetConfig()

	// Look for group attribute in assertion
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			// Check if this is the groups attribute
			if attr.Name == config.GroupsAttribute ||
			   attr.FriendlyName == config.GroupsAttribute ||
			   attr.Name == "groups" ||
			   attr.Name == "memberOf" {
				// Extract all values as groups
				for _, value := range attr.Values {
					if value.Value != "" {
						groups = append(groups, value.Value)
					}
				}
			}
		}
	}

	logger.Debug("Extracted %d groups from SAML assertion", len(groups))
	return groups
}

// GetProviderGroups returns the groups for a user from SAML IdP
func (h *SAMLHandler) GetProviderGroups(c *gin.Context) {
	logger := slogging.GetContextLogger(c)

	// Get user from context
	userID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "User not authenticated",
		})
		return
	}

	// Get cached groups
	groups, err := h.service.GetCachedGroups(c.Request.Context(), userID.(string))
	if err != nil {
		logger.Warn("Failed to get cached groups: %v", err)
		// Return empty groups array instead of error
		groups = []string{}
	}

	c.JSON(http.StatusOK, gin.H{
		"groups": groups,
		"idp":    "saml",
	})
}