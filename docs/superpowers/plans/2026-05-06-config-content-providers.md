# `/config` content_providers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `content_providers` array to `GET /config` so clients render only providers the server can actually serve and gate per-user account-linking UX on the right kind.

**Architecture:** OpenAPI-first. Add `ContentProvider` schema to `ClientConfig`. The handler iterates `ContentSourceRegistry.Names()`, looks up `(kind, default_name, default_icon)` from a static metadata table keyed by source name, and applies operator overrides from `cfg.ContentOAuth.Providers[id]` for delegated providers only. Service and direct sources read from the table only.

**Tech Stack:** Go 1.x, Gin, oapi-codegen v2, PostgreSQL/Oracle (no DB changes here), unit tests in `api/config_handlers_test.go`.

**Spec:** [docs/superpowers/specs/2026-05-06-config-content-providers-design.md](../specs/2026-05-06-config-content-providers-design.md)

**Issue:** [#373](https://github.com/ericfitz/tmi/issues/373)

---

## Task 1: Extend ClientConfig schema in OpenAPI

**Files:**
- Modify: `api-schema/tmi-openapi.json` — `ClientConfig` schema (~line 6584) and add a new `ContentProvider` schema in `components/schemas`.

- [ ] **Step 1: Add `ContentProvider` schema and `content_providers` field**

In `api-schema/tmi-openapi.json`, under `components.schemas`, add a new `ContentProvider` schema. Place it alphabetically (after `ClientConfig` is fine):

```json
"ContentProvider": {
  "type": "object",
  "additionalProperties": false,
  "required": ["id", "name", "kind", "icon"],
  "properties": {
    "id": {
      "type": "string",
      "description": "Source identifier (matches ContentSource.Name())",
      "example": "google_workspace"
    },
    "name": {
      "type": "string",
      "description": "Display label for the provider"
    },
    "kind": {
      "type": "string",
      "enum": ["delegated", "service", "direct"],
      "description": "delegated: per-user OAuth (client must call /me/content_tokens/{id}/authorize); service: operator-credentialed (no per-user link); direct: no auth (e.g., HTTP fetch)"
    },
    "icon": {
      "type": "string",
      "description": "Font Awesome class string (matches OAuth IdP convention). Empty if no default and no override."
    }
  }
}
```

In `ClientConfig.properties`, add a new `content_providers` property after `ui`:

```json
"content_providers": {
  "type": "array",
  "description": "Content providers the server has configured. Order matches server-side registration order.",
  "items": { "$ref": "#/components/schemas/ContentProvider" }
}
```

In the `ClientConfig.example`, add:

```json
"content_providers": [
  { "id": "http", "name": "HTTP", "kind": "direct", "icon": "fa-solid fa-globe" },
  { "id": "google_workspace", "name": "Google Workspace", "kind": "delegated", "icon": "fa-brands fa-google" }
]
```

- [ ] **Step 2: Validate the OpenAPI spec**

Run: `make validate-openapi`
Expected: Validation passes; report at `api-schema/openapi-validation-report.json` shows no new errors.

- [ ] **Step 3: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` is updated to include a `ContentProvider` struct and `ClientConfig.ContentProviders` field. No build errors.

- [ ] **Step 4: Verify build**

Run: `make build-server`
Expected: Build succeeds (the new field is optional in the existing handler until Task 5 wires it in; the generated struct field will simply be empty).

- [ ] **Step 5: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add ContentProvider schema and ClientConfig.content_providers (#373)"
```

---

## Task 2: Add `Name` and `Icon` to ContentOAuthProviderConfig

**Files:**
- Modify: `internal/config/content_oauth.go` (struct definition)
- Test: `internal/config/content_oauth_test.go` (verify yaml decoding)

- [ ] **Step 1: Write the failing test**

Add a new test to `internal/config/content_oauth_test.go`:

```go
func TestContentOAuthProviderConfig_NameAndIcon(t *testing.T) {
	yamlBlob := `
providers:
  google_workspace:
    enabled: true
    name: "GWS Custom"
    icon: "fa-custom"
    client_id: "cid"
    auth_url: "https://example/auth"
    token_url: "https://example/token"
`
	var cfg ContentOAuthConfig
	if err := yaml.Unmarshal([]byte(yamlBlob), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	p := cfg.Providers["google_workspace"]
	if p.Name != "GWS Custom" {
		t.Errorf("Name = %q, want %q", p.Name, "GWS Custom")
	}
	if p.Icon != "fa-custom" {
		t.Errorf("Icon = %q, want %q", p.Icon, "fa-custom")
	}
}
```

If `gopkg.in/yaml.v3` is not already imported in this test file, add the import. Check the existing test file's import block first.

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestContentOAuthProviderConfig_NameAndIcon`
Expected: FAIL — `Name` and `Icon` fields don't exist on the struct (compile error).

- [ ] **Step 3: Add the fields to the struct**

In `internal/config/content_oauth.go`, modify `ContentOAuthProviderConfig`:

```go
type ContentOAuthProviderConfig struct {
	Enabled              bool              `yaml:"enabled"`
	Name                 string            `yaml:"name"`
	Icon                 string            `yaml:"icon"`
	ClientID             string            `yaml:"client_id"`
	ClientSecret         string            `yaml:"client_secret"` //nolint:gosec // G117 - OAuth provider client secret
	AuthURL              string            `yaml:"auth_url"`
	TokenURL             string            `yaml:"token_url"`
	UserinfoURL          string            `yaml:"userinfo_url"`
	RevocationURL        string            `yaml:"revocation_url"`
	RequiredScopes       []string          `yaml:"required_scopes"`
	ExtraAuthorizeParams map[string]string `yaml:"extra_authorize_params"`
}
```

Validation logic in `Validate()` does not need updates (both fields are optional).

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestContentOAuthProviderConfig_NameAndIcon`
Expected: PASS.

- [ ] **Step 5: Run the full config package tests for regressions**

Run: `make test-unit name=^TestContent`
Expected: All existing tests still pass.

- [ ] **Step 6: Commit**

```bash
git add internal/config/content_oauth.go internal/config/content_oauth_test.go
git commit -m "feat(config): add Name and Icon to ContentOAuthProviderConfig (#373)"
```

---

## Task 3: Add content provider metadata table

**Files:**
- Create: `api/content_provider_metadata.go`
- Test: `api/content_provider_metadata_test.go`

- [ ] **Step 1: Write the failing test**

Create `api/content_provider_metadata_test.go`:

```go
package api

import "testing"

func TestLookupContentProviderMeta_KnownIDs(t *testing.T) {
	cases := []struct {
		id   string
		kind string
		name string
		icon string
	}{
		{"http", "direct", "HTTP", "fa-solid fa-globe"},
		{"google_drive", "service", "Google Drive", "fa-brands fa-google-drive"},
		{"google_workspace", "delegated", "Google Workspace", "fa-brands fa-google"},
		{"microsoft", "delegated", "Microsoft 365", "fa-brands fa-microsoft"},
		{"confluence", "delegated", "Atlassian Confluence", "fa-brands fa-confluence"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			m := lookupContentProviderMeta(tc.id)
			if m.Kind != tc.kind {
				t.Errorf("Kind = %q, want %q", m.Kind, tc.kind)
			}
			if m.DefaultName != tc.name {
				t.Errorf("DefaultName = %q, want %q", m.DefaultName, tc.name)
			}
			if m.DefaultIcon != tc.icon {
				t.Errorf("DefaultIcon = %q, want %q", m.DefaultIcon, tc.icon)
			}
		})
	}
}

func TestLookupContentProviderMeta_UnknownID(t *testing.T) {
	m := lookupContentProviderMeta("experimental")
	if m.Kind != "direct" {
		t.Errorf("Kind = %q, want %q", m.Kind, "direct")
	}
	if m.DefaultName != "experimental" {
		t.Errorf("DefaultName = %q, want %q", m.DefaultName, "experimental")
	}
	if m.DefaultIcon != "" {
		t.Errorf("DefaultIcon = %q, want empty", m.DefaultIcon)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestLookupContentProviderMeta`
Expected: FAIL — `lookupContentProviderMeta` is not defined.

- [ ] **Step 3: Create the metadata module**

Create `api/content_provider_metadata.go`:

```go
package api

// contentProviderMeta is the static metadata for a known content source name:
// its kind (delegated/service/direct), and default user-facing display name
// and icon when the operator has not supplied overrides.
type contentProviderMeta struct {
	Kind        string
	DefaultName string
	DefaultIcon string
}

// contentProviderMetaTable maps ContentSource.Name() -> static metadata.
// Unknown ids fall through to a "direct" default in lookupContentProviderMeta.
var contentProviderMetaTable = map[string]contentProviderMeta{
	"http":             {Kind: "direct", DefaultName: "HTTP", DefaultIcon: "fa-solid fa-globe"},
	"google_drive":     {Kind: "service", DefaultName: "Google Drive", DefaultIcon: "fa-brands fa-google-drive"},
	"google_workspace": {Kind: "delegated", DefaultName: "Google Workspace", DefaultIcon: "fa-brands fa-google"},
	"microsoft":        {Kind: "delegated", DefaultName: "Microsoft 365", DefaultIcon: "fa-brands fa-microsoft"},
	"confluence":       {Kind: "delegated", DefaultName: "Atlassian Confluence", DefaultIcon: "fa-brands fa-confluence"},
}

// lookupContentProviderMeta returns the metadata for the given source id.
// Unknown ids are treated as "direct" with the id itself as the default name
// and an empty icon — a safe fallback that future-registered sources can override
// by adding a row to contentProviderMetaTable.
func lookupContentProviderMeta(id string) contentProviderMeta {
	if m, ok := contentProviderMetaTable[id]; ok {
		return m
	}
	return contentProviderMeta{Kind: "direct", DefaultName: id, DefaultIcon: ""}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestLookupContentProviderMeta`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/content_provider_metadata.go api/content_provider_metadata_test.go
git commit -m "feat(api): add content provider metadata table (#373)"
```

---

## Task 4: Wire ContentSourceRegistry into Server

**Files:**
- Modify: `api/server.go` (add field + setter)

- [ ] **Step 1: Locate the existing `SetDocumentContentOAuthRegistry` method**

Run: `rg -n "SetDocumentContentOAuthRegistry" api/server.go`
Expected: One match around line 328.

- [ ] **Step 2: Add the field to the Server struct**

Find the `Server` struct definition (search: `rg -n "^type Server struct" api/server.go`). Add the new field next to existing registry-related fields (alongside or near where `documentContentOAuthRegistry` is declared):

```go
contentSourceRegistry *ContentSourceRegistry
```

- [ ] **Step 3: Add the setter near `SetDocumentContentOAuthRegistry`**

Add immediately after `SetDocumentContentOAuthRegistry`:

```go
// SetContentSourceRegistry attaches the content source registry so the
// /config handler can advertise configured providers. Mirrors the
// SetDocumentContentOAuthRegistry pattern.
func (s *Server) SetContentSourceRegistry(r *ContentSourceRegistry) {
	s.contentSourceRegistry = r
}
```

- [ ] **Step 4: Verify build**

Run: `make build-server`
Expected: PASS (the field is currently unused by handlers — Task 5 will use it).

- [ ] **Step 5: Commit**

```bash
git add api/server.go
git commit -m "feat(api): wire ContentSourceRegistry into Server (#373)"
```

---

## Task 5: Build content_providers in `/config` handler

**Files:**
- Modify: `api/config_handlers.go` (add helper, extend `buildClientConfig`)
- Test: `api/config_handlers_test.go` (new test cases)

This is the central task. We TDD it: write each test, see it fail, implement, see it pass.

- [ ] **Step 1: Write the failing test for empty registry**

In `api/config_handlers_test.go`, add:

```go
func TestBuildContentProviders_EmptyRegistry(t *testing.T) {
	got := buildContentProviders(nil, nil)
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d items", len(got))
	}

	got = buildContentProviders(NewContentSourceRegistry(), nil)
	if got == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d items", len(got))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test-unit name=TestBuildContentProviders_EmptyRegistry`
Expected: FAIL — `buildContentProviders` is not defined.

- [ ] **Step 3: Implement the helper (initial version, empty-only)**

In `api/config_handlers.go`, add at the bottom of the file:

```go
// buildContentProviders constructs the ClientConfig.ContentProviders array
// from the configured ContentSourceRegistry. The kind/default-name/default-icon
// for each source come from the static contentProviderMetaTable; for delegated
// providers, operator-supplied name/icon in cfg.ContentOAuth.Providers[id]
// take precedence over the defaults.
//
// Returns an empty (non-nil) slice when the registry is nil or empty so the
// JSON response renders a deterministic [] rather than null.
func buildContentProviders(sources *ContentSourceRegistry, cfg *config.Config) []ContentProvider {
	out := make([]ContentProvider, 0)
	if sources == nil {
		return out
	}
	for _, id := range sources.Names() {
		meta := lookupContentProviderMeta(id)
		name := meta.DefaultName
		icon := meta.DefaultIcon
		if meta.Kind == "delegated" && cfg != nil {
			if override, ok := cfg.ContentOAuth.Providers[id]; ok {
				if override.Name != "" {
					name = override.Name
				}
				if override.Icon != "" {
					icon = override.Icon
				}
			}
		}
		out = append(out, ContentProvider{
			Id:   id,
			Name: name,
			Kind: ContentProviderKind(meta.Kind),
			Icon: icon,
		})
	}
	return out
}
```

Note: the exact field names on the generated `ContentProvider` struct depend on oapi-codegen output. After Task 1 ran `make generate-api`, check `api/api.go` for the actual struct (search: `rg -n "^type ContentProvider " api/api.go`). Adjust field names if oapi-codegen produced different casing (e.g., `ID` vs `Id`). The `Kind` enum type name will be `ContentProviderKind` if the generator follows the existing pattern (e.g., `ClientConfigUiDefaultTheme`).

If the generated `Kind` field is a typed alias and the constants are exported (e.g., `Delegated`, `Service`, `Direct`), use those constants instead of casting strings:

```go
var kindMap = map[string]ContentProviderKind{
    "delegated": Delegated, // adjust to actual constant names
    "service":   Service,
    "direct":    Direct,
}
```

If the constants conflict with other names in the package (e.g., the existing `Auto`/`Light`/`Dark` for theme), fall back to the string-cast form shown above.

Also ensure the `internal/config` import is already present in `config_handlers.go` (it should be — `buildClientConfig` already uses it for `s.config`). If not, add: `"github.com/ericfitz/tmi/internal/config"`.

- [ ] **Step 4: Run the empty-registry test to verify it passes**

Run: `make test-unit name=TestBuildContentProviders_EmptyRegistry`
Expected: PASS.

- [ ] **Step 5: Write the failing test for mixed sources**

In `api/config_handlers_test.go`, add a fake source helper and test:

```go
type fakeContentSource struct{ name string }

func (f *fakeContentSource) Name() string                                        { return f.name }
func (f *fakeContentSource) CanHandle(_ context.Context, _ string) bool          { return false }
func (f *fakeContentSource) Fetch(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}

func TestBuildContentProviders_MixedSources(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&fakeContentSource{name: "http"})
	reg.Register(&fakeContentSource{name: "google_drive"})
	reg.Register(&fakeContentSource{name: "google_workspace"})

	got := buildContentProviders(reg, nil)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	if got[0].Id != "http" || string(got[0].Kind) != "direct" || got[0].Name != "HTTP" || got[0].Icon != "fa-solid fa-globe" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Id != "google_drive" || string(got[1].Kind) != "service" || got[1].Name != "Google Drive" {
		t.Errorf("got[1] = %+v", got[1])
	}
	if got[2].Id != "google_workspace" || string(got[2].Kind) != "delegated" || got[2].Name != "Google Workspace" {
		t.Errorf("got[2] = %+v", got[2])
	}
}
```

If `context` is not already imported in this test file, add it.

- [ ] **Step 6: Run the mixed-sources test to verify it passes**

Run: `make test-unit name=TestBuildContentProviders_MixedSources`
Expected: PASS.

- [ ] **Step 7: Write the failing test for operator override on delegated**

```go
func TestBuildContentProviders_DelegatedOverride(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&fakeContentSource{name: "google_workspace"})
	reg.Register(&fakeContentSource{name: "confluence"})

	cfg := &config.Config{}
	cfg.ContentOAuth.Providers = map[string]config.ContentOAuthProviderConfig{
		"google_workspace": {Enabled: true, Name: "GWS Custom", Icon: "fa-custom"},
		// confluence intentionally omitted -> falls back to defaults
	}

	got := buildContentProviders(reg, cfg)
	if got[0].Name != "GWS Custom" || got[0].Icon != "fa-custom" {
		t.Errorf("override not applied: %+v", got[0])
	}
	if got[1].Name != "Atlassian Confluence" || got[1].Icon != "fa-brands fa-confluence" {
		t.Errorf("default not used for unconfigured delegated: %+v", got[1])
	}
}
```

Ensure `"github.com/ericfitz/tmi/internal/config"` is imported in this test file. If not yet, add it.

- [ ] **Step 8: Run the override test to verify it passes**

Run: `make test-unit name=TestBuildContentProviders_DelegatedOverride`
Expected: PASS.

- [ ] **Step 9: Write the failing test for unknown source**

```go
func TestBuildContentProviders_UnknownSource(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&fakeContentSource{name: "experimental"})

	got := buildContentProviders(reg, nil)
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].Id != "experimental" || string(got[0].Kind) != "direct" || got[0].Name != "experimental" || got[0].Icon != "" {
		t.Errorf("got[0] = %+v", got[0])
	}
}
```

- [ ] **Step 10: Run the unknown-source test to verify it passes**

Run: `make test-unit name=TestBuildContentProviders_UnknownSource`
Expected: PASS.

- [ ] **Step 11: Write the failing test for override scoped to delegated**

```go
func TestBuildContentProviders_OverrideIgnoredForNonDelegated(t *testing.T) {
	reg := NewContentSourceRegistry()
	reg.Register(&fakeContentSource{name: "google_drive"}) // service kind

	cfg := &config.Config{}
	cfg.ContentOAuth.Providers = map[string]config.ContentOAuthProviderConfig{
		"google_drive": {Enabled: true, Name: "Should Be Ignored", Icon: "fa-ignored"},
	}

	got := buildContentProviders(reg, cfg)
	if got[0].Name != "Google Drive" || got[0].Icon != "fa-brands fa-google-drive" {
		t.Errorf("override leaked into non-delegated source: %+v", got[0])
	}
}
```

- [ ] **Step 12: Run the scope test to verify it passes**

Run: `make test-unit name=TestBuildContentProviders_OverrideIgnoredForNonDelegated`
Expected: PASS.

- [ ] **Step 13: Wire the helper into `buildClientConfig`**

In `api/config_handlers.go`, modify `buildClientConfig` to populate the new field. Add the call near the end of the function, just before the `return ClientConfig{...}` literal:

```go
contentProviders := buildContentProviders(s.contentSourceRegistry, s.config)
```

Then in the returned literal, add the new field. The exact field name matches the generated struct (likely `ContentProviders`):

```go
return ClientConfig{
    Features: ...,
    Operator: ...,
    Limits:   ...,
    Ui:       ...,
    ContentProviders: &contentProviders,  // pointer or value depending on generator output
}
```

Check `api/api.go` after `make generate-api` to see whether the generator made `ContentProviders` a pointer (`*[]ContentProvider`) or a value (`[]ContentProvider`) field. Use whichever the generator produced. If it's a pointer, take the address: `&contentProviders`.

- [ ] **Step 14: Update existing GetClientConfig tests if needed**

The existing tests `TestGetClientConfig_Success` and `TestGetClientConfig_WithoutOperatorInfo` may now need to expect `content_providers: []` in the response. Read those tests and adjust assertions. If they only assert specific top-level fields (not full equality), they may pass unchanged — verify by running them.

Run: `make test-unit name=^TestGetClientConfig`
Expected: PASS. If they fail because of the new field, update assertions to either ignore the new field or assert `content_providers == []` for the no-registry case.

- [ ] **Step 15: Lint and full unit test sweep**

Run: `make lint`
Expected: PASS.

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 16: Commit**

```bash
git add api/config_handlers.go api/config_handlers_test.go
git commit -m "feat(api): populate content_providers in /config response (#373)"
```

---

## Task 6: Wire the registry in main.go

**Files:**
- Modify: `cmd/server/main.go` (add the setter call)

- [ ] **Step 1: Locate the source-registry construction site**

Run: `rg -n "contentSources := api.NewContentSourceRegistry|contentSources.Register\(api.NewHTTPSource" cmd/server/main.go`
Expected: Two matches around lines 1106 and 1219.

- [ ] **Step 2: Add the setter call after HTTPSource registration**

In `cmd/server/main.go`, immediately after the `contentSources.Register(api.NewHTTPSource(timmyURIValidator))` line (~line 1219) and before the `pipeline := buildContentPipeline(...)` line (~line 1221), add:

```go
apiServer.SetContentSourceRegistry(contentSources)
```

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: PASS.

- [ ] **Step 4: Verify the wiring with an integration smoke test**

Run: `make test-unit name=^TestGetClientConfig`
Expected: PASS.

(No full integration test needed — `/config` is public and unit-tested with fake registries. The wiring is verified by build + unit tests.)

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(api): wire ContentSourceRegistry to /config handler (#373)"
```

---

## Task 7: Final validation and close

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: PASS.

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: All tests pass.

- [ ] **Step 3: Validate OpenAPI**

Run: `make validate-openapi`
Expected: PASS.

- [ ] **Step 4: Verify build**

Run: `make build-server`
Expected: PASS.

- [ ] **Step 5: Manual smoke test**

```bash
make start-dev
# In another terminal:
curl -s http://localhost:8080/config | jq '.content_providers'
```

Expected: an array containing at least `{"id":"http","name":"HTTP","kind":"direct","icon":"fa-solid fa-globe"}`. If `google_workspace`, `confluence`, or `microsoft` are configured, they appear too with `kind: "delegated"`.

- [ ] **Step 6: Close the issue**

Per the project workflow (commit lands on `dev/1.4.0`, not `main`, so no auto-close):

```bash
gh issue comment 373 --body "Implemented in dev/1.4.0 — see commits referencing #373."
gh issue close 373
```

---

## Notes on field-name uncertainty

The exact struct/field names oapi-codegen produces depend on the generator version pinned in `oapi-codegen-config.yml`. If you find that:

- `ContentProvider.Id` is actually `ContentProvider.ID` — adjust struct literals.
- `ContentProvider.Kind`'s type is a named alias (e.g., `ContentProviderKind`) with exported constants — prefer constants over `string(meta.Kind)` casts. If the generated constants collide with names already in the `api` package, keep the string-cast form.
- `ClientConfig.ContentProviders` is `*[]ContentProvider` (pointer) — take address when assigning.

These are mechanical. After `make generate-api`, a quick `rg -n "^type ContentProvider" api/api.go` and `rg -n "ContentProviders" api/api.go` will reveal the actual signatures. Adjust the implementation in Task 5 to match before re-running the tests.
