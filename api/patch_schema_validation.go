package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/getkin/kin-openapi/openapi3"
)

// patchInputSchemas maps each patchable entity type to the request schema a
// PUT or POST of that entity is validated against; later schemas cover fields
// that are patchable but not part of the input schema.
var patchInputSchemas = map[reflect.Type][]string{
	reflect.TypeFor[ThreatModel]():           {"ThreatModelInput", "ThreatModelBase"},
	reflect.TypeFor[Threat]():                {"ThreatInput"},
	reflect.TypeFor[Asset]():                 {"AssetInput"},
	reflect.TypeFor[Document]():              {"DocumentInput"},
	reflect.TypeFor[Repository]():            {"RepositoryInput"},
	reflect.TypeFor[DfdDiagram]():            {"DfdDiagramInput"},
	reflect.TypeFor[Team]():                  {"TeamInput"},
	reflect.TypeFor[Project]():               {"ProjectInput"},
	reflect.TypeFor[Note]():                  {"NoteInput"},
	reflect.TypeFor[TeamNote]():              {"TeamNoteInput"},
	reflect.TypeFor[ProjectNote]():           {"ProjectNoteInput"},
	reflect.TypeFor[WebhookSubscription]():   {"WebhookSubscriptionInput"},
	reflect.TypeFor[DBWebhookSubscription](): {"WebhookSubscriptionInput"},
	reflect.TypeFor[Survey]():                {"SurveyInput"},
	reflect.TypeFor[SurveyResponse]():        {"SurveyResponseInput"},
	reflect.TypeFor[Addon]():                 {"CreateAddonRequest"},
}

var (
	patchSpecOnce sync.Once
	patchSpec     *openapi3.T
)

// patchSchemaProperty finds a property in a schema, following allOf members.
func patchSchemaProperty(s *openapi3.Schema, name string, depth int) *openapi3.SchemaRef {
	if s == nil || depth > 8 {
		return nil
	}
	if p, ok := s.Properties[name]; ok {
		return p
	}
	for _, sub := range s.AllOf {
		if p := patchSchemaProperty(sub.Value, name, depth+1); p != nil {
			return p
		}
	}
	return nil
}

// patchSchemaNames looks up a type's registered schemas. A registered nil list
// means "nothing to validate" (test fixtures only); an absent entry is a
// programming error that validatePatchedFields rejects.
func patchSchemaNames(t reflect.Type) ([]string, bool) {
	names, ok := patchInputSchemas[t]
	return names, ok
}

// unescapePointerToken decodes one RFC 6901 reference token.
func unescapePointerToken(tok string) string {
	return strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
}

