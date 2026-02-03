package main

import (
	"context"
	"fmt"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/dns"
)

// DNSManager handles OCI DNS operations
type DNSManager struct {
	client        dns.DnsClient
	zoneID        string
	compartmentID string
}

// NewDNSManager creates a new DNS manager using Resource Principal authentication
func NewDNSManager(ctx context.Context, zoneID, compartmentID string) (*DNSManager, error) {
	// Use Resource Principal authentication (for OCI Functions)
	provider, err := auth.ResourcePrincipalConfigurationProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create resource principal provider: %w", err)
	}

	client, err := dns.NewDnsClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS client: %w", err)
	}

	return &DNSManager{
		client:        client,
		zoneID:        zoneID,
		compartmentID: compartmentID,
	}, nil
}

// CreateACMEChallengeTXTRecord creates a TXT record for ACME DNS-01 challenge
func (m *DNSManager) CreateACMEChallengeTXTRecord(ctx context.Context, domain, challengeValue string) error {
	// The record name is _acme-challenge.domain
	recordName := "_acme-challenge." + domain

	// Use UpdateZoneRecords to add the TXT record
	operation := dns.RecordOperationOperationAdd
	items := []dns.RecordOperation{
		{
			Domain:    common.String(recordName),
			Rtype:     common.String("TXT"),
			Rdata:     common.String(fmt.Sprintf(`"%s"`, challengeValue)),
			Ttl:       common.Int(60), // Short TTL for challenge
			Operation: operation,
		},
	}

	req := dns.PatchZoneRecordsRequest{
		ZoneNameOrId:  common.String(m.zoneID),
		CompartmentId: common.String(m.compartmentID),
		PatchZoneRecordsDetails: dns.PatchZoneRecordsDetails{
			Items: items,
		},
	}

	_, err := m.client.PatchZoneRecords(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to create TXT record: %w", err)
	}

	return nil
}

// DeleteACMEChallengeTXTRecord deletes the ACME challenge TXT record
func (m *DNSManager) DeleteACMEChallengeTXTRecord(ctx context.Context, domain string) error {
	recordName := "_acme-challenge." + domain

	// Delete the RRSet
	deleteReq := dns.DeleteRRSetRequest{
		ZoneNameOrId:  common.String(m.zoneID),
		Domain:        common.String(recordName),
		Rtype:         common.String("TXT"),
		CompartmentId: common.String(m.compartmentID),
	}

	_, err := m.client.DeleteRRSet(ctx, deleteReq)
	if err != nil {
		// Record might not exist, which is fine - check for 404
		if serviceErr, ok := common.IsServiceError(err); ok && serviceErr.GetHTTPStatusCode() == 404 {
			return nil
		}
		return fmt.Errorf("failed to delete TXT record: %w", err)
	}

	return nil
}
