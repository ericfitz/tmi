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
		case "db":
			result, err = seedViaDB(db, entry, refs)
		case "api":
			if token == "" {
				log.Info("Authenticating via OAuth stub for API calls...")
				token, err = authenticateViaOAuthStub(serverURL, user, provider)
				if err != nil {
					return fmt.Errorf("failed to authenticate for API calls: %w", err)
				}
			}
			result, err = seedViaAPI(serverURL, token, entry, refs)
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

	if seedFile.Output != nil && !dryRun {
		if err := writeReferenceFiles(seedFile.Output, refs, serverURL, user, provider); err != nil {
			return fmt.Errorf("failed to write reference files: %w", err)
		}
	}

	log.Info("Data seed complete: %d entries processed", len(seedFile.Seeds))
	return nil
}

func loadSeedFile(path string) (*SeedFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path from CLI flags
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var seedFile SeedFile

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		if err := json.Unmarshal(data, &seedFile); err != nil {
			return nil, fmt.Errorf("failed to parse JSON seed file: %w", err)
		}
	default:
		// Try JSON first for unknown extensions
		if err := json.Unmarshal(data, &seedFile); err != nil {
			return nil, fmt.Errorf("failed to parse seed file as JSON: %w", err)
		}
	}

	if seedFile.FormatVersion == "" {
		return nil, fmt.Errorf("seed file missing format_version")
	}
	if len(seedFile.Seeds) == 0 {
		return nil, fmt.Errorf("seed file has no seeds")
	}

	return &seedFile, nil
}

const (
	kindUser    = "user"
	kindSetting = "setting"
	strategyDB  = "db"
	strategyAPI = "api"
)

func classifyStrategy(kind string) string {
	switch kind {
	case kindUser, kindSetting:
		return strategyDB
	default:
		return strategyAPI
	}
}

func resolveRef(refs RefMap, refName string) (string, error) {
	result, ok := refs[refName]
	if !ok {
		return "", fmt.Errorf("unresolved ref: %q (referenced before creation or missing)", refName)
	}
	return result.ID, nil
}

func resolveRefField(data map[string]any, refFieldName string, refs RefMap) (string, error) {
	refName, ok := data[refFieldName].(string)
	if !ok || refName == "" {
		return "", nil
	}
	return resolveRef(refs, refName)
}
