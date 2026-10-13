package proc_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
)

func ioContent(readBytes, writeBytes uint64) string {
	return fmt.Sprintf("rchar: 0\nwchar: 0\nsyscr: 0\nsyscw: 0\nread_bytes: %d\nwrite_bytes: %d\ncancelled_write_bytes: 0\n",
		readBytes, writeBytes)
}

// steppingClock returns a NowFunc that the test advances by hand, so the
// I/O rate denominator is exact.
func steppingClock() (now func() time.Time, advance func(d time.Duration)) {
	t := time.Unix(1_700_000_000, 0)
	now = func() time.Time { return t }
	advance = func(d time.Duration) { t = t.Add(d) }
	return
}

func newIOCollector(t *testing.T, root string, now func() time.Time, maxProcs uint32) (c *proc.Collector) {
	t.Helper()
	c, err := proc.New(proc.Options{
		Proc:       procfs.New(root),
		NowFunc:    now,
		UserLookup: staticUserLookup(nil),
		ClkTckHz:   100,
		PageSize:   4096,
		NumCPUs:    4,
		MaxProcs:   maxProcs,
	})
	require.NoError(t, err)
	return
}

func TestSample_IORates(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "100.00 100.00\n")
	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 1, name: "io", rssPages: 1})
	writeProcFile(t, root, 100, "io", ioContent(1000, 5000))
	now, advance := steppingClock()
	c := newIOCollector(t, root, now, 0)

	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.False(t, infos[0].IOKnown, "first sighting has no prior counters")

	advance(2 * time.Second)
	writeProcFile(t, root, 100, "io", ioContent(1000+4<<20, 5000+8<<20))
	infos, err = c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.True(t, infos[0].IOKnown)
	assert.Equal(t, uint64(2<<20), infos[0].IOReadBytesPerSec)
	assert.Equal(t, uint64(4<<20), infos[0].IOWriteBytesPerSec)
}

// TestSample_IOUnreadable covers the ptrace-gated shape: no io file means
// unknown, never zero-and-known.
func TestSample_IOUnreadable(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "100.00 100.00\n")
	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 1, name: "other"})
	now, advance := steppingClock()
	c := newIOCollector(t, root, now, 0)

	_, err := c.Sample(context.Background())
	require.NoError(t, err)
	advance(time.Second)
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.False(t, infos[0].IOKnown)
	assert.Zero(t, infos[0].IOReadBytesPerSec)
}

// TestSample_IOPidReuse: a recycled pid's counters restart, and must not
// be delta'd against the previous instance's.
func TestSample_IOPidReuse(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "100.00 100.00\n")
	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 1, name: "a", starttime: 10})
	writeProcFile(t, root, 100, "io", ioContent(0, 0))
	now, advance := steppingClock()
	c := newIOCollector(t, root, now, 0)
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	advance(time.Second)
	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 1, name: "b", starttime: 20})
	writeProcFile(t, root, 100, "io", ioContent(1<<30, 0))
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.False(t, infos[0].IOKnown, "a new instance on a reused pid starts unknown")
}

// TestSample_MaxProcs_KeepsTopIO: a process with no CPU but the most I/O
// survives a cap it would lose on CPU rank alone.
func TestSample_MaxProcs_KeepsTopIO(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "100.00 100.00\n")
	for i := range 5 {
		pid := uint32(1000 + i)
		writeFakeProc(t, root, fakeProc{pid: pid, ppid: 1, name: fmt.Sprintf("p%d", i), rssPages: uint64(100 + i)})
		writeProcFile(t, root, pid, "io", ioContent(0, 0))
	}
	now, advance := steppingClock()
	c := newIOCollector(t, root, now, 2)
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	// 1003 and 1004 burn CPU; 1000 (smallest RSS, idle) does all the I/O.
	advance(time.Second)
	writeGlobalStat(t, root, "cpu  1000 0 0 0 0 0 0 0 0 0\n")
	for i := 3; i < 5; i++ {
		pid := uint32(1000 + i)
		writeFakeProc(t, root, fakeProc{pid: pid, ppid: 1, name: fmt.Sprintf("p%d", i), rssPages: uint64(100 + i), utime: 100})
	}
	writeProcFile(t, root, 1000, "io", ioContent(0, 64<<20))

	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	pids := make([]uint32, 0, len(infos))
	for _, info := range infos {
		pids = append(pids, info.PID)
	}
	assert.Equal(t, []uint32{1000, 1003, 1004}, pids, "top-2 by CPU plus the I/O leader")
	assert.Equal(t, uint64(64<<20), findPid(t, infos, 1000).IOWriteBytesPerSec)
}
