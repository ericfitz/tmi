// Package seed provides database seeding for required initial data.
// This replaces SQL INSERT statements from migrations to enable
// consistent seeding across all supported databases.
package seed

import (
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/api/validation"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// SeedDatabase ensures all required seed data exists.
// This function is idempotent - safe to call multiple times.
func SeedDatabase(db *gorm.DB) error {
	log := slogging.Get()

	log.Info("Seeding database with required data...")

	if err := seedEveryoneGroup(db); err != nil {
		log.Error("Failed to seed 'everyone' group: %v", err)
		return err
	}

	if err := seedWebhookDenyList(db); err != nil {
		log.Error("Failed to seed webhook deny list: %v", err)
		return err
	}

	if err := seedSecurityReviewersGroup(db); err != nil {
		log.Error("Failed to seed 'security-reviewers' group: %v", err)
		return err
	}

	if err := seedAdministratorsGroup(db); err != nil {
		log.Error("Failed to seed 'administrators' group: %v", err)
		return err
	}

	log.Info("Database seeding completed successfully")
	return nil
}

// seedEveryoneGroup ensures the "everyone" pseudo-group exists.
// This group represents all authenticated users and cannot be deleted or have members added.
func seedEveryoneGroup(db *gorm.DB) error {
	log := slogging.Get()

	name := "Everyone (Pseudo-group)"
	group := models.Group{
		InternalUUID: validation.EveryonePseudoGroupUUID,
		Provider:     "*",
		GroupName:    "everyone",
		Name:         &name,
		UsageCount:   0,
	}

	// Use FirstOrCreate for idempotent seeding
	result := db.Where(&models.Group{
		Provider:  "*",
		GroupName: "everyone",
	}).FirstOrCreate(&group)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		log.Info("Created 'everyone' pseudo-group")
	} else {
		log.Debug("'everyone' pseudo-group already exists")
	}

	return nil
}

// seedSecurityReviewersGroup ensures the "security-reviewers" built-in group exists.
// This group is used for security engineers who triage survey responses.
func seedSecurityReviewersGroup(db *gorm.DB) error {
	log := slogging.Get()

	name := "Security Reviewers"
	group := models.Group{
		InternalUUID: validation.SecurityReviewersGroupUUID,
		Provider:     "*",
		GroupName:    "security-reviewers",
		Name:         &name,
		UsageCount:   0,
	}

	// Use FirstOrCreate for idempotent seeding
	result := db.Where(&models.Group{
		Provider:  "*",
		GroupName: "security-reviewers",
	}).FirstOrCreate(&group)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		log.Info("Created 'security-reviewers' group")
	} else {
		log.Debug("'security-reviewers' group already exists")
	}

	return nil
}

// seedAdministratorsGroup ensures the "administrators" built-in group exists.
// This group controls administrative access to the system.
func seedAdministratorsGroup(db *gorm.DB) error {
	log := slogging.Get()

	name := "Administrators"
	group := models.Group{
		InternalUUID: validation.AdministratorsGroupUUID,
		Provider:     "*",
		GroupName:    "administrators",
		Name:         &name,
		UsageCount:   0,
	}

	// Use FirstOrCreate for idempotent seeding
	result := db.Where(&models.Group{
		Provider:  "*",
		GroupName: "administrators",
	}).FirstOrCreate(&group)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		log.Info("Created 'administrators' group")
	} else {
		log.Debug("'administrators' group already exists")
	}

	return nil
}

// webhookDenyEntry represents a single webhook URL deny list entry
type webhookDenyEntry struct {
	Pattern     string
	PatternType string
	Description string
}

// webhookDenyList contains all SSRF prevention patterns
var webhookDenyList = []webhookDenyEntry{
	// Localhost variants
	{"localhost", "glob", "Block localhost"},
	{"127.*", "glob", "Block loopback addresses (127.0.0.0/8)"},
	{"::1", "glob", "Block IPv6 loopback"},

	// Private IPv4 ranges (RFC 1918)
	{"10.*", "glob", "Block private network 10.0.0.0/8"},
	{"172.16.*", "glob", "Block private network 172.16.0.0/12 (first subnet)"},
	{"172.17.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.18.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.19.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.20.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.21.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.22.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.23.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.24.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.25.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.26.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.27.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.28.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.29.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.30.*", "glob", "Block private network 172.16.0.0/12"},
	{"172.31.*", "glob", "Block private network 172.16.0.0/12 (last subnet)"},
	{"192.168.*", "glob", "Block private network 192.168.0.0/16"},

	// Link-local addresses
	{"169.254.*", "glob", "Block link-local addresses (169.254.0.0/16)"},
	{"fe80:*", "glob", "Block IPv6 link-local addresses (fe80::/10)"},

	// Private IPv6 ranges
	{"fc00:*", "glob", "Block IPv6 unique local addresses (fc00::/7)"},
	{"fd00:*", "glob", "Block IPv6 unique local addresses (fd00::/8)"},

	// Cloud metadata endpoints
	{"169.254.169.254", "glob", "Block AWS/Azure/GCP metadata service"},
	{"fd00:ec2::254", "glob", "Block AWS IMDSv2 IPv6 metadata service"},
	{"metadata.google.internal", "glob", "Block GCP metadata service"},
	{"169.254.169.123", "glob", "Block DigitalOcean metadata service"},

	// Kubernetes internal services
	{"kubernetes.default.svc", "glob", "Block Kubernetes internal service"},
	{"10.96.0.*", "glob", "Block common Kubernetes service CIDR"},

	// Docker internal network
	{"172.17.0.1", "glob", "Block Docker default bridge gateway"},

	// Broadcast addresses
	{"255.255.255.255", "glob", "Block broadcast address"},
	{"0.0.0.0", "glob", "Block null address"},
}

