//go:build llm_generated_opus47

package runinfo

import (
	"bytes"
	"os"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInit_AllocatesAndCaches(t *testing.T) {
	Reset()
	os.Unsetenv(EnvVar)
	defer Reset()
	defer os.Unsetenv(EnvVar)

	a, err := Init()
	require.NoError(t, err)
	require.NotNil(t, a)
	assert.NotEmpty(t, a.RunId)
	assert.NotEmpty(t, a.Hostname)
	assert.Greater(t, a.Pid, 0)
	assert.False(t, a.StartedAt.IsZero())

	b, err := Init()
	require.NoError(t, err)
	assert.Same(t, a, b, "Init must be idempotent — same pointer")

	// And the env var is set to the run_id.
	assert.Equal(t, a.RunId, os.Getenv(EnvVar)) //boxer:lint disable=CS011 reason="verifies Init's os.Setenv side effect"
}

func TestInit_InheritsExistingEnv(t *testing.T) {
	Reset()
	defer Reset()
	defer os.Unsetenv(EnvVar)

	preset := "inherited-run-id-1234"
	os.Setenv(EnvVar, preset)

	a, err := Init()
	require.NoError(t, err)
	assert.Equal(t, preset, a.RunId, "Init must reuse existing env var")
	assert.Equal(t, preset, os.Getenv(EnvVar)) //boxer:lint disable=CS011 reason="verifies preset env-var is read unchanged"
}

func TestGet_ErrorsBeforeInit(t *testing.T) {
	Reset()
	defer Reset()

	_, err := Get()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Init has not been called")
}

func TestMustGet_PanicsBeforeInit(t *testing.T) {
	Reset()
	defer Reset()

	assert.Panics(t, func() {
		_ = MustGet()
	})
}

// TestTagLogger_AddsRunIdField verifies the tag survives a normal event.
// The output is CBOR under binary_log builds and JSON otherwise; both
// embed the run_id bytes literally, so byte-level substring match is
// the format-agnostic assertion (matches the convention used by
// app.AppLogger's tests).
func TestTagLogger_AddsRunIdField(t *testing.T) {
	Reset()
	os.Unsetenv(EnvVar)
	defer Reset()
	defer os.Unsetenv(EnvVar)

	inst, err := Init()
	require.NoError(t, err)

	var buf bytes.Buffer
	base := zerolog.New(&buf)
	tagged := TagLogger(base, inst)
	tagged.Info().Msg("hello")

	out := buf.Bytes()
	require.NotEmpty(t, out)
	assert.Contains(t, string(out), inst.RunId, "run_id value must appear in the marshalled event")
	assert.Contains(t, string(out), "hello", "message must appear in the marshalled event")
}

func TestInit_CapturesGoAndVcsFields(t *testing.T) {
	Reset()
	os.Unsetenv(EnvVar)
	defer Reset()
	defer os.Unsetenv(EnvVar)

	inst, err := Init()
	require.NoError(t, err)
	assert.True(t, len(inst.GoVersion) > 0, "GoVersion should be set")
	// VCS fields are environment-dependent — under `go test` the binary
	// is built locally with the repo's git state, so revision should
	// be non-empty most of the time. Under `go test -trimpath` it can
	// be empty; assert presence of at least one of the fields rather
	// than each.
	assert.True(t,
		inst.VcsBuildInfo != "" || inst.VcsRevision != "" || inst.ModulePath != "",
		"at least one VCS/module field should be non-empty under standard test invocation")
}

func TestReset_ClearsSingleton(t *testing.T) {
	Reset()
	os.Unsetenv(EnvVar)
	defer Reset()
	defer os.Unsetenv(EnvVar)

	a, err := Init()
	require.NoError(t, err)

	Reset()
	os.Unsetenv(EnvVar)

	b, err := Init()
	require.NoError(t, err)
	assert.NotSame(t, a, b, "Reset must let the next Init allocate fresh")
	assert.NotEqual(t, a.RunId, b.RunId)
}
