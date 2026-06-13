package mem_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/mem"
)

func TestSample_Modern(t *testing.T) {
	c, _ := mem.New(mem.Options{
		Proc:    procfs.New("testdata/modern"),
		NowFunc: func() time.Time { return time.Unix(1_700_000_000, 0) },
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(1_700_000_000_000), snap.SampledAtUnixMs)
	assert.Equal(t, uint64(16_384_000)<<10, snap.TotalBytes)
	assert.Equal(t, uint64(8_192_000)<<10, snap.FreeBytes)
	assert.Equal(t, uint64(12_288_000)<<10, snap.AvailableBytes)
	assert.Equal(t, uint64(512_000)<<10, snap.BuffersBytes)
	assert.Equal(t, uint64(4_096_000)<<10, snap.CachedBytes)
	assert.Equal(t, uint64(2_048_000)<<10, snap.SwapTotalBytes)
	assert.Equal(t, uint64(1_024_000)<<10, snap.SwapFreeBytes)

	assert.Equal(t, snap.TotalBytes-snap.AvailableBytes, snap.UsedBytes)
	assert.Equal(t, snap.SwapTotalBytes-snap.SwapFreeBytes, snap.SwapUsedBytes)
	assert.Equal(t, uint64(0), snap.ARCSizeBytes)
}

func TestSample_NoMemAvailable_OldKernelFallback(t *testing.T) {
	c, _ := mem.New(mem.Options{
		Proc: procfs.New("testdata/no_avail"),
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Fallback rule: AvailableBytes := FreeBytes + CachedBytes.
	expectedAvail := snap.FreeBytes + snap.CachedBytes
	assert.Equal(t, expectedAvail, snap.AvailableBytes)
	assert.Equal(t, snap.TotalBytes-expectedAvail, snap.UsedBytes)
}

func TestSample_NoSwap(t *testing.T) {
	c, _ := mem.New(mem.Options{
		Proc: procfs.New("testdata/no_swap"),
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(0), snap.SwapTotalBytes)
	assert.Equal(t, uint64(0), snap.SwapUsedBytes)
}

func TestSample_ZFSArc_FoldedIntoCached(t *testing.T) {
	c, _ := mem.New(mem.Options{
		Proc:         procfs.New("testdata/zfs_arc"),
		EnableZFSArc: true,
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	// fixture arcstats: size=2_000_000, c_min=500_000.
	assert.Equal(t, uint64(2_000_000), snap.ARCSizeBytes)
	assert.Equal(t, uint64(500_000), snap.ARCMinBytes)

	// Cached must include arc.size on top of the meminfo Cached value.
	rawCached := uint64(4_096_000) << 10
	assert.Equal(t, rawCached+snap.ARCSizeBytes, snap.CachedBytes)

	// Available picks up (arc.size - c_min) when the ARC could shrink.
	rawAvail := uint64(12_288_000) << 10
	assert.Equal(t, rawAvail+(snap.ARCSizeBytes-snap.ARCMinBytes), snap.AvailableBytes)
}

func TestSample_ZFSArc_Disabled_IgnoresFile(t *testing.T) {
	// Even when an arcstats file exists, EnableZFSArc=false leaves it alone.
	c, _ := mem.New(mem.Options{
		Proc:         procfs.New("testdata/zfs_arc"),
		EnableZFSArc: false,
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint64(0), snap.ARCSizeBytes)
}

func TestSample_ZFSArc_AbsentFile_NotAnError(t *testing.T) {
	// modern fixture has no arcstats; EnableZFSArc=true must not error.
	c, _ := mem.New(mem.Options{
		Proc:         procfs.New("testdata/modern"),
		EnableZFSArc: true,
	})
	_, err := c.Sample(context.Background())
	require.NoError(t, err)
}

func TestSample_MissingMemInfo_Errors(t *testing.T) {
	tmp := t.TempDir()
	c, _ := mem.New(mem.Options{
		Proc: procfs.New(tmp),
	})
	_, err := c.Sample(context.Background())
	require.Error(t, err)
}

func TestSample_MalformedMemInfo_NoTotal(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "meminfo"), []byte("Whatever: 123 kB\n"), 0o644))
	c, _ := mem.New(mem.Options{Proc: procfs.New(tmp)})
	_, err := c.Sample(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MemTotal")
}

func TestSample_LiveProc_Smoke(t *testing.T) {
	if _, statErr := os.Stat("/proc/meminfo"); statErr != nil {
		t.Skipf("no live /proc/meminfo: %v", statErr)
	}
	c, _ := mem.New(mem.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	assert.Greater(t, snap.TotalBytes, uint64(0))
	assert.Greater(t, snap.SampledAtUnixMs, int64(0))

	// Plausibility: every per-category byte count is bounded by total.
	assert.LessOrEqual(t, snap.UsedBytes, snap.TotalBytes)
	assert.LessOrEqual(t, snap.FreeBytes, snap.TotalBytes)
	assert.LessOrEqual(t, snap.AvailableBytes, snap.TotalBytes)
	assert.LessOrEqual(t, snap.CachedBytes, snap.TotalBytes)
	// Used + Available should equal Total (mem.Sample derives Used as
	// Total - Available when Available <= Total, so the invariant is
	// definitional). Allow a few KB rounding slack.
	sum := snap.UsedBytes + snap.AvailableBytes
	if sum > snap.TotalBytes {
		assert.LessOrEqual(t, sum-snap.TotalBytes, uint64(1<<20),
			"Used+Available drifted from Total: total=%d used=%d avail=%d", snap.TotalBytes, snap.UsedBytes, snap.AvailableBytes)
	} else {
		assert.LessOrEqual(t, snap.TotalBytes-sum, uint64(1<<20),
			"Used+Available drifted from Total: total=%d used=%d avail=%d", snap.TotalBytes, snap.UsedBytes, snap.AvailableBytes)
	}

	// Swap consistency.
	assert.LessOrEqual(t, snap.SwapUsedBytes, snap.SwapTotalBytes)
	assert.LessOrEqual(t, snap.SwapFreeBytes, snap.SwapTotalBytes)
}

func TestSample_ContextCancelled(t *testing.T) {
	c, _ := mem.New(mem.Options{
		Proc: procfs.New("testdata/modern"),
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Sample(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
