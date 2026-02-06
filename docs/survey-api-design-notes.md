# Survey API Design Notes

Analysis of the assumed API surface based on the current mock implementations in `tmi-ux`.

## Survey Templates API (`SurveyTemplateService`)

### Respondent Endpoints

| Method | Endpoint | Params/Body | Response |
|--------|----------|-------------|----------|
| `GET` | `surveys/templates` | query: `status`, `search`, `limit`, `offset` | `{ templates[], total, limit, offset }` |
| `GET` | `surveys/templates/{templateId}` | -- | `SurveyTemplate` |
| `GET` | `surveys/templates/{templateId}/versions` | -- | `SurveyVersion[]` |
| `GET` | `surveys/templates/{templateId}/versions/{version}` | -- | `SurveyVersion` |
| `GET` | `surveys/templates/{templateId}/versions/latest` | -- | `SurveyVersion` |

### Admin Endpoints

| Method | Endpoint | Body | Response |
|--------|----------|------|----------|
| `POST` | `admin/surveys/templates` | `{ name, description?, status?, survey_json }` | `SurveyTemplate` |
| `PUT` | `admin/surveys/templates/{templateId}` | `{ name?, description?, status?, survey_json?, change_summary? }` | `SurveyTemplate` |
| `DELETE` | `admin/surveys/templates/{templateId}` | -- | `void` (soft-delete / archive) |
| `POST` | `admin/surveys/templates/{templateId}/clone` | `{ name }` | `SurveyTemplate` |

**Notable behavior:** When a `PUT` includes `survey_json`, the backend is expected to auto-create a new version (incrementing `current_version`). The `DELETE` performs a soft-archive (sets status to `'archived'`), not a hard delete.

## Survey Submissions API (`SurveySubmissionService`)

### Respondent Endpoints

| Method | Endpoint | Params/Body | Response |
|--------|----------|-------------|----------|
| `GET` | `surveys/submissions/mine` | query: `template_id`, `user_id`, `status`, `submitted_after`, `submitted_before`, `limit`, `offset` | `{ submissions[], total, limit, offset }` |
| `GET` | `surveys/submissions/{submissionId}` | -- | `SurveySubmission` |
| `POST` | `surveys/submissions` | `{ template_id }` | `SurveySubmission` (new draft) |
| `PUT` | `surveys/submissions/{submissionId}` | `{ data?, ui_state? }` | `SurveySubmission` (save draft) |
| `POST` | `surveys/submissions/{submissionId}/submit` | `{}` | `SurveySubmission` (transitions draft to submitted) |
| `DELETE` | `surveys/submissions/{submissionId}` | -- | `void` (draft only) |

### Triage/Admin Endpoints

| Method | Endpoint | Params/Body | Response |
|--------|----------|-------------|----------|
| `GET` | `surveys/submissions` | same filters as `/mine` | `{ submissions[], total, limit, offset }` (all users) |
| `PUT` | `surveys/submissions/{submissionId}` | `{ status }` | `SurveySubmission` (status change) |
| `PUT` | `surveys/submissions/{submissionId}` | `{ threat_model_id }` | `SurveySubmission` (link to TM) |

**Notable behavior:** The same `PUT` endpoint is overloaded -- respondents use it to save `data`/`ui_state` on their own drafts, while triage users use it to change `status` or set `threat_model_id`. The mock enforces that only drafts can be updated/deleted by respondents.

## Draft Auto-Save (`SurveyDraftService`)

Client-side only -- no additional API calls. Wraps `SurveySubmissionService.updateDraft()` with a 2-second debounce.

## Data Models

### `SurveyTemplate`

Metadata only (does not include the survey JSON).

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `id` | `string` | yes | Template identifier |
| `name` | `string` | yes | Display name |
| `description` | `string` | no | |
| `status` | `'active' \| 'inactive' \| 'archived'` | yes | |
| `current_version` | `number` | yes | Latest version number |
| `created_at` | `string` (ISO 8601) | yes | |
| `modified_at` | `string` (ISO 8601) | yes | |
| `created_by` | `string` | yes | User ID |
| `modified_by` | `string` | yes | User ID |

### `SurveyVersion`

Immutable version record.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `id` | `string` | yes | Version identifier |
| `template_id` | `string` | yes | Parent template |
| `version` | `number` | yes | Sequential version number |
| `survey_json` | `SurveyJsonSchema` | yes | Full SurveyJS JSON schema |
| `created_at` | `string` (ISO 8601) | yes | |
| `created_by` | `string` | yes | User ID |
| `change_summary` | `string` | no | Description of changes |

### `SurveySubmission`

Response data and lifecycle tracking.

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `id` | `string` | yes | Submission identifier |
| `template_id` | `string` | yes | |
| `template_name` | `string` | no | Denormalized for display |
| `template_version` | `number` | yes | Version used when filling |
| `user_id` | `string` | yes | |
| `user_email` | `string` | yes | |
| `user_display_name` | `string` | no | |
| `status` | `'draft' \| 'submitted' \| 'in_review' \| 'pending_triage'` | yes | |
| `data` | `Record<string, unknown>` | yes | SurveyJS response data |
| `ui_state` | `SurveyUIState` | no | Draft restoration state (`currentPageNo`, `isCompleted`) |
| `created_at` | `string` (ISO 8601) | yes | |
| `modified_at` | `string` (ISO 8601) | yes | |
| `submitted_at` | `string` (ISO 8601) | no | |
| `reviewed_at` | `string` (ISO 8601) | no | |
| `threat_model_id` | `string` | no | Linked TM (set during triage) |

### `SurveyJsonSchema`

The SurveyJS JSON schema with TMI extensions. Includes pages, questions, choice lists, visibility expressions, and `mapsToTmField` annotations for mapping answers to threat model fields.

See `src/app/types/survey.types.ts` for the full type definitions including `SurveyPage`, `SurveyQuestion`, `TmFieldMapping`, and builder types.

## Observations for Real API Design

1. **Admin vs respondent path split** -- Templates use `admin/surveys/templates` for writes but `surveys/templates` for reads. Submissions use a single path with implicit authorization. This inconsistency should be resolved.

2. **`PUT` overloading on submissions** -- Draft saves, status changes, and TM linking all share the same `PUT` endpoint with different body shapes. Consider distinct endpoints for triage actions (e.g., `POST .../transition` or `PUT .../status`).

3. **`/versions/latest`** -- A convenience endpoint that may not be worth implementing server-side since the client already has `current_version` from the template metadata.

4. **Filter for `status` accepts both single value and array** (`SubmissionStatus | SubmissionStatus[]`) -- the API needs to support multi-value query params.

5. **No pagination cursor** -- Uses `offset`/`limit` which can have issues with concurrent inserts. Cursor-based pagination would be more robust.

6. **`template_name` denormalized on submissions** -- The mock copies the template name into each submission for display. The real API would need to either do the same or have the client join.
