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
// SEM@e005ee4f6bf927c842fe7fae5363929a8ad0d794: convert a Gin colon-param route template to OpenAPI curly-brace form (pure)
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
// operation. Opt-in for any path/method: set x-tmi-authz-step-up: "required"
// on the operation.
// SEM@3c2ef72cc84c7ca336332105301ca560fd259567: lookup table mapping route method+path to step-up auth requirement (pure)
type StepUpRouteTable struct {
	required map[stepUpRouteKey]bool
}

// SEM@3c2ef72cc84c7ca336332105301ca560fd259567: composite key of HTTP method and path for step-up route lookup (pure)
type stepUpRouteKey struct {
	method string
	path   string
}

// Required reports whether the given (method, path-template) requires a
// fresh auth_time per the step-up policy.
// SEM@3c2ef72cc84c7ca336332105301ca560fd259567: report whether a route requires fresh step-up authentication (pure)
func (t StepUpRouteTable) Required(method, path string) bool {
	if t.required == nil {
		return false
	}
	return t.required[stepUpRouteKey{method: strings.ToUpper(method), path: path}]
}

// BuildStepUpRouteTable walks the OpenAPI spec and constructs the resolution
// table. Safe to call with a nil spec (returns an empty table).
//
// Two mechanisms register a route:
//  1. Opt-in (any path/method): operation carries x-tmi-authz-step-up: "required".
//  2. Default (admin write methods): any /admin/* POST/PUT/PATCH/DELETE is
//     required unless opted out via x-tmi-authz-step-up: "optional".
// SEM@512260e3fe7e08b889b07b5644777571587d76fb: build step-up route table from OpenAPI spec using opt-in and admin-write defaults (pure)
func BuildStepUpRouteTable(spec *openapi3.T) StepUpRouteTable {
	table := StepUpRouteTable{required: map[stepUpRouteKey]bool{}}
	if spec == nil || spec.Paths == nil {
		return table
	}
	for path, item := range spec.Paths.Map() {
		// Pass 1: opt-in via x-tmi-authz-step-up: "required" on ANY method/path.
		allOps := map[string]*openapi3.Operation{
			"GET":    item.Get,
			"POST":   item.Post,
			"PUT":    item.Put,
			"PATCH":  item.Patch,
			"DELETE": item.Delete,
		}
		for method, op := range allOps {
			if op == nil {
				continue
			}
			if v, ok := op.Extensions["x-tmi-authz-step-up"]; ok {
				if s, ok := v.(string); ok && s == "required" {
					table.required[stepUpRouteKey{method: method, path: path}] = true
				}
			}
		}

		// Pass 2: /admin/* write-method default — required unless opted out.
		if !strings.HasPrefix(path, "/admin/") {
			continue
		}
		writeOps := map[string]*openapi3.Operation{
			"POST":   item.Post,
			"PUT":    item.Put,
			"PATCH":  item.Patch,
			"DELETE": item.Delete,
		}
		for method, op := range writeOps {
			if op == nil {
				continue
			}
			key := stepUpRouteKey{method: method, path: path}
			// Don't override an already-set required=true from pass 1.
			if _, alreadySet := table.required[key]; alreadySet {
				continue
			}
			required := true
			if v, ok := op.Extensions["x-tmi-authz-step-up"]; ok {
				if s, ok := v.(string); ok && s == "optional" {
					required = false
				}
			}
			table.required[key] = required
		}
	}
	return table
}
