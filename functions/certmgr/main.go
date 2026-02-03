package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"time"

	fdk "github.com/fnproject/fdk-go"
)

// Response represents the function response
type Response struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	Domain      string `json:"domain,omitempty"`
	Certificate struct {
		NotAfter      string `json:"not_after,omitempty"`
		DaysRemaining int    `json:"days_remaining,omitempty"`
		Renewed       bool   `json:"renewed"`
	} `json:"certificate,omitempty"`
	Error string `json:"error,omitempty"`
}

func main() {
	fdk.Handle(fdk.HandlerFunc(handler))
}

func handler(ctx context.Context, in io.Reader, out io.Writer) {
	response := Response{
		Status: "success",
	}

	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		response.Status = "error"
		response.Error = fmt.Sprintf("failed to load config: %v", err)
		writeResponse(out, response)
		return
	}

	response.Domain = config.Domain

	log.Printf("Certificate manager starting for domain: %s", config.Domain)

	// Check if this is a dry run
	if config.DryRun {
		log.Println("Dry run mode - skipping actual operations")
		response.Message = "Dry run mode - no operations performed"
		writeResponse(out, response)
		return
	}

	// Run the certificate check and renewal
	renewed, certInfo, err := runCertificateCheck(ctx, config)
	if err != nil {
		response.Status = "error"
		response.Error = fmt.Sprintf("certificate check failed: %v", err)
		writeResponse(out, response)
		return
	}

	response.Certificate.Renewed = renewed
	if certInfo != nil {
		response.Certificate.NotAfter = certInfo.NotAfter.Format(time.RFC3339)
		response.Certificate.DaysRemaining = certInfo.DaysRemaining
	}

	if renewed {
		response.Message = "Certificate renewed successfully"
	} else {
		response.Message = fmt.Sprintf("Certificate valid, %d days remaining", certInfo.DaysRemaining)
	}

	writeResponse(out, response)
}

