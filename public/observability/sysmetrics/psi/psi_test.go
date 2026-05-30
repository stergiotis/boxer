package psi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/psi"
)

func writePressure(t *testing.T, procDir, name, content string) {
	t.Helper()
	p := filepath.Join(procDir, "pressure", name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestSample_ParsesAllResources(t *testing.T) {
	proc := filepath.Join(t.TempDir(), "proc")
	writePressure(t, proc, "cpu",
		"some avg10=1.50 avg60=0.80 avg300=0.20 total=18332564\n"+
			"full avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	writePressure(t, proc, "memory",
		"some avg10=3.00 avg60=2.00 avg300=1.00 total=42\n"+
			"full avg10=2.50 avg60=1.50 avg300=0.50 total=21\n")
	writePressure(t, proc, "io",
		"some avg10=9.90 avg60=5.00 avg300=2.00 total=99\n"+
			"full avg10=8.00 avg60=4.00 avg300=1.00 total=88\n")

	c, err := psi.New(psi.Options{Proc: procfs.New(proc)})
	require.NoError(t, err)
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	assert.True(t, snap.Available)
	assert.InDelta(t, 1.50, snap.CPU.Some.Avg10, 1e-4)
	assert.Equal(t, uint64(18332564), snap.CPU.Some.TotalUs)
	assert.InDelta(t, 0.0, snap.CPU.Full.Avg10, 1e-4)
	assert.InDelta(t, 2.50, snap.Memory.Full.Avg10, 1e-4)
	assert.InDelta(t, 9.90, snap.IO.Some.Avg10, 1e-4)
	assert.Equal(t, uint64(88), snap.IO.Full.TotalUs)
}

func TestSample_UnavailableWhenAbsent(t *testing.T) {
	proc := filepath.Join(t.TempDir(), "proc") // no pressure/ subtree
	require.NoError(t, os.MkdirAll(proc, 0o755))

	c, err := psi.New(psi.Options{Proc: procfs.New(proc)})
	require.NoError(t, err)
	snap, err := c.Sample(context.Background())
	require.NoError(t, err) // absence is not an error
	assert.False(t, snap.Available)
}
