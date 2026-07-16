// Command genconfig writes config-example.yml from the classification registry.
package main

import (
	"os"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// SEM@a60b4f430769f6d36f0e3753a429ea699ba8b1a0: generate and write the annotated config-example.yml file to disk
func main() {
	logger := slogging.Get()
	out, err := config.GenerateExampleConfig()
	if err != nil {
		logger.Error("genconfig: %v", err)
		os.Exit(1)
	}
	if err := os.WriteFile("config-example.yml", out, 0o644); err != nil { // #nosec G306 -- example file, not secret
		logger.Error("genconfig: write config-example.yml: %v", err)
		os.Exit(1)
	}
	logger.Info("genconfig: wrote config-example.yml")
}
