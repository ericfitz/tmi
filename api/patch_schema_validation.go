package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

// patchInputSchemas maps each patchable entity type to the request schema a
// PUT or POST of that entity is validated against; later schemas cover fields
// that are patchable but not part of the input schema.
var patchInputSchemas = map[reflect.Type][]string{
	reflect.TypeFor[ThreatModel]():         {"ThreatModelInput", "ThreatModelBase"},
	reflect.TypeFor[Threat]():              {"ThreatInput"},
	reflect.TypeFor[Asset]():               {"AssetInput"},
	reflect.TypeFor[Document]():            {"DocumentInput"},
	reflect.TypeFor[Repository]():          {"RepositoryInput"},
	reflect.TypeFor[DfdDiagram]():          {"DfdDiagramInput"},
	reflect.TypeFor[Team]():                {"TeamInput"},
	reflect.TypeFor[Project]():             {"ProjectInput"},
	reflect.TypeFor[Note]():                {"NoteInput"},
	reflect.TypeFor[TeamNote]():            {"TeamNoteInput"},
	reflect.TypeFor[ProjectNote]():         {"ProjectNoteInput"},
	reflect.TypeFor[WebhookSubscription](): {"WebhookSubscriptionInput"},
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

// validatePatchedFields enforces the request schema's constraints (maxLength,
// enum, pattern, ...) on every top-level field a patch wrote. JSON Patch
// bypasses the OpenAPI request-body validator, so without this a patch could
// store a value a PUT would reject with 400 (for example a status longer than
// its column, a 500 from the database). Only fields the patch writes are
// checked, so server-managed and legacy stored values are never re-validated.
// Entity types without a registered input schema are skipped. Errors are 400
// RequestErrors that name the field but never echo its value.
func validatePatchedFields[T any](patchedJSON []byte, operations []PatchOperation) error {
	schemaNames, ok := patchInputSchemas[reflect.TypeFor[T]()]
	if !ok {
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

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(patchedJSON, &doc); err != nil {
		return InvalidInputError("Patched entity must be a JSON object")
	}

	checked := map[string]bool{}
	for _, op := range operations {
		// Only fields the patch writes: add/replace targets and the destination
		// of copy/move. Sources (from), test and remove are never validated.
		switch op.Op {
		case string(Add), string(Replace), string(Copy), string(Move):
		default:
			continue
		}
		for _, p := range []string{op.Path} {
			if p == "" {
				continue
			}
			field := topLevelPatchPath(p)
			if len(field) < 2 || checked[field] {
				continue
			}
			checked[field] = true
			name := field[1:]
			raw, present := doc[name]
			if !present {
				continue // removed
			}
			var prop *openapi3.SchemaRef
			for _, sch := range schemas {
				if prop = patchSchemaProperty(sch, name, 0); prop != nil {
					break
				}
			}
			if prop == nil || prop.Value == nil {
				continue // not a request field
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil || value == nil {
				continue
			}
			if err := prop.Value.VisitJSON(value); err != nil {
				return InvalidInputError(fmt.Sprintf("Patched field %q is invalid: %s", name, patchSchemaReason(err)))
			}
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
