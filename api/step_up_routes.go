// Package api provides storage and HTTP handlers for the TMI service.
package api

import (
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// ginPathToOpenAPI converts a Gin route template to OpenAPI path parameter form.
// Gin uses colon-prefixed segments (e.g. /admin/settings/:key) while the OpenAPI
// spec uses curly-brace form (e.g. /admin/settings/{key}). The route table is
// keyed on the OpenAPI form so lookups from the Gin request context require this
// conversion.
//
// Examples:
//
//	/admin/settings/:key               → /admin/settings/{key}
//	/admin/groups/:internal_uuid/members/:member_uuid → /admin/groups/{internal_uuid}/members/{member_uuid}
func ginPathToOpenAPI(p string) string {
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		if strings.HasPrefix(seg, ":") {
			parts[i] = "{" + seg[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// StepUpRouteTable answers "does (method, route-template) require a fresh
// auth_time?" Built once at server boot from the OpenAPI spec; consulted
// per-request by StepUpMiddleware (#355).
//
// Default policy: any /admin/* operation with a write method (POST/PUT/PATCH/
// DELETE) requires step-up. Opt-out via x-tmi-authz-step-up: optional on the
// operation.
type StepUpRouteTable struct {
	required map[stepUpRouteKey]bool
}

type stepUpRouteKey struct {
	method string
	path   string
}

// Required reports whether the given (method, path-template) requires a
// fresh auth_time per the step-up policy.
func (t StepUpRouteTable) Required(method, path string) bool {
	if t.required == nil {
		return false
	}
	return t.required[stepUpRouteKey{method: strings.ToUpper(method), path: path}]
}

// BuildStepUpRouteTable walks the OpenAPI spec and constructs the resolution
// table. Safe to call with a nil spec (returns an empty table).
func BuildStepUpRouteTable(spec *openapi3.T) StepUpRouteTable {
	table := StepUpRouteTable{required: map[stepUpRouteKey]bool{}}
	if spec == nil || spec.Paths == nil {
		return table
	}
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/admin/") {
			continue
		}
		ops := map[string]*openapi3.Operation{
			"POST":   item.Post,
			"PUT":    item.Put,
			"PATCH":  item.Patch,
			"DELETE": item.Delete,
		}
		for method, op := range ops {
			if op == nil {
				continue
			}
			required := true
			if v, ok := op.Extensions["x-tmi-authz-step-up"]; ok {
				if s, ok := v.(string); ok && s == "optional" {
					required = false
				}
			}
			table.required[stepUpRouteKey{method: method, path: path}] = required
		}
	}
	return table
}
