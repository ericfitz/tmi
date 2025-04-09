package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"github.com/joho/godotenv"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig
	Auth     AuthConfig
	Database DatabaseConfig
	Logging  LoggingConfig
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port             string
	Interface        string        // Interface to listen on
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	LogLevel         string
	TLSEnabled       bool          // Enable/disable TLS
	TLSCertFile      string        // Path to TLS certificate file
	TLSKeyFile       string        // Path to TLS private key file
	TLSSubjectName   string        // Subject name for certificate validation
	HTTPToHTTPSRedirect bool       // Whether to redirect HTTP to HTTPS
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	JWTSecret    string
	JWTExpiresIn time.Duration
	OAuthURL     string
	OAuthSecret  string
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	URL      string
	Username string
	Password string
	Name     string
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level           logging.LogLevel
	IsDev           bool
	LogDir          string
	MaxAgeDays      int
	MaxSizeMB       int
	MaxBackups      int
	AlsoLogToConsole bool
}

// LoadEnvFile loads environment variables from .env file
func LoadEnvFile(envFile string) error {
	if envFile == "" {
		// Look for .env file in the current directory
		envFile = ".env"
	}

	// Check if the file exists
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		// If the specified file doesn't exist, try to find .env in parent directories
		if filepath.Base(envFile) == ".env" {
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}

			// Search up to 3 parent directories for .env file
			for i := 0; i < 3; i++ {
				dir = filepath.Dir(dir)
				testPath := filepath.Join(dir, ".env")
				if _, err := os.Stat(testPath); err == nil {
					envFile = testPath
					break
				}
			}
		}
	}

	// If the file exists, load it
	if _, err := os.Stat(envFile); err == nil {
		if err := godotenv.Load(envFile); err != nil {
			return fmt.Errorf("error loading %s file: %w", envFile, err)
		}
		fmt.Printf("Loaded environment from %s\n", envFile)
	} else {
		// Not finding a .env file is not an error - will use environment variables and defaults
		fmt.Println("No .env file found, using environment variables and defaults")
	}

	return nil
}

// LoadConfig loads configuration from environment variables
func LoadConfig() Config {
	// Try to load .env file (ignoring errors as it's optional)
	_ = LoadEnvFile("")

	// Parse log level
	logLevelStr := getEnv("LOG_LEVEL", "info")
	
	// Get hostname for default TLS subject name
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost" // Fallback if hostname can't be determined
	}

	return Config{
		Server: ServerConfig{
			Port:               getEnv("SERVER_PORT", "8080"),
			Interface:          getEnv("SERVER_INTERFACE", "0.0.0.0"),
			ReadTimeout:        parseDuration(getEnv("SERVER_READ_TIMEOUT", "5s")),
			WriteTimeout:       parseDuration(getEnv("SERVER_WRITE_TIMEOUT", "10s")),
			IdleTimeout:        parseDuration(getEnv("SERVER_IDLE_TIMEOUT", "60s")),
			LogLevel:           logLevelStr,
			TLSEnabled:         getEnv("TLS_ENABLED", "false") == "true",
			TLSCertFile:        getEnv("TLS_CERT_FILE", ""),
			TLSKeyFile:         getEnv("TLS_KEY_FILE", ""),
			TLSSubjectName:     getEnv("TLS_SUBJECT_NAME", hostname),
			HTTPToHTTPSRedirect: getEnv("TLS_HTTP_REDIRECT", "true") == "true",
		},
		Auth: AuthConfig{
			JWTSecret:    getEnv("JWT_SECRET", "secret"),
			JWTExpiresIn: parseDuration(getEnv("JWT_EXPIRES_IN", "24h")),
			OAuthURL:     getEnv("OAUTH_URL", "https://oauth-provider.com/auth"),
			OAuthSecret:  getEnv("OAUTH_SECRET", ""),
		},
		Database: DatabaseConfig{
			URL:      getEnv("DB_URL", "localhost"),
			Username: getEnv("DB_USERNAME", ""),
			Password: getEnv("DB_PASSWORD", ""),
			Name:     getEnv("DB_NAME", "tmi"),
		},
		Logging: LoggingConfig{
			Level:            logging.ParseLogLevel(logLevelStr),
			IsDev:            getEnv("ENV", "development") != "production",
			LogDir:           getEnv("LOG_DIR", "logs"),
			MaxAgeDays:       parseInt(getEnv("LOG_MAX_AGE_DAYS", "7"), 7),
			MaxSizeMB:        parseInt(getEnv("LOG_MAX_SIZE_MB", "100"), 100),
			MaxBackups:       parseInt(getEnv("LOG_MAX_BACKUPS", "10"), 10),
			AlsoLogToConsole: getEnv("LOG_TO_CONSOLE", "true") == "true",
		},
	}
}

// Helper function to get environment variables with fallback
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// Helper function to parse duration strings
func parseDuration(val string) time.Duration {
	duration, err := time.ParseDuration(val)
	if err != nil {
		return 0
	}
	return duration
}

// Helper function to parse integer values
func parseInt(val string, fallback int) int {
	if i, err := strconv.Atoi(val); err == nil {
		return i
	}
	return fallback
}