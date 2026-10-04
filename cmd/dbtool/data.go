package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
)

// SEM@27f3772fc6f3f53382ff01e0a9b73204f0c5e377: load a seed file and apply its entries to the database via DB or API strategies (reads DB)
func runDataSeed(db *testdb.TestDB, inputFile, serverURL, user, provider string, dryRun bool) error {
	log := slogging.Get()

	seedFile, err := loadSeedFile(inputFile)
	if err != nil {
		return fmt.Errorf("failed to load seed file: %w", err)
	}

	log.Info("Loaded seed file: %s", seedFile.Description)
	log.Info("  Format version: %s", seedFile.FormatVersion)
	log.Info("  Seeds: %d entries", len(seedFile.Seeds))

	if err := seed.SeedDatabase(db.DB()); err != nil {
		return fmt.Errorf("failed to seed system data: %w", err)
	}

	refs := make(RefMap)
	var token string

	for i, entry := range seedFile.Seeds {
		log.Info("Processing seed %d/%d: kind=%s ref=%s", i+1, len(seedFile.Seeds), entry.Kind, entry.Ref)

		if dryRun {
			log.Info("  [DRY RUN] Would create %s", entry.Kind)
			if entry.Ref != "" {
				refs[entry.Ref] = &SeedResult{Ref: entry.Ref, Kind: entry.Kind, ID: "dry-run-id"}
			}
			continue
		}

		var result *SeedResult

		switch classifyStrategy(entry.Kind) {
		case strategyDB:
			result, err = seedViaDB(db, entry, refs)
		case strategyAPI:
			if token == "" {
				log.Info("Authenticating via OAuth stub for API calls...")
				token, err = authenticateViaOAuthStub(serverURL, user, provider)
				if err != nil {
					return fmt.Errorf("failed to authenticate for API calls: %w", err)
				}
			}
			result, err = seedViaAPI(serverURL, token, entry, refs, db)
		default:
			err = fmt.Errorf("unknown seed kind: %s", entry.Kind)
		}

		if err != nil {
			return fmt.Errorf("failed to seed entry %d (kind=%s, ref=%s): %w", i+1, entry.Kind, entry.Ref, err)
		}

		if entry.Ref != "" && result != nil {
			refs[entry.Ref] = result
			log.Info("  Registered ref %q -> %s", entry.Ref, result.ID)
		}
	}

	// Audit entries are a side effect of the seeding above rather than
	// something that can be seeded, so their ids can only be harvested once
	// the writes that produced them have happened (#597). Best-effort: an
	// empty audit table just leaves entry_id uncovered, as before.
	if !dryRun && token != "" {
		newAPIClient(serverURL, token).
			captureAuditEntryIDs(refs, findRefByKind(refs, kindThreatModel))
	}

	if seedFile.Output != nil && !dryRun {
		if err := writeReferenceFiles(seedFile.Output, refs, serverURL, user, provider); err != nil {
			return fmt.Errorf("failed to write reference files: %w", err)
		}
	}

	log.Info("Data seed complete: %d entries processed", len(seedFile.Seeds))
	return nil
}

// SEM@a34497eeb7ed839ce3929a9839d3329bae19642a: parse a JSON seed spec file and transform it into a SeedFile (pure)
func loadSeedFile(path string) (*SeedFile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path from CLI flags
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".json" && ext != "" {
		return nil, fmt.Errorf("unsupported file extension: %s (expected .json)", ext)
	}

	var spec SeedSpecFile
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse seed-spec JSON: %w", err)
	}

	if spec.Version == "" {
		return nil, fmt.Errorf("seed file missing version field")
	}

	seedFile, err := transformSeedSpec(&spec)
	if err != nil {
		return nil, fmt.Errorf("failed to transform seed-spec: %w", err)
	}

	return seedFile, nil
}

const (
	kindUser             = "user"
	kindSetting          = "setting"
	kindThreatModel      = "threat_model"
	kindTMPatch          = "tm_patch"
	kindTeam             = "team"
	kindProject          = "project"
	kindGroup            = "group"
	kindGroupMember      = "group_member"
	kindDiagram          = "diagram"
	kindDiagramUpdate    = "diagram_update"
	kindThreat           = "threat"
	kindAsset            = "asset"
	kindDocument         = "document"
	kindNote             = "note"
	kindTeamNote         = "team_note"
	kindProjectNote      = "project_note"
	kindFeedback         = "feedback"
	kindTriageNote       = "triage_note"
	kindRepository       = "repository"
	kindWebhook          = "webhook"
	kindWebhookTestDeliv = "webhook_test_delivery"
	kindAddon            = "addon"
	kindClientCredential = "client_credential"
	kindSurvey           = "survey"
	kindSurveyResponse   = "survey_response"
	kindMetadata         = "metadata"

	strategyDB  = "db"
	strategyAPI = "api"
)

// SEM@c08cdcaa7bf770029c89db2c819eb12680bfe4b5: map a seed entry kind to the DB or API seeding strategy (pure)
func classifyStrategy(kind string) string {
	switch kind {
	case kindUser, kindSetting:
		return strategyDB
	case kindThreatModel, kindTMPatch, kindTeam, kindProject,
		kindGroup, kindGroupMember,
		kindDiagram, kindDiagramUpdate,
		kindThreat, kindAsset, kindDocument, kindNote, kindRepository,
		kindTeamNote, kindProjectNote, kindFeedback, kindTriageNote,
		kindWebhook, kindWebhookTestDeliv, kindAddon, kindClientCredential,
		kindSurvey, kindSurveyResponse, kindMetadata:
		return strategyAPI
	default:
		return ""
	}
}

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: look up a previously-seeded entity ID by its symbolic ref name (pure)
func resolveRef(refs RefMap, refName string) (string, error) {
	result, ok := refs[refName]
	if !ok {
		return "", fmt.Errorf("unresolved ref: %q (referenced before creation or missing)", refName)
	}
	return result.ID, nil
}

// SEM@d958f3dc26a0977ee70f472999b9749af2b714d3: extract a ref field from a data map and resolve it to a seeded entity ID (pure)
func resolveRefField(data map[string]any, refFieldName string, refs RefMap) (string, error) {
	refName, ok := data[refFieldName].(string)
	if !ok || refName == "" {
		return "", nil
	}
	return resolveRef(refs, refName)
}
