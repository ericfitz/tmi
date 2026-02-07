# TMI Survey API - Breaking Changes Notice

**Commit**: `bd26290d` — *Rename survey_templates to surveys, add metadata and webhook events*

## Summary

The Survey API has been significantly restructured. The concept formerly called **"survey templates"** is now simply called **"surveys"**, and the identity model has changed from name+version composite keys to server-assigned UUIDs.

---

## 1. Surveys are no longer identified by name + version

**This is the most important change.** Previously, survey templates were addressed by a `{template_id}` (UUID) but also had version-specific endpoints using the composite `{template_id}/versions/{version}` path. That versioning scheme has been removed entirely.

Each survey is now identified **solely by its server-assigned `id` (UUID)**. The `name` and `version` fields still exist as descriptive metadata on the survey object, but they are **not** part of the resource identity or URL path. If you need a specific version of a survey, you must know its UUID.

**Removed endpoints** (version-specific):
- `GET /admin/survey_templates/{template_id}/versions` — list versions
- `GET /admin/survey_templates/{template_id}/versions/{version}` — get specific version
- `GET /intake/templates/{template_id}/versions/{version}` — intake view of specific version

**Removed schemas**: `SurveyTemplateVersion`, `ListSurveyTemplateVersionsResponse`

---

## 2. All paths and schema names renamed

| Before | After |
|--------|-------|
| `/admin/survey_templates` | `/admin/surveys` |
| `/admin/survey_templates/{template_id}` | `/admin/surveys/{survey_id}` |
| `/intake/templates` | `/intake/surveys` |
| `/intake/templates/{template_id}` | `/intake/surveys/{survey_id}` |
| `/intake/responses` | `/intake/survey_responses` |
| `/intake/responses/{response_id}` | `/intake/survey_responses/{response_id}` |
| `/triage/surveys/responses` | `/triage/survey_responses` |
| `/triage/surveys/responses/{response_id}` | `/triage/survey_responses/{response_id}` |

**Schema renames**:
- `SurveyTemplate` / `SurveyTemplateBase` / `SurveyTemplateListItem` → `Survey` / `SurveyBase` / `SurveyListItem`
- `SurveyTemplateStatus` → `SurveyStatus`
- `SurveyTemplateSettings` → `SurveySettings`
- `ListSurveyTemplatesResponse` → `ListSurveysResponse`

---

## 3. Field renames in SurveyResponse

| Before | After |
|--------|-------|
| `template_id` | `survey_id` |
| `template_version` | `survey_version` |

The `survey_version` field is now also present on `SurveyResponseBase` (client-writable), and is captured at creation time as read-only on the full `SurveyResponse`.

---

## 4. New `metadata` field on Survey and SurveyResponse

Both `Survey` and `SurveyResponse` now include an optional `metadata` array field:

```json
"metadata": [
  { "key": "...", "value": "..." }
]
```

This is an array of `Metadata` objects, nullable, max 100 items. You can use this to attach arbitrary key-value pairs to surveys and their responses.

---

## 5. New webhook event types

Six new event types are now emitted:

- `survey.created`, `survey.updated`, `survey.deleted`
- `survey_response.created`, `survey_response.updated`, `survey_response.deleted`

If you subscribe to webhooks, update your event type filters and handlers accordingly. The entity types `"survey"` and `"survey_response"` are also now valid in add-on entity type registrations.

---

## Client Migration Checklist

1. **Update all endpoint URLs** — replace `survey_templates` with `surveys`, `intake/templates` with `intake/surveys`, etc. (see table above)
2. **Remove version-specific endpoint calls** — there are no more `/versions` or `/versions/{version}` sub-paths
3. **Use `survey_id` (UUID) as the sole identifier** for referencing surveys; do not rely on name+version as a composite key
4. **Rename fields** in request/response handling: `template_id` → `survey_id`, `template_version` → `survey_version`
5. **Rename schema/type references** in generated clients: `SurveyTemplate*` → `Survey*`
6. **Handle new `metadata` arrays** on Survey and SurveyResponse objects (nullable, can be ignored if not needed)
7. **Update webhook subscriptions** if you use the new `survey.*` or `survey_response.*` events
