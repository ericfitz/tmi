# Microsoft consumer-account delegated content provider — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing `microsoft` delegated content source so it also accepts personal-Microsoft-account hosts (`onedrive.live.com`, `1drv.ms`, and any subdomain of `onedrive.live.com`), under a multi-audience Entra app configuration.

**Architecture:** Additive. Three host strings are added to two matchers (`URLPatternMatcher.Identify` in `api/content_pipeline.go` and `DelegatedMicrosoftSource.CanHandle` in `api/content_source_microsoft_graph.go`). The Graph share-id encoding works uniformly across audiences, so the fetch path needs no change. A consumer-host integration sub-test is added to lock in the routing end-to-end. Operator wiki gets a multi-audience setup section. The picker-grant flow's behavior on consumer accounts is uncertain; the plan includes a probe task that — if it confirms `grantedToIdentities` does not work — adds an audience-conditional skip to `MicrosoftPickerGrantHandler`. Source code defaults are unchanged; the operator's `tenant_id` config widens to accept `"common"` or `"consumers"` (which the existing `IsConfigured` validation already accepts).

**Tech Stack:** Go (Gin, GORM), Microsoft Graph v1.0, existing TMI content-OAuth + delegated-source infrastructure (`api/content_oauth_*.go`, `api/content_source_delegated.go`).

**Spec:** [docs/superpowers/specs/2026-05-10-microsoft-consumer-account-delegated-provider-design.md](../specs/2026-05-10-microsoft-consumer-account-delegated-provider-design.md)
**Issue:** #297
**Branch:** `dev/1.4.0` (no feature branch — change is small enough to land directly)

---

## File map

**Modify:**
- `api/content_pipeline.go` — `URLPatternMatcher.Identify`: add three consumer host cases.
- `api/content_pipeline_test.go` — `TestURLPatternMatcher_Identify`: flip onedrive.live.com case from "http" to "microsoft", add 1drv.ms and subdomain cases.
- `api/content_source_microsoft_graph.go` — `DelegatedMicrosoftSource.CanHandle`: add three consumer host cases; update doc comment.
- `api/content_source_microsoft_graph_test.go` — `TestDelegatedMicrosoftSource_CanHandle`: flip the two "out of scope" expectations from `false` to `true`, add subdomain case; `TestEncodeMicrosoftShareID`: add a `1drv.ms` case.
- `api/microsoft_delegated_integration_test.go` — add a sub-test exercising fetch via a consumer-host URL.
- `api/microsoft_picker_grant_handler.go` — **conditional**: if probe (Task 7) shows consumer accounts reject `grantedToIdentities`, add audience-detection helper and skip-grant branch.
- `api/microsoft_picker_grant_handler_test.go` — **conditional**: tests for audience-detection skip behavior.

**Create:** none.

**Operator-facing (no commit to repo unless wiki sync is part of repo workflow):**
- GitHub wiki page for Microsoft content provider — add "Multi-audience setup" subsection covering `signInAudience`, `/common/` endpoints, `tenant_id: "common"`, and a personal-account verification checklist.

---

## Task 1: TDD the URL pattern matcher additions

**Files:**
- Modify test: `api/content_pipeline_test.go:11-40` (TestURLPatternMatcher_Identify)
- Modify: `api/content_pipeline.go:69-82` (URLPatternMatcher.Identify switch)

- [ ] **Step 1: Update `TestURLPatternMatcher_Identify` to assert consumer URLs route to "microsoft"**

In `api/content_pipeline_test.go`, replace the existing onedrive.live.com line and add 1drv.ms + subdomain cases. The current block at line 23-28 reads:

```go
		{"https://mycompany.sharepoint.com/sites/team/doc.docx", "microsoft"},
		// onedrive.live.com is consumer MSA — falls through to ProviderHTTP (#297).
		{"https://onedrive.live.com/edit.aspx?id=abc", "http"},
		{"https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/file.docx", "microsoft"},
		{"https://contoso-my.sharepoint.com/personal/alice/Documents/draft.pptx", "microsoft"},
		{"https://contoso.sharepoint.com/personal/_layouts/15/onedrive.aspx", "microsoft"},
```

Replace with:

