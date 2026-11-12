package rotator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemorySecretStore_ConflictOnStaleVersion(t *testing.T) {
	st := NewMemorySecretStore(&Secret{Name: "tmi-secrets", Data: map[string]string{"A": "1"}})
	ctx := context.Background()
	a, err := st.Get(ctx, "tmi-secrets")
	require.NoError(t, err)
	b, err := st.Get(ctx, "tmi-secrets")
	require.NoError(t, err)
	a.Data["A"] = "2"
	require.NoError(t, st.Update(ctx, a))
	b.Data["A"] = "3"
	err = st.Update(ctx, b)
	require.True(t, errors.Is(err, ErrConflict))
	cur, _ := st.Get(ctx, "tmi-secrets")
	require.Equal(t, "2", cur.Data["A"])
	require.Equal(t, 1, st.DataWrites, "annotation-only updates do not count as data writes")
}
