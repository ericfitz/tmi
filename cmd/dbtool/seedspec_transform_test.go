package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransformSeedSpec_EmptyFile(t *testing.T) {
	spec := &SeedSpecFile{Version: "1.0"}
	result, err := transformSeedSpec(spec)
	require.NoError(t, err)
	assert.Equal(t, "1.0", result.FormatVersion)
	assert.Empty(t, result.Seeds)
}

func TestTransformSeedSpec_Users(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Users: []SeedSpecUser{
			{
				ID:            "alice",
				Email:         "alice@test.local",
				DisplayName:   "Alice Test",
				OAuthProvider: "tmi",
				Roles:         SeedSpecUserRoles{IsAdmin: true},
			},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)
	require.Len(t, result.Seeds, 1)

	seed := result.Seeds[0]
	assert.Equal(t, kindUser, seed.Kind)
	assert.Equal(t, "user:alice", seed.Ref)
	assert.Equal(t, "alice", seed.Data["user_id"])
	assert.Equal(t, "tmi", seed.Data["provider"])
	assert.Equal(t, true, seed.Data["admin"])
	assert.Equal(t, "alice@test.local", seed.Data["email"])
	assert.Equal(t, "Alice Test", seed.Data["display_name"])
}

func TestTransformSeedSpec_UserDefaultProvider(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Users: []SeedSpecUser{
			{ID: "bob"},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)
	assert.Equal(t, "tmi", result.Seeds[0].Data["provider"])
}

func TestTransformSeedSpec_SecurityReviewerGroupMember(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Users: []SeedSpecUser{
			{ID: "reviewer", Roles: SeedSpecUserRoles{IsSecurityReviewer: true}},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)

	// Should have user + group_member for security reviewers group
	require.Len(t, result.Seeds, 2)
	assert.Equal(t, kindUser, result.Seeds[0].Kind)
	assert.Equal(t, kindGroupMember, result.Seeds[1].Kind)
	assert.Equal(t, securityReviewersGroupUUID, result.Seeds[1].Data["group_uuid"])
	assert.Equal(t, "user:reviewer", result.Seeds[1].Data["user_ref"])
}

func TestTransformSeedSpec_Teams(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Users:   []SeedSpecUser{{ID: "alice"}},
		Teams: []SeedSpecTeam{
			{
				Name:   "My Team",
				Status: "active",
				Members: []SeedSpecTeamMember{
					{UserID: "alice", Role: "lead"},
				},
			},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)

	var teamSeed *SeedEntry
	for i := range result.Seeds {
		if result.Seeds[i].Kind == kindTeam {
			teamSeed = &result.Seeds[i]
			break
		}
	}
	require.NotNil(t, teamSeed)
	assert.Equal(t, "team:my-team", teamSeed.Ref)
	assert.Equal(t, "My Team", teamSeed.Data["name"])
	assert.Equal(t, "active", teamSeed.Data["status"])

	members, ok := teamSeed.Data["members"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, members, 1)
	assert.Equal(t, "user:alice", members[0]["user_ref"])
	assert.Equal(t, "engineering_lead", members[0]["role"])
}

func TestTransformSeedSpec_Projects(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Projects: []SeedSpecProject{
			{Name: "My Project", Team: "My Team", Status: "active"},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)
	require.Len(t, result.Seeds, 1)

	seed := result.Seeds[0]
	assert.Equal(t, kindProject, seed.Kind)
	assert.Equal(t, "project:my-project", seed.Ref)
	assert.Equal(t, "team:my-team", seed.Data["team_ref"])
}

func TestTransformSeedSpec_ThreatModelWithChildren(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Users:   []SeedSpecUser{{ID: "owner", Email: "owner@test.local", DisplayName: "Owner"}},
		ThreatModels: []SeedSpecThreatModel{
			{
				Name:                 "Test TM",
				Description:          "A test threat model",
				ThreatModelFramework: "STRIDE",
				Owner:                "owner",
				Status:               "active",
				Alias:                []string{"test-alias"},
				Authorization: []SeedSpecAuthz{
					{UserID: "owner", Role: "owner"},
				},
				Threats: []SeedSpecThreat{
					{Name: "Test Threat", Severity: "high"},
				},
				Assets: []SeedSpecAsset{
					{Name: "Test Asset", Type: "data"},
				},
			},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)

	// Count by kind
	kinds := map[string]int{}
	for _, s := range result.Seeds {
		kinds[s.Kind]++
	}

	assert.Equal(t, 1, kinds[kindUser])
	assert.Equal(t, 1, kinds[kindThreatModel])
	assert.Equal(t, 1, kinds[kindTMPatch])
	assert.Equal(t, 1, kinds[kindThreat])
	assert.Equal(t, 1, kinds[kindAsset])
}

func TestTransformSeedSpec_ThreatModelAuthorization(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Users: []SeedSpecUser{
			{ID: "alice", Email: "alice@test.local", DisplayName: "Alice", OAuthProvider: "tmi"},
		},
		ThreatModels: []SeedSpecThreatModel{
			{
				Name: "TM With Auth",
				Authorization: []SeedSpecAuthz{
					{UserID: "alice", Role: "writer"},
				},
			},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)

	var tmSeed *SeedEntry
	for i := range result.Seeds {
		if result.Seeds[i].Kind == kindThreatModel {
			tmSeed = &result.Seeds[i]
			break
		}
	}
	require.NotNil(t, tmSeed)

	authz, ok := tmSeed.Data["authorization"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, authz, 1)
	assert.Equal(t, "user", authz[0]["principal_type"])
	assert.Equal(t, "tmi", authz[0]["provider"])
	assert.Equal(t, "alice", authz[0]["provider_id"])
	assert.Equal(t, "alice@test.local", authz[0]["email"])
	assert.Equal(t, "Alice", authz[0]["display_name"])
	assert.Equal(t, "writer", authz[0]["role"])
}

