package rotator

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
)

const (
	SettingsKeyKey        = "TMI_SECRET_SETTINGS_ENCRYPTION_KEY"
	SettingsKeyIDKey      = "TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"
	SettingsPrevKeyKey    = "TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY"
	SettingsPrevKeyIDKey  = "TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID"
	AnnPromotedAt         = "tmi.dev/rotation-promoted-at."
	settingsRotationName  = "settings-key"
	settingsPhaseStaged   = "staged"
	settingsPhasePromoted = "promoted"
	settingsPhaseReenc    = "reencrypted"
)

// Keyring is the settings-encryption key material held in tmi-secrets.
// SEM@0000000000000000000000000000000000000000: current and previous settings encryption keys with their ids (pure)
type Keyring struct {
	CurrentKeyHex  string
	CurrentID      int
	PreviousKeyHex string
	PreviousID     int
}

// KeyringFromSecret reads and validates the keyring; a missing context id is 1
// (the id every pre-rotation value carries).
// SEM@0000000000000000000000000000000000000000: parse and validate the settings keyring from Secret data (pure)
func KeyringFromSecret(s *Secret) (Keyring, error) {
	kr := Keyring{CurrentKeyHex: s.Data[SettingsKeyKey], CurrentID: 1, PreviousKeyHex: s.Data[SettingsPrevKeyKey]}
	if err := checkHexKey(kr.CurrentKeyHex); err != nil {
		return Keyring{}, fmt.Errorf("%s: %w", SettingsKeyKey, err)
	}
	if v := s.Data[SettingsKeyIDKey]; v != "" {
		id, err := strconv.Atoi(v)
		if err != nil || id <= 0 {
			return Keyring{}, fmt.Errorf("%s is not a positive integer", SettingsKeyIDKey)
		}
		kr.CurrentID = id
	}
	if kr.PreviousKeyHex != "" {
		if err := checkHexKey(kr.PreviousKeyHex); err != nil {
			return Keyring{}, fmt.Errorf("%s: %w", SettingsPrevKeyKey, err)
		}
		id, err := strconv.Atoi(s.Data[SettingsPrevKeyIDKey])
		if err != nil || id <= 0 {
			return Keyring{}, fmt.Errorf("%s is not a positive integer", SettingsPrevKeyIDKey)
		}
		kr.PreviousID = id
	}
	return kr, nil
}

// SEM@0000000000000000000000000000000000000000: validate a 64-hex-char key string (pure)
func checkHexKey(v string) error {
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != 32 {
		return fmt.Errorf("must be 64 hex characters")
	}
	return nil
}

// Encryptor builds the crypto keyring for this Keyring.
// SEM@0000000000000000000000000000000000000000: build a SettingsEncryptor from a Keyring (pure)
func (k Keyring) Encryptor() (*crypto.SettingsEncryptor, error) {
	cur, _ := hex.DecodeString(k.CurrentKeyHex)
	var prev []byte
	if k.PreviousKeyHex != "" {
		prev, _ = hex.DecodeString(k.PreviousKeyHex)
	}
	return crypto.NewSettingsEncryptorFromKeyring(cur, k.CurrentID, prev, k.PreviousID)
}

// SettingsStore is the database side of the settings-key rotation.
// SEM@0000000000000000000000000000000000000000: re-encrypt settings under a keyring and count rows per key id
type SettingsStore interface {
	ReEncrypt(ctx context.Context, kr Keyring) (int, error)
	CountWithID(ctx context.Context, kr Keyring, id int) (int64, error)
}

// GormSettingsStore runs the real SettingsService against the database.
// SEM@0000000000000000000000000000000000000000: SettingsStore backed by api.SettingsService over GORM (writes DB)
type GormSettingsStore struct {
	gormDB *gorm.DB
	redis  *db.RedisDB
}

// SEM@0000000000000000000000000000000000000000: build a GormSettingsStore (pure)
func NewGormSettingsStore(gormDB *gorm.DB, redis *db.RedisDB) *GormSettingsStore {
	return &GormSettingsStore{gormDB: gormDB, redis: redis}
}

// SEM@0000000000000000000000000000000000000000: build a SettingsService for a keyring (pure)
func (g *GormSettingsStore) service(kr Keyring) (*api.SettingsService, error) {
	enc, err := kr.Encryptor()
	if err != nil {
		return nil, err
	}
	svc := api.NewSettingsService(g.gormDB, g.redis)
	svc.SetEncryptor(enc)
	return svc, nil
}

// SEM@0000000000000000000000000000000000000000: re-encrypt every stale settings row under the keyring's current key (writes DB)
func (g *GormSettingsStore) ReEncrypt(ctx context.Context, kr Keyring) (int, error) {
	svc, err := g.service(kr)
	if err != nil {
		return 0, err
	}
	n, rowErrs, err := svc.ReEncryptAll(ctx)
	// Unreadable rows are logged by key only and never fail the rotation: the
	// drop gate (CountWithID(oldID) > 0) keeps the previous key while they remain.
	for _, e := range rowErrs {
		slogging.Get().Warn("Setting could not be re-encrypted key=%s", e.Key)
	}
	return n, err
}

// SEM@0000000000000000000000000000000000000000: count settings rows still under a key id (reads DB)
func (g *GormSettingsStore) CountWithID(ctx context.Context, kr Keyring, id int) (int64, error) {
	svc, err := g.service(kr)
	if err != nil {
		return 0, err
	}
	return svc.CountValuesWithContextID(ctx, id)
}

