package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rtUserID  = "11111111-1111-1111-1111-111111111111"
	rtUserID2 = "22222222-2222-2222-2222-222222222222"
	rtUser    = `{"principal_type":"user","provider":"tmi","provider_id":"alice","display_name":"Alice","email":"alice@tmi.local"}`
)

// storeShaped builds an entity the way the stores return it: server-populated
// members (the resolved "user" object) included.
func storeShaped[T any](t *testing.T, raw string) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal([]byte(raw), &v))
	return v
}

func requireValidationRejects(t *testing.T, err error) {
	t.Helper()
	var reqErr *RequestError
	require.ErrorAs(t, err, &reqErr)
	assert.Equal(t, http.StatusBadRequest, reqErr.Status)
	assert.NotContains(t, reqErr.Message, "aaaa", "must not echo the value")
}

// TestPatchValidation_ServerPopulatedMembersRoundTrip: clients PATCH the shape
// they GET, including response-only members such as the resolved "user".
func TestPatchValidation_ServerPopulatedMembersRoundTrip(t *testing.T) {
	member := func(id, role string) string {
		return `{"user_id":"` + id + `","role":"` + role + `","user":` + rtUser + `}`
	}
	teamRaw := `{"name":"T","members":[` + member(rtUserID, "engineer") + `,` + member(rtUserID2, "engineer") + `],"responsible_parties":[` + member(rtUserID, "engineer") + `]}`
	projectRaw := `{"name":"P","responsible_parties":[` + member(rtUserID, "engineer") + `,` + member(rtUserID2, "engineer") + `]}`

	type tc struct {
		name string
		ops  []PatchOperation
	}
	cases := func(field string) []tc {
		return []tc{
			{"nested replace on stored array", []PatchOperation{pop("replace", "/"+field+"/0/role", "engineering_lead")}},
			{"nested add of custom_role", []PatchOperation{pop("add", "/"+field+"/0/custom_role", "Scribe")}},
			{"whole-field replace with GET shape", []PatchOperation{pop("replace", "/"+field, []any{
				map[string]any{"user_id": rtUserID, "role": "engineer", "user": json.RawMessage(rtUser)},
			})}},
			{"append element with GET shape", []PatchOperation{pop("add", "/"+field+"/-",
				map[string]any{"user_id": rtUserID2, "role": "engineer", "user": json.RawMessage(rtUser)})}},
		}
	}
	for _, field := range []string{"members", "responsible_parties"} {
		for _, c := range cases(field) {
			t.Run("Team/"+field+"/"+c.name, func(t *testing.T) {
				_, err := ApplyPatchOperations(storeShaped[Team](t, teamRaw), c.ops)
				require.NoError(t, err)
			})
		}
	}
	for _, c := range cases("responsible_parties") {
		t.Run("Project/"+c.name, func(t *testing.T) {
			_, err := ApplyPatchOperations(storeShaped[Project](t, projectRaw), c.ops)
			require.NoError(t, err)
		})
	}
}

// TestPatchValidation_StillRejectsWrittenValues: ignoring response-only members
// must not weaken validation of what the client writes.
func TestPatchValidation_StillRejectsWrittenValues(t *testing.T) {
	long := strings.Repeat("a", 5000)
	teamRaw := `{"name":"T","responsible_parties":[{"user_id":"` + rtUserID + `","role":"engineer","user":` + rtUser + `}]}`
	tests := []struct {
		name string
		ops  []PatchOperation
	}{
		{"oversized custom_role nested", []PatchOperation{pop("replace", "/responsible_parties/0/custom_role", long)}},
		{"bad role enum nested", []PatchOperation{pop("replace", "/responsible_parties/0/role", "not_a_role")}},
		{"oversized custom_role in whole-field replace", []PatchOperation{pop("replace", "/responsible_parties", []any{
			map[string]any{"user_id": rtUserID, "custom_role": long, "user": json.RawMessage(rtUser)},
		})}},
		{"element missing required user_id", []PatchOperation{pop("add", "/responsible_parties/-", map[string]any{"role": "engineer"})}},
		{"oversized name", []PatchOperation{pop("replace", "/name", long)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ApplyPatchOperations(storeShaped[Team](t, teamRaw), tt.ops)
			requireValidationRejects(t, err)
		})
	}
}