// seedWebhookDenyList ensures all SSRF prevention patterns exist.
func seedWebhookDenyList(db *gorm.DB) error {
	log := slogging.Get()

	created := 0
	for _, entry := range webhookDenyList {
		desc := entry.Description
		denyEntry := models.WebhookURLDenyList{
			Pattern:     entry.Pattern,
			PatternType: entry.PatternType,
			Description: &desc,
		}

		// Use FirstOrCreate for idempotent seeding
		result := db.Where(&models.WebhookURLDenyList{
			Pattern: entry.Pattern,
		}).FirstOrCreate(&denyEntry)

		if result.Error != nil {
			return result.Error
		}

		if result.RowsAffected > 0 {
			created++
		}
	}

	if created > 0 {
		log.Info("Created %d webhook URL deny list entries", created)
	} else {
		log.Debug("All webhook URL deny list entries already exist")
	}

	return nil
}

// GetWebhookDenyListCount returns the expected number of deny list entries.
// Useful for testing and validation.
func GetWebhookDenyListCount() int {
	return len(webhookDenyList)
}

// DeduplicateGroupMembers removes duplicate rows from the group_members table,
// keeping only the earliest row (by added_at) for each unique
// (group_internal_uuid, user_internal_uuid, subject_type) combination.
// This must run BEFORE AutoMigrate so the new unique index can be created.
// The function is idempotent — it's a no-op when no duplicates exist.
func DeduplicateGroupMembers(db *gorm.DB) error {
	log := slogging.Get()

	// Check if the group_members table exists yet (first run on empty DB)
	if !db.Migrator().HasTable(&models.GroupMember{}) {
		log.Debug("group_members table does not exist yet, skipping dedup")
		return nil
	}

	// Find groups of (group_internal_uuid, user_internal_uuid, subject_type) with more than one row
	type dupGroup struct {
		GroupInternalUUID string `gorm:"column:group_internal_uuid"`
		UserInternalUUID  string `gorm:"column:user_internal_uuid"`
		SubjectType       string `gorm:"column:subject_type"`
		Count             int64  `gorm:"column:cnt"`
	}

	var dups []dupGroup
	err := db.Raw(`
		SELECT group_internal_uuid, user_internal_uuid, subject_type, COUNT(*) AS cnt
		FROM group_members
		WHERE user_internal_uuid IS NOT NULL
		GROUP BY group_internal_uuid, user_internal_uuid, subject_type
		HAVING COUNT(*) > 1
	`).Scan(&dups).Error
	if err != nil {
		return fmt.Errorf("failed to find duplicate group memberships: %w", err)
	}

	if len(dups) == 0 {
		log.Debug("No duplicate group memberships found")
		return nil
	}

	totalRemoved := int64(0)
	for _, dup := range dups {
		// Find the ID of the earliest row to keep (using GORM for cross-database LIMIT compatibility)
		var keepID string
		err := db.Table("group_members").
			Select("id").
			Where("group_internal_uuid = ? AND user_internal_uuid = ? AND subject_type = ?",
				dup.GroupInternalUUID, dup.UserInternalUUID, dup.SubjectType).
			Order("added_at ASC").
			Limit(1).
			Scan(&keepID).Error
		if err != nil {
			return fmt.Errorf("failed to find earliest membership for group %s, user %s: %w",
				dup.GroupInternalUUID, dup.UserInternalUUID, err)
		}

		// Delete all other rows
		result := db.Exec(`
			DELETE FROM group_members
			WHERE group_internal_uuid = ? AND user_internal_uuid = ? AND subject_type = ? AND id != ?
		`, dup.GroupInternalUUID, dup.UserInternalUUID, dup.SubjectType, keepID)
		if result.Error != nil {
			return fmt.Errorf("failed to delete duplicate memberships for group %s, user %s: %w",
				dup.GroupInternalUUID, dup.UserInternalUUID, result.Error)
		}
		totalRemoved += result.RowsAffected
	}

	log.Info("Removed %d duplicate group membership rows across %d groups", totalRemoved, len(dups))
	return nil
}
