# Load YAML configuration into Makefile variables
# Usage: include scripts/load-config.mk
# Requires: CONFIG_FILE variable to be set to the path of the YAML config file

ifndef CONFIG_FILE
$(error CONFIG_FILE variable is not set. Usage: CONFIG_FILE=config/target.yml include scripts/load-config.mk)
endif

# Check if config file exists
ifeq (,$(wildcard $(CONFIG_FILE)))
$(error Configuration file not found: $(CONFIG_FILE))
endif

# Load YAML configuration into Make variables
# Note: Use a temporary file to avoid shell evaluation issues
$(shell uv run scripts/yaml-to-make.py $(CONFIG_FILE) > .config.tmp.mk)
include .config.tmp.mk

# Debug output (uncomment to see loaded variables)
# $(info Loaded configuration from $(CONFIG_FILE))
# $(info NAME = $(NAME))
# $(info DESCRIPTION = $(DESCRIPTION))