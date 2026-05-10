// Package api provides storage and HTTP handlers for the TMI service.
package api

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/gin-gonic/gin"
)

// auditDescriptor describes how to compute the audit-row fields for a single
// gated admin route. Each gated route MUST have a descriptor; a route without
// one falls back to "field_path = method + path, no old/new values" — that
// fallback is a defense-in-depth bug-catcher, not a routine code path.
type auditDescriptor struct {
	Method      string
	PathTpl     string // OpenAPI path template, e.g. /admin/settings/{key}
	FieldPathFn func(c *gin.Context) string
	OldValueFn  func(c *gin.Context) string
	NewValueFn  func(c *gin.Context, body []byte) string
	SummaryFn   func(c *gin.Context) string
}

// SystemSettingReader is a narrow interface implemented by the system-settings
// store; the descriptor table uses it to read the current value before a write.
type SystemSettingReader interface {
	Read(c *gin.Context, key string) string
}

// adminAuditDescriptors returns the descriptors for all gated admin routes.
// Adding a gated route requires adding an entry here; a unit test asserts
// coverage (#355).
func adminAuditDescriptors(reader SystemSettingReader) []auditDescriptor {
	read := func(c *gin.Context, key string) string {
		if reader == nil {
			return ""
		}
		return reader.Read(c, key)
	}

	return []auditDescriptor{
		// /admin/settings/*
		{
			Method:      "PUT",
			PathTpl:     "/admin/settings/{key}",
			FieldPathFn: func(c *gin.Context) string { return "system_settings." + c.Param("key") },
			OldValueFn:  func(c *gin.Context) string { return read(c, c.Param("key")) },
			NewValueFn:  func(c *gin.Context, body []byte) string { return extractJSONField(body, "value") },
			SummaryFn:   func(c *gin.Context) string { return "PUT system_settings." + c.Param("key") },
		},
		{
			Method:      "DELETE",
			PathTpl:     "/admin/settings/{key}",
			FieldPathFn: func(c *gin.Context) string { return "system_settings." + c.Param("key") },
			OldValueFn:  func(c *gin.Context) string { return read(c, c.Param("key")) },
			NewValueFn:  func(c *gin.Context, body []byte) string { return "" },
			SummaryFn:   func(c *gin.Context) string { return "DELETE system_settings." + c.Param("key") },
		},
		{
			Method:      "POST",
			PathTpl:     "/admin/settings/reencrypt",
			FieldPathFn: func(c *gin.Context) string { return "system_settings.encryption.master_key" },
			OldValueFn:  func(c *gin.Context) string { return "" }, // master key never logged verbatim
			NewValueFn:  func(c *gin.Context, body []byte) string { return "" },
			SummaryFn:   func(c *gin.Context) string { return "REENCRYPT system_settings" },
		},

		// /admin/users/*
		{
			Method:      "PATCH",
			PathTpl:     "/admin/users/{internal_uuid}",
			FieldPathFn: func(c *gin.Context) string { return "users." + c.Param("internal_uuid") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn:   func(c *gin.Context) string { return "PATCH user " + c.Param("internal_uuid") },
		},
		{
			Method:      "DELETE",
			PathTpl:     "/admin/users/{internal_uuid}",
			FieldPathFn: func(c *gin.Context) string { return "users." + c.Param("internal_uuid") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return "" },
			SummaryFn:   func(c *gin.Context) string { return "DELETE user " + c.Param("internal_uuid") },
		},
		{
			Method:      "POST",
			PathTpl:     "/admin/users/{internal_uuid}/transfer",
			FieldPathFn: func(c *gin.Context) string { return "users." + c.Param("internal_uuid") + ".ownership_transfer" },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn:   func(c *gin.Context) string { return "TRANSFER ownership for user " + c.Param("internal_uuid") },
		},
		{
			Method:      "POST",
			PathTpl:     "/admin/users/automation",
			FieldPathFn: func(c *gin.Context) string { return "users.automation_grant" },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn:   func(c *gin.Context) string { return "POST users.automation_grant" },
		},
		{
			Method:      "POST",
			PathTpl:     "/admin/users/{internal_uuid}/client_credentials",
			FieldPathFn: func(c *gin.Context) string { return "users." + c.Param("internal_uuid") + ".client_credentials.create" },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn: func(c *gin.Context) string {
				return "CREATE client credential for user " + c.Param("internal_uuid")
			},
		},
		{
			Method:  "DELETE",
			PathTpl: "/admin/users/{internal_uuid}/client_credentials/{credential_id}",
			FieldPathFn: func(c *gin.Context) string {
				return "users." + c.Param("internal_uuid") + ".client_credentials." + c.Param("credential_id")
			},
			OldValueFn: func(c *gin.Context) string { return "" },
			NewValueFn: func(c *gin.Context, body []byte) string { return "" },
			SummaryFn: func(c *gin.Context) string {
				return "DELETE client credential " + c.Param("credential_id") + " for user " + c.Param("internal_uuid")
			},
		},

		// /admin/groups/*
		{
			Method:      "POST",
			PathTpl:     "/admin/groups",
			FieldPathFn: func(c *gin.Context) string { return "groups.create" },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn:   func(c *gin.Context) string { return "CREATE group" },
		},
		{
			Method:      "PATCH",
			PathTpl:     "/admin/groups/{internal_uuid}",
			FieldPathFn: func(c *gin.Context) string { return "groups." + c.Param("internal_uuid") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn:   func(c *gin.Context) string { return "PATCH group " + c.Param("internal_uuid") },
		},
		{
			Method:      "DELETE",
			PathTpl:     "/admin/groups/{internal_uuid}",
			FieldPathFn: func(c *gin.Context) string { return "groups." + c.Param("internal_uuid") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return "" },
			SummaryFn:   func(c *gin.Context) string { return "DELETE group " + c.Param("internal_uuid") },
		},
		{
			Method:  "POST",
			PathTpl: "/admin/groups/{internal_uuid}/members",
			FieldPathFn: func(c *gin.Context) string {
				return "groups." + c.Param("internal_uuid") + ".members.add"
			},
			OldValueFn: func(c *gin.Context) string { return "" },
			NewValueFn: func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn: func(c *gin.Context) string {
				return "ADD member to group " + c.Param("internal_uuid")
			},
		},
		{
			Method:  "DELETE",
			PathTpl: "/admin/groups/{internal_uuid}/members/{member_uuid}",
			FieldPathFn: func(c *gin.Context) string {
				return "groups." + c.Param("internal_uuid") + ".members." + c.Param("member_uuid")
			},
			OldValueFn: func(c *gin.Context) string { return "" },
			NewValueFn: func(c *gin.Context, body []byte) string { return "" },
			SummaryFn: func(c *gin.Context) string {
				return "REMOVE member " + c.Param("member_uuid") + " from group " + c.Param("internal_uuid")
			},
		},

		// /admin/quotas/*
		{
			Method:      "PUT",
			PathTpl:     "/admin/quotas/users/{user_id}",
			FieldPathFn: func(c *gin.Context) string { return "quotas.users." + c.Param("user_id") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn:   func(c *gin.Context) string { return "PUT user quota " + c.Param("user_id") },
		},
		{
			Method:      "DELETE",
			PathTpl:     "/admin/quotas/users/{user_id}",
			FieldPathFn: func(c *gin.Context) string { return "quotas.users." + c.Param("user_id") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return "" },
			SummaryFn:   func(c *gin.Context) string { return "DELETE user quota override " + c.Param("user_id") },
		},
		{
			Method:      "PUT",
			PathTpl:     "/admin/quotas/webhooks/{user_id}",
			FieldPathFn: func(c *gin.Context) string { return "quotas.webhooks." + c.Param("user_id") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn:   func(c *gin.Context) string { return "PUT webhook quota " + c.Param("user_id") },
		},
		{
			Method:      "DELETE",
			PathTpl:     "/admin/quotas/webhooks/{user_id}",
			FieldPathFn: func(c *gin.Context) string { return "quotas.webhooks." + c.Param("user_id") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return "" },
			SummaryFn:   func(c *gin.Context) string { return "DELETE webhook quota override " + c.Param("user_id") },
		},
		{
			Method:      "PUT",
			PathTpl:     "/admin/quotas/addons/{user_id}",
			FieldPathFn: func(c *gin.Context) string { return "quotas.addons." + c.Param("user_id") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return string(body) },
			SummaryFn:   func(c *gin.Context) string { return "PUT addon quota " + c.Param("user_id") },
		},
		{
			Method:      "DELETE",
			PathTpl:     "/admin/quotas/addons/{user_id}",
			FieldPathFn: func(c *gin.Context) string { return "quotas.addons." + c.Param("user_id") },
			OldValueFn:  func(c *gin.Context) string { return "" },
			NewValueFn:  func(c *gin.Context, body []byte) string { return "" },
			SummaryFn:   func(c *gin.Context) string { return "DELETE addon quota override " + c.Param("user_id") },
		},
	}
}

// extractJSONField pulls a single top-level field out of a JSON object as a
// string. Used to extract "value" from PUT /admin/settings/{key} bodies.
// Returns "" on any parse error or missing field — the audit row degrades
// gracefully rather than failing the admin write.
func extractJSONField(body []byte, field string) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	// Unmarshal the raw value as a string if possible; otherwise stringify.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

// readBodyForCapture buffers the body and returns a fresh reader for the handler.
// Used by AdminAuditMiddleware to capture the request body before passing it on.
func readBodyForCapture(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
	return body, nil
}
