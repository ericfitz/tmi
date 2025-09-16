package config

import (
	"flag"
	"fmt"
	"os"

	"github.com/ericfitz/tmi/internal/slogging"
)

// ParseFlags parses command line flags and returns the config file path
func ParseFlags() (configFile string, generateConfig bool, err error) {
	flag.StringVar(&configFile, "config", "", "Path to configuration file")
	flag.BoolVar(&generateConfig, "generate-config", false, "Generate example configuration file")

	// Add help flag
	help := flag.Bool("help", false, "Show help")

	flag.Parse()

	if *help {
		slogging.Get().Debug("Displaying help and exiting")
		flag.Usage()
		os.Exit(0)
	}

	if generateConfig {
		slogging.Get().Info("Configuration generation requested")
		return "", true, nil
	}

	if configFile != "" {
		slogging.Get().Info("Using configuration file: %s", configFile)
	} else {
		slogging.Get().Info("No configuration file specified, using defaults")
	}

	return configFile, false, nil
}

// GenerateExampleConfig generates example configuration files
func GenerateExampleConfig() error {
	slogging.Get().Debug("Displaying configuration setup help to user")

	fmt.Println("Configuration files already exist in the project:")
	fmt.Println("- config-example.yml - Template configuration file")
	fmt.Println("- config-development.yml - Development configuration (if present)")
	fmt.Println("- config-production.yml - Production configuration template")
	fmt.Println("- config-test.yml - Test configuration")
	fmt.Println("")
	fmt.Println("To customize for your environment:")
	fmt.Println("1. Copy config-example.yml to config-development.yml")
	fmt.Println("2. Edit config-development.yml with your settings")
	fmt.Println("3. For production, customize config-production.yml")
	fmt.Println("")
	fmt.Println("Note: Environment variables can override any YAML setting using TMI_ prefix.")
	return nil
}
