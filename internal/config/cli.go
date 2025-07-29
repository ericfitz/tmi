package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ParseFlags parses command line flags and returns the config file path
func ParseFlags() (configFile string, generateConfig bool, err error) {
	flag.StringVar(&configFile, "config", "", "Path to configuration file")
	flag.BoolVar(&generateConfig, "generate-config", false, "Generate example configuration file")

	// Add help flag
	help := flag.Bool("help", false, "Show help")

	flag.Parse()

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if generateConfig {
		return "", true, nil
	}

	return configFile, false, nil
}

// GenerateExampleConfig generates example configuration files
func GenerateExampleConfig() error {
	// Create development config
	devConfig := getDefaultConfig()
	devConfig.Logging.IsDev = true
	devConfig.Database.Postgres.SSLMode = "disable"
	devConfig.Auth.JWT.Secret = "development-secret-change-in-production"

	if err := writeConfigFile("config-development.yaml", devConfig); err != nil {
		return fmt.Errorf("failed to create development config: %w", err)
	}
	fmt.Println("Created config-development.yaml")

	// Create production config
	prodConfig := getDefaultConfig()
	prodConfig.Logging.IsDev = false
	prodConfig.Server.TLSEnabled = true
	prodConfig.Server.TLSCertFile = "/etc/tls/server.crt"
	prodConfig.Server.TLSKeyFile = "/etc/tls/server.key"
	prodConfig.Database.Postgres.Host = "postgres"
	prodConfig.Database.Postgres.SSLMode = "require"
	prodConfig.Database.Redis.Host = "redis"
	prodConfig.Auth.OAuth.CallbackURL = "https://tmi.example.com/auth/callback"

	// Clear secrets that should be set via environment variables
	prodConfig.Auth.JWT.Secret = ""
	prodConfig.Database.Postgres.Password = ""
	for id, provider := range prodConfig.Auth.OAuth.Providers {
		provider.ClientID = ""
		provider.ClientSecret = ""
		prodConfig.Auth.OAuth.Providers[id] = provider
	}

	if err := writeConfigFile("config-production.yaml", prodConfig); err != nil {
		return fmt.Errorf("failed to create production config: %w", err)
	}
	fmt.Println("Created config-production.yaml")

	// Create docker-compose environment file
	if err := writeDockerComposeEnv(); err != nil {
		return fmt.Errorf("failed to create docker-compose.env: %w", err)
	}
	fmt.Println("Created docker-compose.env")

	return nil
}

// writeConfigFile writes a configuration to a YAML file
func writeConfigFile(filename string, config *Config) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(filename)
	if dir != "." {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	// Add header comment
	header := `# TMI Configuration File
# 
# This file contains the configuration for the TMI server.
# Environment variables can override any value using the TMI_ prefix.
# 
# Examples:
#   TMI_SERVER_PORT=9090
#   TMI_DATABASE_POSTGRES_PASSWORD=secret123
#   TMI_AUTH_JWT_SECRET=your-jwt-secret
#   TMI_AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENT_ID=your-google-client-id
#

`

	fullContent := header + string(data)

	if err := os.WriteFile(filename, []byte(fullContent), 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// writeDockerComposeEnv writes an example environment file for Docker Compose
func writeDockerComposeEnv() error {
	content := `# TMI Docker Compose Environment Variables
# Copy this file to .env and customize for your environment

# Database Configuration
TMI_DATABASE_POSTGRES_HOST=postgres
TMI_DATABASE_POSTGRES_PORT=5432
TMI_DATABASE_POSTGRES_USER=tmi_user
TMI_DATABASE_POSTGRES_PASSWORD=change-this-password
TMI_DATABASE_POSTGRES_DATABASE=tmi
TMI_DATABASE_POSTGRES_SSLMODE=disable

TMI_DATABASE_REDIS_HOST=redis
TMI_DATABASE_REDIS_PORT=6379
TMI_DATABASE_REDIS_PASSWORD=
TMI_DATABASE_REDIS_DB=0

# JWT Configuration
TMI_AUTH_JWT_SECRET=change-this-jwt-secret-to-something-secure
TMI_AUTH_JWT_EXPIRATION_SECONDS=3600

# OAuth Configuration
TMI_AUTH_OAUTH_CALLBACK_URL=http://localhost:8080/auth/callback

# Google OAuth (optional - leave empty to disable)
TMI_AUTH_OAUTH_PROVIDERS_GOOGLE_ENABLED=true
TMI_AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENT_ID=your-google-client-id
TMI_AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET=your-google-client-secret

# GitHub OAuth (optional - leave empty to disable)
TMI_AUTH_OAUTH_PROVIDERS_GITHUB_ENABLED=true
TMI_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENT_ID=your-github-client-id
TMI_AUTH_OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET=your-github-client-secret

# Microsoft OAuth (optional - leave empty to disable)
TMI_AUTH_OAUTH_PROVIDERS_MICROSOFT_ENABLED=true
TMI_AUTH_OAUTH_PROVIDERS_MICROSOFT_CLIENT_ID=your-microsoft-client-id
TMI_AUTH_OAUTH_PROVIDERS_MICROSOFT_CLIENT_SECRET=your-microsoft-client-secret

# Server Configuration
TMI_SERVER_PORT=8080
TMI_SERVER_INTERFACE=0.0.0.0

# TLS Configuration (for production)
TMI_TLS_ENABLED=false
TMI_TLS_CERT_FILE=/etc/tls/server.crt
TMI_TLS_KEY_FILE=/etc/tls/server.key

# Logging Configuration
TMI_LOGGING_LEVEL=info
TMI_LOGGING_IS_DEV=false
TMI_LOGGING_ALSO_LOG_TO_CONSOLE=true
`

	if err := os.WriteFile("docker-compose.env", []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write docker-compose.env: %w", err)
	}

	return nil
}
