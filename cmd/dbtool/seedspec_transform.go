package main

import (
	"fmt"
	"strings"
)

const (
	securityReviewersGroupUUID = "00000000-0000-0000-0000-000000000001"
	defaultProvider            = "tmi"
	roleEngineer               = "engineer"
)

// userInfo caches user details for building Principal objects.
type userInfo struct {
	ID          string
	Email       string
	DisplayName string
	Provider    string
}

// transformSeedSpec converts a SeedSpecFile into the internal SeedFile format.
// It emits SeedEntry items in strict dependency order so that RefMap resolution works.
func transformSeedSpec(spec *SeedSpecFile) (*SeedFile, error) {
	users := buildUserLookup(spec.Users)
	var seeds []SeedEntry

	seeds = append(seeds, transformUsers(spec.Users)...)
	seeds = append(seeds, transformAdminSettingsAndQuotas(spec.AdminEntities, users)...)
	seeds = append(seeds, transformGroupsAndMembers(spec.AdminEntities, spec.Users)...)
	seeds = append(seeds, transformTeams(spec.Teams)...)
	seeds = append(seeds, transformProjects(spec.Projects)...)

	tmSeeds, err := transformThreatModels(spec.ThreatModels, users, spec.Projects)
	if err != nil {
		return nil, err
	}
	seeds = append(seeds, tmSeeds...)

	seeds = append(seeds, transformSurveys(spec.Surveys)...)
	seeds = append(seeds, transformSurveyResponses(spec.SurveyResponses, users)...)
	seeds = append(seeds, transformAdminWebhooksAndAddons(spec.AdminEntities)...)
	seeds = append(seeds, transformStandaloneMetadata(spec.Metadata)...)

	var output *SeedOutput
	if spec.Output != nil {
		output = &SeedOutput{
			ReferenceFile: spec.Output.ReferenceFile,
			ReferenceYAML: spec.Output.ReferenceYAML,
		}
	}

	return &SeedFile{
		FormatVersion: spec.Version,
		Description:   spec.Description,
		Output:        output,
		Seeds:         seeds,
	}, nil
}

func buildUserLookup(users []SeedSpecUser) map[string]userInfo {
	m := make(map[string]userInfo, len(users))
	for _, u := range users {
		provider := u.OAuthProvider
		if provider == "" {
			provider = defaultProvider
		}
		m[u.ID] = userInfo{
			ID:          u.ID,
			Email:       u.Email,
			DisplayName: u.DisplayName,
			Provider:    provider,
		}
	}
	return m
}

func transformUsers(users []SeedSpecUser) []SeedEntry {
	var seeds []SeedEntry
	for _, u := range users {
		provider := u.OAuthProvider
		if provider == "" {
			provider = defaultProvider
		}
		data := map[string]any{
			"user_id":  u.ID,
			"provider": provider,
			"admin":    u.Roles.IsAdmin,
		}
		if u.Email != "" {
			data["email"] = u.Email
		}
		if u.DisplayName != "" {
			data["display_name"] = u.DisplayName
		}
		if u.APIQuota != nil {
			data["api_quota"] = map[string]any{
				"rpm": u.APIQuota.RPM,
				"rph": u.APIQuota.RPH,
			}
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindUser,
			Ref:  userRef(u.ID),
			Data: data,
		})
	}
	return seeds
}

func transformAdminSettingsAndQuotas(admin *SeedSpecAdmin, users map[string]userInfo) []SeedEntry {
	if admin == nil {
		return nil
	}
	var seeds []SeedEntry

	for _, s := range admin.Settings {
		seeds = append(seeds, SeedEntry{
			Kind: kindSetting,
			Data: map[string]any{
				"key":   s.Key,
				"value": s.Value,
				"type":  "string",
			},
		})
	}

	for _, q := range admin.Quotas {
		rpm, rph := 0, 0
		switch q.Period {
		case "minute":
			rpm = q.RateLimit
		default:
			rph = q.RateLimit
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindUser,
			Ref:  userRef(q.User) + ":quota",
			Data: map[string]any{
				"user_id":  q.User,
				"provider": userProvider(users, q.User),
				"api_quota": map[string]any{
					"rpm": rpm,
					"rph": rph,
				},
			},
		})
	}

	return seeds
}