// patchValueAt walks the patched document along the pointer tokens. A "-"
// token addresses the last array element (the one an add just appended).
func patchValueAt(doc any, tokens []string) (any, bool) {
	cur := doc
	for _, raw := range tokens {
		tok := unescapePointerToken(raw)
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[tok]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			idx := len(node) - 1
			if tok != "-" {
				n, err := strconv.Atoi(tok)
				if err != nil {
					return nil, false
				}
				idx = n
				if idx < 0 {
					idx += len(node) // json-patch accepts negative indexes from the end
				}
			}
			if idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// patchSchemaAt descends from a property schema along the remaining pointer
// tokens (array items, object properties, additionalProperties). It returns the
// deepest schema it could address and how many tokens it consumed; when the
// walk leaves the described schema (oneOf/anyOf elements, undeclared members),
// the caller validates the nearest addressable ancestor instead.
func patchSchemaAt(s *openapi3.Schema, tokens []string) (*openapi3.Schema, int) {
	for i, raw := range tokens {
		var next *openapi3.SchemaRef
		if s.Items != nil {
			next = s.Items
		} else if p := patchSchemaProperty(s, unescapePointerToken(raw), 0); p != nil {
			next = p
		} else if s.AdditionalProperties.Schema != nil {
			next = s.AdditionalProperties.Schema
		}
		if next == nil || next.Value == nil {
			return s, i
		}
		s = next.Value
	}
	return s, len(tokens)
}

// pruneToSchema drops object members the schema does not describe, recursively
// through arrays and described objects. Entities carry response-only members the
// server populates (a team member's resolved "user"); clients round-trip them,
// and the entity decode ignores unknown members, so they are not a write. Free
// form objects (no declared properties) and composed schemas are left intact.
func pruneToSchema(v any, s *openapi3.Schema, depth int) any {
	if s == nil || depth > 8 {
		return v
	}
	switch node := v.(type) {
	case []any:
		if s.Items == nil {
			return v
		}
		for i := range node {
			node[i] = pruneToSchema(node[i], s.Items.Value, depth+1)
		}
	case map[string]any:
		if len(s.Properties) == 0 && len(s.AllOf) == 0 {
			return v
		}
		for k, val := range node {
			p := patchSchemaProperty(s, k, 0)
			if p == nil {
				delete(node, k)
				continue
			}
			node[k] = pruneToSchema(val, p.Value, depth+1)
		}
	}
	return v
}

// validatePatchedFields enforces the request schema's constraints (maxLength,
// enum, pattern, ...) on every value a patch writes. JSON Patch bypasses the
// OpenAPI request-body validator, so without this a patch could store a value a
// PUT would reject with 400 (for example a status longer than its column, a 500
// from the database).
//
// Only the written value is validated: the value at each add/replace/copy/move
// destination pointer, checked against the sub-schema at that pointer, so a
// nested write never re-validates unrelated stored siblings. Members the schema
// does not describe (server-populated response-only members such as "user" in
// team members) are pruned first rather than rejected. A type with no entry in
// patchInputSchemas is an error, never a silent pass. Errors are 400
// RequestErrors that name the path but never echo the value.
func validatePatchedFields[T any](originalJSON []byte, operations []PatchOperation) error {
	return validatePatchedFieldsFor(reflect.TypeFor[T](), originalJSON, operations)
}

func validatePatchedFieldsFor(typ reflect.Type, originalJSON []byte, operations []PatchOperation) error {
	schemaNames, ok := patchSchemaNames(typ)
	if !ok {
		return fmt.Errorf("no patch request schema registered for %s; add it to patchInputSchemas", typ)
	}
	if len(schemaNames) == 0 {
		return nil
	}
	patchSpecOnce.Do(func() {
		if swagger, err := GetSwagger(); err == nil {
			patchSpec = swagger
		}
	})
	if patchSpec == nil {
		return fmt.Errorf("embedded OpenAPI spec unavailable for patch validation")
	}
	var schemas []*openapi3.Schema
	for _, name := range schemaNames {
		ref, found := patchSpec.Components.Schemas[name]
		if !found || ref.Value == nil {
			return fmt.Errorf("request schema %q not found for patch validation", name)
		}
		schemas = append(schemas, ref.Value)
	}

	// Replay the operations one at a time on a working copy and validate each
	// write right after it is applied: array positions shift as operations run,
	// so a pointer looked up in the final document can address a different
	// element than the one written.
	work := originalJSON
	for _, op := range operations {
		patchBytes, err := convertOperationsToJSONPatch([]PatchOperation{op})
		if err != nil {
			return InvalidInputError("Patch operation is invalid")
		}
		patch, err := jsonpatch.DecodePatch(patchBytes)
		if err != nil {
			return InvalidInputError("Patch operation is invalid")
		}
		if work, err = patch.Apply(work); err != nil {
			return InvalidInputError("Patch operation could not be applied")
		}
		// Only values the patch writes: add/replace targets and the destination
		// of copy/move. Sources (from), test and remove are never validated.
		switch op.Op {
		case string(Add), string(Replace), string(Copy), string(Move):
		default:
			continue
		}
		if len(op.Path) < 2 || op.Path[0] != '/' {
			continue
		}
		tokens := strings.Split(op.Path[1:], "/")
		name := unescapePointerToken(tokens[0])
		var prop *openapi3.SchemaRef
		for _, sch := range schemas {
			if prop = patchSchemaProperty(sch, name, 0); prop != nil {
				break
			}
		}
		if prop == nil || prop.Value == nil {
			continue // not a request field
		}
		// work stays raw, exactly as the real apply sees it; only this copy gets
		// the normalization ApplyPatchOperations runs on the final document.
		normalized := fixOwnerField(fixImageField(fixMetadataField(work, originalJSON), originalJSON), originalJSON)
		var doc map[string]any
		if err := json.Unmarshal(normalized, &doc); err != nil {
			return InvalidInputError("Patched entity must be a JSON object")
		}
		target, consumed := patchSchemaAt(prop.Value, tokens[1:])
		// Validate the value at the deepest addressable pointer.
		value, present := patchValueAt(doc, tokens[:1+consumed])
		if !present {
			return InvalidInputError(fmt.Sprintf("Patched field %q could not be resolved for validation", op.Path))
		}
		if value == nil && len(tokens) == 1 {
			continue // top-level field cleared to null
		}
		value = pruneToSchema(value, target, 0)
		if err := target.VisitJSON(value); err != nil {
			return InvalidInputError(fmt.Sprintf("Patched field %q is invalid: %s", op.Path, patchSchemaReason(err)))
		}
	}
	return nil
}

// patchSchemaReason extracts the constraint message without the offending value.
func patchSchemaReason(err error) string {
	var se *openapi3.SchemaError
	if errors.As(err, &se) && se.Reason != "" {
		return se.Reason
	}
	return "does not satisfy the request schema"
}