// SettingsKeyRotation rotates the settings-encryption key: stage, promote,
// re-encrypt, and (after previousGrace, once nothing references the old id) drop.
// SEM@0000000000000000000000000000000000000000: phased rotation of the settings encryption key with deferred drop of the previous key
type SettingsKeyRotation struct {
	store         SettingsStore
	previousGrace time.Duration
}

// SEM@0000000000000000000000000000000000000000: build a SettingsKeyRotation (pure)
func NewSettingsKeyRotation(store SettingsStore, previousGrace time.Duration) *SettingsKeyRotation {
	return &SettingsKeyRotation{store: store, previousGrace: previousGrace}
}

// SEM@0000000000000000000000000000000000000000: return the rotation name (pure)
func (r *SettingsKeyRotation) Name() string { return settingsRotationName }

// SEM@0000000000000000000000000000000000000000: advance the settings-key rotation from its recorded phase
func (r *SettingsKeyRotation) Run(ctx context.Context, env *Env) error {
	logger := slogging.Get()
	s, err := env.Secrets.Get(ctx, env.SecretName)
	if err != nil {
		return err
	}
	kr, err := KeyringFromSecret(s)
	if err != nil {
		return err
	}
	name := r.Name()
	phase := s.Annotations[AnnPhase+name]

	if phase == "" {
		if kr.PreviousID != 0 {
			return fmt.Errorf("previous key id %d still present; refusing to start a new rotation", kr.PreviousID)
		}
		next, err := NewHexKey()
		if err != nil {
			return err
		}
		newID := kr.CurrentID + 1
		if err := env.Transition(ctx, name, "", settingsPhaseStaged, func(s *Secret) {
			s.Data[SettingsPrevKeyKey] = next
			s.Data[SettingsPrevKeyIDKey] = strconv.Itoa(newID)
		}); err != nil {
			return err
		}
		logger.Info("Settings key staged id=%d", newID)
		if s, err = env.Secrets.Get(ctx, env.SecretName); err != nil {
			return err
		}
		phase = settingsPhaseStaged
	}

	if phase == settingsPhaseStaged {
		if err := env.WaitServerRolled(ctx, s, name); err != nil {
			return fmt.Errorf("staged key rollout: %w", err)
		}
		kr, _ := KeyringFromSecret(s)
		if err := env.Transition(ctx, name, settingsPhaseStaged, settingsPhasePromoted, func(s *Secret) {
			s.Data[SettingsKeyKey] = kr.PreviousKeyHex
			s.Data[SettingsKeyIDKey] = strconv.Itoa(kr.PreviousID)
			s.Data[SettingsPrevKeyKey] = kr.CurrentKeyHex
			s.Data[SettingsPrevKeyIDKey] = strconv.Itoa(kr.CurrentID)
		}); err != nil {
			return err
		}
		logger.Info("Settings key promoted id=%d previous_id=%d", kr.PreviousID, kr.CurrentID)
		if s, err = env.Secrets.Get(ctx, env.SecretName); err != nil {
			return err
		}
		phase = settingsPhasePromoted
	}

	if phase == settingsPhasePromoted {
		if err := env.WaitServerRolled(ctx, s, name); err != nil {
			return fmt.Errorf("promoted key rollout: %w", err)
		}
		kr, err := KeyringFromSecret(s)
		if err != nil {
			return err
		}
		n, err := r.store.ReEncrypt(ctx, kr)
		if err != nil {
			return fmt.Errorf("re-encrypt after %d rows: %w", n, err)
		}
		logger.Info("Settings re-encrypted rows=%d id=%d", n, kr.CurrentID)
		now := env.Now().UTC().Format(time.RFC3339)
		if err := env.Transition(ctx, name, settingsPhasePromoted, settingsPhaseReenc, func(s *Secret) {
			s.Annotations[AnnPromotedAt+name] = now
			s.Annotations[AnnRotatedAt+name] = now
		}); err != nil {
			return err
		}
		return nil // the drop is decided by a later run
	}

	if phase != settingsPhaseReenc {
		return fmt.Errorf("unknown %s phase %q", name, phase)
	}
	promotedAt, err := time.Parse(time.RFC3339, s.Annotations[AnnPromotedAt+name])
	if err != nil {
		return fmt.Errorf("missing promoted-at annotation for %s", name)
	}
	if env.Now().Before(promotedAt.Add(r.previousGrace)) {
		logger.Info("Settings previous key kept until %s (Redis entries under the old id may still be live)", promotedAt.Add(r.previousGrace).Format(time.RFC3339))
		return nil
	}
	remaining, err := r.store.CountWithID(ctx, kr, kr.PreviousID)
	if err != nil {
		return err
	}
	if remaining > 0 {
		n, err := r.store.ReEncrypt(ctx, kr)
		if err != nil {
			return fmt.Errorf("re-encrypt %d remaining rows: %w", remaining, err)
		}
		logger.Info("Settings re-encrypted late rows=%d; previous key kept until the next run", n)
		return nil
	}
	if err := env.Transition(ctx, name, settingsPhaseReenc, "", func(s *Secret) {
		delete(s.Data, SettingsPrevKeyKey)
		delete(s.Data, SettingsPrevKeyIDKey)
		delete(s.Annotations, AnnPromotedAt+name)
		s.Annotations[AnnRotatedAt+name] = promotedAt.UTC().Format(time.RFC3339)
	}); err != nil {
		return err
	}
	logger.Info("Settings previous key dropped id=%d", kr.PreviousID)
	return nil
}
