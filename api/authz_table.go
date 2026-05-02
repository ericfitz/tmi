package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

// Ownership is the resource-level access tier required by an operation.
type Ownership string

const (
	OwnershipNone   Ownership = "none"
	OwnershipReader Ownership = "reader"
	OwnershipWriter Ownership = "writer"
	OwnershipOwner  Ownership = "owner"
)

// AuthzRoleName is a named role gate. Defined values for slice 1: admin.
// Future slices register security_reviewer, automation, confidential_reviewer.
type AuthzRoleName string

const (
	RoleAuthzAdmin                AuthzRoleName = "admin"
	RoleAuthzSecurityReviewer     AuthzRoleName = "security_reviewer"
	RoleAuthzAutomation           AuthzRoleName = "automation"
	RoleAuthzConfidentialReviewer AuthzRoleName = "confidential_reviewer"
)

var validOwnerships = map[Ownership]struct{}{
	OwnershipNone:   {},
	OwnershipReader: {},
	OwnershipWriter: {},
	OwnershipOwner:  {},
}

var validRoles = map[AuthzRoleName]struct{}{
	RoleAuthzAdmin:                {},
	RoleAuthzSecurityReviewer:     {},
	RoleAuthzAutomation:           {},
	RoleAuthzConfidentialReviewer: {},
}

// AuthzRule is the per-operation declaration sourced from x-tmi-authz.
type AuthzRule struct {
	Ownership Ownership
	Roles     []AuthzRoleName
	Public    bool
	Audit     string // "required" | "optional" | ""
}

// AuthzTable indexes rules by (method, normalized-path-template).
// Lookups against concrete request paths use template matching (e.g.
// /admin/users/abc -> /admin/users/{id}).
type AuthzTable struct {
	// byMethodPath maps method -> path template -> rule.
	// Path templates are stored exactly as written in the OpenAPI spec
	// (with curly-brace parameters preserved).
	byMethodPath map[string]map[string]AuthzRule
}

var (
	globalAuthzTable     *AuthzTable
	globalAuthzTableOnce sync.Once
	globalAuthzTableErr  error
)

// LoadGlobalAuthzTable parses the embedded OpenAPI spec once and caches the
// resulting AuthzTable. Subsequent calls return the cached value. Errors from
// the first call are persisted and re-returned on every subsequent call.
func LoadGlobalAuthzTable() (*AuthzTable, error) {
	globalAuthzTableOnce.Do(func() {
		swagger, err := GetSwagger()
		if err != nil {
			globalAuthzTableErr = fmt.Errorf("load openapi spec: %w", err)
			return
		}
		globalAuthzTable, globalAuthzTableErr = buildAuthzTable(swagger)
	})
	return globalAuthzTable, globalAuthzTableErr
}

// loadAuthzTableFromJSON is exposed for tests; it parses a raw JSON spec
// string instead of relying on the embedded production spec.
func loadAuthzTableFromJSON(data []byte) (*AuthzTable, error) {
	loader := openapi3.NewLoader()
	swagger, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("load openapi from json: %w", err)
	}
	return buildAuthzTable(swagger)
}

func buildAuthzTable(swagger *openapi3.T) (*AuthzTable, error) {
	tbl := &AuthzTable{
		byMethodPath: make(map[string]map[string]AuthzRule),
	}

	for path, item := range swagger.Paths.Map() {
		ops := map[string]*openapi3.Operation{
			http.MethodGet:    item.Get,
			http.MethodPost:   item.Post,
			http.MethodPut:    item.Put,
			http.MethodPatch:  item.Patch,
			http.MethodDelete: item.Delete,
		}
		for method, op := range ops {
			if op == nil {
				continue
			}
			rawAuthz, ok := op.Extensions["x-tmi-authz"]
			if !ok {
				continue
			}
			rule, err := parseAuthzExtension(rawAuthz)
			if err != nil {
				return nil, fmt.Errorf("invalid x-tmi-authz on %s %s: %w", method, path, err)
			}
			if _, ok := tbl.byMethodPath[method]; !ok {
				tbl.byMethodPath[method] = make(map[string]AuthzRule)
			}
			tbl.byMethodPath[method][path] = rule
		}
	}
	return tbl, nil
}

func parseAuthzExtension(raw any) (AuthzRule, error) {
	var rule AuthzRule
	// kin-openapi exposes extensions as raw JSON message or already-decoded.
	// Normalize to JSON bytes and decode into our struct.
	var data []byte
	switch v := raw.(type) {
	case json.RawMessage:
		data = v
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return rule, fmt.Errorf("marshal: %w", err)
		}
	}
	var aux struct {
		Ownership string   `json:"ownership"`
		Roles     []string `json:"roles"`
		Public    bool     `json:"public"`
		Audit     string   `json:"audit"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return rule, fmt.Errorf("unmarshal: %w", err)
	}
	if aux.Ownership == "" {
		return rule, fmt.Errorf("ownership is required")
	}
	rule.Ownership = Ownership(aux.Ownership)
	if _, ok := validOwnerships[rule.Ownership]; !ok {
		return rule, fmt.Errorf("invalid ownership %q", aux.Ownership)
	}
	for _, r := range aux.Roles {
		role := AuthzRoleName(r)
		if _, ok := validRoles[role]; !ok {
			return rule, fmt.Errorf("invalid role %q", r)
		}
		rule.Roles = append(rule.Roles, role)
	}
	rule.Public = aux.Public
	// Note: `audit` field is captured but not validated in slice 1. The plan
	// (#341) defers audit-emission enforcement to slice 8 (#371) which will
	// add VALID_AUDIT-style validation along with runtime enforcement.
	rule.Audit = aux.Audit

	if rule.Public {
		if rule.Ownership != OwnershipNone {
			return rule, fmt.Errorf("public=true requires ownership=none")
		}
		if len(rule.Roles) > 0 {
			return rule, fmt.Errorf("public=true requires roles=[]")
		}
	}
	return rule, nil
}

// Lookup matches a concrete request path against the table's templates and
// returns the rule for (method, matched-template). Matching mirrors the
// strategy in findPathItem (api/openapi_middleware.go): exact match wins,
// otherwise the template with the most literal segments wins.
func (t *AuthzTable) Lookup(method, requestPath string) (AuthzRule, bool) {
	methodRules := t.byMethodPath[strings.ToUpper(method)]
	if methodRules == nil {
		return AuthzRule{}, false
	}

	// Exact match first.
	if rule, ok := methodRules[requestPath]; ok {
		return rule, true
	}

	// Trailing slashes are tolerated (strings.Trim removes them); a request
	// for "/admin/users/" matches the "/admin/users" template. The "/" root
	// path correctly matches a "/" template (both split to a single empty
	// segment). This mirrors findPathItem in api/openapi_middleware.go.
	requestParts := strings.Split(strings.Trim(requestPath, "/"), "/")
	bestRule, found := AuthzRule{}, false
	bestLiteral := -1
	for tmpl, rule := range methodRules {
		tmplParts := strings.Split(strings.Trim(tmpl, "/"), "/")
		if len(tmplParts) != len(requestParts) {
			continue
		}
		match := true
		literal := 0
		for i, p := range tmplParts {
			if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
				continue
			}
			if p != requestParts[i] {
				match = false
				break
			}
			literal++
		}
		if match && literal > bestLiteral {
			bestRule = rule
			bestLiteral = literal
			found = true
		}
	}
	return bestRule, found
}
