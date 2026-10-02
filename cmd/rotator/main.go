// Command rotator rotates the high-value secrets in tmi-secrets (#965). It runs
// as a CronJob and exits non-zero when any rotation fails, leaving its phase
// annotation for the next run to resume. It never prints secret values.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/rotator"
	"github.com/ericfitz/tmi/internal/slogging"
)

// SEM@3b682947: configuration options for the secret rotator
type options struct {
	Force                 string
	Namespace             string
	SecretName            string
	ServerDeployment      string
	RolloutTimeout        time.Duration
	SettingsPreviousGrace time.Duration
	RedisHost, RedisPort  string
	RedisDB               int
	RedisTLSEnabled       bool
	RedisTLSCAFile        string
	DatabaseURL           string
	OracleWallet          string
}

// SEM@3b682947: parse rotator options from the environment with defaults (pure)
func loadOptions(getenv func(string) string) (options, error) {
	get := func(k, def string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return def
	}
	o := options{
		Force:            getenv("ROTATE"),
		Namespace:        get("TMI_ROTATOR_NAMESPACE", "tmi-platform"),
		SecretName:       get("TMI_ROTATOR_SECRET", "tmi-secrets"),
		ServerDeployment: get("TMI_ROTATOR_SERVER_DEPLOYMENT", "tmi-server"),
		RedisHost:        get("TMI_REDIS_HOST", "redis"),
		RedisPort:        get("TMI_REDIS_PORT", "6379"),
		RedisTLSEnabled:  get("TMI_REDIS_TLS_ENABLED", "false") == "true",
		RedisTLSCAFile:   getenv("TMI_REDIS_TLS_CA_FILE"),
		DatabaseURL:      getenv("TMI_DATABASE_URL"),
		OracleWallet:     getenv("TMI_ORACLE_WALLET_LOCATION"),
	}
	var err error
	if o.RolloutTimeout, err = time.ParseDuration(get("TMI_ROTATOR_ROLLOUT_TIMEOUT", "10m")); err != nil {
		return o, fmt.Errorf("TMI_ROTATOR_ROLLOUT_TIMEOUT: %w", err)
	}
	if o.SettingsPreviousGrace, err = time.ParseDuration(get("TMI_ROTATOR_SETTINGS_PREVIOUS_GRACE", "192h")); err != nil {
		return o, fmt.Errorf("TMI_ROTATOR_SETTINGS_PREVIOUS_GRACE: %w", err)
	}
	if o.RedisDB, err = strconv.Atoi(get("TMI_REDIS_DB", "0")); err != nil {
		return o, fmt.Errorf("TMI_REDIS_DB: %w", err)
	}
	return o, nil
}

// SEM@3b682947: validate whether an error is a Redis authentication failure (pure)
func isRedisAuthError(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return strings.Contains(m, "WRONGPASS") || strings.Contains(m, "NOAUTH")
}

// SEM@3b682947: run the rotator and exit with its status code
func main() {
	os.Exit(run())
}

// SEM@3b682947: wire cluster, Redis and DB clients and run every rotation; return the exit code
func run() int {
	logger := slogging.Get()
	o, err := loadOptions(os.Getenv)
	if err != nil {
		logger.Error("Invalid rotator configuration: %v", err)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*o.RolloutTimeout+5*time.Minute)
	defer cancel()

	restCfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		logger.Error("Kubernetes client config: %v", err)
		return 2
	}
	cs, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		logger.Error("Kubernetes client: %v", err)
		return 2
	}
	secrets := rotator.NewKubeSecretStore(cs, o.Namespace)
	env := &rotator.Env{
		Secrets:          secrets,
		Rollouts:         rotator.NewKubeRolloutWaiter(cs, o.Namespace),
		SecretName:       o.SecretName,
		ServerDeployment: o.ServerDeployment,
		RolloutTimeout:   o.RolloutTimeout,
		Now:              time.Now,
	}

	// Redis: the password is whatever the Secret holds right now (never this
	// pod's env, which may predate a swap).
	sec, err := secrets.Get(ctx, o.SecretName)
	if err != nil {
		logger.Error("Read %s: %v", o.SecretName, err)
		return 2
	}
	redisDB, err := db.NewRedisDB(db.RedisConfig{
		Host: o.RedisHost, Port: o.RedisPort, DB: o.RedisDB,
		PasswordFunc: rotator.RedisPasswordFromSecret(secrets, o.SecretName),
		TLSEnabled:   o.RedisTLSEnabled, TLSCAFile: o.RedisTLSCAFile,
	})
	if err != nil {
		if isRedisAuthError(err) {
			logger.Error("Redis rejected the password in %s; if a rotation was interrupted, restart Redis (kubectl rollout restart deploy/redis) so it reloads the password from the Secret", o.SecretName)
		} else {
			logger.Error("Redis connection failed")
		}
		return 2
	}
	defer func() { _ = redisDB.Close() }()

	dbURL := o.DatabaseURL
	if dbURL == "" {
		dbURL = sec.Data["TMI_DATABASE_URL"]
	}
	gormCfg, err := db.ParseDatabaseURL(dbURL)
	if err != nil {
		// err embeds the raw URL, including the password: log a fixed message.
		logger.Error("TMI_DATABASE_URL is not a valid database URL")
		return 2
	}
	if o.OracleWallet != "" {
		gormCfg.OracleWalletLocation = o.OracleWallet
	}
	gormDB, err := db.NewGormDB(*gormCfg)
	if err != nil {
		logger.Error("Database connection failed")
		return 2
	}
	defer func() { _ = gormDB.Close() }()

	// Settings key first: it needs no Redis credential change, so a Redis
	// password problem cannot strand it.
	rotations := []rotator.Rotation{
		rotator.NewSettingsKeyRotation(rotator.NewGormSettingsStore(gormDB.DB(), redisDB), o.SettingsPreviousGrace),
		rotator.NewRedisPasswordRotation(rotator.NewGoRedisACL(redisDB.GetClient())),
	}
	if err := rotator.Run(ctx, env, rotations, o.Force); err != nil {
		logger.Error("Rotation run failed: %v", err)
		return 1
	}
	logger.Info("Rotation run complete")
	return 0
}