```go
		{"https://mycompany.sharepoint.com/sites/team/doc.docx", "microsoft"},
		// onedrive.live.com and 1drv.ms are consumer Microsoft accounts (#297) —
		// served by the same delegated provider under a multi-audience Entra app.
		{"https://onedrive.live.com/edit.aspx?id=abc", "microsoft"},
		{"https://1drv.ms/b/s!abc123", "microsoft"},
		{"https://my.onedrive.live.com/personal/foo", "microsoft"},
		{"https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/file.docx", "microsoft"},
		{"https://contoso-my.sharepoint.com/personal/alice/Documents/draft.pptx", "microsoft"},
		{"https://contoso.sharepoint.com/personal/_layouts/15/onedrive.aspx", "microsoft"},
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `make test-unit name=TestURLPatternMatcher_Identify`
Expected: FAIL — assertions for the three consumer URLs return `"http"` instead of `"microsoft"`.

- [ ] **Step 3: Add the host cases to `URLPatternMatcher.Identify`**

In `api/content_pipeline.go`, the current switch at lines 69-82 reads:

```go
	switch {
	case host == googleHostDocs || host == googleHostDrive:
		return ProviderGoogleDrive
	case strings.HasSuffix(host, ".atlassian.net") && strings.Contains(lower, "/wiki/"):
		return ProviderConfluence
	// onedrive.live.com (consumer Microsoft accounts) is intentionally NOT
	// matched here. Tracked in #297; for now consumer URLs fall through to
	// ProviderHTTP rather than being misidentified as Entra-managed Microsoft.
	case strings.HasSuffix(host, ".sharepoint.com"):
		return ProviderMicrosoft
	default:
		return ProviderHTTP
	}
```

Replace with:

```go
	switch {
	case host == googleHostDocs || host == googleHostDrive:
		return ProviderGoogleDrive
	case strings.HasSuffix(host, ".atlassian.net") && strings.Contains(lower, "/wiki/"):
		return ProviderConfluence
	// Microsoft is multi-audience (#286 work/school + #297 consumer) under a
	// single delegated provider. The hosts below cover both audiences:
	//   - *.sharepoint.com           — Entra-managed (OneDrive-for-Business + SharePoint)
	//   - onedrive.live.com (or *.)  — consumer OneDrive
	//   - 1drv.ms                    — consumer OneDrive short link
	case strings.HasSuffix(host, ".sharepoint.com"),
		host == "onedrive.live.com",
		strings.HasSuffix(host, ".onedrive.live.com"),
		host == "1drv.ms":
		return ProviderMicrosoft
	default:
		return ProviderHTTP
	}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `make test-unit name=TestURLPatternMatcher_Identify`
Expected: PASS for all cases.

- [ ] **Step 5: Run the full unit test suite to catch unrelated breakage**

Run: `make test-unit`
Expected: All tests pass. (If other tests assumed onedrive.live.com routes to "http", fix them now — search with `rg -n "onedrive\\.live\\.com|1drv\\.ms" api/`.)

- [ ] **Step 6: Commit**

