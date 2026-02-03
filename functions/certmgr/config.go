package main

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds the certificate manager configuration
type Config struct {
	Domain         string
	DNSZoneID      string
	ACMEEmail      string
	RenewalDays    int
	LoadBalancerID string
	VaultID        string
	VaultKeyID     string
	CompartmentID  string
	NamePrefix     string
	ACMEDirectory  string
	DryRun         bool
}

// LoadConfig loads configuration from environment variables
func LoadConfig() (*Config, error) {
	config := &Config{
		Domain:         os.Getenv("CERTMGR_DOMAIN"),
		DNSZoneID:      os.Getenv("CERTMGR_DNS_ZONE_ID"),
		ACMEEmail:      os.Getenv("CERTMGR_ACME_EMAIL"),
		LoadBalancerID: os.Getenv("CERTMGR_LB_ID"),
		VaultID:        os.Getenv("CERTMGR_VAULT_ID"),
		VaultKeyID:     os.Getenv("CERTMGR_VAULT_KEY_ID"),
		CompartmentID:  os.Getenv("CERTMGR_COMPARTMENT_ID"),
		NamePrefix:     os.Getenv("CERTMGR_NAME_PREFIX"),
		ACMEDirectory:  os.Getenv("CERTMGR_ACME_DIRECTORY"),
		DryRun:         os.Getenv("CERTMGR_DRY_RUN") == "true",
	}

	// Default name prefix
	if config.NamePrefix == "" {
		config.NamePrefix = "tmi"
	}

	// Parse renewal days with default
	renewalDaysStr := os.Getenv("CERTMGR_RENEWAL_DAYS")
	if renewalDaysStr == "" {
		config.RenewalDays = 30
	} else {
		days, err := strconv.Atoi(renewalDaysStr)
		if err != nil {
			return nil, fmt.Errorf("invalid CERTMGR_RENEWAL_DAYS: %w", err)
		}
		config.RenewalDays = days
	}

	// Default ACME directory to staging
	if config.ACMEDirectory == "" {
		config.ACMEDirectory = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}

	// Validate required fields
	if config.Domain == "" {
		return nil, fmt.Errorf("CERTMGR_DOMAIN is required")
	}
	if config.DNSZoneID == "" {
		return nil, fmt.Errorf("CERTMGR_DNS_ZONE_ID is required")
	}
	if config.ACMEEmail == "" {
		return nil, fmt.Errorf("CERTMGR_ACME_EMAIL is required")
	}
	if config.LoadBalancerID == "" {
		return nil, fmt.Errorf("CERTMGR_LB_ID is required")
	}
	if config.VaultID == "" {
		return nil, fmt.Errorf("CERTMGR_VAULT_ID is required")
	}
	if config.VaultKeyID == "" {
		return nil, fmt.Errorf("CERTMGR_VAULT_KEY_ID is required")
	}

	return config, nil
}