// TestPatchValidation_UnrelatedStoredElementDoesNotBlockNestedWrite: one stored
// element failing the request schema must not block a patch to another element.
func TestPatchValidation_UnrelatedStoredElementDoesNotBlockNestedWrite(t *testing.T) {
	raw := `{"name":"T","responsible_parties":[` +
		`{"user_id":"` + rtUserID + `","custom_role":"` + strings.Repeat("x", 5000) + `"},` +
		`{"user_id":"` + rtUserID2 + `","role":"engineer"}]}`
	_, err := ApplyPatchOperations(storeShaped[Team](t, raw), []PatchOperation{
		pop("replace", "/responsible_parties/1/role", "engineering_lead"),
	})
	require.NoError(t, err)
}

// TestPatchInputSchemas_RejectByDefault: a patched type without a registered
// schema is an error, never a silent skip.
func TestPatchInputSchemas_RejectByDefault(t *testing.T) {
	type unregistered struct {
		Name string `json:"name"`
	}
	_, err := ApplyPatchOperations(unregistered{Name: "a"}, []PatchOperation{pop("replace", "/name", "b")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no patch request schema registered")
}

// TestPatchInputSchemas_RegisteredSchemasExist guards typos in the registry and
// checks that every type the handlers patch is registered.
func TestPatchInputSchemas_RegisteredSchemasExist(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)
	for typ, names := range patchInputSchemas {
		for _, n := range names {
			ref, ok := swagger.Components.Schemas[n]
			assert.True(t, ok && ref.Value != nil, "%s: schema %q missing from spec", typ, n)
		}
	}
	for _, typ := range []reflect.Type{
		reflect.TypeFor[ThreatModel](), reflect.TypeFor[Threat](), reflect.TypeFor[Asset](),
		reflect.TypeFor[Document](), reflect.TypeFor[Repository](), reflect.TypeFor[DfdDiagram](),
		reflect.TypeFor[Team](), reflect.TypeFor[Project](), reflect.TypeFor[Note](),
		reflect.TypeFor[TeamNote](), reflect.TypeFor[ProjectNote](),
		reflect.TypeFor[WebhookSubscription](), reflect.TypeFor[DBWebhookSubscription](),
		reflect.TypeFor[Survey](), reflect.TypeFor[SurveyResponse](), reflect.TypeFor[Addon](),
	} {
		names, ok := patchInputSchemas[typ]
		assert.True(t, ok && len(names) > 0, "%s has no registered patch schema", typ)
	}
}

// boundedStringField returns the alphabetically first string property with a
// maxLength, and whether it is plain (no enum, pattern or format) so a value at
// the limit is expected to validate.
func boundedStringField(roots []*openapi3.Schema) (string, uint64, bool) {
	names := map[string]bool{}
	for _, r := range roots {
		for k := range r.Properties {
			names[k] = true
		}
		for _, sub := range r.AllOf {
			for k := range sub.Value.Properties {
				names[k] = true
			}
		}
	}
	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, r := range roots {
			p := patchSchemaProperty(r, k, 0)
			if p == nil {
				continue
			}
			v := p.Value
			if v.Type != nil && v.Type.Is("string") && v.MaxLength != nil && v.Enum == nil {
				return k, *v.MaxLength, v.Pattern == "" && v.Format == ""
			}
			break
		}
	}
	return "", 0, false
}

// TestPatchValidation_OversizedRejectedPerRegisteredType: for each registered
// type, a string property one over its maxLength is rejected (and one at the
// limit passes when it has no pattern or format).
func TestPatchValidation_OversizedRejectedPerRegisteredType(t *testing.T) {
	swagger, err := GetSwagger()
	require.NoError(t, err)
	checkedTypes := 0
	for typ, names := range patchInputSchemas {
		if len(names) == 0 {
			continue
		}
		var roots []*openapi3.Schema
		for _, n := range names {
			roots = append(roots, swagger.Components.Schemas[n].Value)
		}
		field, limit, plain := boundedStringField(roots)
		if field == "" {
			continue
		}
		checkedTypes++
		t.Run(typ.String(), func(t *testing.T) {
			ops := []PatchOperation{pop("add", "/"+field, "x")}
			doc := func(n int) []byte {
				b, _ := json.Marshal(map[string]any{field: strings.Repeat("a", n)})
				return b
			}
			requireValidationRejects(t, validatePatchedFieldsFor(typ, doc(int(limit)+1), ops))
			if plain {
				require.NoError(t, validatePatchedFieldsFor(typ, doc(int(limit)), ops))
			}
		})
	}
	assert.GreaterOrEqual(t, checkedTypes, 8, "per-type coverage unexpectedly small")
}
