package config

import (
	"flag"
	"fmt"
	"os"
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
	fmt.Println("Configuration files already exist in the project:")
	fmt.Println("- config-example.yaml - Template configuration file")
	fmt.Println("- config-development.yaml - Development configuration (if present)")
	fmt.Println("- config-production.yaml - Production configuration template")
	fmt.Println("- config-test.yaml - Test configuration")
	fmt.Println("")
	fmt.Println("To customize for your environment:")
	fmt.Println("1. Copy config-example.yaml to config-development.yaml")
	fmt.Println("2. Edit config-development.yaml with your settings")
	fmt.Println("3. For production, customize config-production.yaml")
	fmt.Println("")
	fmt.Println("Note: Environment variables can override any YAML setting using TMI_ prefix.")
	return nil
}
