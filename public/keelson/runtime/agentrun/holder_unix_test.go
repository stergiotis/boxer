//go:build unix

package agentrun

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/runinfo"
)

// A holder is alive while its process runs and does not name another run;
// a reaped process, or a reused pid carrying another run's id, is gone.
func TestLocalLiveness(t *testing.T) {
	ctx := context.Background()
	live := LocalLiveness{}
	alive, err := live.Alive(ctx, Holder{Run: "any", Pid: os.Getpid()})
	require.NoError(t, err)
	assert.True(t, alive, "this process")
	alive, _ = live.Alive(ctx, Holder{Run: "any", Pid: 0})
	assert.False(t, alive)

	done := exec.Command("true")
	require.NoError(t, done.Run())
	alive, err = live.Alive(ctx, Holder{Run: "any", Pid: done.Process.Pid})
	require.NoError(t, err)
	assert.False(t, alive, "a reaped process")

	other := exec.Command("sleep", "5")
	other.Env = append(os.Environ(), runinfo.EnvVar+"=run-b")
	require.NoError(t, other.Start())
	t.Cleanup(func() { _ = other.Process.Kill(); _ = other.Wait() })
	alive, err = live.Alive(ctx, Holder{Run: "run-b", Pid: other.Process.Pid})
	require.NoError(t, err)
	assert.True(t, alive, "the process carries the holder's run")
	if _, statErr := os.Stat("/proc/self/environ"); statErr == nil {
		// Until the child has exec'd, its environment is still this
		// process's; wait for its own.
		environ := "/proc/" + strconv.Itoa(other.Process.Pid) + "/environ"
		require.Eventually(t, func() bool {
			env, rerr := os.ReadFile(environ)
			return rerr == nil && bytes.Contains(env, []byte(runinfo.EnvVar+"=run-b"))
		}, 5*time.Second, 10*time.Millisecond)
		alive, err = live.Alive(ctx, Holder{Run: "run-a", Pid: other.Process.Pid})
		require.NoError(t, err)
		assert.False(t, alive, "a pid carrying another run's id is not the holder")
	}
}

func TestNamesAnotherRun(t *testing.T) {
	env := []byte("A=1\x00" + runinfo.EnvVar + "=r1\x00B=2\x00")
	assert.False(t, namesAnotherRun(env, "r1"))
	assert.True(t, namesAnotherRun(env, "r2"))
	assert.False(t, namesAnotherRun([]byte("A=1\x00"), "r2"), "no run id: not another run's")
}
