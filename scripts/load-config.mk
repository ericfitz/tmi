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
CONFIG_TMP_FILE := .config.$(shell echo $(CONFIG_FILE) | sed 's|/|_|g' | sed 's|\.yml||').mk
$(shell uv run scripts/yaml-to-make.py $(CONFIG_FILE) > $(CONFIG_TMP_FILE))
include $(CONFIG_TMP_FILE)

# Debug output (uncomment to see loaded variables)
# $(info Loaded configuration from $(CONFIG_FILE))
# $(info NAME = $(NAME))
# $(info DESCRIPTION = $(DESCRIPTION))