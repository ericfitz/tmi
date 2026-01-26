// Package main implements cats-seed, a database-agnostic CLI tool for seeding
// CATS fuzzing test data. It works with all databases TMI supports (PostgreSQL,
// Oracle, MySQL, SQL Server, SQLite) by using GORM through the testdb package.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/seed"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"github.com/google/uuid"
)

const (
	defaultUser     = "charlie"
	defaultProvider = "tmi"

	// High quota values for CATS fuzzing (14000+ tests)
	maxRequestsPerMinute = 100000
	maxRequestsPerHour   = 1000000
)

func main() {
	// Command line flags
	var (
		configFile = flag.String("config", "", "Path to TMI configuration file (required)")
		user       = flag.String("user", defaultUser, "Provider user ID for test user")
		provider   = flag.String("provider", defaultProvider, "OAuth provider for test user")
		dryRun     = flag.Bool("dry-run", false, "Show what would be done without making changes")
		verbose    = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	// Validate required flags
	if *configFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --config flag is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: cats-seed --config=<config-file> [OPTIONS]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  # PostgreSQL")
		fmt.Fprintln(os.Stderr, "  cats-seed --config=config-development.yml")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  # Oracle ADB (requires oci-env.sh sourced first)")
		fmt.Fprintln(os.Stderr, "  source scripts/oci-env.sh")
		fmt.Fprintln(os.Stderr, "  cats-seed --config=config-development-oci.yml")
		os.Exit(1)
	}

	// Initialize logging
	logLevel := slogging.LogLevelInfo
	if *verbose {
		logLevel = slogging.LogLevelDebug
	}
	if err := slogging.Initialize(slogging.Config{
		Level:            logLevel,
		IsDev:            true,
		AlsoLogToConsole: true,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logger: %v\n", err)
	}
	log := slogging.Get()

	log.Info("CATS Test Data Seeding Tool")
	log.Info("  Config:   %s", *configFile)
	log.Info("  User:     %s", *user)
	log.Info("  Provider: %s", *provider)
	if *dryRun {
		log.Info("  Mode:     DRY RUN (no changes will be made)")
	}

	// Create database connection using testdb package
	log.Info("Connecting to database...")
	db, err := testdb.New(*configFile)
	if err != nil {
		log.Error("Failed to connect to database: %v", err)
		os.Exit(1)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Error("Error closing database: %v", closeErr)
		}
	}()

	log.Info("Connected to %s database", db.DialectName())

	// All databases use GORM AutoMigrate for schema management
	// This provides a single source of truth (api/models/models.go) for all supported databases
	log.Info("Ensuring database schema is up to date via AutoMigrate...")
	if err := db.AutoMigrate(); err != nil {
		// Oracle-specific errors that are benign when re-running migrations:
		// - ORA-00955: name is already used by an existing object (table already exists)
		// - ORA-01442: column to be modified to NOT NULL is already NOT NULL
		// These are GORM/Oracle compatibility issues, not actual problems
		errStr := err.Error()
		if strings.Contains(errStr, "ORA-00955") || strings.Contains(errStr, "ORA-01442") {
			log.Debug("Oracle migration notice (benign): %v", err)
		} else {
			log.Error("Failed to auto-migrate schema: %v", err)
			os.Exit(1)
		}
	}
	log.Info("  Schema verified")

	// Seed required data (everyone group, webhook deny list)
	if err := seed.SeedDatabase(db.DB()); err != nil {
		log.Error("Failed to seed database: %v", err)
		os.Exit(1)
	}

	// Step 1: Find or create the test user
	log.Info("Step 1: Finding or creating test user %s@%s...", *user, *provider)
	testUser, created, err := findOrCreateUser(db, *user, *provider, *dryRun)
	if err != nil {
		log.Error("Failed to find/create user: %v", err)
		os.Exit(1)
	}
	if *dryRun {
		log.Info("  [DRY RUN] Would find or create user %s@%s", *user, *provider)
	} else if created {
		log.Info("  Created new user: %s (UUID: %s)", *testUser.ProviderUserID, testUser.InternalUUID)
	} else {
		log.Info("  User already exists: %s (UUID: %s)", *testUser.ProviderUserID, testUser.InternalUUID)
	}

	// Step 2: Grant admin privileges
	log.Info("Step 2: Granting admin privileges...")
	wasAdmin, err := grantAdminPrivileges(db, testUser, *dryRun)
	if err != nil {
		log.Error("Failed to grant admin privileges: %v", err)
		os.Exit(1)
	}
	if *dryRun {
		log.Info("  [DRY RUN] Would grant admin privileges to %s", *user)
	} else if wasAdmin {
		log.Info("  User %s is already an administrator", *user)
	} else {
		log.Info("  Granted admin privileges to %s", *user)
	}

	// Step 3: Set maximum API quotas
	log.Info("Step 3: Setting maximum API quotas...")
	err = setMaxQuotas(db, testUser.InternalUUID, *dryRun)
	if err != nil {
		log.Error("Failed to set API quotas: %v", err)
		os.Exit(1)
	}
	if *dryRun {
		log.Info("  [DRY RUN] Would set quotas: %d/min, %d/hour", maxRequestsPerMinute, maxRequestsPerHour)
	} else {
		log.Info("  Set quotas: %d requests/min, %d requests/hour", maxRequestsPerMinute, maxRequestsPerHour)
	}

	// Summary
	fmt.Println("")
	if *dryRun {
		fmt.Println("DRY RUN complete. No changes were made.")
	} else {
		fmt.Println("CATS database seeding complete!")
		fmt.Println("")
		fmt.Println("Next steps:")
		fmt.Println("  1. Run test data creation: ./scripts/cats-create-test-data.sh")
		fmt.Println("  2. Run CATS fuzzing: make cats-fuzz")
	}
}

