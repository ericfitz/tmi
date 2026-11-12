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

// PrintConfigHelp prints guidance about configuration files. It is invoked
// when the server binary is run with --generate-config.
func PrintConfigHelp() error {
	slogging.Get().Debug("Displaying configuration setup help to user")

	fmt.Println("config-example.yml is the only configuration template tracked in")
	fmt.Println("the repository. It is generated from the classification registry")
	fmt.Println("and contains the bootstrap-only keys (server, database, auth.jwt,")
	fmt.Println("logging, secrets) with secret values shown as vault:// placeholders.")
	fmt.Println("")
	fmt.Println("The working config files (config-development.yml, config-test.yml,")
	fmt.Println("config-production.yml) are local-only and gitignored — they carry")
	fmt.Println("real secrets and must never be committed.")
	fmt.Println("")
	fmt.Println("To set up your environment:")
	fmt.Println("1. Copy config-example.yml to config-development.yml (or config-test.yml,")
	fmt.Println("   or config-production.yml).")
	fmt.Println("2. Populate the secret values locally (replace the vault:// placeholders).")
	fmt.Println("3. Run the server with --config <your-file>.")
	fmt.Println("")
	fmt.Println("Note: Environment variables can override any YAML setting (e.g.,")
	fmt.Println("TMI_SERVER_PORT, TMI_DATABASE_URL). Operational config (OAuth providers,")
	fmt.Println("timeouts, etc.) lives in the database settings service, not in these files.")
	return nil
}