func writeResponse(out io.Writer, response Response) {
	if err := json.NewEncoder(out).Encode(response); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func runCertificateCheck(ctx context.Context, config *Config) (bool, *CertificateInfo, error) {
	// Set a timeout for the entire operation
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	// Initialize managers
	log.Println("Initializing OCI clients...")

	vaultMgr, err := NewVaultManager(ctx, config.VaultID, config.VaultKeyID, config.CompartmentID, config.NamePrefix)
	if err != nil {
		return false, nil, fmt.Errorf("failed to create vault manager: %w", err)
	}

	// Check existing certificate
	log.Println("Checking existing certificate...")
	certInfo, err := vaultMgr.GetCertificate(ctx)
	if err != nil {
		return false, nil, fmt.Errorf("failed to get certificate: %w", err)
	}

	// If certificate exists and has enough days remaining, skip renewal
	if certInfo != nil && certInfo.DaysRemaining > config.RenewalDays {
		log.Printf("Certificate valid for %d more days (threshold: %d), skipping renewal",
			certInfo.DaysRemaining, config.RenewalDays)
		return false, certInfo, nil
	}

	// Need to renew certificate
	if certInfo != nil {
		log.Printf("Certificate expires in %d days, renewing...", certInfo.DaysRemaining)
	} else {
		log.Println("No existing certificate found, requesting new certificate...")
	}

	// Get or create ACME account key
	accountKeyPEM, err := vaultMgr.GetAccountKey(ctx)
	if err != nil {
		return false, certInfo, fmt.Errorf("failed to get account key: %w", err)
	}

	var accountKey *ecdsa.PrivateKey
	if accountKeyPEM == "" {
		log.Println("Generating new ACME account key...")
		accountKey, err = GenerateAccountKey()
		if err != nil {
			return false, certInfo, fmt.Errorf("failed to generate account key: %w", err)
		}

		keyPEM, err := EncodeAccountKey(accountKey)
		if err != nil {
			return false, certInfo, fmt.Errorf("failed to encode account key: %w", err)
		}

		if err := vaultMgr.StoreAccountKey(ctx, keyPEM); err != nil {
			return false, certInfo, fmt.Errorf("failed to store account key: %w", err)
		}
	} else {
		log.Println("Using existing ACME account key...")
		accountKey, err = DecodeAccountKey(accountKeyPEM)
		if err != nil {
			return false, certInfo, fmt.Errorf("failed to decode account key: %w", err)
		}
	}

	// Create ACME client
	acmeClient := NewACMEClient(config.ACMEDirectory, config.ACMEEmail, accountKey)

	// Register account
	log.Println("Registering ACME account...")
	if err := acmeClient.RegisterAccount(ctx); err != nil {
		return false, certInfo, fmt.Errorf("failed to register ACME account: %w", err)
	}

	// Initialize DNS manager
	dnsMgr, err := NewDNSManager(ctx, config.DNSZoneID, config.CompartmentID)
	if err != nil {
		return false, certInfo, fmt.Errorf("failed to create DNS manager: %w", err)
	}

	// Request certificate and get DNS challenge
	log.Println("Requesting certificate authorization...")
	auth, challenge, err := acmeClient.RequestCertificate(ctx, config.Domain)
	if err != nil {
		return false, certInfo, fmt.Errorf("failed to request certificate: %w", err)
	}

	// Get the DNS challenge record value
	challengeValue, err := acmeClient.GetDNSChallengeRecord(challenge)
	if err != nil {
		return false, certInfo, fmt.Errorf("failed to get DNS challenge record: %w", err)
	}

	// Create DNS TXT record
	log.Printf("Creating DNS TXT record for challenge...")
	if err := dnsMgr.CreateACMEChallengeTXTRecord(ctx, config.Domain, challengeValue); err != nil {
		return false, certInfo, fmt.Errorf("failed to create DNS TXT record: %w", err)
	}

	// Ensure cleanup of DNS record
	defer func() {
		log.Println("Cleaning up DNS TXT record...")
		if err := dnsMgr.DeleteACMEChallengeTXTRecord(context.Background(), config.Domain); err != nil {
			log.Printf("Warning: failed to delete DNS TXT record: %v", err)
		}
	}()

	// Wait for DNS propagation
	log.Println("Waiting for DNS propagation...")
	time.Sleep(60 * time.Second)

	// Accept the challenge
	log.Println("Accepting ACME challenge...")
	if err := acmeClient.AcceptChallenge(ctx, challenge); err != nil {
		return false, certInfo, fmt.Errorf("failed to accept challenge: %w", err)
	}

	// Wait for authorization
	log.Println("Waiting for authorization...")
	if err := acmeClient.WaitForAuthorization(ctx, auth); err != nil {
		return false, certInfo, fmt.Errorf("failed waiting for authorization: %w", err)
	}

	// Finalize and get certificate
	log.Println("Finalizing certificate...")
	cert, err := acmeClient.FinalizeCertificate(ctx, config.Domain)
	if err != nil {
		return false, certInfo, fmt.Errorf("failed to finalize certificate: %w", err)
	}

	// Store certificate in vault
	log.Println("Storing certificate in vault...")
	if err := vaultMgr.StoreCertificate(ctx, cert); err != nil {
		return false, certInfo, fmt.Errorf("failed to store certificate: %w", err)
	}

	// Update load balancer
	log.Println("Updating load balancer certificate...")
	lbMgr, err := NewLoadBalancerManager(ctx, config.LoadBalancerID)
	if err != nil {
		return false, certInfo, fmt.Errorf("failed to create load balancer manager: %w", err)
	}

	certName := config.NamePrefix + "-cert"
	if err := lbMgr.UpdateCertificate(ctx, certName, cert); err != nil {
		return false, certInfo, fmt.Errorf("failed to update load balancer certificate: %w", err)
	}

	// Update listener to use new certificate (if not already using it)
	log.Println("Updating load balancer listener...")
	if err := lbMgr.UpdateListenerCertificate(ctx, "https", certName); err != nil {
		// This might fail if listener doesn't exist yet, which is OK
		log.Printf("Warning: failed to update listener: %v", err)
	}

	log.Printf("Certificate renewed successfully! Valid until: %s", cert.NotAfter)

	newCertInfo := &CertificateInfo{
		NotAfter:      cert.NotAfter,
		DaysRemaining: int(time.Until(cert.NotAfter).Hours() / 24),
	}

	return true, newCertInfo, nil
}
