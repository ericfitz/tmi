package rotator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: test that the rollout waiter keeps waiting until generation, replicas and availability all settle
func TestKubeRolloutWaiter_NotRolledCases(t *testing.T) {
	i32 := func(v int32) *int32 { return &v }
	tests := []struct {
		name     string
		replicas *int32
		gen      int64
		status   appsv1.DeploymentStatus
		wantErr  bool
	}{
		{name: "rolled", replicas: i32(2), gen: 4, status: appsv1.DeploymentStatus{ObservedGeneration: 4, Replicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2}},
		{name: "nil replicas defaults to 1", gen: 4, status: appsv1.DeploymentStatus{ObservedGeneration: 4, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}},
		{name: "observedGeneration lagging", replicas: i32(1), gen: 4, wantErr: true, status: appsv1.DeploymentStatus{ObservedGeneration: 3, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}},
		{name: "available below want", replicas: i32(2), gen: 4, wantErr: true, status: appsv1.DeploymentStatus{ObservedGeneration: 4, Replicas: 2, UpdatedReplicas: 2, AvailableReplicas: 1}},
		{name: "old replicas still present (replicas above want)", replicas: i32(1), gen: 4, wantErr: true, status: appsv1.DeploymentStatus{ObservedGeneration: 4, Replicas: 2, UpdatedReplicas: 1, AvailableReplicas: 1}},
		{name: "updated below want", replicas: i32(2), gen: 4, wantErr: true, status: appsv1.DeploymentStatus{ObservedGeneration: 4, Replicas: 2, UpdatedReplicas: 1, AvailableReplicas: 2}},
		{name: "generation not past since", replicas: i32(1), gen: 3, wantErr: true, status: appsv1.DeploymentStatus{ObservedGeneration: 3, Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := fake.NewClientset(&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "tmi-server", Namespace: "tmi-platform", Generation: tt.gen},
				Spec:       appsv1.DeploymentSpec{Replicas: tt.replicas},
				Status:     tt.status,
			})
			w := NewKubeRolloutWaiter(cs, "tmi-platform")
			w.Poll = 2 * time.Millisecond
			err := w.WaitRolled(context.Background(), "tmi-server", 3, 20*time.Millisecond)
			if tt.wantErr {
				require.ErrorContains(t, err, "did not finish rolling")
				return
			}
			require.NoError(t, err)
		})
	}
}

// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: test that the rollout waiter errors when the Deployment is missing
func TestKubeRolloutWaiter_MissingDeployment(t *testing.T) {
	w := NewKubeRolloutWaiter(fake.NewClientset(), "tmi-platform")
	_, err := w.Generation(context.Background(), "tmi-server")
	require.Error(t, err)
	require.Error(t, w.WaitRolled(context.Background(), "tmi-server", 0, time.Millisecond))
}

// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: test that the Redis rotation refuses an unknown phase without touching Redis or the Secret
func TestRedisPasswordRotation_UnknownPhase(t *testing.T) {
	sec := swappedSecret()
	sec.Annotations[AnnPhase+"redis-password"] = "bogus"
	env, st := testEnv(sec)
	acl := &fakeACL{passwords: map[string]bool{"old": true, "new": true}}
	err := NewRedisPasswordRotation(acl).Run(context.Background(), env)
	require.ErrorContains(t, err, `unknown redis-password phase "bogus"`)
	require.Empty(t, acl.added)
	require.Empty(t, acl.setOnly)
	require.Zero(t, st.DataWrites)
}

// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: test that a rotation with no current Redis password fails before changing anything
func TestRedisPasswordRotation_EmptyPassword(t *testing.T) {
	for name, data := range map[string]map[string]string{
		"missing": {},
		"empty":   {"TMI_REDIS_PASSWORD": ""},
	} {
		t.Run(name, func(t *testing.T) {
			env, st := testEnv(&Secret{Name: "tmi-secrets", Data: data, Annotations: map[string]string{}})
			acl := &fakeACL{}
			err := NewRedisPasswordRotation(acl).Run(context.Background(), env)
			require.ErrorContains(t, err, "has no TMI_REDIS_PASSWORD to rotate")
			require.Empty(t, acl.added)
			require.Zero(t, st.DataWrites)
			s, _ := st.Get(context.Background(), "tmi-secrets")
			require.Empty(t, s.Annotations[AnnPhase+"redis-password"])
		})
	}
}

// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: test that aclError keeps only the error class and never echoes the password
func TestAclError_KeepsClassDropsPassword(t *testing.T) {
	const pw = "s3cret-pw-value"
	tests := []struct {
		in   string
		want string
	}{
		{"NOAUTH Authentication required. " + pw, "redis ACL SETUSER default failed: NOAUTH"},
		{"WRONGPASS invalid username-password pair " + pw, "redis ACL SETUSER default failed: WRONGPASS"},
		{"NOPERM this user has no permissions " + pw, "redis ACL SETUSER default failed: NOPERM"},
		{"ERR Error in ACL SETUSER modifier '>" + pw + "'", "redis ACL SETUSER default failed"},
	}
	for _, tt := range tests {
		err := aclError(errors.New(tt.in))
		require.EqualError(t, err, tt.want)
		require.NotContains(t, err.Error(), pw)
	}
	require.NoError(t, aclError(nil))
}

