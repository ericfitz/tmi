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
// SEM@070c69a19a7fed18f17f2bd3475d508172778494: store an off-cluster copy of rotated key material
type Escrow interface {
	Put(ctx context.Context, payload []byte) error
}

// NoopEscrow is used when no escrow is configured (dev clusters).
// SEM@070c69a19a7fed18f17f2bd3475d508172778494: discard escrow writes when no escrow target is configured (pure)
type NoopEscrow struct{}

// SEM@070c69a19a7fed18f17f2bd3475d508172778494: accept and discard an escrow payload (pure)
func (NoopEscrow) Put(context.Context, []byte) error { return nil }

// SecretsManagerPutter is the slice of the Secrets Manager client the escrow uses.
// SEM@070c69a19a7fed18f17f2bd3475d508172778494: write a new secret version to Secrets Manager
type SecretsManagerPutter interface {
	PutSecretValue(ctx context.Context, in *secretsmanager.PutSecretValueInput, optFns ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
}

// SecretsManagerEscrow writes each payload as a new version of one secret.
// SEM@070c69a19a7fed18f17f2bd3475d508172778494: escrow key material as a new Secrets Manager secret version
type SecretsManagerEscrow struct {
	client    SecretsManagerPutter
	secretARN string
}

// SEM@070c69a19a7fed18f17f2bd3475d508172778494: build a Secrets Manager escrow for one secret (pure)
func NewSecretsManagerEscrow(client SecretsManagerPutter, secretARN string) *SecretsManagerEscrow {
	return &SecretsManagerEscrow{client: client, secretARN: secretARN}
}

// SEM@070c69a19a7fed18f17f2bd3475d508172778494: store the payload as a new escrow secret version (calls AWS)
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
// SEM@070c69a19a7fed18f17f2bd3475d508172778494: parse the AWS region out of a Secrets Manager secret ARN (pure)
func RegionFromSecretARN(arn string) (string, error) {
	f := strings.Split(arn, ":")
	if len(f) < 7 || f[0] != "arn" || f[2] != "secretsmanager" || f[3] == "" {
		return "", errors.New("not a Secrets Manager secret ARN with a region")
	}
	return f[3], nil
}