func transformGroupsAndMembers(admin *SeedSpecAdmin, users []SeedSpecUser) []SeedEntry {
	var seeds []SeedEntry

	if admin != nil {
		for _, g := range admin.Groups {
			ref := groupRef(g.Name)
			seeds = append(seeds, SeedEntry{
				Kind: kindGroup,
				Ref:  ref,
				Data: map[string]any{
					"group_name": sanitizeName(g.Name),
					"name":       g.Name,
				},
			})
			for _, memberID := range g.Members {
				seeds = append(seeds, SeedEntry{
					Kind: kindGroupMember,
					Data: map[string]any{
						"group_ref": ref,
						"user_ref":  userRef(memberID),
					},
				})
			}
		}
	}

	// Security reviewer group membership
	for _, u := range users {
		if u.Roles.IsSecurityReviewer {
			seeds = append(seeds, SeedEntry{
				Kind: kindGroupMember,
				Data: map[string]any{
					"group_uuid": securityReviewersGroupUUID,
					"user_ref":   userRef(u.ID),
				},
			})
		}
	}

	return seeds
}

func transformTeams(teams []SeedSpecTeam) []SeedEntry {
	var seeds []SeedEntry
	for _, t := range teams {
		members := make([]map[string]any, 0, len(t.Members))
		for _, m := range t.Members {
			members = append(members, map[string]any{
				"user_ref": userRef(m.UserID),
				"role":     mapTeamRole(m.Role),
			})
		}
		data := map[string]any{
			"name":    t.Name,
			"members": members,
		}
		if t.Status != "" {
			data["status"] = t.Status
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindTeam,
			Ref:  teamRef(t.Name),
			Data: data,
		})
	}
	return seeds
}

func transformProjects(projects []SeedSpecProject) []SeedEntry {
	var seeds []SeedEntry
	for _, p := range projects {
		data := map[string]any{
			"name": p.Name,
		}
		if p.Team != "" {
			data["team_ref"] = teamRef(p.Team)
		}
		if p.Status != "" {
			data["status"] = p.Status
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindProject,
			Ref:  projectRef(p.Name),
			Data: data,
		})
	}
	return seeds
}

func transformThreatModels(tms []SeedSpecThreatModel, users map[string]userInfo, _ []SeedSpecProject) ([]SeedEntry, error) {
	var seeds []SeedEntry
	for _, tm := range tms {
		ref := tmRef(tm.Name)

		seeds = append(seeds, transformTMCreate(tm, ref, users))

		patches := buildTMPatches(users, tm)
		if len(patches) > 0 {
			seeds = append(seeds, SeedEntry{
				Kind: kindTMPatch,
				Data: map[string]any{
					"tm_ref":  ref,
					"patches": patches,
				},
			})
		}

		seeds = append(seeds, transformThreats(tm.Threats, ref, tm.Name)...)
		seeds = append(seeds, transformAssets(tm.Assets, ref, tm.Name)...)
		seeds = append(seeds, transformDocuments(tm.Documents, ref, tm.Name)...)
		seeds = append(seeds, transformNotes(tm.Notes, ref, tm.Name)...)
		seeds = append(seeds, transformRepositories(tm.Repositories, ref, tm.Name)...)

		diagramSeeds, err := transformDiagrams(tm.Diagrams, ref, tm.Name)
		if err != nil {
			return nil, err
		}
		seeds = append(seeds, diagramSeeds...)
	}
	return seeds, nil
}

func transformTMCreate(tm SeedSpecThreatModel, ref string, users map[string]userInfo) SeedEntry {
	tmData := map[string]any{
		"name": tm.Name,
	}
	if tm.Description != "" {
		tmData["description"] = tm.Description
	}
	if tm.ThreatModelFramework != "" {
		tmData["threat_model_framework"] = tm.ThreatModelFramework
	}
	if tm.IssueURI != "" {
		tmData["issue_uri"] = tm.IssueURI
	}
	if tm.IsConfidential {
		tmData["is_confidential"] = true
	}
	if len(tm.Metadata) > 0 {
		tmData["metadata"] = kvToMaps(tm.Metadata)
	}
	if len(tm.Authorization) > 0 {
		authz := make([]map[string]any, 0, len(tm.Authorization))
		for _, a := range tm.Authorization {
			authz = append(authz, buildPrincipal(users, a.UserID, a.Role))
		}
		tmData["authorization"] = authz
	}
	return SeedEntry{Kind: kindThreatModel, Ref: ref, Data: tmData}
}

