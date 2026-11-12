package rotator

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/stretchr/testify/require"
)

type fakePutter struct {
	in  []*secretsmanager.PutSecretValueInput
	err error
}

func (f *fakePutter) PutSecretValue(_ context.Context, in *secretsmanager.PutSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	f.in = append(f.in, in)
	if f.err != nil {
		return nil, f.err
	}
	return &secretsmanager.PutSecretValueOutput{}, nil
}

func TestNoopEscrow_Put(t *testing.T) {
	require.NoError(t, NoopEscrow{}.Put(context.Background(), []byte("x")))
}

func TestSecretsManagerEscrow_PutWritesSecretString(t *testing.T) {
	p := &fakePutter{}
	arn := "arn:aws:secretsmanager:us-east-1:111122223333:secret:tmi-settings-key-escrow-AbCdEf"
	require.NoError(t, NewSecretsManagerEscrow(p, arn).Put(context.Background(), []byte(`{"a":1}`)))
	require.Len(t, p.in, 1)
	require.Equal(t, arn, aws.ToString(p.in[0].SecretId))
	require.Equal(t, `{"a":1}`, aws.ToString(p.in[0].SecretString))
}

func TestSecretsManagerEscrow_PutErrorOmitsPayload(t *testing.T) {
	p := &fakePutter{err: errors.New("AccessDeniedException")}
	err := NewSecretsManagerEscrow(p, "arn:aws:secretsmanager:us-east-1:1:secret:x").Put(context.Background(), []byte("SECRETKEYHEX"))
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRETKEYHEX")
	require.ErrorContains(t, err, "AccessDeniedException")
}

func TestRegionFromSecretARN(t *testing.T) {
	r, err := RegionFromSecretARN("arn:aws:secretsmanager:us-east-1:111122223333:secret:name-AbCdEf")
	require.NoError(t, err)
	require.Equal(t, "us-east-1", r)
	for _, bad := range []string{"", "not-an-arn", "arn:aws:s3:::bucket", "arn:aws:secretsmanager::111122223333:secret:x", "arn:aws:secretsmanager:us-east-1"} {
		_, err := RegionFromSecretARN(bad)
		require.Error(t, err, bad)
	}
}
