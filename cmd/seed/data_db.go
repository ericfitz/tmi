package main

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"github.com/google/uuid"
)

const administratorsGroupUUID = "00000000-0000-0000-0000-000000000002"

func seedViaDB(db *testdb.TestDB, entry SeedEntry, refs RefMap) (*SeedResult, error) {
	switch entry.Kind {
	case kindUser:
		return seedUser(db, entry)
	case kindSetting:
		return seedSetting(db, entry)
	default:
		return nil, fmt.Errorf("unsupported DB seed kind: %s", entry.Kind)
	}
}

func seedUser(db *testdb.TestDB, entry SeedEntry) (*SeedResult, error) {
	log := slogging.Get()

	userID, _ := entry.Data["user_id"].(string)
	providerName, _ := entry.Data["provider"].(string)
	if userID == "" || providerName == "" {
		return nil, fmt.Errorf("user seed requires user_id and provider")
	}

	var user models.User
	result := db.DB().Where(
		"provider_user_id = ? AND provider = ?",
		userID,
		providerName,
	).First(&user)

	if result.Error != nil {
		user = models.User{
			InternalUUID:   uuid.New().String(),
			Provider:       providerName,
			ProviderUserID: &userID,
			Email:          fmt.Sprintf("%s@tmi.local", userID),
			Name:           fmt.Sprintf("%s (Seed User)", capitalize(userID)),
			EmailVerified:  models.DBBool(true),
		}
		if err := db.DB().Create(&user).Error; err != nil {
			return nil, fmt.Errorf("failed to create user: %w", err)
		}
		log.Info("  Created user: %s (UUID: %s)", userID, user.InternalUUID)
	} else {
		log.Info("  User already exists: %s (UUID: %s)", userID, user.InternalUUID)
	}

	if admin, ok := entry.Data["admin"].(bool); ok && admin {
		if err := grantAdmin(db, &user); err != nil {
			return nil, err
		}
	}

	if quota, ok := entry.Data["api_quota"].(map[string]any); ok {
		if err := setQuotas(db, user.InternalUUID, quota); err != nil {
			return nil, err
		}
	}

	return &SeedResult{
		Ref:  entry.Ref,
		Kind: kindUser,
		ID:   user.InternalUUID,
		Extra: map[string]string{
			"provider":         providerName,
			"provider_user_id": userID,
			"email":            user.Email,
		},
	}, nil
}

func grantAdmin(db *testdb.TestDB, user *models.User) error {
	log := slogging.Get()

	var count int64
	db.DB().Model(&models.GroupMember{}).
		Where("group_internal_uuid = ? AND user_internal_uuid = ? AND subject_type = ?",
			administratorsGroupUUID, user.InternalUUID, "user").
		Count(&count)

	if count > 0 {
		log.Info("  User is already an administrator")
		return nil
	}

	notes := "Granted by tmi-seed"
	member := models.GroupMember{
		ID:                uuid.New().String(),
		GroupInternalUUID: administratorsGroupUUID,
		UserInternalUUID:  &user.InternalUUID,
		SubjectType:       "user",
		Notes:             &notes,
	}

	if err := db.DB().Create(&member).Error; err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "unique constraint") ||
			strings.Contains(errStr, "ORA-00001") ||
			strings.Contains(errStr, "duplicate key") {
			log.Info("  User is already an administrator")
			return nil
		}
		return fmt.Errorf("failed to grant admin: %w", err)
	}

	log.Info("  Granted admin privileges")
	return nil
}

func setQuotas(db *testdb.TestDB, userInternalUUID string, quota map[string]any) error {
	log := slogging.Get()

	rpm := intFromAny(quota["rpm"], 0)
	rph := intFromAny(quota["rph"], 0)

	if rpm == 0 && rph == 0 {
		return nil
	}

	var existing models.UserAPIQuota
	result := db.DB().Where("user_internal_uuid = ?", userInternalUUID).First(&existing)

	if result.Error == nil {
		updates := map[string]any{}
		if rpm > 0 {
			updates["max_requests_per_minute"] = rpm
		}
		if rph > 0 {
			updates["max_requests_per_hour"] = rph
		}
		if err := db.DB().Model(&existing).Updates(updates).Error; err != nil {
			return fmt.Errorf("failed to update quotas: %w", err)
		}
	} else {
		q := models.UserAPIQuota{
			UserInternalUUID:     userInternalUUID,
			MaxRequestsPerMinute: rpm,
		}
		if rph > 0 {
			q.MaxRequestsPerHour = &rph
		}
		if err := db.DB().Create(&q).Error; err != nil {
			return fmt.Errorf("failed to create quotas: %w", err)
		}
	}

	log.Info("  Set quotas: %d/min, %d/hour", rpm, rph)
	return nil
}

func seedSetting(db *testdb.TestDB, entry SeedEntry) (*SeedResult, error) {
	log := slogging.Get()

	key, _ := entry.Data["key"].(string)
	value, _ := entry.Data["value"].(string)
	settingType, _ := entry.Data["type"].(string)
	if key == "" || settingType == "" {
		return nil, fmt.Errorf("setting seed requires key and type")
	}

	description, _ := entry.Data["description"].(string)

	setting := models.SystemSetting{
		SettingKey:  key,
		Value:       value,
		SettingType: settingType,
		Description: &description,
	}

	var existing models.SystemSetting
	if err := db.DB().Where("setting_key = ?", key).First(&existing).Error; err == nil {
		if err := db.DB().Model(&existing).Updates(map[string]any{
			"value":        value,
			"setting_type": settingType,
			"description":  description,
		}).Error; err != nil {
			return nil, fmt.Errorf("failed to update setting %s: %w", key, err)
		}
		log.Info("  Updated setting: %s", key)
	} else {
		if err := db.DB().Create(&setting).Error; err != nil {
			return nil, fmt.Errorf("failed to create setting %s: %w", key, err)
		}
		log.Info("  Created setting: %s", key)
	}

	return &SeedResult{Ref: entry.Ref, Kind: kindSetting, ID: key}, nil
}

func intFromAny(v any, defaultVal int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return defaultVal
	}
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}