func transformThreats(threats []SeedSpecThreat, ref, tmName string) []SeedEntry {
	var seeds []SeedEntry
	for _, t := range threats {
		data := map[string]any{
			"threat_model_ref": ref,
			"name":             t.Name,
		}
		if t.Description != "" {
			data["description"] = t.Description
		}
		if len(t.ThreatType) > 0 {
			data["threat_type"] = t.ThreatType
		}
		if t.Severity != "" {
			data["severity"] = t.Severity
		}
		if t.Score != 0 {
			data["score"] = t.Score
		}
		if t.Priority != "" {
			data["priority"] = t.Priority
		}
		if t.Status != "" {
			data["status"] = t.Status
		}
		if t.Mitigated {
			data["mitigated"] = true
		}
		if t.Mitigation != "" {
			data["mitigation"] = t.Mitigation
		}
		if len(t.CWEID) > 0 {
			data["cwe_id"] = t.CWEID
		}
		if len(t.CVSS) > 0 {
			cvss := make([]map[string]any, 0, len(t.CVSS))
			for _, c := range t.CVSS {
				// API CVSS schema only accepts vector and score (no version field)
				cvss = append(cvss, map[string]any{
					"vector": c.Vector,
					"score":  c.Score,
				})
			}
			data["cvss"] = cvss
		}
		if t.IssueURI != "" {
			data["issue_uri"] = t.IssueURI
		}
		if t.IncludeInReport {
			data["include_in_report"] = true
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindThreat,
			Ref:  childRef("threat", tmName, t.Name),
			Data: data,
		})
	}
	return seeds
}

func transformAssets(assets []SeedSpecAsset, ref, tmName string) []SeedEntry {
	var seeds []SeedEntry
	for _, a := range assets {
		data := map[string]any{
			"threat_model_ref": ref,
			"name":             a.Name,
		}
		if a.Description != "" {
			data["description"] = a.Description
		}
		if a.Type != "" {
			data["type"] = a.Type
		}
		if a.Criticality != "" {
			data["criticality"] = a.Criticality
		}
		if len(a.Classification) > 0 {
			data["classification"] = a.Classification
		}
		if a.Sensitivity != "" {
			data["sensitivity"] = a.Sensitivity
		}
		if a.IncludeInReport {
			data["include_in_report"] = true
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindAsset,
			Ref:  childRef("asset", tmName, a.Name),
			Data: data,
		})
	}
	return seeds
}

func transformDocuments(docs []SeedSpecDocument, ref, tmName string) []SeedEntry {
	var seeds []SeedEntry
	for _, d := range docs {
		data := map[string]any{
			"threat_model_ref": ref,
			"name":             d.Name,
		}
		if d.URI != "" {
			data["uri"] = d.URI
		}
		if d.Description != "" {
			data["description"] = d.Description
		}
		if d.IncludeInReport {
			data["include_in_report"] = true
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindDocument,
			Ref:  childRef("document", tmName, d.Name),
			Data: data,
		})
	}
	return seeds
}

func transformNotes(notes []SeedSpecNote, ref, tmName string) []SeedEntry {
	var seeds []SeedEntry
	for _, n := range notes {
		data := map[string]any{
			"threat_model_ref": ref,
			"name":             n.Name,
		}
		if n.Content != "" {
			data["content"] = n.Content
		}
		if n.Description != "" {
			data["description"] = n.Description
		}
		if n.IncludeInReport {
			data["include_in_report"] = true
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindNote,
			Ref:  childRef("note", tmName, n.Name),
			Data: data,
		})
	}
	return seeds
}

func transformRepositories(repos []SeedSpecRepository, ref, tmName string) []SeedEntry {
	var seeds []SeedEntry
	for _, r := range repos {
		data := map[string]any{
			"threat_model_ref": ref,
		}
		if r.Name != "" {
			data["name"] = r.Name
		}
		if r.URI != "" {
			data["uri"] = r.URI
		}
		if r.Type != "" {
			data["type"] = r.Type
		}
		if r.Description != "" {
			data["description"] = r.Description
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindRepository,
			Ref:  childRef("repo", tmName, r.Name),
			Data: data,
		})
	}
	return seeds
}

