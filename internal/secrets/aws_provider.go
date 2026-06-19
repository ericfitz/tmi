package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/ericfitz/tmi/internal/slogging"
)

// AWSProvider retrieves secrets from AWS Secrets Manager.
// It expects secrets to be stored as JSON key-value pairs within a single secret.
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: secrets provider that fetches and caches JSON key-value pairs from AWS Secrets Manager
type AWSProvider struct {
	client     *secretsmanager.Client
	secretName string
	region     string

	// Cache for the parsed secret values
	cache    map[string]string
	cacheMu  sync.RWMutex
	cacheSet bool
}

// NewAWSProvider creates a new AWS Secrets Manager provider
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: connect to AWS Secrets Manager and build a caching AWSProvider for the given secret
func NewAWSProvider(ctx context.Context, region, secretName string) (*AWSProvider, error) {
	logger := slogging.Get()

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := secretsmanager.NewFromConfig(cfg)

	logger.Info("AWS Secrets Manager provider initialized for secret: %s in region: %s", secretName, region)

	return &AWSProvider{
		client:     client,
		secretName: secretName,
		region:     region,
		cache:      make(map[string]string),
	}, nil
}

// GetSecret retrieves a specific key from the AWS secret.
// The secret is expected to be a JSON object with key-value pairs.
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: fetch a secret value by key from the AWS Secrets Manager cache, loading on first call
func (p *AWSProvider) GetSecret(ctx context.Context, key string) (string, error) {
	logger := slogging.Get()

	// Check cache first
	p.cacheMu.RLock()
	if p.cacheSet {
		if value, ok := p.cache[key]; ok {
			p.cacheMu.RUnlock()
			logger.Debug("AWS Secrets Manager cache hit for key: %s", key)
			return value, nil
		}
		p.cacheMu.RUnlock()
		logger.Debug("AWS Secrets Manager cache miss for key: %s", key)
		return "", ErrSecretNotFound
	}
	p.cacheMu.RUnlock()

	// Load secrets from AWS
	if err := p.loadSecrets(ctx); err != nil {
		return "", err
	}

	// Check cache again after loading
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()

	if value, ok := p.cache[key]; ok {
		logger.Debug("AWS Secrets Manager retrieved key: %s", key)
		return value, nil
	}

	return "", ErrSecretNotFound
}

// ListSecrets returns all keys in the AWS secret
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: list all secret keys available in the cached AWS secret
func (p *AWSProvider) ListSecrets(ctx context.Context) ([]string, error) {
	// Ensure cache is loaded
	if !p.cacheSet {
		if err := p.loadSecrets(ctx); err != nil {
			return nil, err
		}
	}

	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()

	keys := make([]string, 0, len(p.cache))
	for key := range p.cache {
		keys = append(keys, key)
	}

	return keys, nil
}

// Name returns the provider name
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: return the provider type identifier for this AWS provider (pure)
func (p *AWSProvider) Name() string {
	return string(ProviderTypeAWS)
}

// Close releases resources (no-op for AWS provider)
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: release provider resources; no-op for the AWS provider (pure)
func (p *AWSProvider) Close() error {
	return nil
}

// loadSecrets fetches and caches the secret from AWS Secrets Manager
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: fetch and cache the full JSON secret from AWS Secrets Manager (mutates shared state)
func (p *AWSProvider) loadSecrets(ctx context.Context) error {
	logger := slogging.Get()

	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(p.secretName),
	}

	result, err := p.client.GetSecretValue(ctx, input)
	if err != nil {
		var notFoundErr *types.ResourceNotFoundException
		if ok := isAWSError(err, &notFoundErr); ok {
			return fmt.Errorf("%w: AWS secret '%s' not found", ErrSecretNotFound, p.secretName)
		}
		return fmt.Errorf("failed to retrieve AWS secret: %w", err)
	}

	if result.SecretString == nil {
		return fmt.Errorf("AWS secret '%s' has no string value", p.secretName)
	}

	// Parse JSON secret
	var secrets map[string]string
	if err := json.Unmarshal([]byte(*result.SecretString), &secrets); err != nil {
		return fmt.Errorf("failed to parse AWS secret as JSON: %w", err)
	}

	// Update cache
	p.cacheMu.Lock()
	p.cache = secrets
	p.cacheSet = true
	p.cacheMu.Unlock()

	logger.Info("Loaded %d secrets from AWS Secrets Manager", len(secrets))
	return nil
}

// InvalidateCache clears the cached secrets, forcing a reload on next access
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: clear the in-memory secret cache to force a reload on next access (mutates shared state)
func (p *AWSProvider) InvalidateCache() {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	p.cache = make(map[string]string)
	p.cacheSet = false
}

// isAWSError checks if an error is a specific AWS error type
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: check whether an error matches a specific AWS SDK error type via unwrapping (pure)
func isAWSError[T error](err error, target *T) bool {
	var awsErr T
	if ok := asError(err, &awsErr); ok {
		*target = awsErr
		return true
	}
	return false
}

// asError is a helper that wraps errors.As for generic types
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: unwrap an error chain to find a value assignable to the target type (pure)
func asError[T any](err error, target *T) bool {
	for err != nil {
		if e, ok := err.(T); ok {
			*target = e
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
