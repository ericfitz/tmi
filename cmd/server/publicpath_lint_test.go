package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/ericfitz/tmi/api"
	"github.com/getkin/kin-openapi/openapi3"
)

// TestPublicPathLint compares the middleware's public-path decision (isPublicPath,
// the exact function the server uses) against what the OpenAPI spec says about
// authentication for every operation, and fails on any divergence. It exists
// because the two sources of truth are maintained by hand, in different files and
// languages, and drift is silent in both directions (#650, #662):
//
//   - middleware public / spec private -> an endpoint is unauthenticated when the
//     contract says it is not
//   - middleware private / spec public -> the endpoint is permanently unreachable
//
// A spec operation is "public" if it declares `security: []` OR
// `x-public-endpoint: true`; the two are treated as equivalent assertions. An
// operation that authenticates in its handler (dual HMAC-or-JWT) opts out of the
// comparison with `x-auth-in-handler: true`, which is the deliberate
// representation of a middleware-public / spec-private endpoint.
func TestPublicPathLint(t *testing.T) {
	swagger, err := api.GetSpec()
	if err != nil {
		t.Fatalf("load OpenAPI spec: %v", err)
	}

	type mismatch struct {
		method, path string
		specPublic   bool
		mwPublic     bool
	}
	var mismatches []mismatch

	forEachOperation(swagger, func(method, path string, op *openapi3.Operation) {
		if extBool(op, "x-auth-in-handler") {
			// Auth is enforced in the handler; the middleware deliberately passes
			// the request through as public. Not drift.
			return
		}
		specPublic := specSaysPublic(op)
		mwPublic := isPublicPath(path)
		if specPublic != mwPublic {
			mismatches = append(mismatches, mismatch{method, path, specPublic, mwPublic})
		}
	})

	if len(mismatches) > 0 {
		sort.Slice(mismatches, func(i, j int) bool {
			if mismatches[i].path == mismatches[j].path {
				return mismatches[i].method < mismatches[j].method
			}
			return mismatches[i].path < mismatches[j].path
		})
		for _, m := range mismatches {
			dir := "middleware PUBLIC but spec PRIVATE (unauthenticated hole)"
			if m.specPublic {
				dir = "spec PUBLIC but middleware PRIVATE (endpoint unreachable, cf #650)"
			}
			t.Errorf("%s %s: %s", m.method, m.path, dir)
		}
		t.Errorf("%d public-path divergence(s) between cmd/server/main.go and the OpenAPI spec; "+
			"reconcile publicPaths/publicPathPrefixes/isPublicSAMLPath with security:[]/x-public-endpoint, "+
			"or mark deliberate handler-auth endpoints with x-auth-in-handler:true", len(mismatches))
	}
}

// TestPublicPathPrefixNoOverreach verifies that no public prefix matches a spec
// operation the spec declares private -- a prefix that is broader than intended
// silently exempts endpoints from auth. (x-auth-in-handler endpoints are exempt,
// being deliberately handler-authenticated.)
func TestPublicPathPrefixNoOverreach(t *testing.T) {
	swagger, err := api.GetSpec()
	if err != nil {
		t.Fatalf("load OpenAPI spec: %v", err)
	}

	forEachOperation(swagger, func(method, path string, op *openapi3.Operation) {
		if specSaysPublic(op) || extBool(op, "x-auth-in-handler") {
			return
		}
		for _, prefix := range publicPathPrefixes {
			if strings.HasPrefix(path, prefix) {
				t.Errorf("prefix %q in publicPathPrefixes matches spec-private operation %s %s; "+
					"tighten the prefix or mark the operation x-auth-in-handler:true", prefix, method, path)
			}
		}
	})
}

// forEachOperation invokes fn for every (method, path, operation) in the spec.
func forEachOperation(swagger *openapi3.T, fn func(method, path string, op *openapi3.Operation)) {
	for path, item := range swagger.Paths.Map() {
		for method, op := range map[string]*openapi3.Operation{
			http.MethodGet:    item.Get,
			http.MethodPost:   item.Post,
			http.MethodPut:    item.Put,
			http.MethodPatch:  item.Patch,
			http.MethodDelete: item.Delete,
		} {
			if op != nil {
				fn(method, path, op)
			}
		}
	}
}

// specSaysPublic reports whether an operation asserts it is unauthenticated.
// `security: []` (an explicit, non-nil, empty requirement) and
// `x-public-endpoint: true` are equivalent assertions of "public". A nil/absent
// `security` means the operation inherits the global requirement and is NOT
// public unless x-public-endpoint says so.
func specSaysPublic(op *openapi3.Operation) bool {
	if op.Security != nil && len(*op.Security) == 0 {
		return true
	}
	return extBool(op, "x-public-endpoint")
}

// extBool decodes a boolean vendor extension from an operation, tolerating the
// several shapes kin-openapi may hand back (json.RawMessage, bool, string).
func extBool(op *openapi3.Operation, key string) bool {
	raw, ok := op.Extensions[key]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case json.RawMessage:
		var b bool
		return json.Unmarshal(v, &b) == nil && b
	case []byte:
		var b bool
		return json.Unmarshal(v, &b) == nil && b
	case string:
		return v == "true"
	default:
		return false
	}
}