func transformDiagrams(diagrams []SeedSpecDiagram, ref, tmName string) ([]SeedEntry, error) {
	var seeds []SeedEntry
	for _, d := range diagrams {
		dRef := childRef("diagram", tmName, d.Name)
		diagramType := d.Type
		if diagramType == "" || diagramType == "dfd" {
			diagramType = "DFD-1.0.0"
		}

		seeds = append(seeds, SeedEntry{
			Kind: kindDiagram,
			Ref:  dRef,
			Data: map[string]any{
				"threat_model_ref": ref,
				"name":             d.Name,
				"type":             diagramType,
			},
		})

		if len(d.Nodes) > 0 || len(d.Edges) > 0 {
			cells, err := transformDiagramCells(d.Nodes, d.Edges)
			if err != nil {
				return nil, fmt.Errorf("failed to transform diagram %q cells: %w", d.Name, err)
			}
			updateData := map[string]any{
				"tm_ref":      ref,
				"diagram_ref": dRef,
				"name":        d.Name,
				"type":        diagramType,
				"cells":       cells,
			}
			if d.Description != "" {
				updateData["description"] = d.Description
			}
			seeds = append(seeds, SeedEntry{
				Kind: kindDiagramUpdate,
				Data: updateData,
			})
		}
	}
	return seeds, nil
}

func transformSurveys(surveys []SeedSpecSurvey) []SeedEntry {
	var seeds []SeedEntry
	for _, s := range surveys {
		data := map[string]any{
			"name": s.Name,
		}
		if s.Description != "" {
			data["description"] = s.Description
		}
		if s.Version != "" {
			data["version"] = s.Version
		}
		if s.Status != "" {
			data["status"] = s.Status
		}
		if len(s.SurveyJSON) > 0 {
			data["survey_json"] = s.SurveyJSON
		}
		if len(s.Settings) > 0 {
			data["settings"] = s.Settings
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindSurvey,
			Ref:  surveyRef(s.Name),
			Data: data,
		})
	}
	return seeds
}

func transformSurveyResponses(responses []SeedSpecSurveyResp, users map[string]userInfo) []SeedEntry {
	var seeds []SeedEntry
	for i, sr := range responses {
		data := map[string]any{}
		if sr.Survey != "" {
			data["survey_ref"] = surveyRef(sr.Survey)
		}
		if len(sr.Responses) > 0 {
			data["answers"] = sr.Responses
		}
		if sr.Status != "" {
			data["status"] = sr.Status
		}
		if sr.User != "" {
			u := users[sr.User]
			provider := u.Provider
			if provider == "" {
				provider = defaultProvider
			}
			data["authorization"] = []map[string]any{
				{
					"principal_type": "user",
					"provider":       provider,
					"provider_id":    sr.User,
					"role":           "owner",
				},
			}
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindSurveyResponse,
			Ref:  fmt.Sprintf("survey-response:%d", i),
			Data: data,
		})
	}
	return seeds
}

func transformAdminWebhooksAndAddons(admin *SeedSpecAdmin) []SeedEntry {
	if admin == nil {
		return nil
	}
	var seeds []SeedEntry

	for _, w := range admin.Webhooks {
		data := map[string]any{
			"name": w.Name,
			"url":  w.URL,
		}
		if len(w.Events) > 0 {
			data["events"] = w.Events
		}
		if w.HMACSecret != "" {
			data["secret"] = w.HMACSecret
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindWebhook,
			Ref:  webhookRef(w.Name),
			Data: data,
		})
	}

	for _, wt := range admin.WebhookTestDeliveries {
		seeds = append(seeds, SeedEntry{
			Kind: kindWebhookTestDeliv,
			Data: map[string]any{
				"webhook_ref": webhookRef(wt.Webhook),
			},
		})
	}

	for _, a := range admin.Addons {
		data := map[string]any{
			"name": a.Name,
		}
		if a.Webhook != "" {
			data["webhook_ref"] = webhookRef(a.Webhook)
		}
		if a.ThreatModel != "" {
			data["threat_model_ref"] = tmRef(a.ThreatModel)
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindAddon,
			Data: data,
		})
	}

	for _, cc := range admin.ClientCredentials {
		data := map[string]any{
			"name": cc.Name,
		}
		if cc.Description != "" {
			data["description"] = cc.Description
		}
		seeds = append(seeds, SeedEntry{
			Kind: kindClientCredential,
			Ref:  fmt.Sprintf("cred:%s", sanitizeName(cc.Name)),
			Data: data,
		})
	}

	return seeds
}

