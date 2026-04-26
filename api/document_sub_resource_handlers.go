package api

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// DocumentSubResourceHandler provides handlers for document sub-resource operations
type DocumentSubResourceHandler struct {
	documentStore    DocumentStore
	db               *sql.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	// URI validator for SSRF protection on uri fields
	documentURIValidator *URIValidator
	// Content pipeline for detecting content sources and validating access
	contentPipeline *ContentPipeline
	// contentTokens resolves the calling user's linked providers for per-viewer diagnostics
	contentTokens ContentTokenRepository
	// serviceAccountEmail is used to populate the share_with_service_account remediation
	serviceAccountEmail string
	// microsoftApplicationObjectID is the TMI Entra app's object id used to build
	// the share_with_application remediation for Microsoft documents.
	microsoftApplicationObjectID string
	// contentOAuthRegistry resolves registered OAuth providers for picker_registration validation
	contentOAuthRegistry *ContentOAuthProviderRegistry
}

// SetDocumentURIValidator sets the URI validator for document uri fields
func (h *DocumentSubResourceHandler) SetDocumentURIValidator(v *URIValidator) {
	h.documentURIValidator = v
}

// SetContentPipeline sets the content pipeline for content source detection and access validation
func (h *DocumentSubResourceHandler) SetContentPipeline(p *ContentPipeline) {
	h.contentPipeline = p
}

// SetContentTokens sets the content token repository used to look up the caller's linked
// providers when assembling per-viewer access diagnostics. Optional: when nil, diagnostics
// are still assembled but with empty linkedProviders.
func (h *DocumentSubResourceHandler) SetContentTokens(r ContentTokenRepository) {
	h.contentTokens = r
}

// SetServiceAccountEmail sets the service-account email address included in the
// share_with_service_account remediation. Optional: when empty, the param is an empty string.
func (h *DocumentSubResourceHandler) SetServiceAccountEmail(s string) {
	h.serviceAccountEmail = s
}

// SetMicrosoftApplicationObjectID sets the TMI Entra application object id
// included in the share_with_application remediation for Microsoft documents.
// Optional: when empty, the param is an empty string.
func (h *DocumentSubResourceHandler) SetMicrosoftApplicationObjectID(id string) {
	h.microsoftApplicationObjectID = id
}

// SetContentOAuthRegistry sets the content-OAuth provider registry used to validate
// picker_registration payloads at document attach time. Optional: when nil,
// picker_registration is rejected with 422 (provider_not_registered).
func (h *DocumentSubResourceHandler) SetContentOAuthRegistry(r *ContentOAuthProviderRegistry) {
	h.contentOAuthRegistry = r
}

// pickerRegistrationSniff is used to extract picker_registration from the raw request
// body before the typed parse (Document response type doesn't carry that field).
type pickerRegistrationSniff struct {
	PickerRegistration *struct {
		ProviderID string `json:"provider_id"`
		FileID     string `json:"file_id"`
		MimeType   string `json:"mime_type"`
	} `json:"picker_registration"`
}

// validatePickerRegistration validates the picker_registration payload against
// the URI, the registered provider list, and the caller's active linked token.
// Returns true if validation passed (or if no picker_registration was present),
// false if a response has already been written (error case).
func (h *DocumentSubResourceHandler) validatePickerRegistration(
	c *gin.Context, uri string, sniff pickerRegistrationSniff, userInternalUUID string,
) bool {
	if sniff.PickerRegistration == nil {
		return true
	}
	pr := sniff.PickerRegistration
	if pr.ProviderID == "" || pr.FileID == "" || pr.MimeType == "" {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_picker_registration",
			Message: "picker_registration must include non-empty provider_id, file_id, and mime_type",
		})
		return false
	}
	fileID, ok := extractGoogleDriveFileID(uri)
	if !ok || fileID != pr.FileID {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusBadRequest,
			Code:    "picker_file_id_mismatch",
			Message: "picker_registration.file_id does not match the file id in uri",
		})
		return false
	}
	if h.contentOAuthRegistry == nil {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "provider_not_registered",
			Message: fmt.Sprintf("content provider %q is not configured on this server", pr.ProviderID),
		})
		return false
	}
	if _, ok := h.contentOAuthRegistry.Get(pr.ProviderID); !ok {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "provider_not_registered",
			Message: fmt.Sprintf("content provider %q is not configured on this server", pr.ProviderID),
		})
		return false
	}
	if h.contentTokens == nil || userInternalUUID == "" {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "token_not_linked_or_failed",
			Message: "caller has no active linked token for this provider",
		})
		return false
	}
	token, tokenErr := h.contentTokens.GetByUserAndProvider(c.Request.Context(), userInternalUUID, pr.ProviderID)
	if tokenErr != nil || token == nil || token.Status != ContentTokenStatusActive {
		HandleRequestError(c, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "token_not_linked_or_failed",
			Message: "caller has no active linked token for this provider",
		})
		return false
	}
	return true
}

