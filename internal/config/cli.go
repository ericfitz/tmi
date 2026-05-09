package config

import (
	"flag"
	"fmt"
	"os"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Flags is the parsed result of ParseFlags.
type Flags struct {
	ConfigFile     string
	GenerateConfig bool
	ShowVersion    bool
}

// ParseFlags parses command line flags and returns the config file path.
//
// Deprecated: prefer ParseFlagsExt, which also reports --version.
func ParseFlags() (configFile string, generateConfig bool, err error) {
	f, err := ParseFlagsExt()
	if err != nil {
		return "", false, err
	}
	return f.ConfigFile, f.GenerateConfig, nil
}

// ParseFlagsExt parses command line flags and returns the full Flags struct.
// --version / -v is a re-entrant, side-effect-free path: it can be invoked
// while a server is already running because it does not touch the database,
// listening sockets, or any shared on-disk state.
func ParseFlagsExt() (Flags, error) {
	var f Flags
	flag.StringVar(&f.ConfigFile, "config", "", "Path to configuration file")
	flag.BoolVar(&f.GenerateConfig, "generate-config", false, "Generate example configuration file")
	flag.BoolVar(&f.ShowVersion, "version", false, "Print version, build, architecture, and commit information and exit")
	flag.BoolVar(&f.ShowVersion, "v", false, "Print version information and exit (shorthand for --version)")

	help := flag.Bool("help", false, "Show help")

	flag.Parse()

	if *help {
		slogging.Get().Debug("Displaying help and exiting")
		flag.Usage()
		os.Exit(0)
	}

	if f.ShowVersion {
		// Caller is responsible for printing — keep this function side-effect-free
		// for the version path so it can run before logger init.
		return f, nil
	}

	if f.GenerateConfig {
		slogging.Get().Info("Configuration generation requested")
		return f, nil
	}

	if f.ConfigFile != "" {
		slogging.Get().Info("Using configuration file: %s", f.ConfigFile)
	} else {
		slogging.Get().Info("No configuration file specified, using defaults")
	}

	return f, nil
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
	fmt.Println("Note: Environment variables can override any YAML setting (e.g., SERVER_PORT, POSTGRES_HOST).")
	return nil
}
