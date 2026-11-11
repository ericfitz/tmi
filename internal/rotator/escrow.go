package rotator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

// Escrow stores an off-cluster copy of key material before a rotation makes
// it the only live copy (#1009). Implementations must never echo the payload
// in errors or logs.
type Escrow interface {
	Put(ctx context.Context, payload []byte) error
}

// NoopEscrow is used when no escrow is configured (dev clusters).
type NoopEscrow struct{}

func (NoopEscrow) Put(context.Context, []byte) error { return nil }

// SecretsManagerPutter is the slice of the Secrets Manager client the escrow uses.
type SecretsManagerPutter interface {
	PutSecretValue(ctx context.Context, in *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
}

// SecretsManagerEscrow writes each payload as a new version of one secret.
type SecretsManagerEscrow struct {
	client    SecretsManagerPutter
	secretARN string
}

func NewSecretsManagerEscrow(client SecretsManagerPutter, secretARN string) *SecretsManagerEscrow {
	return &SecretsManagerEscrow{client: client, secretARN: secretARN}
}

func (e *SecretsManagerEscrow) Put(ctx context.Context, payload []byte) error {
	_, err := e.client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(e.secretARN),
		SecretString: aws.String(string(payload)),
	})
	if err != nil {
		// The SDK error names the secret and the API error code, never the value.
		return fmt.Errorf("escrow write to Secrets Manager failed: %w", err)
	}
	return nil
}

// RegionFromSecretARN returns the region field of a Secrets Manager secret ARN.
func RegionFromSecretARN(arn string) (string, error) {
	f := strings.Split(arn, ":")
	if len(f) < 7 || f[0] != "arn" || f[2] != "secretsmanager" || f[3] == "" {
		return "", errors.New("not a Secrets Manager secret ARN with a region")
	}
	return f[3], nil
}