```bash
git add api/content_pipeline.go api/content_pipeline_test.go
git commit -m "$(cat <<'EOF'
feat(api): route consumer Microsoft hosts to microsoft provider (#297)

Adds onedrive.live.com (and *.onedrive.live.com) and 1drv.ms to the
URLPatternMatcher's microsoft branch. Consumer Microsoft accounts now
share the same provider id as Entra-managed work/school under a
multi-audience Entra app.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: TDD the source CanHandle additions

**Files:**
- Modify test: `api/content_source_microsoft_graph_test.go:66-84` (TestDelegatedMicrosoftSource_CanHandle)
- Modify: `api/content_source_microsoft_graph.go:130-140` (DelegatedMicrosoftSource.CanHandle)

- [ ] **Step 1: Update `TestDelegatedMicrosoftSource_CanHandle` to assert true for consumer hosts**

In `api/content_source_microsoft_graph_test.go`, the current block at lines 66-84 reads:

```go
func TestDelegatedMicrosoftSource_CanHandle(t *testing.T) {
	s := &DelegatedMicrosoftSource{}
	cases := []struct {
		uri      string
		expected bool
	}{
		{"https://contoso.sharepoint.com/sites/Marketing/Doc.docx", true},
		{"https://contoso-my.sharepoint.com/personal/alice/Documents/draft.pptx", true},
		{"https://onedrive.live.com/redir?resid=1234", false}, // personal — out of scope
		{"https://1drv.ms/abc", false},                        // personal short link — out of scope
		{"https://docs.google.com/document/d/abc/edit", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			assert.Equal(t, tc.expected, s.CanHandle(context.Background(), tc.uri))
		})
	}
}
```

Replace with:

```go
func TestDelegatedMicrosoftSource_CanHandle(t *testing.T) {
	s := &DelegatedMicrosoftSource{}
	cases := []struct {
		uri      string
		expected bool
	}{
		// Entra-managed (work/school) — #286.
		{"https://contoso.sharepoint.com/sites/Marketing/Doc.docx", true},
		{"https://contoso-my.sharepoint.com/personal/alice/Documents/draft.pptx", true},
		// Consumer Microsoft accounts — #297, multi-audience Entra app.
		{"https://onedrive.live.com/redir?resid=1234", true},
		{"https://my.onedrive.live.com/personal/foo", true},
		{"https://1drv.ms/b/s!abc123", true},
		// Other providers — must remain false.
		{"https://docs.google.com/document/d/abc/edit", false},
		{"https://example.com/file.pdf", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			assert.Equal(t, tc.expected, s.CanHandle(context.Background(), tc.uri))
		})
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `make test-unit name=TestDelegatedMicrosoftSource_CanHandle`
Expected: FAIL on the three consumer-host cases.

- [ ] **Step 3: Add the host cases to `CanHandle`**

In `api/content_source_microsoft_graph.go`, the current method at lines 130-140 reads:

```go
// CanHandle returns true for *.sharepoint.com hosts (covers OneDrive-for-Business
// at *-my.sharepoint.com and any SharePoint site). Personal Microsoft account
// hosts (onedrive.live.com, 1drv.ms) are deliberately not handled here; they
// will be picked up by a future personal-account sub-project.
func (s *DelegatedMicrosoftSource) CanHandle(_ context.Context, uri string) bool {
	if uri == "" {
		return false
	}
	host := extractHost(strings.ToLower(uri))
	return strings.HasSuffix(host, ".sharepoint.com")
}
```

Replace with:

```go
// CanHandle returns true for hosts served by the multi-audience Microsoft
// delegated provider:
//   - *.sharepoint.com       — Entra-managed OneDrive-for-Business + SharePoint (#286)
//   - onedrive.live.com      — consumer OneDrive root (#297)
//   - *.onedrive.live.com    — consumer OneDrive regional/tenant subdomains (#297)
//   - 1drv.ms                — consumer OneDrive short link (#297)
//
// All four route to the same DelegatedMicrosoftSource because Microsoft Graph
// /shares/{shareId}/driveItem resolves uniformly across audiences once the
// user has consented and per-file permission is in place.
func (s *DelegatedMicrosoftSource) CanHandle(_ context.Context, uri string) bool {
	if uri == "" {
		return false
	}
	host := extractHost(strings.ToLower(uri))
	switch {
	case strings.HasSuffix(host, ".sharepoint.com"):
		return true
	case host == "onedrive.live.com", strings.HasSuffix(host, ".onedrive.live.com"):
		return true
	case host == "1drv.ms":
		return true
	}
	return false
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `make test-unit name=TestDelegatedMicrosoftSource_CanHandle`
Expected: PASS for all cases.

- [ ] **Step 5: Commit**

```bash
git add api/content_source_microsoft_graph.go api/content_source_microsoft_graph_test.go
git commit -m "$(cat <<'EOF'
feat(api): DelegatedMicrosoftSource.CanHandle accepts consumer hosts (#297)

Extends the source's host matcher to onedrive.live.com (and subdomains)
and 1drv.ms. Same provider id, same Graph fetch path; multi-audience
Entra app handles the auth side.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Add a 1drv.ms case to TestEncodeMicrosoftShareID

**Files:**
- Modify test: `api/content_source_microsoft_graph_test.go:15-30` (TestEncodeMicrosoftShareID)

- [ ] **Step 1: Add a 1drv.ms case**

The current block at lines 15-30 reads:

```go
func TestEncodeMicrosoftShareID(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		// Examples from Microsoft Graph documentation.
		{"https://onedrive.live.com/redir?resid=1234", "u!aHR0cHM6Ly9vbmVkcml2ZS5saXZlLmNvbS9yZWRpcj9yZXNpZD0xMjM0"},
		{"https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/Doc.docx",
			"u!aHR0cHM6Ly9jb250b3NvLnNoYXJlcG9pbnQuY29tL3NpdGVzL01hcmtldGluZy9TaGFyZWQlMjBEb2N1bWVudHMvRG9jLmRvY3g"},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			assert.Equal(t, tc.want, encodeMicrosoftShareID(tc.uri))
		})
	}
}
```

Replace with:

```go
func TestEncodeMicrosoftShareID(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		// Examples from Microsoft Graph documentation.
		{"https://onedrive.live.com/redir?resid=1234", "u!aHR0cHM6Ly9vbmVkcml2ZS5saXZlLmNvbS9yZWRpcj9yZXNpZD0xMjM0"},
		{"https://contoso.sharepoint.com/sites/Marketing/Shared%20Documents/Doc.docx",
			"u!aHR0cHM6Ly9jb250b3NvLnNoYXJlcG9pbnQuY29tL3NpdGVzL01hcmtldGluZy9TaGFyZWQlMjBEb2N1bWVudHMvRG9jLmRvY3g"},
		// 1drv.ms short link — Graph encodes it the same way; server-side
		// follows the redirect when resolving /shares/{shareId}/driveItem (#297).
		// Expected value computed via:
		//   printf '%s' 'https://1drv.ms/b/s!abc123' | base64 | tr '+/' '-_' | tr -d '='
		{"https://1drv.ms/b/s!abc123", "u!aHR0cHM6Ly8xZHJ2Lm1zL2IvcyFhYmMxMjM"},
	}
	for _, tc := range cases {
		t.Run(tc.uri, func(t *testing.T) {
			assert.Equal(t, tc.want, encodeMicrosoftShareID(tc.uri))
		})
	}
}
```

(The expected value `u!aHR0cHM6Ly8xZHJ2Lm1zL2IvcyFhYmMxMjM` is the base64url encoding of `https://1drv.ms/b/s!abc123` with `=` padding stripped, prefixed with `u!` per Microsoft Graph share-id convention.)

- [ ] **Step 2: Run the test and confirm it passes**

Run: `make test-unit name=TestEncodeMicrosoftShareID`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add api/content_source_microsoft_graph_test.go
git commit -m "$(cat <<'EOF'
test(api): add 1drv.ms case to TestEncodeMicrosoftShareID (#297)

Locks in that the existing share-id encoder produces the correct
Graph share id for consumer-OneDrive short links.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Integration test — consumer paste-URL fetch end-to-end

**Files:**
- Modify: `api/microsoft_delegated_integration_test.go` (add a sub-test that uses a consumer-host URL)

- [ ] **Step 1: Read the existing paste-URL integration sub-test**

Read `api/microsoft_delegated_integration_test.go` from start to end to identify the sub-test that exercises `fetchByURL` against the stub Graph server (look for a `TestMicrosoftDelegated_PasteURL_Forbidden` or similar). The new sub-test will follow the same wiring pattern: build a stub Graph server, register a token row, construct `DelegatedMicrosoftSource` pointing at the stub, call `Fetch`, assert success.

Note: the file uses the build tag `//go:build dev || test || integration` (line 1). All new code must remain inside this file or another file with the same build tag.

- [ ] **Step 2: Add a happy-path consumer paste-URL sub-test**

At the end of the file (after the last existing test function), append:

```go
// =============================================================================
// Test: Experience 1 — consumer Microsoft account paste-URL fetch
// =============================================================================

// TestMicrosoftDelegated_ConsumerPasteURL_HappyPath exercises Experience 1
// for a personal Microsoft account: the user has linked their consumer
// Microsoft account; they paste a onedrive.live.com URL on a TMI document;
// the server resolves /shares/{shareId}/driveItem and downloads the content.
//
// Routing precondition: TestURLPatternMatcher_Identify and
// TestDelegatedMicrosoftSource_CanHandle must have already verified that
// onedrive.live.com routes to the microsoft provider. This sub-test verifies
// the fetch path itself succeeds for a consumer-host URL — the Graph stub
// is audience-agnostic, so a green test confirms the integration is wired
// correctly end-to-end.
func TestMicrosoftDelegated_ConsumerPasteURL_HappyPath(t *testing.T) {
	stub := newStubMicrosoftGraphServer(t)

	tokens := newMockContentTokenRepo(map[string]*ContentToken{
		"u1:microsoft": {
			UserID:       "u1",
			ProviderID:   ProviderMicrosoft,
			AccessToken:  "valid-access",
			RefreshToken: "valid-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
		},
	}, func(userID, providerID, oldRefresh string) (*ContentOAuthTokenResponse, error) {
		if userID == "u1" && providerID == ProviderMicrosoft {
			return &ContentOAuthTokenResponse{
				AccessToken:  "valid-access",
				RefreshToken: "valid-refresh",
				ExpiresIn:    3600,
			}, nil
		}
		return nil, errors.New("unexpected refresh")
	})

	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubContentOAuthProvider{
		id:      ProviderMicrosoft,
		authURL: "https://stub/authorize",
	})

	source := &DelegatedMicrosoftSource{
		GraphBaseURL: stub.URL,
		safeClient:   newPermissiveSafeClient(t),
	}
	source.Delegated = &DelegatedSource{
		ProviderID: ProviderMicrosoft,
		Tokens:     tokens,
		Registry:   registry,
		DoFetch: func(ctx context.Context, token, uri string) ([]byte, string, error) {
			return source.fetchByURL(ctx, token, uri)
		},
	}

	ctx := WithUserID(context.Background(), "u1")
	body, contentType, err := source.Fetch(ctx,
		"https://onedrive.live.com/redir?resid=1234")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))
	assert.Equal(t, "text/plain", contentType)
}
```

**Inspection note for the implementer:** the sub-test references three helpers — `newMockContentTokenRepo`, `stubContentOAuthProvider`, `newPermissiveSafeClient`, and `WithUserID`. These are present in the existing integration-test file or sibling test files. If any helper has a different name in the actual codebase, mirror what the existing `TestMicrosoftDelegated_PickerGrantThenFetch` test uses. Do **not** invent new helpers — the goal is exact parity with the existing paste-URL test, only with a consumer-host URL.

- [ ] **Step 3: Run the new test and confirm it passes**

Run: `make test-integration name=TestMicrosoftDelegated_ConsumerPasteURL_HappyPath`
Expected: PASS.

If the test fails because of helper-name mismatches, fix the helper references; do not change source code to make the test pass.

- [ ] **Step 4: Run the full integration suite to catch regressions**

Run: `make test-integration`
Expected: No new failures relative to the pre-existing integration-test baseline. Any failures must be either pre-existing (verify by running the same test on a clean tree) or fixed.

- [ ] **Step 5: Commit**

```bash
git add api/microsoft_delegated_integration_test.go
git commit -m "$(cat <<'EOF'
test(api): integration test for consumer Microsoft paste-URL fetch (#297)

Asserts that DelegatedMicrosoftSource.Fetch succeeds end-to-end for an
onedrive.live.com URL against the existing stub Graph server. Locks in
that the multi-audience routing reaches the same fetch path used for
SharePoint URLs.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Lint, build, full unit + integration test pass

**Files:** none (verification only)

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: 0 issues.

If issues are reported, fix them in a small follow-up commit before proceeding.

- [ ] **Step 2: Run build**

Run: `make build-server`
Expected: clean build.

- [ ] **Step 3: Run unit tests**

Run: `make test-unit`
Expected: All tests pass. Note the test count — it should be at minimum 1606 (sub-project 4 baseline) plus the new tests in Tasks 1–4.

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
Expected: All tests pass except the four pre-existing failures (`TestAuthFlowRateLimiting_MultiScope`, `TestClientCredentialsCRUD`, `TestIPRateLimiting_PublicEndpoints`, `TestCascadeDeletion`) noted in #249's sub-project 4 completion comment. No new failures introduced.

- [ ] **Step 5: No commit (this task is a gate, not a change).**

---

## Task 6: Operator wiki — multi-audience Microsoft setup

**Files:** GitHub wiki page for the Microsoft delegated content provider (operator-facing). Per CLAUDE.md, do **not** add or update markdown files in the `docs/` directory; the wiki is the authoritative documentation surface.

- [ ] **Step 1: Locate the existing Microsoft content-provider wiki page**

The implementer will need to identify the wiki page that documents the Microsoft delegated content provider operator setup. If sub-project 4 (Google Workspace delegated picker) followed a `tmi-wiki-*.md` staging convention (visible in #249's first sub-project comment), check the repo root and any `tmi-wiki-*.md` files for prior work. Otherwise, the page is on the GitHub Wiki at <https://github.com/ericfitz/tmi/wiki> under the Microsoft / OneDrive / SharePoint section. If unsure, ask the user where to put the operator instructions.

- [ ] **Step 2: Add a "Multi-audience setup" subsection**

Add a subsection (markdown) covering the following points. Phrase as operator instructions, not narrative:

1. **Entra app registration:** `signInAudience = AzureADandPersonalMicrosoftAccount`. Required to accept tokens from both work/school and personal Microsoft accounts under one TMI provider id.
2. **OAuth endpoints:** in the TMI `content_oauth.providers.microsoft` section, set `auth_url` and `token_url` to the `/common/` endpoint:
   - `auth_url: https://login.microsoftonline.com/common/oauth2/v2.0/authorize`
   - `token_url: https://login.microsoftonline.com/common/oauth2/v2.0/token`
   - Operators who want to limit the link button to one audience may instead use `/consumers/`, `/organizations/`, or a tenant id — TMI does not constrain this.
3. **Required scopes:** unchanged from the work/school setup. List: `openid`, `offline_access`, `User.Read`, `Files.SelectedOperations.Selected`, `Files.ReadWrite`. Note that `Files.ReadWrite` is only used by the picker-grant endpoint; operators not exposing the picker UI may omit it (Experience 1 paste-URL works without it).
4. **`content_sources.microsoft` config:** set `tenant_id: "common"` (or `"consumers"` if locking to consumer only). `client_id` and `application_object_id` come from the multi-audience Entra app registration.
5. **Picker origin:** `picker_origin` for consumer pickers is `https://onedrive.live.com`. For mixed-audience deployments where a single tmi-ux instance must support both, this is operator-policy decision — typically the consumer origin since the picker SDK negotiates the audience at runtime. Document the trade-off.

- [ ] **Step 3: Add a "Personal-account verification checklist"**

Add a checklist subsection an operator runs after setup to confirm consumer support is working:

1. Link a personal Microsoft account: `POST /me/content_tokens/microsoft/authorize` and complete the consent flow.
2. Verify token row created: `GET /me/content_tokens` shows a `microsoft` entry.
3. Create a TMI document with a consumer URL (e.g., `https://onedrive.live.com/...` or `https://1drv.ms/...`).
4. Observe the document's diagnostics: `microsoft_not_shared` reason with the share-with-TMI remediation snippet (Experience 1 path).
5. Run the share-with-TMI snippet against the file (operator-side, using the personal-account user's identity — see snippet in document diagnostics).
6. Wait for the access poller (default tick interval) and re-fetch the document: `access_status` flips to `accessible`.
7. (Optional, only if tmi-ux picker integration is wired:) pick a consumer-OneDrive file via the Microsoft File Picker, verify the document attaches, picker_grant call succeeds (or is skipped per Task 7 outcome), and content extracts.

- [ ] **Step 4: Add a "Picker-grant on consumer accounts" note**

Add a short note acknowledging the contingency. The note's wording depends on the outcome of Task 7 below — if Outcome 1 (picker grant works on consumer accounts), the note is "Picker grant works identically for both audiences." If Outcome 2 (skip on consumer), the note is "On consumer accounts the server skips the Graph permissions API call; the Microsoft File Picker SDK has already issued the per-file scope, and subsequent fetches succeed under that scope."

- [ ] **Step 5: Commit (only if the wiki content is staged in-repo as a `tmi-wiki-*.md` file).**

If wiki content is staged in-repo:

```bash
git add tmi-wiki-microsoft-content-provider.md  # or wherever it lives
git commit -m "$(cat <<'EOF'
docs(wiki): multi-audience Microsoft content provider setup (#297)

Documents Entra app signInAudience, /common/ OAuth endpoints,
tenant_id="common" config, and the personal-account verification
checklist.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

If the wiki is updated directly via the GitHub web UI, no repo commit is needed for this task; record the change in the task summary.

---

## Task 7: Probe Microsoft Graph picker-grant behavior on a consumer account

**Files:** none yet — this task decides whether Task 8 below is needed.

This task requires either a live personal Microsoft account or documented evidence from Microsoft's Graph documentation. The implementer should default to **option B (documentation-driven)** unless a real consumer account is available.

- [ ] **Step 1 (option A — live probe, preferred if a consumer account is available)**

With a personal Microsoft account linked to a multi-audience TMI deployment:

1. Pick a personal-OneDrive file via the Microsoft File Picker (or simulate by hand-crafting a `(drive_id, item_id)` from a known consumer file).
2. POST `(drive_id, item_id)` to TMI's `/me/microsoft/picker_grants` endpoint.
3. Observe the Graph response in the TMI server logs (`/logs` skill or `logs/tmi.log`). Two outcomes:
   - **2xx** → Outcome 1: consumer accounts honor `grantedToIdentities` with an application grantee. Record the response body. **Skip Task 8.**
   - **4xx with an error like `invalidRequest` or `notSupported`** → Outcome 2: consumer accounts reject the application-grantee form. **Proceed to Task 8.**

- [ ] **Step 2 (option B — documentation-driven)**

If no consumer account is available, default to **Outcome 2 (skip on consumer)** as the safer choice. Microsoft's documented per-file permissions on consumer OneDrive are user-targeted (sharing links, individual recipients), not application-targeted. Implementing the audience-conditional skip costs little and is a no-op on work/school accounts. **Proceed to Task 8.**

- [ ] **Step 3: Record the outcome**

Add a one-line note to `docs/superpowers/specs/2026-05-10-microsoft-consumer-account-delegated-provider-design.md` near §5 ("Picker-grant audience awareness (conditional)") indicating which outcome was confirmed and how (live probe vs documentation-driven default). Commit:

```bash
git add docs/superpowers/specs/2026-05-10-microsoft-consumer-account-delegated-provider-design.md
git commit -m "$(cat <<'EOF'
docs(specs): record Microsoft consumer picker-grant probe outcome (#297)

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 8 (conditional on Task 7 = Outcome 2): Audience-conditional skip in picker-grant

**Skip this task if Task 7 confirmed Outcome 1 (picker grant works on consumer accounts).**

**Files:**
- Modify: `api/microsoft_picker_grant_handler.go` (add audience detection + early-return branch)
- Modify: `api/microsoft_picker_grant_handler_test.go` (add tests for the skip behavior)

- [ ] **Step 1: Identify how to detect consumer-account tokens**

Read `api/microsoft_picker_grant_handler.go:Handle` to locate the point after token retrieval (around line 112) and before the Graph permissions POST. The detection can use one of:

- The well-known consumer tenant id `9188040d-6c67-4c5b-b112-36a304b66dad` if the stored token row carries a tenant id field, or
- Inspecting the OAuth `id_token` claims persisted at link time, or
- Inspecting the userinfo response (Graph `/me`) — the `userPrincipalName` for consumer accounts ends in `@outlook.com`, `@hotmail.com`, `@live.com`, or `@msn.com`.

The simplest and most portable check is **userPrincipalName suffix on the linked token's account label**. The `ContentToken` row already stores `AccountLabel` populated from `FetchAccountInfo` at link time — verify with `rg -n "AccountLabel" api/content_oauth*.go api/content_token*.go`. If `AccountLabel` is the linked-account email/UPN, suffix-match it.

If `AccountLabel` is unavailable or unreliable, add a lightweight `/me` probe inside the handler before the grant call. Prefer the AccountLabel approach to avoid an extra Graph call per grant.

- [ ] **Step 2: Write a failing test for the consumer-account skip**

In `api/microsoft_picker_grant_handler_test.go`, add a test:

```go
// TestMicrosoftPickerGrantHandler_ConsumerAccountSkipsGrant verifies that
// when the linked Microsoft token is for a consumer account (personal MSA),
// the handler returns 200 without calling the Graph permissions endpoint.
// Consumer accounts do not honor the grantedToIdentities application grantee
// form (#297, Task 7 Outcome 2); the picker SDK has already issued per-file
// scope, so the grant call would only fail and is skipped.
func TestMicrosoftPickerGrantHandler_ConsumerAccountSkipsGrant(t *testing.T) {
	graphCalls := 0
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphCalls++
		http.Error(w, "should not be called for consumer account", http.StatusInternalServerError)
	}))
	defer stub.Close()

	tokens := newMockContentTokenRepo(map[string]*ContentToken{
		"u1:microsoft": {
			UserID:       "u1",
			ProviderID:   ProviderMicrosoft,
			AccessToken:  "valid",
			RefreshToken: "valid-refresh",
			ExpiresAt:    time.Now().Add(time.Hour),
			AccountLabel: "alice@outlook.com", // consumer account marker
		},
	}, nil)

	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubContentOAuthProvider{id: ProviderMicrosoft})

	h := NewMicrosoftPickerGrantHandler(
		tokens, registry,
		"app-object-id-123",
		stub.URL,
		func(c *gin.Context) (string, bool) { return "u1", true },
		newPermissiveURIValidator(t),
	)

	req := httptest.NewRequest(http.MethodPost, "/me/microsoft/picker_grants",
		bytes.NewReader([]byte(`{"drive_id":"b!abc","item_id":"01XYZ"}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.Handle(c)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 0, graphCalls, "Graph permissions API must not be called for consumer accounts")
}
```

**Inspection note:** mirror existing test setup conventions in `api/microsoft_picker_grant_handler_test.go`. The helpers `newMockContentTokenRepo`, `stubContentOAuthProvider`, and `newPermissiveURIValidator` are existing test helpers; if any have different names or signatures in this file, follow the file's conventions exactly.

- [ ] **Step 3: Run the test and confirm it fails**

Run: `make test-unit name=TestMicrosoftPickerGrantHandler_ConsumerAccountSkipsGrant`
Expected: FAIL — handler currently calls Graph regardless of audience.

- [ ] **Step 4: Add the audience-detection helper and skip branch**

In `api/microsoft_picker_grant_handler.go`, add a small helper:

```go
// isConsumerMicrosoftAccount returns true when the linked token's account
// label is a consumer-MSA email. Consumer accounts do not honor the
// grantedToIdentities application-grantee form on Graph's per-file
// permissions endpoint (#297, Task 7 Outcome 2), so the handler skips
// the grant call and relies on the picker SDK's per-file scope.
func isConsumerMicrosoftAccount(label string) bool {
	if label == "" {
		return false
	}
	lower := strings.ToLower(label)
	return strings.HasSuffix(lower, "@outlook.com") ||
		strings.HasSuffix(lower, "@hotmail.com") ||
		strings.HasSuffix(lower, "@live.com") ||
		strings.HasSuffix(lower, "@msn.com")
}
```

(Add `strings` to the imports if not already present.)

In `Handle`, after the successful token retrieval and refresh path (around line 200, just before the Graph permissions POST is constructed), insert:

```go
	if isConsumerMicrosoftAccount(tok.AccountLabel) {
		log.Info("microsoft picker_grant: consumer account %s — skipping Graph permissions call (per-file scope already granted by picker SDK)", tok.AccountLabel)
		c.JSON(http.StatusOK, gin.H{
			"granted": true,
			"reason":  "consumer_picker_scope",
		})
		return
	}
```

The exact response body shape must match the existing 2xx response shape for work/school grants — read the surrounding code to confirm. If the existing handler returns `{"id": "perm-..."}` from the Graph response, the consumer skip path returns a synthetic equivalent with the same key set, e.g. `{"id": "consumer-picker-scope"}`. **Do not invent a new response shape; mirror the existing one and add a single field if needed for telemetry.**

- [ ] **Step 5: Run the test and confirm it passes**

Run: `make test-unit name=TestMicrosoftPickerGrantHandler_ConsumerAccountSkipsGrant`
Expected: PASS.

- [ ] **Step 6: Run the full picker-grant test file to confirm no regressions**

Run: `make test-unit name=TestMicrosoftPickerGrantHandler`
Expected: All existing picker-grant tests still pass (work/school grants must still call Graph; only consumer accounts skip).

- [ ] **Step 7: Commit**

```bash
git add api/microsoft_picker_grant_handler.go api/microsoft_picker_grant_handler_test.go
git commit -m "$(cat <<'EOF'
feat(api): skip Graph permissions call for consumer Microsoft accounts (#297)

Consumer MSA accounts (outlook.com, hotmail.com, live.com, msn.com) do not
honor the grantedToIdentities application-grantee form on Graph's per-file
permissions endpoint. The Microsoft File Picker SDK has already granted
per-file scope on the user's token, so the picker-grant handler returns
200 immediately for consumer accounts and lets the existing fetch path
read the file under that scope.

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Final lint, build, test, and security regression scan

**Files:** none (verification only)

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: 0 issues.

- [ ] **Step 2: Run build**

Run: `make build-server`
Expected: clean build.

- [ ] **Step 3: Run unit tests**

Run: `make test-unit`
Expected: All tests pass. Total count = sub-project 4 baseline (1606) + tests added in Tasks 1, 2, 3, and (if Task 8 was needed) Task 8.

- [ ] **Step 4: Run integration tests**

Run: `make test-integration`
Expected: No new failures relative to the four pre-existing failures noted in Task 5.

- [ ] **Step 5: Security regression scan**

Per project policy (CLAUDE.md "Session Completion"), run the security-regression skill against the staged changes. The change adds three host-matcher cases and (conditionally) one audience-detection helper — none are security-sensitive paths (no SSRF/redirect/auth/error-classification changes). Expected: clean.

- [ ] **Step 6: No commit (this task is a gate, not a change).**

---

## Task 10: Close issue #297

**Files:** none (GitHub state)

- [ ] **Step 1: Confirm work is on `dev/1.4.0`**

```bash
git branch --show-current
```

Expected: `dev/1.4.0`. (Since work landed directly on `dev/1.4.0`, `Closes #297` in commit messages will NOT auto-close the issue — the branch is not the default branch. Per [feedback memory](../../../.claude/projects/-Users-efitz-Projects-tmi/memory/feedback_issue_closure_workflow.md), close manually.)

- [ ] **Step 2: Comment on the issue with a summary**

```bash
gh issue comment 297 --body "Resolved on dev/1.4.0.

**What landed:**
- URLPatternMatcher and DelegatedMicrosoftSource.CanHandle accept onedrive.live.com (and *.onedrive.live.com) and 1drv.ms.
- Integration sub-test exercises consumer-host paste-URL fetch end-to-end.
- [If Task 8 ran:] Microsoft picker-grant handler skips the Graph permissions call for consumer accounts, returning 2xx immediately and relying on the picker SDK's per-file scope.
- Wiki updated with multi-audience Entra app setup and personal-account verification checklist.

**Verification:**
- make lint, make build-server: clean.
- make test-unit: all pass.
- make test-integration: no new failures.

Commits: <list of commit SHAs from Tasks 1–8>"
```

- [ ] **Step 3: Close the issue**

```bash
gh issue close 297
```

Expected: issue moves to closed/done.

---

## Self-review (run after writing the plan)

(For the plan author — checked once, fixed inline.)

**Spec coverage:**
- Spec §"Architecture changes" → Tasks 1, 2, 3, 4 (matchers + tests + integration).
- Spec §"OAuth endpoint defaults" → Task 6 (wiki, since dev config does not enable Microsoft).
- Spec §"Picker-grant audience awareness (conditional)" → Tasks 7, 8.
- Spec §"Configuration" → Task 6 (wiki documents `tenant_id: "common"`; no source change).
- Spec §"Testing" → Tasks 1, 2, 3, 4, 9.
- Spec §"Definition of done" item 6 (oracle-db-admin N/A) → noted; no DB-touching change.
- Spec §"Definition of done" item 7 (manual issue closure) → Task 10.

**Placeholder scan:** none. Task 3's expected base64url value is computed in the plan and embedded directly in the test code, with the shell command shown alongside as the derivation receipt.

**Type consistency:** `ContentToken.AccountLabel` is referenced in Task 8 Step 1 with an inspection note to verify the field name; the file map flags this as a verification step, not an assumption.

**Gaps:** none material. tmi-ux changes are explicitly out of scope per the spec; if a follow-up tmi-ux issue is needed, that gets filed at session-completion time, not in this plan.