func TestTransformSeedSpec_TMPatchFields(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Users:   []SeedSpecUser{{ID: "owner", Email: "owner@test.local", DisplayName: "Owner"}},
		Projects: []SeedSpecProject{
			{Name: "My Project"},
		},
		ThreatModels: []SeedSpecThreatModel{
			{
				Name:             "Patched TM",
				Owner:            "owner",
				Status:           "active",
				SecurityReviewer: "owner",
				ProjectID:        "My Project",
				Alias:            []string{"my-alias"},
			},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)

	var patchSeed *SeedEntry
	for i := range result.Seeds {
		if result.Seeds[i].Kind == kindTMPatch {
			patchSeed = &result.Seeds[i]
			break
		}
	}
	require.NotNil(t, patchSeed)
	assert.Equal(t, "tm:patched-tm", patchSeed.Data["tm_ref"])

	patches, ok := patchSeed.Data["patches"].([]map[string]any)
	require.True(t, ok)

	pathSet := map[string]bool{}
	for _, p := range patches {
		pathSet[p["path"].(string)] = true
	}
	assert.True(t, pathSet["/owner"])
	assert.True(t, pathSet["/status"])
	assert.True(t, pathSet["/security_reviewer"])
	assert.True(t, pathSet["/project_id"])
	assert.True(t, pathSet["/alias"])
}

func TestTransformSeedSpec_DependencyOrdering(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Users:   []SeedSpecUser{{ID: "alice"}},
		Teams:   []SeedSpecTeam{{Name: "Team A"}},
		Projects: []SeedSpecProject{
			{Name: "Project A", Team: "Team A"},
		},
		ThreatModels: []SeedSpecThreatModel{
			{
				Name:      "TM A",
				Owner:     "alice",
				ProjectID: "Project A",
				Threats:   []SeedSpecThreat{{Name: "Threat 1"}},
			},
		},
		AdminEntities: &SeedSpecAdmin{
			Settings: []SeedSpecKV{{Key: "key1", Value: "val1"}},
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)

	// Verify ordering: users/settings before teams before projects before TMs before children
	kindOrder := make([]string, 0, len(result.Seeds))
	for _, s := range result.Seeds {
		kindOrder = append(kindOrder, s.Kind)
	}

	indexOf := func(kind string) int {
		for i, k := range kindOrder {
			if k == kind {
				return i
			}
		}
		return -1
	}

	assert.Less(t, indexOf(kindUser), indexOf(kindTeam))
	assert.Less(t, indexOf(kindSetting), indexOf(kindTeam))
	assert.Less(t, indexOf(kindTeam), indexOf(kindProject))
	assert.Less(t, indexOf(kindProject), indexOf(kindThreatModel))
	assert.Less(t, indexOf(kindThreatModel), indexOf(kindTMPatch))
	assert.Less(t, indexOf(kindTMPatch), indexOf(kindThreat))
}

func TestTransformSeedSpec_Output(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Output: &SeedSpecOutput{
			ReferenceFile: "out/ref.json",
			ReferenceYAML: "out/ref.yml",
		},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)
	require.NotNil(t, result.Output)
	assert.Equal(t, "out/ref.json", result.Output.ReferenceFile)
	assert.Equal(t, "out/ref.yml", result.Output.ReferenceYAML)
}

func TestTransformSeedSpec_Surveys(t *testing.T) {
	spec := &SeedSpecFile{
		Version: "1.0",
		Surveys: []SeedSpecSurvey{
			{Name: "Test Survey", Status: "active"},
		},
		SurveyResponses: []SeedSpecSurveyResp{
			{Survey: "Test Survey", User: "alice"},
		},
		Users: []SeedSpecUser{{ID: "alice"}},
	}

	result, err := transformSeedSpec(spec)
	require.NoError(t, err)

	var surveySeed, responseSeed *SeedEntry
	for i := range result.Seeds {
		switch result.Seeds[i].Kind {
		case kindSurvey:
			surveySeed = &result.Seeds[i]
		case kindSurveyResponse:
			responseSeed = &result.Seeds[i]
		}
	}

	require.NotNil(t, surveySeed)
	assert.Equal(t, "survey:test-survey", surveySeed.Ref)

	require.NotNil(t, responseSeed)
	assert.Equal(t, "survey:test-survey", responseSeed.Data["survey_ref"])
}

func TestMapTeamRole(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"lead", "engineering_lead"},
		{"member", "engineer"},
		{"engineer", "engineer"},
		{"product_manager", "product_manager"},
		{"pm", "product_manager"},
		{"security", "security_specialist"},
		{"", "engineer"},
		{"other", "other"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, mapTeamRole(tt.input), "mapTeamRole(%q)", tt.input)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Team", "my-team"},
		{"CATS Test Threat Model", "cats-test-threat-model"},
		{"Hello World! @#$", "hello-world-"},
		{"simple", "simple"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, sanitizeName(tt.input), "sanitizeName(%q)", tt.input)
	}
}

func TestRefHelpers(t *testing.T) {
	assert.Equal(t, "user:alice", userRef("alice"))
	assert.Equal(t, "team:my-team", teamRef("My Team"))
	assert.Equal(t, "project:my-project", projectRef("My Project"))
	assert.Equal(t, "tm:test-tm", tmRef("Test TM"))
	assert.Equal(t, "group:eng-group", groupRef("Eng Group"))
	assert.Equal(t, "survey:intake", surveyRef("Intake"))
	assert.Equal(t, "webhook:my-hook", webhookRef("My Hook"))
	assert.Equal(t, "threat:tm1:t1", childRef("threat", "TM1", "T1"))
}
