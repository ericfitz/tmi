# Design: Update Arazzo Workflows for 1.3.x/1.4.x API Changes

**Issue:** [#236](https://github.com/ericfitz/tmi/issues/236)
**Date:** 2026-04-05
**Branch:** dev/1.4.0

## Problem

The TMI API has expanded significantly since the Arazzo workflow files were last generated. The `api-workflows.json` knowledge base covers only 7 end-to-end workflow sequences and is missing 8 endpoint categories added in 1.3.x and 1.4.x. The Arazzo files contain 44 stale operationId references that no longer exist in the current OpenAPI spec.

## Approach

Update `api-workflows.json` first with all new endpoint categories and workflow sequences, then update the enhance script's prerequisite map and path parameter detection, then regenerate and validate.

## Changes

### 1. `api-workflows.json` — New `authenticated_endpoints` categories

Add the following categories alongside existing ones (`addons`, `collaboration`, `other`, `user`, `webhooks`):

#### `admin_users` (6 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listAdminUsers | GET /admin/users | List all users |
| getAdminUser | GET /admin/users/{internal_uuid} | Get user details |
| updateAdminUser | PATCH /admin/users/{internal_uuid} | Update user |
| deleteAdminUser | DELETE /admin/users/{internal_uuid} | Delete user |
| transferAdminUserOwnership | POST /admin/users/{internal_uuid}/transfer | Transfer ownership |
| createAutomationAccount | POST /admin/users/automation | Create automation account |

#### `admin_groups` (8 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listAdminGroups | GET /admin/groups | List groups |
| createAdminGroup | POST /admin/groups | Create group |
| getAdminGroup | GET /admin/groups/{internal_uuid} | Get group |
| updateAdminGroup | PATCH /admin/groups/{internal_uuid} | Update group |
| deleteAdminGroup | DELETE /admin/groups/{internal_uuid} | Delete group |
| listGroupMembers | GET /admin/groups/{internal_uuid}/members | List members |
| addGroupMember | POST /admin/groups/{internal_uuid}/members | Add member |
| removeGroupMember | DELETE /admin/groups/{internal_uuid}/members/{member_uuid} | Remove member |

#### `admin_quotas` (12 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listUserAPIQuotas | GET /admin/quotas/users | List user API quotas |
| getUserAPIQuota | GET /admin/quotas/users/{user_id} | Get user quota |
| updateUserAPIQuota | PUT /admin/quotas/users/{user_id} | Update user quota |
| deleteUserAPIQuota | DELETE /admin/quotas/users/{user_id} | Delete user quota |
| listAddonInvocationQuotas | GET /admin/quotas/addons | List addon quotas |
| getAddonInvocationQuota | GET /admin/quotas/addons/{user_id} | Get addon quota |
| updateAddonInvocationQuota | PUT /admin/quotas/addons/{user_id} | Update addon quota |
| deleteAddonInvocationQuota | DELETE /admin/quotas/addons/{user_id} | Delete addon quota |
| listWebhookQuotas | GET /admin/quotas/webhooks | List webhook quotas |
| getWebhookQuota | GET /admin/quotas/webhooks/{user_id} | Get webhook quota |
| updateWebhookQuota | PUT /admin/quotas/webhooks/{user_id} | Update webhook quota |
| deleteWebhookQuota | DELETE /admin/quotas/webhooks/{user_id} | Delete webhook quota |

#### `admin_settings` (6 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listSystemSettings | GET /admin/settings | List settings |
| getSystemSetting | GET /admin/settings/{key} | Get setting |
| updateSystemSetting | PUT /admin/settings/{key} | Update setting |
| deleteSystemSetting | DELETE /admin/settings/{key} | Delete setting |
| migrateSystemSettings | POST /admin/settings/migrate | Migrate settings from config |
| reencryptSystemSettings | POST /admin/settings/reencrypt | Re-encrypt settings |

#### `admin_timmy` (2 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| getTimmyStatus | GET /admin/timmy/status | Get Timmy service status |
| getTimmyUsage | GET /admin/timmy/usage | Get Timmy usage stats |

#### `admin_client_credentials` (3 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listAdminUserClientCredentials | GET /admin/users/{internal_uuid}/client_credentials | List user credentials |
| createAdminUserClientCredential | POST /admin/users/{internal_uuid}/client_credentials | Create credential |
| deleteAdminUserClientCredential | DELETE /admin/users/{internal_uuid}/client_credentials/{credential_id} | Delete credential |

#### `surveys_admin` (14 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listAdminSurveys | GET /admin/surveys | List surveys |
| createAdminSurvey | POST /admin/surveys | Create survey |
| getAdminSurvey | GET /admin/surveys/{survey_id} | Get survey |
| updateAdminSurvey | PUT /admin/surveys/{survey_id} | Update survey |
| patchAdminSurvey | PATCH /admin/surveys/{survey_id} | Patch survey |
| deleteAdminSurvey | DELETE /admin/surveys/{survey_id} | Delete survey |
| getAdminSurveyMetadata | GET /admin/surveys/{survey_id}/metadata | Get metadata |
| createAdminSurveyMetadata | POST /admin/surveys/{survey_id}/metadata | Create metadata |
| getAdminSurveyMetadataByKey | GET /admin/surveys/{survey_id}/metadata/{key} | Get metadata by key |
| updateAdminSurveyMetadataByKey | PUT /admin/surveys/{survey_id}/metadata/{key} | Update metadata key |
| deleteAdminSurveyMetadataByKey | DELETE /admin/surveys/{survey_id}/metadata/{key} | Delete metadata key |
| bulkCreateAdminSurveyMetadata | POST /admin/surveys/{survey_id}/metadata/bulk | Bulk create metadata |
| bulkReplaceAdminSurveyMetadata | PUT /admin/surveys/{survey_id}/metadata/bulk | Bulk replace metadata |
| bulkUpsertAdminSurveyMetadata | PATCH /admin/surveys/{survey_id}/metadata/bulk | Bulk upsert metadata |

#### `surveys_intake` (18 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listIntakeSurveys | GET /intake/surveys | List available surveys |
| getIntakeSurvey | GET /intake/surveys/{survey_id} | Get survey |
| listIntakeSurveyResponses | GET /intake/survey_responses | List responses |
| createIntakeSurveyResponse | POST /intake/survey_responses | Submit response |
| getIntakeSurveyResponse | GET /intake/survey_responses/{survey_response_id} | Get response |
| updateIntakeSurveyResponse | PUT /intake/survey_responses/{survey_response_id} | Update response |
| patchIntakeSurveyResponse | PATCH /intake/survey_responses/{survey_response_id} | Patch response |
| deleteIntakeSurveyResponse | DELETE /intake/survey_responses/{survey_response_id} | Delete response |
| getIntakeSurveyResponseMetadata | GET /intake/survey_responses/{survey_response_id}/metadata | Get metadata |
| createIntakeSurveyResponseMetadata | POST /intake/survey_responses/{survey_response_id}/metadata | Create metadata |
| getIntakeSurveyResponseMetadataByKey | GET /intake/survey_responses/{survey_response_id}/metadata/{key} | Get metadata by key |
| updateIntakeSurveyResponseMetadataByKey | PUT /intake/survey_responses/{survey_response_id}/metadata/{key} | Update metadata key |
| deleteIntakeSurveyResponseMetadataByKey | DELETE /intake/survey_responses/{survey_response_id}/metadata/{key} | Delete metadata key |
| bulkCreateIntakeSurveyResponseMetadata | POST /intake/survey_responses/{survey_response_id}/metadata/bulk | Bulk create metadata |
| bulkReplaceIntakeSurveyResponseMetadata | PUT /intake/survey_responses/{survey_response_id}/metadata/bulk | Bulk replace metadata |
| bulkUpsertIntakeSurveyResponseMetadata | PATCH /intake/survey_responses/{survey_response_id}/metadata/bulk | Bulk upsert metadata |
| listIntakeSurveyResponseTriageNotes | GET /intake/survey_responses/{survey_response_id}/triage_notes | List triage notes |
| getIntakeSurveyResponseTriageNote | GET /intake/survey_responses/{survey_response_id}/triage_notes/{triage_note_id} | Get triage note |

#### `surveys_triage` (9 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listTriageSurveyResponses | GET /triage/survey_responses | List responses for triage |
| getTriageSurveyResponse | GET /triage/survey_responses/{survey_response_id} | Get response |
| patchTriageSurveyResponse | PATCH /triage/survey_responses/{survey_response_id} | Update triage status |
| getTriageSurveyResponseMetadata | GET /triage/survey_responses/{survey_response_id}/metadata | Get metadata |
| getTriageSurveyResponseMetadataByKey | GET /triage/survey_responses/{survey_response_id}/metadata/{key} | Get metadata by key |
| listTriageSurveyResponseTriageNotes | GET /triage/survey_responses/{survey_response_id}/triage_notes | List triage notes |
| createTriageSurveyResponseTriageNote | POST /triage/survey_responses/{survey_response_id}/triage_notes | Create triage note |
| getTriageSurveyResponseTriageNote | GET /triage/survey_responses/{survey_response_id}/triage_notes/{triage_note_id} | Get triage note |
| createThreatModelFromSurveyResponse | POST /triage/survey_responses/{survey_response_id}/create_threat_model | Promote to threat model |

#### `teams` (20 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listTeams | GET /teams | List teams |
| createTeam | POST /teams | Create team |
| getTeam | GET /teams/{team_id} | Get team |
| updateTeam | PUT /teams/{team_id} | Update team |
| patchTeam | PATCH /teams/{team_id} | Patch team |
| deleteTeam | DELETE /teams/{team_id} | Delete team |
| listTeamNotes | GET /teams/{team_id}/notes | List notes |
| createTeamNote | POST /teams/{team_id}/notes | Create note |
| getTeamNote | GET /teams/{team_id}/notes/{team_note_id} | Get note |
| updateTeamNote | PUT /teams/{team_id}/notes/{team_note_id} | Update note |
| patchTeamNote | PATCH /teams/{team_id}/notes/{team_note_id} | Patch note |
| deleteTeamNote | DELETE /teams/{team_id}/notes/{team_note_id} | Delete note |
| getTeamMetadata | GET /teams/{team_id}/metadata | Get metadata |
| createTeamMetadata | POST /teams/{team_id}/metadata | Create metadata |
| getTeamMetadataByKey | GET /teams/{team_id}/metadata/{key} | Get metadata by key (note: operationId not in spec, verify) |
| updateTeamMetadata | PUT /teams/{team_id}/metadata/{key} | Update metadata key |
| deleteTeamMetadata | DELETE /teams/{team_id}/metadata/{key} | Delete metadata key |
| bulkCreateTeamMetadata | POST /teams/{team_id}/metadata/bulk | Bulk create metadata |
| bulkReplaceTeamMetadata | PUT /teams/{team_id}/metadata/bulk | Bulk replace metadata |
| bulkUpsertTeamMetadata | PATCH /teams/{team_id}/metadata/bulk | Bulk upsert metadata |

#### `projects` (19 operations, same shape as teams)
| operationId | Action | Description |
|-------------|--------|-------------|
| listProjects | GET /projects | List projects |
| createProject | POST /projects | Create project |
| getProject | GET /projects/{project_id} | Get project |
| updateProject | PUT /projects/{project_id} | Update project |
| patchProject | PATCH /projects/{project_id} | Patch project |
| deleteProject | DELETE /projects/{project_id} | Delete project |
| listProjectNotes | GET /projects/{project_id}/notes | List notes |
| createProjectNote | POST /projects/{project_id}/notes | Create note |
| getProjectNote | GET /projects/{project_id}/notes/{project_note_id} | Get note |
| updateProjectNote | PUT /projects/{project_id}/notes/{project_note_id} | Update note |
| patchProjectNote | PATCH /projects/{project_id}/notes/{project_note_id} | Patch note |
| deleteProjectNote | DELETE /projects/{project_id}/notes/{project_note_id} | Delete note |
| getProjectMetadata | GET /projects/{project_id}/metadata | Get metadata |
| createProjectMetadata | POST /projects/{project_id}/metadata | Create metadata |
| updateProjectMetadata | PUT /projects/{project_id}/metadata/{key} | Update metadata key |
| deleteProjectMetadata | DELETE /projects/{project_id}/metadata/{key} | Delete metadata key |
| bulkCreateProjectMetadata | POST /projects/{project_id}/metadata/bulk | Bulk create metadata |
| bulkReplaceProjectMetadata | PUT /projects/{project_id}/metadata/bulk | Bulk replace metadata |
| bulkUpsertProjectMetadata | PATCH /projects/{project_id}/metadata/bulk | Bulk upsert metadata |

#### `chat` (6 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| listTimmyChatSessions | GET /threat_models/{threat_model_id}/chat/sessions | List chat sessions |
| createTimmyChatSession | POST /threat_models/{threat_model_id}/chat/sessions | Create session |
| getTimmyChatSession | GET /threat_models/{threat_model_id}/chat/sessions/{session_id} | Get session |
| deleteTimmyChatSession | DELETE /threat_models/{threat_model_id}/chat/sessions/{session_id} | Delete session |
| listTimmyChatMessages | GET /threat_models/{threat_model_id}/chat/sessions/{session_id}/messages | List messages |
| createTimmyChatMessage | POST /threat_models/{threat_model_id}/chat/sessions/{session_id}/messages | Send message |

#### `user_self_service` (13 operations)
Expand existing `user` category or add alongside it:

| operationId | Action | Description |
|-------------|--------|-------------|
| getCurrentUserProfile | GET /me | Get profile |
| deleteUserAccount | DELETE /me | Delete account |
| getCurrentUserPreferences | GET /me/preferences | Get preferences |
| createCurrentUserPreferences | POST /me/preferences | Create preferences |
| updateCurrentUserPreferences | PUT /me/preferences | Update preferences |
| listCurrentUserClientCredentials | GET /me/client_credentials | List credentials |
| createCurrentUserClientCredential | POST /me/client_credentials | Create credential |
| deleteCurrentUserClientCredential | DELETE /me/client_credentials/{credential_id} | Delete credential |
| getCurrentUserSessions | GET /me/sessions | List sessions |
| listMyGroups | GET /me/groups | List my groups |
| listMyGroupMembers | GET /me/groups/{internal_uuid}/members | List group members |
| transferCurrentUserOwnership | POST /me/transfer | Transfer ownership |
| logoutCurrentUser | POST /me/logout | Logout |

#### `saml` (add to `public_endpoints`)
| operationId | Action | Description |
|-------------|--------|-------------|
| getSAMLProviders | GET /saml/providers | List SAML providers |
| initiateSAMLLogin | GET /saml/{provider}/login | Start SAML login |
| processSAMLResponse | POST /saml/acs | SAML ACS callback |
| processSAMLLogout | GET /saml/slo | SAML SLO (GET) |
| processSAMLLogoutPost | POST /saml/slo | SAML SLO (POST) |
| getSAMLMetadata | GET /saml/{provider}/metadata | Get SP metadata |
| listSAMLUsers | GET /saml/providers/{idp}/users | List SAML users |

#### `audit_trail` (9 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| getThreatModelAuditTrail | GET /threat_models/{threat_model_id}/audit_trail | TM audit trail |
| getAuditEntry | GET /threat_models/{threat_model_id}/audit_trail/{entry_id} | Get entry |
| rollbackToVersion | POST /threat_models/{threat_model_id}/audit_trail/{entry_id}/rollback | Rollback |
| getAssetAuditTrail | GET /threat_models/{threat_model_id}/assets/{asset_id}/audit_trail | Asset audit trail |
| getDiagramAuditTrail | GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/audit_trail | Diagram audit trail |
| getDocumentAuditTrail | GET /threat_models/{threat_model_id}/documents/{document_id}/audit_trail | Document audit trail |
| getThreatAuditTrail | GET /threat_models/{threat_model_id}/threats/{threat_id}/audit_trail | Threat audit trail |
| getNoteAuditTrail | GET /threat_models/{threat_model_id}/notes/{note_id}/audit_trail | Note audit trail |
| getRepositoryAuditTrail | GET /threat_models/{threat_model_id}/repositories/{repository_id}/audit_trail | Repo audit trail |

#### `restore` (7 operations)
| operationId | Action | Description |
|-------------|--------|-------------|
| restoreThreatModel | POST /threat_models/{threat_model_id}/restore | Restore TM |
| restoreAsset | POST /threat_models/{threat_model_id}/assets/{asset_id}/restore | Restore asset |
| restoreDiagram | POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/restore | Restore diagram |
| restoreDocument | POST /threat_models/{threat_model_id}/documents/{document_id}/restore | Restore document |
| restoreThreat | POST /threat_models/{threat_model_id}/threats/{threat_id}/restore | Restore threat |
| restoreNote | POST /threat_models/{threat_model_id}/notes/{note_id}/restore | Restore note |
| restoreRepository | POST /threat_models/{threat_model_id}/repositories/{repository_id}/restore | Restore repo |

#### `config` (1 operation)
| operationId | Action | Description |
|-------------|--------|-------------|
| getClientConfig | GET /config | Get client configuration |

### 2. `api-workflows.json` — New `complete_workflow_sequences`

Add 8 new sequences to the existing 7:

#### `survey_lifecycle` (~14 steps)
OAuth -> admin creates survey -> admin adds metadata -> intake lists surveys -> intake submits response -> intake adds metadata -> triage lists responses -> triage reviews response -> triage adds notes -> triage promotes to threat model -> get created threat model

#### `team_project_management` (~16 steps)
OAuth -> create team -> update team -> add team note -> add team metadata -> create project -> update project -> add project note -> add project metadata -> list teams -> list projects -> delete project note -> delete team note -> delete project -> delete team

#### `chat_session_workflow` (~9 steps)
OAuth -> create threat model -> create chat session -> send message -> list messages -> get session -> list sessions -> delete session

#### `admin_user_management` (~14 steps)
OAuth -> list users -> create automation account -> get user -> update user -> create client credential -> list credentials -> delete credential -> create group -> add group member -> list members -> manage quotas -> transfer ownership -> delete user

#### `user_self_service_workflow` (~11 steps)
OAuth -> get profile -> create preferences -> update preferences -> create client credential -> list credentials -> list sessions -> list groups -> transfer ownership -> logout -> delete account

#### `saml_authentication` (~6 steps)
Get providers -> initiate login -> process ACS response -> get metadata -> list users -> SLO

#### `audit_trail_and_restore` (~13 steps)
OAuth -> create TM -> create asset -> create document -> get TM audit trail -> get asset audit trail -> get audit entry -> rollback to version -> delete asset (soft) -> restore asset -> delete TM (soft) -> restore TM

#### `admin_settings_management` (~8 steps)
OAuth -> list settings -> get setting -> update setting -> delete setting -> migrate settings -> reencrypt settings -> get Timmy status

### 3. `scripts/enhance-arazzo-with-workflows.py` — Updates

#### New `prereq_map` entries
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

#### New path parameter dependency detection
Add detection for these path parameters in `_add_complete_sequences()`:
- `{survey_id}` -> depends on survey creation step
- `{survey_response_id}` -> depends on survey response creation step
- `{team_id}` -> depends on team creation step
- `{project_id}` -> depends on project creation step
- `{session_id}` -> depends on chat session creation step
- `{internal_uuid}` -> depends on user/group creation step (context-dependent)
- `{credential_id}` -> depends on credential creation step
- `{triage_note_id}` -> depends on triage note creation step
- `{entry_id}` -> depends on audit trail retrieval step
- `{team_note_id}` -> depends on team note creation step
- `{project_note_id}` -> depends on project note creation step

### 4. Regenerate and Validate

1. Run `make generate-arazzo`
2. Run `make validate-arazzo` — expect 0 errors
3. Verify: no stale operationId references remain (all operationIds in Arazzo exist in OpenAPI)
4. Verify: workflow count increases from 308 to ~316+ (8 new complete sequences)

## Out of Scope

- Changing the enhance script's architecture or generation approach
- Adding operationId-based references instead of operationPath (existing warning is acceptable)
- Updating the arazzo-generation.md documentation (deprecated docs/ directory)
- Adding Itarazzo execution testing
