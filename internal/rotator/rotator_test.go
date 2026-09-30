package rotator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/stretchr/testify/require"
)

type fakeRotation struct {
	name string
	runs int
	err  error
}

func (f *fakeRotation) Name() string { return f.name }
func (f *fakeRotation) Run(context.Context, *Env) error {
	f.runs++
	return f.err
}

func testEnv(s *Secret) (*Env, *MemorySecretStore) {
	st := NewMemorySecretStore(s)
	return &Env{
		Secrets:          st,
		Rollouts:         NewFakeRolloutWaiter(st, "tmi-server"),
		SecretName:       "tmi-secrets",
		ServerDeployment: "tmi-server",
		RolloutTimeout:   time.Second,
		Now:              func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) },
	}, st
}

func TestRun_SkipsNotDue_RunsForcedAndInProgress(t *testing.T) {
	env, _ := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{
		AnnRotatedAt + "fresh":  "2026-09-27T00:00:00Z",
		AnnRotatedAt + "forced": "2026-09-27T00:00:00Z",
		AnnRotatedAt + "stuck":  "2026-09-27T00:00:00Z",
		AnnPhase + "stuck":      "swapped",
	}})
	fresh, forced, stuck := &fakeRotation{name: "fresh"}, &fakeRotation{name: "forced"}, &fakeRotation{name: "stuck"}
	require.NoError(t, Run(context.Background(), env, []Rotation{fresh, forced, stuck}, "forced"))
	require.Equal(t, 0, fresh.runs)
	require.Equal(t, 1, forced.runs)
	require.Equal(t, 1, stuck.runs, "a recorded phase resumes even when not due")
}

func TestRun_FailureIsReportedAfterOthersRun(t *testing.T) {
	env, _ := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{}})
	bad := &fakeRotation{name: "bad", err: errors.New("boom")}
	good := &fakeRotation{name: "good"}
	err := Run(context.Background(), env, []Rotation{bad, good}, "")
	require.Error(t, err)
	require.Equal(t, 1, good.runs)
}

func TestTransition_WritesPhaseAndGenerationAtomically(t *testing.T) {
	env, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{"K": "old"}, Annotations: map[string]string{}})
	err := env.Transition(context.Background(), "k", "", "swapped", func(s *Secret) { s.Data["K"] = "new" })
	require.NoError(t, err)
	s, _ := st.Get(context.Background(), "tmi-secrets")
	require.Equal(t, "new", s.Data["K"])
	require.Equal(t, "swapped", s.Annotations[AnnPhase+"k"])
	require.Equal(t, "0", s.Annotations[AnnGeneration+"k"], "generation before the write")
	require.NoError(t, env.WaitServerRolled(context.Background(), s, "k"))

	// Completing a rotation clears the phase and stamps rotated-at.
	err = env.Transition(context.Background(), "k", "swapped", "", nil)
	require.NoError(t, err)
	s, _ = st.Get(context.Background(), "tmi-secrets")
	require.Empty(t, s.Annotations[AnnPhase+"k"])
	require.Equal(t, "2026-09-28T12:00:00Z", s.Annotations[AnnRotatedAt+"k"])
}

func TestNewPasswordAndHexKey(t *testing.T) {
	p, err := NewPassword()
	require.NoError(t, err)
	require.Len(t, p, 32)
	k, err := NewHexKey()
	require.NoError(t, err)
	require.Len(t, k, 64)
	q, _ := NewPassword()
	require.NotEqual(t, p, q)
}

func TestTransition_SecondRunFromSamePhaseConflicts(t *testing.T) {
	env1, st := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{"K": "old"}, Annotations: map[string]string{}})
	env2 := *env1
	ctx := context.Background()
	require.NoError(t, env1.Transition(ctx, "k", "", "swapped", func(s *Secret) { s.Data["K"] = "first" }))
	err := env2.Transition(ctx, "k", "", "swapped", func(s *Secret) { s.Data["K"] = "second" })
	require.True(t, errors.Is(err, ErrConflict))
	s, _ := st.Get(ctx, "tmi-secrets")
	require.Equal(t, "first", s.Data["K"])
	require.Equal(t, "swapped", s.Annotations[AnnPhase+"k"])
	require.Equal(t, 1, st.DataWrites)
}

// The status line feeds the CloudWatch metric filter ($.age_days); slogging's
// default redaction drops attrs whose key looks secret-ish, so the rotation name
// must not be logged under "secret".
func TestRun_StatusLineSurvivesRedaction(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, slogging.Initialize(slogging.Config{Level: slogging.LogLevelInfo, LogDir: dir, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1}))
	env, _ := testEnv(&Secret{Name: "tmi-secrets", Data: map[string]string{}, Annotations: map[string]string{
		AnnRotatedAt + "fresh": "2026-09-27T00:00:00Z",
	}})
	require.NoError(t, Run(context.Background(), env, []Rotation{&fakeRotation{name: "fresh"}}, ""))
	b, err := os.ReadFile(filepath.Join(dir, "tmi.log"))
	require.NoError(t, err)
	var found bool
	for _, line := range strings.Split(string(b), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil || m["msg"] != "rotation status" {
			continue
		}
		found = true
		require.Equal(t, "fresh", m["rotation"])
		require.EqualValues(t, 1, m["age_days"])
	}
	require.True(t, found, "no rotation status line in %s", b)
}
