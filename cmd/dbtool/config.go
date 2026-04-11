package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/secrets"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"gopkg.in/yaml.v3"
)

func runConfigSeed(db *testdb.TestDB, inputFile, outputFile string, overwrite, dryRun bool) error {
	log := slogging.Get()

	cfg, err := config.Load(inputFile)
	if err != nil {
		return fmt.Errorf("failed to load config %s: %w", inputFile, err)
	}

	allSettings := cfg.GetMigratableSettings()
	log.Info("Found %d settings in config", len(allSettings))

	var infraSettings []config.MigratableSetting
	var dbSettings []config.MigratableSetting

	for _, s := range allSettings {
		if config.IsInfrastructureKey(s.Key) {
			infraSettings = append(infraSettings, s)
		} else {
			dbSettings = append(dbSettings, s)
		}
	}

	log.Info("  Infrastructure (stay in file): %d settings", len(infraSettings))
	log.Info("  DB-eligible (move to database): %d settings", len(dbSettings))

	if dryRun {
		log.Info("")
		log.Info("[DRY RUN] Infrastructure settings (would stay in config file):")
		for _, s := range infraSettings {
			displayValue := s.Value
			if s.Secret {
				displayValue = "<secret>"
			}
			log.Info("  %s = %s [%s] (source: %s)", s.Key, displayValue, s.Type, s.Source)
		}
		log.Info("")
		log.Info("[DRY RUN] DB-eligible settings (would write to database):")
		for _, s := range dbSettings {
			displayValue := s.Value
			if s.Secret {
				displayValue = "<secret>"
			}
			log.Info("  %s = %s [%s] (source: %s)", s.Key, displayValue, s.Type, s.Source)
		}

		if outputFile == "" {
			outputFile = deriveOutputPath(inputFile)
		}
		log.Info("")
		log.Info("[DRY RUN] Would write migrated config to: %s", outputFile)
		return nil
	}

	// Initialize encryptor if secrets config is available
	var encryptor *crypto.SettingsEncryptor
	secretsProvider, err := secrets.NewProvider(context.Background(), &cfg.Secrets)
	if err != nil {
		log.Debug("No secrets provider available for encryption: %v", err)
	} else {
		enc, encErr := crypto.NewSettingsEncryptor(context.Background(), secretsProvider)
		if encErr != nil {
			log.Debug("No encryptor available: %v", encErr)
		} else {
			encryptor = enc
			log.Info("Settings encryption enabled")
		}
		if closeErr := secretsProvider.Close(); closeErr != nil {
			log.Debug("Failed to close secrets provider: %v", closeErr)
		}
	}

	// Write DB-eligible settings
	var written, skipped int
	for _, s := range dbSettings {
		var existing models.SystemSetting
		exists := db.DB().Where("setting_key = ?", s.Key).First(&existing).Error == nil

		if exists && !overwrite {
			skipped++
			log.Debug("  Skipping existing: %s", s.Key)
			continue
		}

		value := s.Value
		if s.Secret && encryptor != nil && value != "" {
			encrypted, encErr := encryptor.Encrypt(value)
			if encErr != nil {
				return fmt.Errorf("failed to encrypt setting %s: %w", s.Key, encErr)
			}
			value = encrypted
		}

		description := s.Description
		setting := models.SystemSetting{
			SettingKey:  s.Key,
			Value:       value,
			SettingType: s.Type,
			Description: &description,
			ModifiedAt:  time.Now(),
		}

		if exists {
			if updateErr := db.DB().Model(&existing).Updates(map[string]any{
				"value":        value,
				"setting_type": s.Type,
				"description":  description,
				"modified_at":  time.Now(),
			}).Error; updateErr != nil {
				return fmt.Errorf("failed to update setting %s: %w", s.Key, updateErr)
			}
		} else {
			if createErr := db.DB().Create(&setting).Error; createErr != nil {
				return fmt.Errorf("failed to create setting %s: %w", s.Key, createErr)
			}
		}
		written++
		log.Debug("  Wrote: %s", s.Key)
	}

	log.Info("Settings written to database: %d written, %d skipped", written, skipped)

	// Generate migrated YAML
	if outputFile == "" {
		outputFile = deriveOutputPath(inputFile)
	}

	if err := writeMigratedConfig(infraSettings, outputFile); err != nil {
		return fmt.Errorf("failed to write migrated config: %w", err)
	}

	log.Info("Migrated config written to: %s", outputFile)
	log.Info("")
	log.Info("Next steps:")
	log.Info("  1. Review the migrated config: %s", outputFile)
	log.Info("  2. Backup your current config")
	log.Info("  3. Replace your config with the migrated version")
	log.Info("  4. Restart the server")

	return nil
}

func writeMigratedConfig(infraSettings []config.MigratableSetting, outputPath string) error {
	root := make(map[string]any)

	for _, s := range infraSettings {
		parts := strings.Split(s.Key, ".")
		current := root
		for i, part := range parts {
			if i == len(parts)-1 {
				current[part] = s.Value
			} else {
				if _, ok := current[part]; !ok {
					current[part] = make(map[string]any)
				}
				if next, ok := current[part].(map[string]any); ok {
					current = next
				}
			}
		}
	}

	header := fmt.Sprintf("# TMI Configuration (infrastructure keys only)\n"+
		"# Generated by tmi-seed --mode=config on %s\n"+
		"# Non-infrastructure settings have been migrated to the database.\n"+
		"# See: https://github.com/ericfitz/tmi/wiki/Configuration-Management\n\n",
		time.Now().UTC().Format(time.RFC3339))

	yamlData, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("failed to marshal YAML: %w", err)
	}

	output := []byte(header)
	output = append(output, yamlData...)

	return os.WriteFile(outputPath, output, 0o600)
}

func deriveOutputPath(inputPath string) string {
	ext := ""
	base := inputPath
	for _, e := range []string{".yml", ".yaml", ".json"} {
		if strings.HasSuffix(inputPath, e) {
			ext = e
			base = strings.TrimSuffix(inputPath, e)
			break
		}
	}
	if ext == "" {
		ext = ".yml"
	}
	return base + "-migrated" + ext
}