// failingSettings fails ReEncrypt.
// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: settings store test double whose ReEncrypt always fails
type failingSettings struct {
	fakeSettings
	err error
}

// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: fail re-encryption with the canned error (test double)
func (f *failingSettings) ReEncrypt(context.Context, Keyring) (int, error) { return 0, f.err }

// SEM@32eda6c88eee02283fa8feb03bb2d42252451618: build a settings-key secret fixture with both keys present, in the given phase
func settingsSecretInPhase(phase string) *Secret {
	sec := settingsSecret()
	sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"] = "2"
	sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_KEY"] = "0000000000000000000000000000000000000000000000000000000000000002"
	sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID"] = "1"
	sec.Annotations[AnnGeneration+"settings-key"] = "0"
	if phase != "" {
		sec.Annotations[AnnPhase+"settings-key"] = phase
	}
	return sec
}

// SEM@070c69a19a7fed18f17f2bd3475d508172778494: test that a rotation does not start while a previous key pair is still present
func TestSettingsKeyRotation_RefusesToStartWithPreviousPair(t *testing.T) {
	env, st := testEnv(settingsSecretInPhase(""))
	fs := &fakeSettings{rows: map[int]int64{}}
	err := NewSettingsKeyRotation(fs, time.Hour, NoopEscrow{}).Run(context.Background(), env)
	require.ErrorContains(t, err, "refusing to start a new rotation")
	require.Zero(t, st.DataWrites)
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"settings-key"])
	require.Zero(t, fs.reencrypt)
}

// SEM@070c69a19a7fed18f17f2bd3475d508172778494: test that a ReEncrypt failure in the promoted phase keeps the phase for the next run
func TestSettingsKeyRotation_ReEncryptErrorKeepsPromotedPhase(t *testing.T) {
	env, st := testEnv(settingsSecretInPhase("promoted"))
	st.DataWrites = 1 // the promote write already rolled the server
	fs := &failingSettings{fakeSettings: fakeSettings{rows: map[int]int64{}}, err: errors.New("db down")}
	err := NewSettingsKeyRotation(fs, time.Hour, NoopEscrow{}).Run(context.Background(), env)
	require.ErrorContains(t, err, "db down")
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "promoted", s.Annotations[AnnPhase+"settings-key"])
	require.Empty(t, s.Annotations[AnnPromotedAt+"settings-key"])
	require.Equal(t, 1, st.DataWrites)
}

// SEM@070c69a19a7fed18f17f2bd3475d508172778494: test that a peer advancing the phase makes the settings-key transition fail with a conflict
func TestSettingsKeyRotation_PhaseCASConflict(t *testing.T) {
	advance := func(st *MemorySecretStore, to string) func() {
		return func() {
			s, _ := st.Get(context.Background(), "tmi-secrets")
			s.Annotations[AnnPhase+"settings-key"] = to
			require.NoError(t, st.Update(context.Background(), s))
		}
	}
	t.Run("promoted to reencrypted", func(t *testing.T) {
		env, st := testEnv(settingsSecretInPhase("promoted"))
		st.DataWrites = 1
		env.Rollouts = &afterWaitWaiter{RolloutWaiter: env.Rollouts, after: advance(st, "reencrypted")}
		err := NewSettingsKeyRotation(&fakeSettings{rows: map[int]int64{}}, time.Hour, NoopEscrow{}).Run(context.Background(), env)
		require.ErrorIs(t, err, ErrConflict)
		s, _ := st.Get(context.Background(), "tmi-secrets")
		require.Equal(t, "reencrypted", s.Annotations[AnnPhase+"settings-key"], "peer's phase is untouched")
		require.Empty(t, s.Annotations[AnnPromotedAt+"settings-key"])
	})
	t.Run("staged to promoted", func(t *testing.T) {
		sec := settingsSecretInPhase("staged")
		sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"] = "1"
		sec.Data["TMI_SECRET_SETTINGS_ENCRYPTION_PREVIOUS_CONTEXT_ID"] = "2"
		env, st := testEnv(sec)
		st.DataWrites = 1
		env.Rollouts = &afterWaitWaiter{RolloutWaiter: env.Rollouts, after: advance(st, "promoted")}
		err := NewSettingsKeyRotation(&fakeSettings{rows: map[int]int64{}}, time.Hour, NoopEscrow{}).Run(context.Background(), env)
		require.ErrorIs(t, err, ErrConflict)
		s, _ := st.Get(context.Background(), "tmi-secrets")
		require.Equal(t, "1", s.Data["TMI_SECRET_SETTINGS_ENCRYPTION_CONTEXT_ID"], "no second promotion written")
	})
}