// NewDocumentSubResourceHandler creates a new document sub-resource handler
func NewDocumentSubResourceHandler(documentStore DocumentStore, db *sql.DB, cache *CacheService, invalidator *CacheInvalidator) *DocumentSubResourceHandler {
	return &DocumentSubResourceHandler{
		documentStore:    documentStore,
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// GetDocuments retrieves all documents for a threat model with pagination
// GET /threat_models/{threat_model_id}/documents
func (h *DocumentSubResourceHandler) GetDocuments(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetDocuments - retrieving documents for threat model")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Parse pagination parameters
	limit := parseIntParam(c.DefaultQuery("limit", "20"), 20)
	offset := parseIntParam(c.DefaultQuery("offset", "0"), 0)

	// Validate pagination parameters
	if limit < 1 || limit > 100 {
		HandleRequestError(c, InvalidInputError("Limit must be between 1 and 100"))
		return
	}
	if offset < 0 {
		HandleRequestError(c, InvalidInputError("Offset must be non-negative"))
		return
	}

	// Get authenticated user (should be set by middleware)
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving documents for threat model %s (user: %s, offset: %d, limit: %d)",
		threatModelID, user.Email, offset, limit)

	// Get documents from store (authorization is handled by middleware)
	documents, err := h.documentStore.List(c.Request.Context(), threatModelID, offset, limit)
	if err != nil {
		logger.Error("Failed to retrieve documents: %v", err)
		HandleRequestError(c, ServerError("Failed to retrieve documents"))
		return
	}

	// Get total count for pagination
	total, err := h.documentStore.Count(c.Request.Context(), threatModelID)
	if err != nil {
		logger.Warn("Failed to get document count, using page size: %v", err)
		total = len(documents)
	}

	logger.Debug("Successfully retrieved %d documents (total: %d)", len(documents), total)
	c.JSON(http.StatusOK, ListDocumentsResponse{
		Documents: documents,
		Total:     total,
		Limit:     limit,
		Offset:    offset,
	})
}

// GetDocument retrieves a specific document by ID
// GET /threat_models/{threat_model_id}/documents/{document_id}
func (h *DocumentSubResourceHandler) GetDocument(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("GetDocument - retrieving specific document")

	// Extract document ID from URL
	documentID := c.Param("document_id")
	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}

	// Validate document ID format
	if _, err := ParseUUID(documentID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid document ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Retrieving document %s (user: %s)", documentID, user.Email)

	// Get document from store
	document, err := h.documentStore.Get(c.Request.Context(), documentID)
	if err != nil {
		logger.Error("Failed to retrieve document %s: %v", documentID, err)
		HandleRequestError(c, NotFoundError("Document not found"))
		return
	}

	// Per-viewer access diagnostics (#249 sub-project 4).
	// Assembled when the document is not currently accessible.
	if document.AccessStatus != nil && *document.AccessStatus != DocumentAccessStatusAccessible {
		reasonCode, reasonDetail, updatedAt, reasonErr := h.documentStore.GetAccessReason(c.Request.Context(), documentID)
		if reasonErr != nil {
			logger.Warn("GetDocument: failed to load access reason for %s: %v", documentID, reasonErr)
		} else if reasonCode != "" {
			linkedProviders := map[string]bool{}
			if h.contentTokens != nil && user.InternalUUID != "" {
				tokens, listErr := h.contentTokens.ListByUser(c.Request.Context(), user.InternalUUID)
				if listErr != nil {
					logger.Warn("GetDocument: failed to list content tokens for caller: %v", listErr)
				} else {
					for _, t := range tokens {
						if t.Status == ContentTokenStatusActive {
							linkedProviders[t.ProviderID] = true
						}
					}
				}
			}
			providerID := ""
			if document.ContentSource != nil {
				providerID = *document.ContentSource
			}
			diag := BuildAccessDiagnostics(BuilderContext{
				ReasonCode:            reasonCode,
				ReasonDetail:          reasonDetail,
				ProviderID:            providerID,
				CallerUserEmail:       user.Email,
				CallerLinkedProviders: linkedProviders,
				ServiceAccountEmail:   h.serviceAccountEmail,
				// MicrosoftDriveID and MicrosoftItemID are left empty here; they
				// are not yet extractable from the document model in the paste-URL
				// flow. Task 12 will populate these when the document carries
				// picker metadata or resolved drive/item ids from ValidateAccess.
				MicrosoftApplicationObjectID: h.microsoftApplicationObjectID,
			})
			document.AccessDiagnostics = toWireDiagnostics(diag)
			document.AccessStatusUpdatedAt = updatedAt
		}
	}

	logger.Debug("Successfully retrieved document %s", documentID)
	c.JSON(http.StatusOK, document)
}

// CreateDocument creates a new document in a threat model
// POST /threat_models/{threat_model_id}/documents
func (h *DocumentSubResourceHandler) CreateDocument(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("CreateDocument - creating new document")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Sniff for picker_registration before the typed parse — Document (response
	// type) doesn't carry picker_registration; only DocumentInput does. We
	// extract it separately, then reset the body so the existing typed parse
	// can run unchanged.
	var sniff pickerRegistrationSniff
	rawBody, readErr := io.ReadAll(c.Request.Body)
	if readErr != nil {
		HandleRequestError(c, InvalidInputError("Failed to read request body"))
		return
	}
	_ = json.Unmarshal(rawBody, &sniff) // ignore parse errors; ValidateAndParseRequest will surface them
	c.Request.Body = io.NopCloser(bytes.NewReader(rawBody))

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["document_create"]
	document, err := ValidateAndParseRequest[Document](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Sanitize text fields (defense-in-depth)
	document.Name = SanitizePlainText(document.Name)
	document.Description = SanitizeOptionalString(document.Description)
	document.Uri = SanitizePlainText(document.Uri)
	if err := SanitizeMetadataSlice(document.Metadata); err != nil {
		HandleRequestError(c, err)
		return
	}
	if err := validateURI(h.documentURIValidator, "uri", document.Uri); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Validate picker_registration if present.
	if !h.validatePickerRegistration(c, document.Uri, sniff, user.InternalUUID) {
		return
	}

	// Detect content source and validate provider (URL-based dispatch).
	// Skipped when picker_registration is present — the delegated source
	// (registered under the picker provider id) handles validation via the
	// poller, dispatched through FindSourceForDocument.
	var accessStatus, contentSource string
	if sniff.PickerRegistration != nil {
		contentSource = sniff.PickerRegistration.ProviderID
		accessStatus = AccessStatusUnknown
	} else if h.contentPipeline != nil {
		matcher := h.contentPipeline.Matcher()
		provider := matcher.Identify(document.Uri)

		if provider != "" && provider != ProviderHTTP {
			// Known non-HTTP provider — check if a source for this specific provider is registered
			_, hasSource := h.contentPipeline.Sources().FindSourceByName(provider)
			if !hasSource {
				HandleRequestError(c, &RequestError{
					Status:  422,
					Code:    "provider_not_configured",
					Message: fmt.Sprintf("%s document access is not configured on this server. Contact your administrator.", provider),
				})
				return
			}
			contentSource = provider
			accessStatus = AccessStatusUnknown // will be updated after creation if source supports validation
		} else if provider == ProviderHTTP {
			contentSource = ProviderHTTP
			accessStatus = AccessStatusUnknown
		}
	}

	// Generate UUID if not provided
	if document.Id == nil {
		id := uuid.New()
		document.Id = &id
	}

	logger.Debug("Creating document %s in threat model %s (user: %s)",
		document.Id.String(), threatModelID, user.Email)

	// Create document in store
	if err := h.documentStore.Create(c.Request.Context(), document, threatModelID); err != nil {
		logger.Error("Failed to create document: %v", err)
		HandleRequestError(c, ServerError("Failed to create document"))
		return
	}

	// Persist access tracking and (if applicable) picker metadata on the row.
	if sniff.PickerRegistration != nil {
		pr := sniff.PickerRegistration
		if err := h.documentStore.SetPickerMetadata(c.Request.Context(),
			document.Id.String(), pr.ProviderID, pr.FileID, pr.MimeType); err != nil {
			logger.Warn("Failed to set picker metadata for document %s: %v", document.Id.String(), err)
		}
		// Reflect access fields in the response.
		status := DocumentAccessStatus(AccessStatusUnknown)
		document.AccessStatus = &status
		cs := pr.ProviderID
		document.ContentSource = &cs
	} else if h.contentPipeline != nil && accessStatus != "" {
		// Try access validation for non-HTTP providers
		if contentSource != "" && contentSource != ProviderHTTP {
			src, _ := h.contentPipeline.Sources().FindSourceByName(contentSource)
			if validator, ok := src.(AccessValidator); ok {
				accessible, valErr := validator.ValidateAccess(c.Request.Context(), document.Uri)
				if valErr != nil {
					logger.Warn("Access validation failed for %s: %v", document.Uri, valErr)
				}
				if accessible {
					accessStatus = AccessStatusAccessible
				} else {
					if requester, ok := src.(AccessRequester); ok {
						if reqErr := requester.RequestAccess(c.Request.Context(), document.Uri); reqErr != nil {
							logger.Warn("Access request failed for %s: %v", document.Uri, reqErr)
						}
					}
					accessStatus = AccessStatusPendingAccess
				}
			}
		}
		// Update access fields in database
		if err := h.documentStore.UpdateAccessStatus(c.Request.Context(), document.Id.String(), accessStatus, contentSource); err != nil {
			logger.Warn("Failed to update access status for document %s: %v", document.Id.String(), err)
		}

		// Reflect access fields in the response
		status := DocumentAccessStatus(accessStatus)
		document.AccessStatus = &status
		if contentSource != "" {
			document.ContentSource = &contentSource
		}
	}

	RecordAuditCreate(c, threatModelID, "document", document.Id.String(), document)
	invalidateThreatModelCaches(c, threatModelID)

	logger.Debug("Successfully created document %s", document.Id.String())
	c.JSON(http.StatusCreated, document)
}

// UpdateDocument updates an existing document
// PUT /threat_models/{threat_model_id}/documents/{document_id}
func (h *DocumentSubResourceHandler) UpdateDocument(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("UpdateDocument - updating existing document")

	// Extract document ID from URL
	documentID := c.Param("document_id")
	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}

	// Validate document ID format
	documentUUID, err := ParseUUID(documentID)
	if err != nil {
		HandleRequestError(c, InvalidIDError("Invalid document ID format, must be a valid UUID"))
		return
	}

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body with prohibited field checking
	config := ValidationConfigs["document_update"]
	document, err := ValidateAndParseRequest[Document](c, config)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Sanitize text fields (defense-in-depth)
	document.Name = SanitizePlainText(document.Name)
	document.Description = SanitizeOptionalString(document.Description)
	document.Uri = SanitizePlainText(document.Uri)
	if err := SanitizeMetadataSlice(document.Metadata); err != nil {
		HandleRequestError(c, err)
		return
	}
	if err := validateURI(h.documentURIValidator, "uri", document.Uri); err != nil {
		HandleRequestError(c, err)
		return
	}

	// Set ID from URL (override any value in body)
	document.Id = &documentUUID

	logger.Debug("Updating document %s (user: %s)", documentID, user.Email)

	// Capture pre-mutation state for audit
	existingDoc, _ := h.documentStore.Get(c.Request.Context(), documentID)
	var preState []byte
	if existingDoc != nil {
		preState, _ = SerializeForAudit(existingDoc)
	}

	// Update document in store
	if err := h.documentStore.Update(c.Request.Context(), document, threatModelID); err != nil {
		logger.Error("Failed to update document %s: %v", documentID, err)
		// Check if the error indicates document not found
		if strings.Contains(err.Error(), "not found") {
			HandleRequestError(c, NotFoundError("Document not found"))
			return
		}
		HandleRequestError(c, ServerError("Failed to update document"))
		return
	}

	RecordAuditUpdate(c, "updated", threatModelID, "document", documentID, preState, document)
	invalidateThreatModelCaches(c, threatModelID)

	logger.Debug("Successfully updated document %s", documentID)
	c.JSON(http.StatusOK, document)
}

// DeleteDocument deletes a document
// DELETE /threat_models/{threat_model_id}/documents/{document_id}
func (h *DocumentSubResourceHandler) DeleteDocument(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("DeleteDocument - deleting document")

	// Extract document ID from URL
	documentID := c.Param("document_id")
	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}

	// Validate document ID format
	if _, err := ParseUUID(documentID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid document ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Deleting document %s (user: %s)", documentID, user.Email)

	// Capture pre-deletion state for audit
	existingDoc, _ := h.documentStore.Get(c.Request.Context(), documentID)
	var preState []byte
	if existingDoc != nil {
		preState, _ = SerializeForAudit(existingDoc)
	}

	// Delete document from store
	if err := h.documentStore.Delete(c.Request.Context(), documentID); err != nil {
		logger.Error("Failed to delete document %s: %v", documentID, err)
		// Check if the error indicates document not found
		if strings.Contains(err.Error(), "not found") {
			HandleRequestError(c, NotFoundError("Document not found"))
			return
		}
		HandleRequestError(c, ServerError("Failed to delete document"))
		return
	}

	threatModelID := c.Param("threat_model_id")
	RecordAuditDelete(c, threatModelID, "document", documentID, preState)
	invalidateThreatModelCaches(c, threatModelID)

	logger.Debug("Successfully deleted document %s", documentID)
	c.Status(http.StatusNoContent)
}

// BulkCreateDocuments creates multiple documents in a single request
// POST /threat_models/{threat_model_id}/documents/bulk
func (h *DocumentSubResourceHandler) BulkCreateDocuments(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkCreateDocuments - creating multiple documents")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of documents
	var documents []Document
	if err := c.ShouldBindJSON(&documents); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(documents) == 0 {
		HandleRequestError(c, InvalidInputError("No documents provided"))
		return
	}

	if len(documents) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 documents allowed per bulk operation"))
		return
	}

	// Validate each document
	for _, document := range documents {
		if document.Name == "" {
			HandleRequestError(c, InvalidInputError("Document name is required for all documents"))
			return
		}
		if document.Uri == "" {
			HandleRequestError(c, InvalidInputError("Document URI is required for all documents"))
			return
		}
	}

	// Generate UUIDs and sanitize text fields
	for i := range documents {
		document := &documents[i]

		// Sanitize text fields (defense-in-depth)
		document.Name = SanitizePlainText(document.Name)
		document.Description = SanitizeOptionalString(document.Description)
		document.Uri = SanitizePlainText(document.Uri)
		if err := SanitizeMetadataSlice(document.Metadata); err != nil {
			HandleRequestError(c, err)
			return
		}
		if err := validateURI(h.documentURIValidator, "uri", document.Uri); err != nil {
			HandleRequestError(c, err)
			return
		}

		if document.Id == nil {
			id := uuid.New()
			document.Id = &id
		}
	}

	logger.Debug("Bulk creating %d documents in threat model %s (user: %s)",
		len(documents), threatModelID, user.Email)

	// Create documents in store
	if err := h.documentStore.BulkCreate(c.Request.Context(), documents, threatModelID); err != nil {
		logger.Error("Failed to bulk create documents: %v", err)
		HandleRequestError(c, ServerError("Failed to create documents"))
		return
	}

	invalidateThreatModelCaches(c, threatModelID)

	logger.Debug("Successfully bulk created %d documents", len(documents))
	c.JSON(http.StatusCreated, documents)
}

// PatchDocument applies JSON patch operations to a document
// PATCH /threat_models/{threat_model_id}/documents/{document_id}
func (h *DocumentSubResourceHandler) PatchDocument(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("PatchDocument - applying patch operations to document")

	// Extract document ID from URL
	documentID := c.Param("document_id")
	if documentID == "" {
		HandleRequestError(c, InvalidIDError("Missing document ID"))
		return
	}

	// Validate document ID format
	if _, err := ParseUUID(documentID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid document ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}
	userRole, err := GetResourceRole(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse patch operations from request body
	operations, err := ParsePatchRequest(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	if len(operations) == 0 {
		HandleRequestError(c, InvalidInputError("No patch operations provided"))
		return
	}

	// Validate patch authorization
	if err := ValidatePatchAuthorization(operations, userRole); err != nil {
		HandleRequestError(c, ForbiddenError("Insufficient permissions for requested patch operations"))
		return
	}

	// Sanitize text values in patch operations (defense-in-depth)
	SanitizePatchOperations(operations, []string{"/name", "/description", "/uri"})
	if err := ValidateURIPatchOperations(h.documentURIValidator, operations, []string{"/uri"}); err != nil {
		HandleRequestError(c, err)
		return
	}

	logger.Debug("Applying %d patch operations to document %s (user: %s)",
		len(operations), documentID, user.Email)

	// Capture pre-mutation state for audit
	existingDoc, _ := h.documentStore.Get(c.Request.Context(), documentID)
	var preState []byte
	if existingDoc != nil {
		preState, _ = SerializeForAudit(existingDoc)
	}

	// Apply patch operations
	updatedDocument, err := h.documentStore.Patch(c.Request.Context(), documentID, operations)
	if err != nil {
		HandleRequestError(c, ServerError("Failed to patch document"))
		return
	}

	threatModelID := c.Param("threat_model_id")
	RecordAuditUpdate(c, "patched", threatModelID, "document", documentID, preState, updatedDocument)
	invalidateThreatModelCaches(c, threatModelID)

	logger.Info("Successfully patched document %s (user: %s)", documentID, user.Email)
	c.JSON(http.StatusOK, updatedDocument)
}

// BulkUpdateDocuments updates or creates multiple documents (upsert operation)
// PUT /threat_models/{threat_model_id}/documents/bulk
func (h *DocumentSubResourceHandler) BulkUpdateDocuments(c *gin.Context) {
	logger := slogging.GetContextLogger(c)
	logger.Debug("BulkUpdateDocuments - upserting multiple documents")

	// Extract threat model ID from URL
	threatModelID := c.Param("threat_model_id")
	if threatModelID == "" {
		HandleRequestError(c, InvalidIDError("Missing threat model ID"))
		return
	}

	// Validate threat model ID format
	if _, err := ParseUUID(threatModelID); err != nil {
		HandleRequestError(c, InvalidIDError("Invalid threat model ID format, must be a valid UUID"))
		return
	}

	// Get authenticated user
	user, err := GetAuthenticatedUser(c)
	if err != nil {
		HandleRequestError(c, err)
		return
	}

	// Parse and validate request body as array of documents
	var documents []Document
	if err := c.ShouldBindJSON(&documents); err != nil {
		HandleRequestError(c, InvalidInputError("Invalid request body: "+err.Error()))
		return
	}

	// Basic validation
	if len(documents) == 0 {
		HandleRequestError(c, InvalidInputError("No documents provided"))
		return
	}

	if len(documents) > 50 {
		HandleRequestError(c, InvalidInputError("Maximum 50 documents allowed per bulk operation"))
		return
	}

	// Validate each document
	for _, document := range documents {
		if document.Id == nil {
			HandleRequestError(c, InvalidInputError("Document ID is required for all documents in bulk update"))
			return
		}
		if document.Name == "" {
			HandleRequestError(c, InvalidInputError("Document name is required for all documents"))
			return
		}
	}

	// Sanitize text fields (defense-in-depth)
	for i := range documents {
		documents[i].Name = SanitizePlainText(documents[i].Name)
		documents[i].Description = SanitizeOptionalString(documents[i].Description)
		documents[i].Uri = SanitizePlainText(documents[i].Uri)
		if err := SanitizeMetadataSlice(documents[i].Metadata); err != nil {
			HandleRequestError(c, err)
			return
		}
		if err := validateURI(h.documentURIValidator, "uri", documents[i].Uri); err != nil {
			HandleRequestError(c, err)
			return
		}
	}

	logger.Debug("Bulk updating %d documents for threat model %s (user: %s)", len(documents), threatModelID, user.Email)

	// Upsert each document
	upsertedDocuments := make([]Document, 0, len(documents))
	for _, document := range documents {
		// Check if document exists
		_, err := h.documentStore.Get(c.Request.Context(), document.Id.String())
		if err != nil {
			// Document doesn't exist, create it
			if err := h.documentStore.Create(c.Request.Context(), &document, threatModelID); err != nil {
				logger.Error("Failed to create document %s: %v", document.Id.String(), err)
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to create document %s", document.Id.String())))
				return
			}
			upsertedDocuments = append(upsertedDocuments, document)
		} else {
			// Document exists, update it
			if err := h.documentStore.Update(c.Request.Context(), &document, threatModelID); err != nil {
				logger.Error("Failed to update document %s: %v", document.Id.String(), err)
				HandleRequestError(c, ServerError(fmt.Sprintf("Failed to update document %s", document.Id.String())))
				return
			}
			upsertedDocuments = append(upsertedDocuments, document)
		}
	}

	invalidateThreatModelCaches(c, threatModelID)

	logger.Info("Successfully bulk upserted %d documents for threat model %s (user: %s)", len(upsertedDocuments), threatModelID, user.Email)
	c.JSON(http.StatusOK, upsertedDocuments)
}
