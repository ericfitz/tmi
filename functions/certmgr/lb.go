package main

import (
	"context"
	"fmt"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/loadbalancer"
)

// LoadBalancerManager handles OCI Load Balancer certificate operations
type LoadBalancerManager struct {
	client         loadbalancer.LoadBalancerClient
	loadBalancerID string
}

// NewLoadBalancerManager creates a new Load Balancer manager using Resource Principal authentication
func NewLoadBalancerManager(ctx context.Context, loadBalancerID string) (*LoadBalancerManager, error) {
	// Use Resource Principal authentication (for OCI Functions)
	provider, err := auth.ResourcePrincipalConfigurationProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create resource principal provider: %w", err)
	}

	client, err := loadbalancer.NewLoadBalancerClientWithConfigurationProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create load balancer client: %w", err)
	}

	return &LoadBalancerManager{
		client:         client,
		loadBalancerID: loadBalancerID,
	}, nil
}

// UpdateCertificate updates the SSL certificate on the load balancer
func (m *LoadBalancerManager) UpdateCertificate(ctx context.Context, certName string, cert *Certificate) error {
	// Create the certificate on the load balancer
	createCertReq := loadbalancer.CreateCertificateRequest{
		LoadBalancerId: common.String(m.loadBalancerID),
		CreateCertificateDetails: loadbalancer.CreateCertificateDetails{
			CertificateName:   common.String(certName),
			PublicCertificate: common.String(cert.CertificatePEM),
			PrivateKey:        common.String(cert.PrivateKeyPEM),
			CaCertificate:     common.String(cert.IssuerPEM),
		},
	}

	createResp, err := m.client.CreateCertificate(ctx, createCertReq)
	if err != nil {
		// If certificate already exists, we need to delete and recreate
		// OCI Load Balancer doesn't support updating certificates in-place
		if isConflictError(err) {
			if err := m.deleteCertificate(ctx, certName); err != nil {
				return fmt.Errorf("failed to delete existing certificate: %w", err)
			}

			// Retry creation
			createResp, err = m.client.CreateCertificate(ctx, createCertReq)
			if err != nil {
				return fmt.Errorf("failed to create certificate after deletion: %w", err)
			}
		} else {
			return fmt.Errorf("failed to create certificate: %w", err)
		}
	}

	// Wait for the work request to complete
	if err := m.waitForWorkRequest(ctx, *createResp.OpcWorkRequestId); err != nil {
		return fmt.Errorf("failed waiting for certificate creation: %w", err)
	}

	return nil
}

// deleteCertificate deletes a certificate from the load balancer
func (m *LoadBalancerManager) deleteCertificate(ctx context.Context, certName string) error {
	deleteReq := loadbalancer.DeleteCertificateRequest{
		LoadBalancerId:  common.String(m.loadBalancerID),
		CertificateName: common.String(certName),
	}

	deleteResp, err := m.client.DeleteCertificate(ctx, deleteReq)
	if err != nil {
		return fmt.Errorf("failed to delete certificate: %w", err)
	}

	// Wait for the work request to complete
	if err := m.waitForWorkRequest(ctx, *deleteResp.OpcWorkRequestId); err != nil {
		return fmt.Errorf("failed waiting for certificate deletion: %w", err)
	}

	return nil
}

// waitForWorkRequest waits for an OCI work request to complete
func (m *LoadBalancerManager) waitForWorkRequest(ctx context.Context, workRequestID string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		getReq := loadbalancer.GetWorkRequestRequest{
			WorkRequestId: common.String(workRequestID),
		}

		getResp, err := m.client.GetWorkRequest(ctx, getReq)
		if err != nil {
			return fmt.Errorf("failed to get work request: %w", err)
		}

		switch getResp.LifecycleState {
		case loadbalancer.WorkRequestLifecycleStateSucceeded:
			return nil
		case loadbalancer.WorkRequestLifecycleStateFailed:
			return fmt.Errorf("work request failed")
		default:
			// Still in progress, wait and retry
			time.Sleep(2 * time.Second)
		}
	}
}

// isConflictError checks if the error is a conflict (resource already exists)
func isConflictError(err error) bool {
	if serviceErr, ok := common.IsServiceError(err); ok {
		return serviceErr.GetHTTPStatusCode() == 409
	}
	return false
}

// GetListenerCertificateName gets the certificate name used by the HTTPS listener
func (m *LoadBalancerManager) GetListenerCertificateName(ctx context.Context, listenerName string) (string, error) {
	getReq := loadbalancer.GetLoadBalancerRequest{
		LoadBalancerId: common.String(m.loadBalancerID),
	}

	getResp, err := m.client.GetLoadBalancer(ctx, getReq)
	if err != nil {
		return "", fmt.Errorf("failed to get load balancer: %w", err)
	}

	listener, ok := getResp.Listeners[listenerName]
	if !ok {
		return "", fmt.Errorf("listener %s not found", listenerName)
	}

	if listener.SslConfiguration == nil || listener.SslConfiguration.CertificateName == nil {
		return "", fmt.Errorf("no SSL configuration on listener %s", listenerName)
	}

	return *listener.SslConfiguration.CertificateName, nil
}

// UpdateListenerCertificate updates the HTTPS listener to use a new certificate
func (m *LoadBalancerManager) UpdateListenerCertificate(ctx context.Context, listenerName, certName string) error {
	// Get current listener configuration
	getReq := loadbalancer.GetLoadBalancerRequest{
		LoadBalancerId: common.String(m.loadBalancerID),
	}

	getResp, err := m.client.GetLoadBalancer(ctx, getReq)
	if err != nil {
		return fmt.Errorf("failed to get load balancer: %w", err)
	}

	listener, ok := getResp.Listeners[listenerName]
	if !ok {
		return fmt.Errorf("listener %s not found", listenerName)
	}

	// Update the listener with new certificate
	updateReq := loadbalancer.UpdateListenerRequest{
		LoadBalancerId: common.String(m.loadBalancerID),
		ListenerName:   common.String(listenerName),
		UpdateListenerDetails: loadbalancer.UpdateListenerDetails{
			DefaultBackendSetName: listener.DefaultBackendSetName,
			Port:                  listener.Port,
			Protocol:              listener.Protocol,
			SslConfiguration: &loadbalancer.SslConfigurationDetails{
				CertificateName:       common.String(certName),
				VerifyDepth:           common.Int(1),
				VerifyPeerCertificate: common.Bool(false),
				Protocols:             []string{"TLSv1.2", "TLSv1.3"},
				CipherSuiteName:       common.String("oci-modern-ssl-cipher-suite-v1"),
			},
		},
	}

	updateResp, err := m.client.UpdateListener(ctx, updateReq)
	if err != nil {
		return fmt.Errorf("failed to update listener: %w", err)
	}

	// Wait for the work request to complete
	if err := m.waitForWorkRequest(ctx, *updateResp.OpcWorkRequestId); err != nil {
		return fmt.Errorf("failed waiting for listener update: %w", err)
	}

	return nil
}