// findOrCreateUser finds an existing user or creates a new one.
// Returns the user, whether it was newly created, and any error.
func findOrCreateUser(db *testdb.TestDB, providerUserID, provider string, dryRun bool) (*models.User, bool, error) {
	// Check if user already exists
	var user models.User
	result := db.DB().Where(
		"provider_user_id = ? AND provider = ?",
		providerUserID,
		provider,
	).First(&user)

	if result.Error == nil {
		// User exists
		return &user, false, nil
	}

	// User doesn't exist - create if not dry run
	if dryRun {
		// Return a fake user for dry run
		fakeUser := &models.User{
			InternalUUID:   uuid.New().String(),
			Provider:       provider,
			ProviderUserID: &providerUserID,
			Email:          fmt.Sprintf("%s@tmi.local", providerUserID),
		}
		return fakeUser, true, nil
	}

	// Create new user
	user = models.User{
		InternalUUID:   uuid.New().String(),
		Provider:       provider,
		ProviderUserID: &providerUserID,
		Email:          fmt.Sprintf("%s@tmi.local", providerUserID),
		Name:           fmt.Sprintf("%s (CATS Test User)", capitalize(providerUserID)),
		EmailVerified:  models.OracleBool(true),
	}

	if err := db.DB().Create(&user).Error; err != nil {
		return nil, false, fmt.Errorf("failed to create user: %w", err)
	}

	return &user, true, nil
}

// grantAdminPrivileges grants administrator privileges to a user.
// Returns whether the user was already an admin and any error.
func grantAdminPrivileges(db *testdb.TestDB, user *models.User, dryRun bool) (bool, error) {
	// Check if user is already an administrator
	var count int64
	db.DB().Model(&models.Administrator{}).
		Where("user_internal_uuid = ? AND subject_type = ?", user.InternalUUID, "user").
		Count(&count)

	if count > 0 {
		return true, nil // Already admin
	}

	if dryRun {
		return false, nil
	}

	// Grant admin privileges
	admin := models.Administrator{
		ID:               uuid.New().String(),
		UserInternalUUID: &user.InternalUUID,
		SubjectType:      "user",
		Provider:         user.Provider,
		Notes:            ptrString("Auto-granted for CATS fuzzing - allows comprehensive API testing"),
	}

	if err := db.DB().Create(&admin).Error; err != nil {
		// Handle duplicate key errors gracefully - user may already be admin
		// This can happen with Oracle's NULL handling in unique constraints
		errStr := err.Error()
		if strings.Contains(errStr, "unique constraint") ||
			strings.Contains(errStr, "ORA-00001") ||
			strings.Contains(errStr, "duplicate key") {
			return true, nil // Treat as already admin
		}
		return false, fmt.Errorf("failed to grant admin privileges: %w", err)
	}

	return false, nil
}

// setMaxQuotas sets maximum API quotas for a user to prevent rate limiting during fuzzing.
func setMaxQuotas(db *testdb.TestDB, userInternalUUID string, dryRun bool) error {
	if dryRun {
		return nil
	}

	maxHour := maxRequestsPerHour

	// Check if quota record exists
	var existingQuota models.UserAPIQuota
	result := db.DB().Where("user_internal_uuid = ?", userInternalUUID).First(&existingQuota)

	if result.Error == nil {
		// Update existing record
		return db.DB().Model(&existingQuota).Updates(map[string]interface{}{
			"max_requests_per_minute": maxRequestsPerMinute,
			"max_requests_per_hour":   maxHour,
		}).Error
	}

	// Create new quota record
	quota := models.UserAPIQuota{
		UserInternalUUID:     userInternalUUID,
		MaxRequestsPerMinute: maxRequestsPerMinute,
		MaxRequestsPerHour:   &maxHour,
	}

	return db.DB().Create(&quota).Error
}

// ptrString returns a pointer to the given string.
func ptrString(s string) *string {
	return &s
}

// capitalize capitalizes the first letter of a string.
func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
