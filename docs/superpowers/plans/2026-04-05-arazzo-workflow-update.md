# Arazzo Workflow Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Update `api-workflows.json` with all 1.3.x/1.4.x API endpoints, fix stale paths, update the enhance script's dependency detection, and regenerate Arazzo files.

**Architecture:** The Arazzo generation pipeline reads `api-workflows.json` (knowledge base) + OpenAPI spec, generates a scaffold via Redocly CLI, then enhances it with TMI-specific workflow sequences via `scripts/enhance-arazzo-with-workflows.py`. We update the knowledge base and enhance script, then regenerate.

**Tech Stack:** JSON (api-workflows.json), Python (enhance script), Make (generate-arazzo, validate-arazzo)

**Spec:** `docs/superpowers/specs/2026-04-05-arazzo-workflow-update-design.md`
**Issue:** [#236](https://github.com/ericfitz/tmi/issues/236)

---

## Task Overview

1. Fix stale paths in existing `authenticated_endpoints` and `complete_workflow_sequences`
2. Add new `authenticated_endpoints` categories (admin, surveys, teams, projects, chat, user self-service, audit trail, restore, config, webhook-deliveries)
3. Add new `complete_workflow_sequences` (8 new sequences)
4. Update `notes` section for new resource types
5. Update enhance script `prereq_map` and path parameter dependency detection
6. Regenerate Arazzo files and validate
7. Verify no stale operationIds remain
8. Commit

---

### Task 1: Fix stale paths in existing sections

**Files:**
- Modify: `api-schema/api-workflows.json`

The existing `authenticated_endpoints.webhooks` section uses old paths (`/webhooks/...` instead of `/admin/webhooks/...`). The `invocations` section references paths that no longer exist. The `complete_workflow_sequences.webhook_workflow` uses stale paths too.

- [ ] **Step 1: Update webhook paths from `/webhooks/...` to `/admin/webhooks/...`**

Use jq to update the webhooks section. Replace the entire `authenticated_endpoints.webhooks` with corrected paths:

```bash
jq '.authenticated_endpoints.webhooks = {
  "listWebhookSubscriptions": {
    "action": "GET /admin/webhooks/subscriptions",
    "description": "List webhook subscriptions",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createWebhookSubscription": {
    "action": "POST /admin/webhooks/subscriptions",
    "description": "Create webhook subscription",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getWebhookSubscription": {
    "action": "GET /admin/webhooks/subscriptions/{webhook_id}",
    "description": "Get webhook subscription",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "deleteWebhookSubscription": {
    "action": "DELETE /admin/webhooks/subscriptions/{webhook_id}",
    "description": "Delete webhook subscription",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "testWebhookSubscription": {
    "action": "POST /admin/webhooks/subscriptions/{webhook_id}/test",
    "description": "Test webhook subscription",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "listWebhookDeliveries": {
    "action": "GET /admin/webhooks/deliveries",
    "description": "List webhook deliveries",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getWebhookDelivery": {
    "action": "GET /admin/webhooks/deliveries/{delivery_id}",
    "description": "Get webhook delivery details",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 2: Remove stale `invocations` section and add `webhook_deliveries`**

The `/invocations` endpoints no longer exist. Replace with the new `/webhook-deliveries/` endpoints:

```bash
jq 'del(.authenticated_endpoints.invocations) | .authenticated_endpoints.webhook_deliveries = {
  "getWebhookDeliveryPublic": {
    "action": "GET /webhook-deliveries/{delivery_id}",
    "description": "Get webhook delivery (for webhook listeners)",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateWebhookDeliveryStatus": {
    "action": "PUT /webhook-deliveries/{delivery_id}/status",
    "description": "Update webhook delivery status (for webhook listeners)",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 3: Fix webhook_workflow sequence paths**

```bash
jq '.complete_workflow_sequences.webhook_workflow = [
  {"step": 1, "action": "GET /oauth2/authorize", "description": "Start OAuth", "auth_required": false},
  {"step": 2, "action": "GET /oauth2/callback", "description": "OAuth callback", "auth_required": false},
  {"step": 3, "action": "POST /oauth2/token", "description": "Get tokens", "auth_required": false, "body_required": true},
  {"step": 4, "action": "POST /admin/webhooks/subscriptions", "description": "Create webhook subscription", "auth_required": true, "body_required": true},
  {"step": 5, "action": "GET /admin/webhooks/subscriptions", "description": "List subscriptions", "auth_required": true},
  {"step": 6, "action": "POST /admin/webhooks/subscriptions/{webhook_id}/test", "description": "Test webhook", "auth_required": true, "body_required": true},
  {"step": 7, "action": "GET /admin/webhooks/deliveries", "description": "List webhook deliveries", "auth_required": true},
  {"step": 8, "action": "DELETE /admin/webhooks/subscriptions/{webhook_id}", "description": "Delete subscription", "auth_required": true}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 4: Validate JSON is well-formed**

```bash
jq empty api-schema/api-workflows.json && echo "Valid JSON" || echo "INVALID JSON"
```

Expected: `Valid JSON`

- [ ] **Step 5: Commit**

```bash
git add api-schema/api-workflows.json
git commit -m "fix(api): correct stale webhook paths and remove invocations in api-workflows.json

Fixes #236"
```

---

### Task 2: Add new `authenticated_endpoints` categories

**Files:**
- Modify: `api-schema/api-workflows.json`

Add all new endpoint categories. Due to the large size of the JSON, use jq to surgically add each category.

- [ ] **Step 1: Add `admin_users` category**

```bash
jq '.authenticated_endpoints.admin_users = {
  "listAdminUsers": {
    "action": "GET /admin/users",
    "description": "List all users",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createAutomationAccount": {
    "action": "POST /admin/users/automation",
    "description": "Create automation account",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getAdminUser": {
    "action": "GET /admin/users/{internal_uuid}",
    "description": "Get user details",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateAdminUser": {
    "action": "PATCH /admin/users/{internal_uuid}",
    "description": "Update user",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteAdminUser": {
    "action": "DELETE /admin/users/{internal_uuid}",
    "description": "Delete user",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "transferAdminUserOwnership": {
    "action": "POST /admin/users/{internal_uuid}/transfer",
    "description": "Transfer user ownership of resources",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 2: Add `admin_groups` category**

```bash
jq '.authenticated_endpoints.admin_groups = {
  "listAdminGroups": {
    "action": "GET /admin/groups",
    "description": "List groups",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createAdminGroup": {
    "action": "POST /admin/groups",
    "description": "Create group",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getAdminGroup": {
    "action": "GET /admin/groups/{internal_uuid}",
    "description": "Get group",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateAdminGroup": {
    "action": "PATCH /admin/groups/{internal_uuid}",
    "description": "Update group",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteAdminGroup": {
    "action": "DELETE /admin/groups/{internal_uuid}",
    "description": "Delete group",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listGroupMembers": {
    "action": "GET /admin/groups/{internal_uuid}/members",
    "description": "List group members",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "addGroupMember": {
    "action": "POST /admin/groups/{internal_uuid}/members",
    "description": "Add group member",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "removeGroupMember": {
    "action": "DELETE /admin/groups/{internal_uuid}/members/{member_uuid}",
    "description": "Remove group member",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 3: Add `admin_quotas` category**

```bash
jq '.authenticated_endpoints.admin_quotas = {
  "listUserAPIQuotas": {
    "action": "GET /admin/quotas/users",
    "description": "List user API quotas",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getUserAPIQuota": {
    "action": "GET /admin/quotas/users/{user_id}",
    "description": "Get user API quota",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateUserAPIQuota": {
    "action": "PUT /admin/quotas/users/{user_id}",
    "description": "Update user API quota",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteUserAPIQuota": {
    "action": "DELETE /admin/quotas/users/{user_id}",
    "description": "Delete user API quota",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listAddonInvocationQuotas": {
    "action": "GET /admin/quotas/addons",
    "description": "List addon invocation quotas",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getAddonInvocationQuota": {
    "action": "GET /admin/quotas/addons/{user_id}",
    "description": "Get addon invocation quota",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateAddonInvocationQuota": {
    "action": "PUT /admin/quotas/addons/{user_id}",
    "description": "Update addon invocation quota",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteAddonInvocationQuota": {
    "action": "DELETE /admin/quotas/addons/{user_id}",
    "description": "Delete addon invocation quota",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listWebhookQuotas": {
    "action": "GET /admin/quotas/webhooks",
    "description": "List webhook quotas",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getWebhookQuota": {
    "action": "GET /admin/quotas/webhooks/{user_id}",
    "description": "Get webhook quota",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateWebhookQuota": {
    "action": "PUT /admin/quotas/webhooks/{user_id}",
    "description": "Update webhook quota",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteWebhookQuota": {
    "action": "DELETE /admin/quotas/webhooks/{user_id}",
    "description": "Delete webhook quota",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 4: Add `admin_settings` category**

```bash
jq '.authenticated_endpoints.admin_settings = {
  "listSystemSettings": {
    "action": "GET /admin/settings",
    "description": "List system settings",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getSystemSetting": {
    "action": "GET /admin/settings/{key}",
    "description": "Get system setting by key",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateSystemSetting": {
    "action": "PUT /admin/settings/{key}",
    "description": "Update system setting",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteSystemSetting": {
    "action": "DELETE /admin/settings/{key}",
    "description": "Delete system setting",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "migrateSystemSettings": {
    "action": "POST /admin/settings/migrate",
    "description": "Migrate settings from config files to database",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "reencryptSystemSettings": {
    "action": "POST /admin/settings/reencrypt",
    "description": "Re-encrypt all encrypted settings",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 5: Add `admin_timmy` category**

```bash
jq '.authenticated_endpoints.admin_timmy = {
  "getTimmyStatus": {
    "action": "GET /admin/timmy/status",
    "description": "Get Timmy AI service status",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getTimmyUsage": {
    "action": "GET /admin/timmy/usage",
    "description": "Get Timmy AI usage statistics",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 6: Add `admin_client_credentials` category**

```bash
jq '.authenticated_endpoints.admin_client_credentials = {
  "listAdminUserClientCredentials": {
    "action": "GET /admin/users/{internal_uuid}/client_credentials",
    "description": "List client credentials for a user",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createAdminUserClientCredential": {
    "action": "POST /admin/users/{internal_uuid}/client_credentials",
    "description": "Create client credential for a user",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteAdminUserClientCredential": {
    "action": "DELETE /admin/users/{internal_uuid}/client_credentials/{credential_id}",
    "description": "Delete client credential for a user",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 7: Add `surveys_admin` category**

```bash
jq '.authenticated_endpoints.surveys_admin = {
  "listAdminSurveys": {
    "action": "GET /admin/surveys",
    "description": "List surveys",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createAdminSurvey": {
    "action": "POST /admin/surveys",
    "description": "Create survey",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getAdminSurvey": {
    "action": "GET /admin/surveys/{survey_id}",
    "description": "Get survey",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateAdminSurvey": {
    "action": "PUT /admin/surveys/{survey_id}",
    "description": "Update survey",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "patchAdminSurvey": {
    "action": "PATCH /admin/surveys/{survey_id}",
    "description": "Partially update survey",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteAdminSurvey": {
    "action": "DELETE /admin/surveys/{survey_id}",
    "description": "Delete survey",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getAdminSurveyMetadata": {
    "action": "GET /admin/surveys/{survey_id}/metadata",
    "description": "Get survey metadata",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createAdminSurveyMetadata": {
    "action": "POST /admin/surveys/{survey_id}/metadata",
    "description": "Create survey metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getAdminSurveyMetadataByKey": {
    "action": "GET /admin/surveys/{survey_id}/metadata/{key}",
    "description": "Get survey metadata by key",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateAdminSurveyMetadataByKey": {
    "action": "PUT /admin/surveys/{survey_id}/metadata/{key}",
    "description": "Update survey metadata by key",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteAdminSurveyMetadataByKey": {
    "action": "DELETE /admin/surveys/{survey_id}/metadata/{key}",
    "description": "Delete survey metadata by key",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "bulkCreateAdminSurveyMetadata": {
    "action": "POST /admin/surveys/{survey_id}/metadata/bulk",
    "description": "Bulk create survey metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "bulkReplaceAdminSurveyMetadata": {
    "action": "PUT /admin/surveys/{survey_id}/metadata/bulk",
    "description": "Bulk replace survey metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "bulkUpsertAdminSurveyMetadata": {
    "action": "PATCH /admin/surveys/{survey_id}/metadata/bulk",
    "description": "Bulk upsert survey metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 8: Add `surveys_intake` category**

```bash
jq '.authenticated_endpoints.surveys_intake = {
  "listIntakeSurveys": {
    "action": "GET /intake/surveys",
    "description": "List available surveys for intake",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getIntakeSurvey": {
    "action": "GET /intake/surveys/{survey_id}",
    "description": "Get survey for intake",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listIntakeSurveyResponses": {
    "action": "GET /intake/survey_responses",
    "description": "List survey responses",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createIntakeSurveyResponse": {
    "action": "POST /intake/survey_responses",
    "description": "Submit survey response",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getIntakeSurveyResponse": {
    "action": "GET /intake/survey_responses/{survey_response_id}",
    "description": "Get survey response",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateIntakeSurveyResponse": {
    "action": "PUT /intake/survey_responses/{survey_response_id}",
    "description": "Update survey response",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "patchIntakeSurveyResponse": {
    "action": "PATCH /intake/survey_responses/{survey_response_id}",
    "description": "Partially update survey response",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteIntakeSurveyResponse": {
    "action": "DELETE /intake/survey_responses/{survey_response_id}",
    "description": "Delete survey response",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getIntakeSurveyResponseMetadata": {
    "action": "GET /intake/survey_responses/{survey_response_id}/metadata",
    "description": "Get survey response metadata",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createIntakeSurveyResponseMetadata": {
    "action": "POST /intake/survey_responses/{survey_response_id}/metadata",
    "description": "Create survey response metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getIntakeSurveyResponseMetadataByKey": {
    "action": "GET /intake/survey_responses/{survey_response_id}/metadata/{key}",
    "description": "Get survey response metadata by key",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateIntakeSurveyResponseMetadataByKey": {
    "action": "PUT /intake/survey_responses/{survey_response_id}/metadata/{key}",
    "description": "Update survey response metadata by key",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteIntakeSurveyResponseMetadataByKey": {
    "action": "DELETE /intake/survey_responses/{survey_response_id}/metadata/{key}",
    "description": "Delete survey response metadata by key",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "bulkCreateIntakeSurveyResponseMetadata": {
    "action": "POST /intake/survey_responses/{survey_response_id}/metadata/bulk",
    "description": "Bulk create survey response metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "bulkReplaceIntakeSurveyResponseMetadata": {
    "action": "PUT /intake/survey_responses/{survey_response_id}/metadata/bulk",
    "description": "Bulk replace survey response metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "bulkUpsertIntakeSurveyResponseMetadata": {
    "action": "PATCH /intake/survey_responses/{survey_response_id}/metadata/bulk",
    "description": "Bulk upsert survey response metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "listIntakeSurveyResponseTriageNotes": {
    "action": "GET /intake/survey_responses/{survey_response_id}/triage_notes",
    "description": "List triage notes for survey response",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getIntakeSurveyResponseTriageNote": {
    "action": "GET /intake/survey_responses/{survey_response_id}/triage_notes/{triage_note_id}",
    "description": "Get triage note",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 9: Add `surveys_triage` category**

```bash
jq '.authenticated_endpoints.surveys_triage = {
  "listTriageSurveyResponses": {
    "action": "GET /triage/survey_responses",
    "description": "List survey responses for triage",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getTriageSurveyResponse": {
    "action": "GET /triage/survey_responses/{survey_response_id}",
    "description": "Get survey response for triage",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "patchTriageSurveyResponse": {
    "action": "PATCH /triage/survey_responses/{survey_response_id}",
    "description": "Update triage status",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getTriageSurveyResponseMetadata": {
    "action": "GET /triage/survey_responses/{survey_response_id}/metadata",
    "description": "Get triage response metadata",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getTriageSurveyResponseMetadataByKey": {
    "action": "GET /triage/survey_responses/{survey_response_id}/metadata/{key}",
    "description": "Get triage response metadata by key",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listTriageSurveyResponseTriageNotes": {
    "action": "GET /triage/survey_responses/{survey_response_id}/triage_notes",
    "description": "List triage notes",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createTriageSurveyResponseTriageNote": {
    "action": "POST /triage/survey_responses/{survey_response_id}/triage_notes",
    "description": "Create triage note",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getTriageSurveyResponseTriageNote": {
    "action": "GET /triage/survey_responses/{survey_response_id}/triage_notes/{triage_note_id}",
    "description": "Get triage note",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createThreatModelFromSurveyResponse": {
    "action": "POST /triage/survey_responses/{survey_response_id}/create_threat_model",
    "description": "Promote survey response to threat model",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 10: Add `teams` category**

```bash
jq '.authenticated_endpoints.teams = {
  "listTeams": {
    "action": "GET /teams",
    "description": "List teams",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createTeam": {
    "action": "POST /teams",
    "description": "Create team",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getTeam": {
    "action": "GET /teams/{team_id}",
    "description": "Get team",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateTeam": {
    "action": "PUT /teams/{team_id}",
    "description": "Update team",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "patchTeam": {
    "action": "PATCH /teams/{team_id}",
    "description": "Partially update team",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteTeam": {
    "action": "DELETE /teams/{team_id}",
    "description": "Delete team",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listTeamNotes": {
    "action": "GET /teams/{team_id}/notes",
    "description": "List team notes",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createTeamNote": {
    "action": "POST /teams/{team_id}/notes",
    "description": "Create team note",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getTeamNote": {
    "action": "GET /teams/{team_id}/notes/{team_note_id}",
    "description": "Get team note",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateTeamNote": {
    "action": "PUT /teams/{team_id}/notes/{team_note_id}",
    "description": "Update team note",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "patchTeamNote": {
    "action": "PATCH /teams/{team_id}/notes/{team_note_id}",
    "description": "Partially update team note",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteTeamNote": {
    "action": "DELETE /teams/{team_id}/notes/{team_note_id}",
    "description": "Delete team note",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getTeamMetadata": {
    "action": "GET /teams/{team_id}/metadata",
    "description": "Get team metadata",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createTeamMetadata": {
    "action": "POST /teams/{team_id}/metadata",
    "description": "Create team metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "updateTeamMetadata": {
    "action": "PUT /teams/{team_id}/metadata/{key}",
    "description": "Update team metadata by key",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteTeamMetadata": {
    "action": "DELETE /teams/{team_id}/metadata/{key}",
    "description": "Delete team metadata by key",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "bulkCreateTeamMetadata": {
    "action": "POST /teams/{team_id}/metadata/bulk",
    "description": "Bulk create team metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "bulkReplaceTeamMetadata": {
    "action": "PUT /teams/{team_id}/metadata/bulk",
    "description": "Bulk replace team metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "bulkUpsertTeamMetadata": {
    "action": "PATCH /teams/{team_id}/metadata/bulk",
    "description": "Bulk upsert team metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 11: Add `projects` category**

```bash
jq '.authenticated_endpoints.projects = {
  "listProjects": {
    "action": "GET /projects",
    "description": "List projects",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createProject": {
    "action": "POST /projects",
    "description": "Create project",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getProject": {
    "action": "GET /projects/{project_id}",
    "description": "Get project",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateProject": {
    "action": "PUT /projects/{project_id}",
    "description": "Update project",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "patchProject": {
    "action": "PATCH /projects/{project_id}",
    "description": "Partially update project",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteProject": {
    "action": "DELETE /projects/{project_id}",
    "description": "Delete project",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listProjectNotes": {
    "action": "GET /projects/{project_id}/notes",
    "description": "List project notes",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createProjectNote": {
    "action": "POST /projects/{project_id}/notes",
    "description": "Create project note",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "getProjectNote": {
    "action": "GET /projects/{project_id}/notes/{project_note_id}",
    "description": "Get project note",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "updateProjectNote": {
    "action": "PUT /projects/{project_id}/notes/{project_note_id}",
    "description": "Update project note",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "patchProjectNote": {
    "action": "PATCH /projects/{project_id}/notes/{project_note_id}",
    "description": "Partially update project note",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteProjectNote": {
    "action": "DELETE /projects/{project_id}/notes/{project_note_id}",
    "description": "Delete project note",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getProjectMetadata": {
    "action": "GET /projects/{project_id}/metadata",
    "description": "Get project metadata",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createProjectMetadata": {
    "action": "POST /projects/{project_id}/metadata",
    "description": "Create project metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "updateProjectMetadata": {
    "action": "PUT /projects/{project_id}/metadata/{key}",
    "description": "Update project metadata by key",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteProjectMetadata": {
    "action": "DELETE /projects/{project_id}/metadata/{key}",
    "description": "Delete project metadata by key",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "bulkCreateProjectMetadata": {
    "action": "POST /projects/{project_id}/metadata/bulk",
    "description": "Bulk create project metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "bulkReplaceProjectMetadata": {
    "action": "PUT /projects/{project_id}/metadata/bulk",
    "description": "Bulk replace project metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "bulkUpsertProjectMetadata": {
    "action": "PATCH /projects/{project_id}/metadata/bulk",
    "description": "Bulk upsert project metadata",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 12: Add `chat` category**

```bash
jq '.authenticated_endpoints.chat = {
  "listTimmyChatSessions": {
    "action": "GET /threat_models/{threat_model_id}/chat/sessions",
    "description": "List chat sessions for a threat model",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "createTimmyChatSession": {
    "action": "POST /threat_models/{threat_model_id}/chat/sessions",
    "description": "Create chat session",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "getTimmyChatSession": {
    "action": "GET /threat_models/{threat_model_id}/chat/sessions/{session_id}",
    "description": "Get chat session",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "deleteTimmyChatSession": {
    "action": "DELETE /threat_models/{threat_model_id}/chat/sessions/{session_id}",
    "description": "Delete chat session",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "listTimmyChatMessages": {
    "action": "GET /threat_models/{threat_model_id}/chat/sessions/{session_id}/messages",
    "description": "List chat messages in a session",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "createTimmyChatMessage": {
    "action": "POST /threat_models/{threat_model_id}/chat/sessions/{session_id}/messages",
    "description": "Send chat message",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete", "threat_model_create"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 13: Expand `user` category to `user_self_service`**

Replace the existing minimal `user` category with the full self-service set:

```bash
jq '.authenticated_endpoints.user = {
  "getCurrentUserProfile": {
    "action": "GET /me",
    "description": "Get current user profile",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "deleteUserAccount": {
    "action": "DELETE /me",
    "description": "Delete user account and all data",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getCurrentUserPreferences": {
    "action": "GET /me/preferences",
    "description": "Get user preferences",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createCurrentUserPreferences": {
    "action": "POST /me/preferences",
    "description": "Create user preferences",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "updateCurrentUserPreferences": {
    "action": "PUT /me/preferences",
    "description": "Update user preferences",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "listCurrentUserClientCredentials": {
    "action": "GET /me/client_credentials",
    "description": "List my client credentials",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "createCurrentUserClientCredential": {
    "action": "POST /me/client_credentials",
    "description": "Create client credential",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "deleteCurrentUserClientCredential": {
    "action": "DELETE /me/client_credentials/{credential_id}",
    "description": "Delete client credential",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "getCurrentUserSessions": {
    "action": "GET /me/sessions",
    "description": "List active sessions",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listMyGroups": {
    "action": "GET /me/groups",
    "description": "List my groups",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "listMyGroupMembers": {
    "action": "GET /me/groups/{internal_uuid}/members",
    "description": "List group members",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "transferCurrentUserOwnership": {
    "action": "POST /me/transfer",
    "description": "Transfer ownership of resources",
    "auth_required": true,
    "body_required": true,
    "prereqs": ["oauth_complete"]
  },
  "logoutCurrentUser": {
    "action": "POST /me/logout",
    "description": "Logout current user",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 14: Add `audit_trail` category**

```bash
jq '.authenticated_endpoints.audit_trail = {
  "getThreatModelAuditTrail": {
    "action": "GET /threat_models/{threat_model_id}/audit_trail",
    "description": "Get threat model audit trail",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "getAuditEntry": {
    "action": "GET /threat_models/{threat_model_id}/audit_trail/{entry_id}",
    "description": "Get specific audit trail entry",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "rollbackToVersion": {
    "action": "POST /threat_models/{threat_model_id}/audit_trail/{entry_id}/rollback",
    "description": "Rollback to a previous version",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "getAssetAuditTrail": {
    "action": "GET /threat_models/{threat_model_id}/assets/{asset_id}/audit_trail",
    "description": "Get asset audit trail",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create", "asset_create"]
  },
  "getDiagramAuditTrail": {
    "action": "GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/audit_trail",
    "description": "Get diagram audit trail",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create", "diagram_create"]
  },
  "getDocumentAuditTrail": {
    "action": "GET /threat_models/{threat_model_id}/documents/{document_id}/audit_trail",
    "description": "Get document audit trail",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create", "document_create"]
  },
  "getThreatAuditTrail": {
    "action": "GET /threat_models/{threat_model_id}/threats/{threat_id}/audit_trail",
    "description": "Get threat audit trail",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create", "threat_create"]
  },
  "getNoteAuditTrail": {
    "action": "GET /threat_models/{threat_model_id}/notes/{note_id}/audit_trail",
    "description": "Get note audit trail",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create", "note_create"]
  },
  "getRepositoryAuditTrail": {
    "action": "GET /threat_models/{threat_model_id}/repositories/{repository_id}/audit_trail",
    "description": "Get repository audit trail",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create", "repository_create"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 15: Add `restore` category**

```bash
jq '.authenticated_endpoints.restore = {
  "restoreThreatModel": {
    "action": "POST /threat_models/{threat_model_id}/restore",
    "description": "Restore soft-deleted threat model",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  },
  "restoreAsset": {
    "action": "POST /threat_models/{threat_model_id}/assets/{asset_id}/restore",
    "description": "Restore soft-deleted asset",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "restoreDiagram": {
    "action": "POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/restore",
    "description": "Restore soft-deleted diagram",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "restoreDocument": {
    "action": "POST /threat_models/{threat_model_id}/documents/{document_id}/restore",
    "description": "Restore soft-deleted document",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "restoreThreat": {
    "action": "POST /threat_models/{threat_model_id}/threats/{threat_id}/restore",
    "description": "Restore soft-deleted threat",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "restoreNote": {
    "action": "POST /threat_models/{threat_model_id}/notes/{note_id}/restore",
    "description": "Restore soft-deleted note",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  },
  "restoreRepository": {
    "action": "POST /threat_models/{threat_model_id}/repositories/{repository_id}/restore",
    "description": "Restore soft-deleted repository",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete", "threat_model_create"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 16: Add `config` and `saml_admin` categories**

```bash
jq '.authenticated_endpoints.config = {
  "getClientConfig": {
    "action": "GET /config",
    "description": "Get client configuration",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
} | .authenticated_endpoints.saml_admin = {
  "listSAMLUsers": {
    "action": "GET /saml/providers/{idp}/users",
    "description": "List SAML users for a provider",
    "auth_required": true,
    "body_required": false,
    "prereqs": ["oauth_complete"]
  }
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 17: Validate JSON**

```bash
jq empty api-schema/api-workflows.json && echo "Valid JSON" || echo "INVALID JSON"
```

Expected: `Valid JSON`

- [ ] **Step 18: Commit**

```bash
git add api-schema/api-workflows.json
git commit -m "feat(api): add all new endpoint categories to api-workflows.json

Adds: admin_users, admin_groups, admin_quotas, admin_settings, admin_timmy,
admin_client_credentials, surveys_admin, surveys_intake, surveys_triage,
teams, projects, chat, audit_trail, restore, config, saml_admin,
webhook_deliveries. Expands user to full self-service.

Fixes #236"
```

---

### Task 3: Add new `complete_workflow_sequences`

**Files:**
- Modify: `api-schema/api-workflows.json`

Add 8 new end-to-end workflow sequences. Each sequence starts with the OAuth flow (steps 1-3) then proceeds through the domain-specific operations.

- [ ] **Step 1: Add `survey_lifecycle` sequence**

```bash
jq '.complete_workflow_sequences.survey_lifecycle = [
  {"step": 1, "action": "GET /oauth2/authorize", "description": "Start OAuth", "auth_required": false},
  {"step": 2, "action": "GET /oauth2/callback", "description": "OAuth callback", "auth_required": false},
  {"step": 3, "action": "POST /oauth2/token", "description": "Get tokens", "auth_required": false, "body_required": true},
  {"step": 4, "action": "POST /admin/surveys", "description": "Admin creates survey", "auth_required": true, "body_required": true},
  {"step": 5, "action": "GET /admin/surveys/{survey_id}", "description": "Admin gets survey", "auth_required": true},
  {"step": 6, "action": "POST /admin/surveys/{survey_id}/metadata", "description": "Admin adds survey metadata", "auth_required": true, "body_required": true},
  {"step": 7, "action": "GET /intake/surveys", "description": "Intake lists available surveys", "auth_required": true},
  {"step": 8, "action": "POST /intake/survey_responses", "description": "Intake submits survey response", "auth_required": true, "body_required": true},
  {"step": 9, "action": "POST /intake/survey_responses/{survey_response_id}/metadata", "description": "Intake adds response metadata", "auth_required": true, "body_required": true},
  {"step": 10, "action": "GET /triage/survey_responses", "description": "Triage lists responses", "auth_required": true},
  {"step": 11, "action": "PATCH /triage/survey_responses/{survey_response_id}", "description": "Triage updates status", "auth_required": true, "body_required": true},
  {"step": 12, "action": "POST /triage/survey_responses/{survey_response_id}/triage_notes", "description": "Triage adds note", "auth_required": true, "body_required": true},
  {"step": 13, "action": "POST /triage/survey_responses/{survey_response_id}/create_threat_model", "description": "Promote to threat model", "auth_required": true, "body_required": true},
  {"step": 14, "action": "DELETE /admin/surveys/{survey_id}", "description": "Admin deletes survey", "auth_required": true}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 2: Add `team_project_management` sequence**

```bash
jq '.complete_workflow_sequences.team_project_management = [
  {"step": 1, "action": "GET /oauth2/authorize", "description": "Start OAuth", "auth_required": false},
  {"step": 2, "action": "GET /oauth2/callback", "description": "OAuth callback", "auth_required": false},
  {"step": 3, "action": "POST /oauth2/token", "description": "Get tokens", "auth_required": false, "body_required": true},
  {"step": 4, "action": "POST /teams", "description": "Create team", "auth_required": true, "body_required": true},
  {"step": 5, "action": "PUT /teams/{team_id}", "description": "Update team", "auth_required": true, "body_required": true},
  {"step": 6, "action": "POST /teams/{team_id}/notes", "description": "Add team note", "auth_required": true, "body_required": true},
  {"step": 7, "action": "POST /teams/{team_id}/metadata", "description": "Add team metadata", "auth_required": true, "body_required": true},
  {"step": 8, "action": "POST /projects", "description": "Create project", "auth_required": true, "body_required": true},
  {"step": 9, "action": "PUT /projects/{project_id}", "description": "Update project", "auth_required": true, "body_required": true},
  {"step": 10, "action": "POST /projects/{project_id}/notes", "description": "Add project note", "auth_required": true, "body_required": true},
  {"step": 11, "action": "POST /projects/{project_id}/metadata", "description": "Add project metadata", "auth_required": true, "body_required": true},
  {"step": 12, "action": "GET /teams", "description": "List teams", "auth_required": true},
  {"step": 13, "action": "GET /projects", "description": "List projects", "auth_required": true},
  {"step": 14, "action": "DELETE /projects/{project_id}", "description": "Delete project", "auth_required": true},
  {"step": 15, "action": "DELETE /teams/{team_id}", "description": "Delete team", "auth_required": true}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 3: Add `chat_session_workflow` sequence**

```bash
jq '.complete_workflow_sequences.chat_session_workflow = [
  {"step": 1, "action": "GET /oauth2/authorize", "description": "Start OAuth", "auth_required": false},
  {"step": 2, "action": "GET /oauth2/callback", "description": "OAuth callback", "auth_required": false},
  {"step": 3, "action": "POST /oauth2/token", "description": "Get tokens", "auth_required": false, "body_required": true},
  {"step": 4, "action": "POST /threat_models", "description": "Create threat model", "auth_required": true, "body_required": true},
  {"step": 5, "action": "POST /threat_models/{threat_model_id}/chat/sessions", "description": "Create chat session", "auth_required": true, "body_required": true},
  {"step": 6, "action": "POST /threat_models/{threat_model_id}/chat/sessions/{session_id}/messages", "description": "Send chat message", "auth_required": true, "body_required": true},
  {"step": 7, "action": "GET /threat_models/{threat_model_id}/chat/sessions/{session_id}/messages", "description": "List chat messages", "auth_required": true},
  {"step": 8, "action": "GET /threat_models/{threat_model_id}/chat/sessions/{session_id}", "description": "Get chat session", "auth_required": true},
  {"step": 9, "action": "GET /threat_models/{threat_model_id}/chat/sessions", "description": "List chat sessions", "auth_required": true},
  {"step": 10, "action": "DELETE /threat_models/{threat_model_id}/chat/sessions/{session_id}", "description": "Delete chat session", "auth_required": true}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 4: Add `admin_user_management` sequence**

```bash
jq '.complete_workflow_sequences.admin_user_management = [
  {"step": 1, "action": "GET /oauth2/authorize", "description": "Start OAuth", "auth_required": false},
  {"step": 2, "action": "GET /oauth2/callback", "description": "OAuth callback", "auth_required": false},
  {"step": 3, "action": "POST /oauth2/token", "description": "Get tokens", "auth_required": false, "body_required": true},
  {"step": 4, "action": "GET /admin/users", "description": "List users", "auth_required": true},
  {"step": 5, "action": "POST /admin/users/automation", "description": "Create automation account", "auth_required": true, "body_required": true},
  {"step": 6, "action": "GET /admin/users/{internal_uuid}", "description": "Get user", "auth_required": true},
  {"step": 7, "action": "PATCH /admin/users/{internal_uuid}", "description": "Update user", "auth_required": true, "body_required": true},
  {"step": 8, "action": "POST /admin/users/{internal_uuid}/client_credentials", "description": "Create client credential", "auth_required": true, "body_required": true},
  {"step": 9, "action": "GET /admin/users/{internal_uuid}/client_credentials", "description": "List credentials", "auth_required": true},
  {"step": 10, "action": "DELETE /admin/users/{internal_uuid}/client_credentials/{credential_id}", "description": "Delete credential", "auth_required": true},
  {"step": 11, "action": "POST /admin/groups", "description": "Create group", "auth_required": true, "body_required": true},
  {"step": 12, "action": "POST /admin/groups/{internal_uuid}/members", "description": "Add member to group", "auth_required": true, "body_required": true},
  {"step": 13, "action": "GET /admin/groups/{internal_uuid}/members", "description": "List group members", "auth_required": true},
  {"step": 14, "action": "PUT /admin/quotas/users/{user_id}", "description": "Set user quota", "auth_required": true, "body_required": true},
  {"step": 15, "action": "DELETE /admin/users/{internal_uuid}", "description": "Delete user", "auth_required": true}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 5: Add `user_self_service_workflow` sequence**

```bash
jq '.complete_workflow_sequences.user_self_service_workflow = [
  {"step": 1, "action": "GET /oauth2/authorize", "description": "Start OAuth", "auth_required": false},
  {"step": 2, "action": "GET /oauth2/callback", "description": "OAuth callback", "auth_required": false},
  {"step": 3, "action": "POST /oauth2/token", "description": "Get tokens", "auth_required": false, "body_required": true},
  {"step": 4, "action": "GET /me", "description": "Get user profile", "auth_required": true},
  {"step": 5, "action": "POST /me/preferences", "description": "Create preferences", "auth_required": true, "body_required": true},
  {"step": 6, "action": "PUT /me/preferences", "description": "Update preferences", "auth_required": true, "body_required": true},
  {"step": 7, "action": "POST /me/client_credentials", "description": "Create client credential", "auth_required": true, "body_required": true},
  {"step": 8, "action": "GET /me/client_credentials", "description": "List credentials", "auth_required": true},
  {"step": 9, "action": "DELETE /me/client_credentials/{credential_id}", "description": "Delete credential", "auth_required": true},
  {"step": 10, "action": "GET /me/sessions", "description": "List sessions", "auth_required": true},
  {"step": 11, "action": "GET /me/groups", "description": "List groups", "auth_required": true},
  {"step": 12, "action": "POST /me/logout", "description": "Logout", "auth_required": true}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 6: Add `saml_authentication` sequence**

```bash
jq '.complete_workflow_sequences.saml_authentication = [
  {"step": 1, "action": "GET /saml/providers", "description": "List SAML providers", "auth_required": false},
  {"step": 2, "action": "GET /saml/{provider}/login", "description": "Initiate SAML login", "auth_required": false},
  {"step": 3, "action": "POST /saml/acs", "description": "Process SAML assertion", "auth_required": false, "body_required": true},
  {"step": 4, "action": "GET /saml/{provider}/metadata", "description": "Get SP metadata", "auth_required": false},
  {"step": 5, "action": "GET /saml/providers/{idp}/users", "description": "List SAML users", "auth_required": true},
  {"step": 6, "action": "GET /saml/slo", "description": "SAML single logout", "auth_required": false}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 7: Add `audit_trail_and_restore` sequence**

```bash
jq '.complete_workflow_sequences.audit_trail_and_restore = [
  {"step": 1, "action": "GET /oauth2/authorize", "description": "Start OAuth", "auth_required": false},
  {"step": 2, "action": "GET /oauth2/callback", "description": "OAuth callback", "auth_required": false},
  {"step": 3, "action": "POST /oauth2/token", "description": "Get tokens", "auth_required": false, "body_required": true},
  {"step": 4, "action": "POST /threat_models", "description": "Create threat model", "auth_required": true, "body_required": true},
  {"step": 5, "action": "POST /threat_models/{threat_model_id}/assets", "description": "Create asset", "auth_required": true, "body_required": true},
  {"step": 6, "action": "POST /threat_models/{threat_model_id}/documents", "description": "Create document", "auth_required": true, "body_required": true},
  {"step": 7, "action": "GET /threat_models/{threat_model_id}/audit_trail", "description": "Get TM audit trail", "auth_required": true},
  {"step": 8, "action": "GET /threat_models/{threat_model_id}/assets/{asset_id}/audit_trail", "description": "Get asset audit trail", "auth_required": true},
  {"step": 9, "action": "GET /threat_models/{threat_model_id}/audit_trail/{entry_id}", "description": "Get audit entry", "auth_required": true},
  {"step": 10, "action": "POST /threat_models/{threat_model_id}/audit_trail/{entry_id}/rollback", "description": "Rollback to version", "auth_required": true},
  {"step": 11, "action": "DELETE /threat_models/{threat_model_id}/assets/{asset_id}", "description": "Delete asset (soft)", "auth_required": true},
  {"step": 12, "action": "POST /threat_models/{threat_model_id}/assets/{asset_id}/restore", "description": "Restore asset", "auth_required": true},
  {"step": 13, "action": "POST /threat_models/{threat_model_id}/restore", "description": "Restore threat model", "auth_required": true}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 8: Add `admin_settings_management` sequence**

```bash
jq '.complete_workflow_sequences.admin_settings_management = [
  {"step": 1, "action": "GET /oauth2/authorize", "description": "Start OAuth", "auth_required": false},
  {"step": 2, "action": "GET /oauth2/callback", "description": "OAuth callback", "auth_required": false},
  {"step": 3, "action": "POST /oauth2/token", "description": "Get tokens", "auth_required": false, "body_required": true},
  {"step": 4, "action": "GET /admin/settings", "description": "List settings", "auth_required": true},
  {"step": 5, "action": "GET /admin/settings/{key}", "description": "Get setting", "auth_required": true},
  {"step": 6, "action": "PUT /admin/settings/{key}", "description": "Update setting", "auth_required": true, "body_required": true},
  {"step": 7, "action": "DELETE /admin/settings/{key}", "description": "Delete setting", "auth_required": true},
  {"step": 8, "action": "POST /admin/settings/migrate", "description": "Migrate settings", "auth_required": true},
  {"step": 9, "action": "POST /admin/settings/reencrypt", "description": "Re-encrypt settings", "auth_required": true},
  {"step": 10, "action": "GET /admin/timmy/status", "description": "Get Timmy status", "auth_required": true}
]' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 9: Validate JSON**

```bash
jq empty api-schema/api-workflows.json && echo "Valid JSON" || echo "INVALID JSON"
```

Expected: `Valid JSON`

- [ ] **Step 10: Commit**

```bash
git add api-schema/api-workflows.json
git commit -m "feat(api): add 8 new complete workflow sequences to api-workflows.json

Adds: survey_lifecycle, team_project_management, chat_session_workflow,
admin_user_management, user_self_service_workflow, saml_authentication,
audit_trail_and_restore, admin_settings_management.

Fixes #236"
```

---

### Task 4: Update `notes` section

**Files:**
- Modify: `api-schema/api-workflows.json`

Update the notes section to reflect the expanded API surface.

- [ ] **Step 1: Update notes**

```bash
jq '.notes = {
  "oauth_flow": "The API uses OAuth 2.0 authorization code flow with PKCE (RFC 7636). Requires: (1) /oauth2/authorize to get code, (2) /oauth2/token to exchange code+verifier for tokens.",
  "authentication": "All endpoints under /threat_models, /admin, /intake, /triage, /teams, /projects, /me, /addons, and /config require Bearer token authentication using JWT tokens obtained from the OAuth flow.",
  "hierarchies": "The API follows hierarchical structure: threat_model -> {threats, diagrams, documents, assets, notes, repositories} -> metadata. Child objects cannot exist without parent objects. Audit trails and restore are available for all hierarchical resources.",
  "websockets": "Real-time collaboration available for diagrams via WebSocket at /ws/diagrams/{id} endpoint (not included in REST API workflow).",
  "bulk_operations": "Many endpoints support bulk operations (POST/PUT/PATCH /bulk) for efficiency with multiple objects.",
  "metadata_pattern": "All primary objects support metadata operations: list, create, get_key, set_key, delete_key, bulk_create/bulk_replace/bulk_upsert.",
  "pkce_security": "PKCE (Proof Key for Code Exchange) is mandatory for OAuth flows. Generate code_verifier (43-128 chars), compute code_challenge = base64url(sha256(code_verifier)).",
  "survey_workflow": "Surveys follow a lifecycle: admin creates survey -> intake submits responses -> triage reviews and promotes to threat model.",
  "admin_operations": "Admin endpoints under /admin/ cover users, groups, quotas, settings, webhooks, surveys, and Timmy AI management.",
  "saml_authentication": "SAML 2.0 is supported as an alternative to OAuth, with endpoints for SSO login, ACS, SLO, and SP metadata.",
  "audit_and_restore": "All threat model resources support soft-delete with restore capability, plus audit trails with rollback.",
  "client_credentials": "Machine-to-machine authentication via client credentials grant (RFC 6749 Section 4.4), manageable via /me/ and /admin/ endpoints."
}' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 2: Update version**

```bash
jq '.version = "3.0.0"' api-schema/api-workflows.json > /tmp/workflows.json && mv /tmp/workflows.json api-schema/api-workflows.json
```

- [ ] **Step 3: Validate and commit**

```bash
jq empty api-schema/api-workflows.json && echo "Valid JSON" || echo "INVALID JSON"
git add api-schema/api-workflows.json
git commit -m "chore(api): update api-workflows.json notes and version to 3.0.0"
```

---

### Task 5: Update enhance script prerequisites and dependency detection

**Files:**
- Modify: `scripts/enhance-arazzo-with-workflows.py`

Add new entries to `prereq_map` and extend path parameter dependency detection in `_add_complete_sequences()`.

- [ ] **Step 1: Add new prereq_map entries**

In `scripts/enhance-arazzo-with-workflows.py`, find the `self.prereq_map` dict (around line 43) and add new entries after the existing ones. Use the Edit tool to add these entries after `"diagram_collaboration_start_session": "start_collaboration_session",`:

```python
            "survey_create": "create_admin_survey",
            "survey_response_create": "create_intake_survey_response",
            "team_create": "create_team",
            "project_create": "create_project",
            "chat_session_create": "create_chat_session",
            "admin_group_create": "create_admin_group",
            "automation_account_create": "create_automation_account",
            "triage_note_create": "create_triage_note",
```

- [ ] **Step 2: Add new path parameter dependency detection**

In `_add_complete_sequences()` (around line 394-421), after the existing `{repository_id}` check, add new path parameter dependency checks:

```python
                if "{survey_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_survey")
                    )
                if "{survey_response_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_survey_response")
                    )
                if "{team_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_team")
                    )
                if "{project_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_project")
                    )
                if "{session_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_session")
                    )
                if "{internal_uuid}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_user")
                    )
                if "{credential_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_credential")
                    )
                if "{entry_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_get_audit_trail")
                    )
                if "{team_note_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_team_note")
                    )
                if "{project_note_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_project_note")
                    )
                if "{triage_note_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_triage_note")
                    )
                if "{delivery_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_list_webhook_delivery")
                    )
                if "{webhook_id}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_create_webhook")
                    )
                if "{member_uuid}" in path:
                    dependencies.append(
                        self._sanitize_id(f"{workflow_prefix}_tmi_list_group_member")
                    )
```

- [ ] **Step 3: Add sample payloads for new resource types**

In `_generate_sample_payload()` (around line 535-668), add new payload generators before the generic fallback `return {}`:

```python
        # Surveys
        if "/admin/surveys" in path and method == "POST" and "metadata" not in path:
            return {
                "name": "Sample Survey",
                "description": "Generated for Arazzo workflow testing",
            }

        # Survey responses
        if "/survey_responses" in path and method == "POST" and "metadata" not in path and "triage_notes" not in path and "create_threat_model" not in path:
            return {
                "survey_id": "$steps.create_survey.outputs.survey_id",
                "answers": {"q1": "answer1"},
            }

        # Triage notes
        if "/triage_notes" in path and method == "POST":
            return {
                "content": "Triage review note",
            }

        # Create threat model from survey
        if "/create_threat_model" in path and method == "POST":
            return {
                "name": "Threat Model from Survey",
            }

        # Teams
        if path.startswith("/teams") and method == "POST" and "notes" not in path and "metadata" not in path:
            return {
                "name": "Sample Team",
                "description": "Test team",
            }

        # Team notes
        if "/teams" in path and "/notes" in path and method == "POST":
            return {
                "content": "Team note content",
            }

        # Projects
        if path.startswith("/projects") and method == "POST" and "notes" not in path and "metadata" not in path:
            return {
                "name": "Sample Project",
                "description": "Test project",
            }

        # Project notes
        if "/projects" in path and "/notes" in path and method == "POST":
            return {
                "content": "Project note content",
            }

        # Chat sessions
        if "/chat/sessions" in path and method == "POST" and "messages" not in path:
            return {
                "name": "Chat session",
            }

        # Chat messages
        if "/chat/sessions" in path and "/messages" in path and method == "POST":
            return {
                "content": "Hello, Timmy",
            }

        # Automation accounts
        if "/admin/users/automation" in path and method == "POST":
            return {
                "name": "automation-bot",
                "description": "Test automation account",
            }

        # Client credentials
        if "/client_credentials" in path and method == "POST":
            return {
                "name": "API Key",
                "description": "Test credential",
            }

        # Groups
        if "/admin/groups" in path and method == "POST" and "members" not in path:
            return {
                "name": "Sample Group",
                "description": "Test group",
            }

        # Group members
        if "/members" in path and method == "POST":
            return {
                "user_id": "$steps.create_user.outputs.user_id",
            }

        # Settings
        if "/admin/settings" in path and method == "PUT":
            return {
                "value": "sample_setting_value",
            }

        # Quotas
        if "/admin/quotas" in path and method == "PUT":
            return {
                "limit": 1000,
            }

        # Webhook delivery status
        if "/webhook-deliveries" in path and "/status" in path and method == "PUT":
            return {
                "status": "acknowledged",
            }
```

- [ ] **Step 4: Commit**

```bash
git add scripts/enhance-arazzo-with-workflows.py
git commit -m "feat(api): update enhance script with new prereqs, dependencies, and payloads

Adds prereq_map entries for surveys, teams, projects, chat, admin resources.
Adds path parameter dependency detection for 14 new ID types.
Adds sample payloads for all new resource types.

Fixes #236"
```

---

### Task 6: Regenerate and validate Arazzo files

**Files:**
- Generated: `api-schema/tmi.arazzo.yaml`, `api-schema/tmi.arazzo.json`

- [ ] **Step 1: Regenerate Arazzo files**

```bash
make generate-arazzo
```

Expected: Script completes successfully, prints workflow and step counts.

- [ ] **Step 2: Validate Arazzo files**

```bash
make validate-arazzo
```

Expected: `Arazzo specifications are valid` — 0 errors.

- [ ] **Step 3: Check for stale operationIds**

Run a comparison to verify all Arazzo operationIds exist in the OpenAPI spec:

```bash
# Get operationIds from OpenAPI
jq -r '[.paths | to_entries[] | .value | to_entries[] | .value.operationId // empty] | sort[]' api-schema/tmi-openapi.json > /tmp/openapi-opids.txt

# Get operationIds from Arazzo (strip sourceDescription prefix)
jq -r '[.workflows[].steps[]? | .operationId // empty] | unique | sort | .[] | sub("\\$sourceDescriptions\\.tmi-openapi\\."; "")' api-schema/tmi.arazzo.json > /tmp/arazzo-opids.txt

# Find stale references
echo "=== Stale Arazzo operationIds (NOT in OpenAPI) ==="
comm -13 /tmp/openapi-opids.txt /tmp/arazzo-opids.txt
```

Expected: Empty output (no stale references).

- [ ] **Step 4: Count workflows and steps**

```bash
echo "Workflows: $(jq '[.workflows[].workflowId] | length' api-schema/tmi.arazzo.json)"
echo "Steps: $(jq '[.workflows[].steps[]?] | length' api-schema/tmi.arazzo.json)"
```

Expected: More than the previous 308 workflows (8 new complete sequences added).

- [ ] **Step 5: Commit generated files**

```bash
git add api-schema/tmi.arazzo.yaml api-schema/tmi.arazzo.json
git commit -m "chore(api): regenerate Arazzo files with complete 1.4.x API coverage

Closes #236"
```

---

### Task 7: Final verification and cleanup

- [ ] **Step 1: Run full validation suite**

```bash
make validate-openapi
make validate-arazzo
```

Both should pass.

- [ ] **Step 2: Verify scaffold files are up to date**

```bash
ls -la api-schema/arazzo/scaffolds/base-scaffold.arazzo.yaml
```

Confirm the scaffold was regenerated (timestamp should be current).

- [ ] **Step 3: Close issue**

```bash
gh issue close 236 --repo ericfitz/tmi --reason completed
```