func transformStandaloneMetadata(metadata []SeedSpecMetadataEntry) []SeedEntry {
	var seeds []SeedEntry
	for _, m := range metadata {
		seeds = append(seeds, SeedEntry{
			Kind: kindMetadata,
			Data: map[string]any{
				"target_ref":  m.Target,
				"target_kind": m.TargetKind,
				"key":         m.Key,
				"value":       m.Value,
			},
		})
	}
	return seeds
}

// buildTMPatches creates JSON Patch operations for ThreatModel fields
// not settable via ThreatModelInput (owner, status, security_reviewer, project_id, alias).
func buildTMPatches(users map[string]userInfo, tm SeedSpecThreatModel) []map[string]any {
	var patches []map[string]any

	// Order matters: set status/project/alias first, then security_reviewer,
	// then owner last (ownership transfer must be done by current owner).
	if tm.Status != "" {
		patches = append(patches, map[string]any{
			"op":    "replace",
			"path":  "/status",
			"value": tm.Status,
		})
	}
	if tm.ProjectID != "" {
		patches = append(patches, map[string]any{
			"op":          "replace",
			"path":        "/project_id",
			"project_ref": projectRef(tm.ProjectID),
		})
	}
	if len(tm.Alias) > 0 {
		patches = append(patches, map[string]any{
			"op":    "replace",
			"path":  "/alias",
			"value": tm.Alias,
		})
	}
	if tm.SecurityReviewer != "" {
		patches = append(patches, map[string]any{
			"op":    "replace",
			"path":  "/security_reviewer",
			"value": buildPrincipal(users, tm.SecurityReviewer, ""),
		})
	}
	if tm.Owner != "" {
		patches = append(patches, map[string]any{
			"op":    "replace",
			"path":  "/owner",
			"value": buildPrincipal(users, tm.Owner, ""),
		})
	}

	return patches
}

// buildPrincipal creates an Authorization/Principal map for a user.
func buildPrincipal(users map[string]userInfo, userID, role string) map[string]any {
	u, ok := users[userID]
	provider := defaultProvider
	email := userID + "@tmi.local"
	displayName := capitalize(userID) + " (TMI User)"
	if ok {
		if u.Provider != "" {
			provider = u.Provider
		}
		if u.Email != "" {
			email = u.Email
		}
		if u.DisplayName != "" {
			displayName = u.DisplayName
		}
	}

	p := map[string]any{
		"principal_type": "user",
		"provider":       provider,
		"provider_id":    userID,
		"email":          email,
		"display_name":   displayName,
	}
	if role != "" {
		p["role"] = role
	}
	return p
}

func kvToMaps(kvs []SeedSpecKV) []map[string]any {
	result := make([]map[string]any, 0, len(kvs))
	for _, kv := range kvs {
		result = append(result, map[string]any{
			"key":   kv.Key,
			"value": kv.Value,
		})
	}
	return result
}

func userProvider(users map[string]userInfo, userID string) string {
	if u, ok := users[userID]; ok && u.Provider != "" {
		return u.Provider
	}
	return defaultProvider
}

func mapTeamRole(role string) string {
	switch strings.ToLower(role) {
	case "lead", "engineering_lead":
		return "engineering_lead"
	case "member", roleEngineer:
		return roleEngineer
	case "product_manager", "pm":
		return "product_manager"
	case "business_leader":
		return "business_leader"
	case "security_specialist", "security":
		return "security_specialist"
	default:
		if role == "" {
			return roleEngineer
		}
		return role
	}
}

// Ref name helpers — deterministic, sanitized names for RefMap.
func sanitizeName(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, s)
	return s
}

func userRef(id string) string      { return "user:" + id }
func teamRef(name string) string    { return "team:" + sanitizeName(name) }
func projectRef(name string) string { return "project:" + sanitizeName(name) }
func tmRef(name string) string      { return "tm:" + sanitizeName(name) }
func groupRef(name string) string   { return "group:" + sanitizeName(name) }
func surveyRef(name string) string  { return "survey:" + sanitizeName(name) }
func webhookRef(name string) string { return "webhook:" + sanitizeName(name) }

func childRef(kind, tmName, childName string) string {
	return fmt.Sprintf("%s:%s:%s", kind, sanitizeName(tmName), sanitizeName(childName))
}
