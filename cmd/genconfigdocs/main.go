// Command genconfigdocs writes config-reference.md from the classification registry.
package main

import (
	"os"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

func main() {
	logger := slogging.Get()
	out, err := config.GenerateReferenceMarkdown()
	if err != nil {
		logger.Error("genconfigdocs: %v", err)
		os.Exit(1)
	}
	if err := os.WriteFile("config-reference.md", out, 0o644); err != nil { //nolint:gosec // doc artifact, not secret
		logger.Error("genconfigdocs: write config-reference.md: %v", err)
		os.Exit(1)
	}
	logger.Info("genconfigdocs: wrote config-reference.md")
}
