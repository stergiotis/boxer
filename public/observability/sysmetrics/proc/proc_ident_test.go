package proc_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

func writeProcFile(t *testing.T, root string, pid uint32, name, content string) {
	t.Helper()
	dir := filepath.Join(root, strconv.FormatUint(uint64(pid), 10))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func newIdentCollector(t *testing.T, root string) (c *proc.Collector) {
	t.Helper()
	c, err := proc.New(proc.Options{
		Proc:       procfs.New(root),
		UserLookup: staticUserLookup(nil),
		ClkTckHz:   100,
		PageSize:   4096,
		NumCPUs:    4,
	})
	require.NoError(t, err)
	return
}

func findPid(t *testing.T, infos []sysmsnap.ProcInfo, pid uint32) (info sysmsnap.ProcInfo) {
	t.Helper()
	for _, i := range infos {
		if i.PID == pid {
			return i
		}
	}
	t.Fatalf("pid %d not in sample (%d rows)", pid, len(infos))
	return
}

// TestSample_ComponentAndCgroupUnit proves the ADR-0126 identity fields:
// a marked process carries its environ value and cgroup unit; an
// unmarked one (no environ file — the denied-read shape) stays empty.
func TestSample_ComponentAndCgroupUnit(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")

	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 1, name: "marked", uid: 1000, rssPages: 10, starttime: 50})
	writeProcFile(t, root, 100, "environ", "HOME=/opt/x\x00BOXER_COMPONENT=imzero2-demo\x00LANG=C\x00")
	writeProcFile(t, root, 100, "cgroup", "0::/system.slice/imzero2-demo.service\n")

	writeFakeProc(t, root, fakeProc{pid: 200, ppid: 1, name: "plain", uid: 1000, rssPages: 10, starttime: 50})

	c := newIdentCollector(t, root)
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)

	marked := findPid(t, infos, 100)
	assert.Equal(t, "imzero2-demo", marked.Component)
	assert.Equal(t, "imzero2-demo.service", marked.CgroupUnit)

	plain := findPid(t, infos, 200)
	assert.Empty(t, plain.Component)
	assert.Empty(t, plain.CgroupUnit)
}

// TestSample_IdentCachePerInstance proves the once-per-(pid,starttime)
// read: a changed environ is invisible while the instance lives (the
// real file is exec-frozen; the cache mirrors that), and a recycled pid
// (new starttime) re-reads.
func TestSample_IdentCachePerInstance(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")

	fp := fakeProc{pid: 100, ppid: 1, name: "recycled", uid: 1000, rssPages: 10, starttime: 50}
	writeFakeProc(t, root, fp)
	writeProcFile(t, root, 100, "environ", "BOXER_COMPONENT=first\x00")

	c := newIdentCollector(t, root)
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first", findPid(t, infos, 100).Component)

	// Same instance, mutated fixture: the cache must serve the old value.
	writeProcFile(t, root, 100, "environ", "BOXER_COMPONENT=second\x00")
	infos, err = c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "first", findPid(t, infos, 100).Component)

	// Recycled pid: new starttime invalidates the cache entry.
	fp.starttime = 60
	writeFakeProc(t, root, fp)
	infos, err = c.Sample(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "second", findPid(t, infos, 100).Component)
}

// TestSample_MarkedProcsAreCapExempt proves the phase-2 contract: at
// most MaxProcs unmarked processes plus every marked one.
func TestSample_MarkedProcsAreCapExempt(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")

	// The marked process is the SMALLEST by the cap's RSS tiebreak — a
	// plain cap would evict exactly it.
	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 1, name: "markedidle", uid: 1000, rssPages: 1, starttime: 50})
	writeProcFile(t, root, 100, "environ", "BOXER_COMPONENT=sysmetricsd\x00")
	writeFakeProc(t, root, fakeProc{pid: 200, ppid: 1, name: "big", uid: 1000, rssPages: 1000, starttime: 50})
	writeFakeProc(t, root, fakeProc{pid: 300, ppid: 1, name: "mid", uid: 1000, rssPages: 500, starttime: 50})

	c, err := proc.New(proc.Options{
		Proc:       procfs.New(root),
		UserLookup: staticUserLookup(nil),
		ClkTckHz:   100,
		PageSize:   4096,
		NumCPUs:    4,
		MaxProcs:   1,
	})
	require.NoError(t, err)
	infos, serr := c.Sample(context.Background())
	require.NoError(t, serr)

	require.Len(t, infos, 2, "one capped unmarked + the exempt marked")
	assert.Equal(t, "sysmetricsd", findPid(t, infos, 100).Component)
	assert.Equal(t, uint32(200), findPid(t, infos, 200).PID, "top unmarked survives the cap")
}

// TestSample_RealProc_OwnComponentMark is the end-to-end proof on a live
// /proc: when the test binary itself was spawned with BOXER_COMPONENT
// set (the exec-frozen environ carries it), the collector must report it
// on our own row. Run explicitly via:
//
//	BOXER_COMPONENT=verify-mark go test -run RealProc ./public/observability/sysmetrics/proc/
func TestSample_RealProc_OwnComponentMark(t *testing.T) {
	mark := os.Getenv(proc.DefaultComponentEnvVar)
	if mark == "" {
		t.Skipf("%s not set in this test binary's spawn environment", proc.DefaultComponentEnvVar)
	}
	if _, statErr := os.Stat("/proc/self/environ"); statErr != nil {
		t.Skipf("no live /proc: %v", statErr)
	}
	c, err := proc.New(proc.Options{})
	require.NoError(t, err)
	infos, serr := c.Sample(context.Background())
	require.NoError(t, serr)
	own := findPid(t, infos, uint32(os.Getpid()))
	assert.Equal(t, mark, own.Component)
}
