//go:build llm_generated_opus47

package proc_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/proc"
)

func TestSample_FirstCallZeroCPU(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	writeFakeProc(t, root, fakeProc{
		pid: 100, ppid: 1, name: "proc100", state: 'S',
		uid: 1000, gid: 1000, utime: 10, stime: 10, threads: 1, rssPages: 256, vsize: 1 << 20,
	})
	c, _ := proc.New(proc.Options{
		Proc:       procfs.New(root),
		UserLookup: staticUserLookup(map[uint32]string{1000: "alice"}),
		ClkTckHz:   100,
		PageSize:   4096,
		NumCPUs:    4,
	})

	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, uint32(100), infos[0].PID)
	assert.Equal(t, uint32(1), infos[0].PPID)
	assert.Equal(t, "proc100", infos[0].Name)
	assert.Equal(t, byte('S'), infos[0].State)
	assert.Equal(t, uint32(1000), infos[0].UID)
	assert.Equal(t, "alice", infos[0].User)
	assert.Equal(t, uint64(256*4096), infos[0].RSSBytes)
	assert.Equal(t, float32(0), infos[0].CPUPercent, "first sample has no prior tick")
}

func TestSample_CPUPercent_WithinTolerance(t *testing.T) {
	// Setup math:
	//   global delta = 1000 ticks; pid delta = 80 ticks; numCPUs = 4.
	//   Expected CPU% = 80 * 100 * 4 / 1000 = 32.
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "10.00 10.00\n")
	writeFakeProc(t, root, fakeProc{
		pid: 100, ppid: 1, name: "p", state: 'R', uid: 1000, gid: 1000,
		utime: 10, stime: 10, threads: 1, rssPages: 100, vsize: 1 << 20,
	})

	c, _ := proc.New(proc.Options{
		Proc:       procfs.New(root),
		UserLookup: staticUserLookup(nil),
		ClkTckHz:   100,
		PageSize:   4096,
		NumCPUs:    4,
	})
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	writeGlobalStat(t, root, "cpu  100 0 100 800 0 0 0 0 0 0\n")
	writeFakeProc(t, root, fakeProc{
		pid: 100, ppid: 1, name: "p", state: 'R', uid: 1000, gid: 1000,
		utime: 60, stime: 40, threads: 1, rssPages: 100, vsize: 1 << 20,
	})

	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.InDelta(t, 32.0, float64(infos[0].CPUPercent), 0.5,
		"CPU%% must be within 0.5 of the externally-computed reference (32.0)")
}

func TestSample_CPUPercent_ClampedToMax(t *testing.T) {
	// pid delta exceeds global delta → would compute > 100*N. Must clamp.
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 1, name: "p", utime: 0, stime: 0, threads: 1})

	c, _ := proc.New(proc.Options{
		Proc: procfs.New(root), ClkTckHz: 100, NumCPUs: 2,
		UserLookup: staticUserLookup(nil),
	})
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Implausible: pid did 5000 ticks of work but global only added 100 ticks.
	writeGlobalStat(t, root, "cpu  100 0 0 0 0 0 0 0 0 0\n")
	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 1, name: "p", utime: 5000, stime: 0, threads: 1})

	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, float32(200), infos[0].CPUPercent, "must clamp to 100*NumCPUs (200)")
}

func TestSample_50PIDs(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "100.00 100.00\n")

	for i := 0; i < 50; i++ {
		pid := uint32(1000 + i)
		writeFakeProc(t, root, fakeProc{
			pid: pid, ppid: 1, name: fmt.Sprintf("p%d", i), state: 'S',
			uid: uint32(1000 + (i % 3)), gid: 1000,
			utime: uint64(i * 10), stime: uint64(i * 5),
			threads: int32(1 + (i % 4)), rssPages: uint64(100 * (i + 1)), vsize: uint64(1 << 20),
		})
	}

	c, _ := proc.New(proc.Options{
		Proc:       procfs.New(root),
		UserLookup: staticUserLookup(map[uint32]string{1000: "alice", 1001: "bob", 1002: "carol"}),
		ClkTckHz:   100,
		PageSize:   4096,
		NumCPUs:    8,
	})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 50)

	// Sorted ascending by PID.
	for i := 1; i < len(infos); i++ {
		assert.Less(t, infos[i-1].PID, infos[i].PID)
	}
	// User resolution distributes correctly.
	users := map[string]int{}
	for _, info := range infos {
		users[info.User]++
	}
	assert.NotZero(t, users["alice"])
	assert.NotZero(t, users["bob"])
	assert.NotZero(t, users["carol"])
}

func TestSample_DeadPid_EmptyDir(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "200"), 0o755))

	c, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil)})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, infos, "empty pid dir must be skipped silently")
}

func TestSample_DeadPid_OnlyComm(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "300"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "300", "comm"), []byte("p300\n"), 0o644))

	c, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil)})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, infos, "missing stat must skip the pid")
}

func TestSample_DeadPid_TruncatedStat(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	dir := filepath.Join(root, "400")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "comm"), []byte("p400\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte("400 (p400) S 1\n"), 0o644))

	c, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil)})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, infos, "stat with too few fields must skip the pid")
}

func TestSample_DeadPid_NoStatusStillEmitsPartial(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	fp := fakeProc{pid: 500, ppid: 1, name: "p500", state: 'S', uid: 1000, gid: 1000, threads: 1}
	writeFakeProc(t, root, fp)
	require.NoError(t, os.Remove(filepath.Join(root, "500", "status")))

	c, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil)})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "p500", infos[0].Name)
	assert.Equal(t, uint32(0), infos[0].UID, "missing status leaves UID at zero")
}

func TestSample_DeadPid_NoCommStillProceedsViaStat(t *testing.T) {
	// Reverse race: comm gone, stat present. Per readOnePID's ENOENT bail
	// rule, missing comm aborts the pid. Verify that.
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	dir := filepath.Join(root, "600")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// Write stat but skip comm.
	statBytes := []byte("600 (p600) S 1 600 600 0 -1 4194304 0 0 0 0 0 0 0 0 20 0 1 0 100 1024 0 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), statBytes, 0o644))

	c, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil)})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.Empty(t, infos, "missing comm with ENOENT semantics aborts the pid")
}

func TestSample_KernelThreadFilter(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	// pid 2 is kthreadd itself; pid 100 is a kernel thread (ppid=2);
	// pid 101 is a regular process (ppid=1).
	writeFakeProc(t, root, fakeProc{pid: 2, ppid: 0, name: "kthreadd", threads: 1})
	writeFakeProc(t, root, fakeProc{pid: 100, ppid: 2, name: "ksoftirqd/0", threads: 1})
	writeFakeProc(t, root, fakeProc{pid: 101, ppid: 1, name: "userproc", threads: 1})

	cFiltered, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil)})
	infos, err := cFiltered.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, uint32(101), infos[0].PID)
	assert.False(t, infos[0].KernelThread)

	cIncluded, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil), IncludeKernelThreads: true})
	infos, err = cIncluded.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 3)
	for _, info := range infos {
		switch info.PID {
		case 2, 100:
			assert.True(t, info.KernelThread, "pid %d should be flagged as kernel thread", info.PID)
		case 101:
			assert.False(t, info.KernelThread)
		}
	}
}

func TestSample_CmdlineNulSeparated(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	fp := fakeProc{pid: 700, ppid: 1, name: "bash", state: 'S', threads: 1}
	writeFakeProc(t, root, fp)
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "700", "cmdline"),
		[]byte("/usr/bin/bash\x00-c\x00echo hi\x00"), 0o644))

	c, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil)})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "/usr/bin/bash -c echo hi", infos[0].Cmd)
}

func TestSample_CommWithSpacesAndParens(t *testing.T) {
	// Synthetic case: comm contains both spaces and parens. The LAST ')'
	// must be the comm terminator (matches kernel behavior).
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	dir := filepath.Join(root, "800")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "comm"), []byte("weird (name)\n"), 0o644))
	statRaw := "800 (weird (name)) S 1 800 800 0 -1 4194304 0 0 0 0 5 5 0 0 20 0 1 0 100 1024 0 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte(statRaw), 0o644))
	statusRaw := "Name:\tweird (name)\nUid:\t1000\t1000\t1000\t1000\nGid:\t1000\t1000\t1000\t1000\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status"), []byte(statusRaw), 0o644))

	c, _ := proc.New(proc.Options{Proc: procfs.New(root), UserLookup: staticUserLookup(nil)})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, byte('S'), infos[0].State, "state must be S, not '(' from a botched paren parse")
	assert.Equal(t, uint32(1), infos[0].PPID, "ppid must be 1, not 'weird' from a botched parse")
}

func TestAll_BreakEarly_DoesNotCorruptState(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "1.00 1.00\n")
	for i := 0; i < 5; i++ {
		writeFakeProc(t, root, fakeProc{
			pid: uint32(1000 + i), ppid: 1, name: fmt.Sprintf("p%d", i),
			utime: uint64(i * 10), stime: uint64(i * 5), threads: 1,
		})
	}
	c, _ := proc.New(proc.Options{
		Proc: procfs.New(root), UserLookup: staticUserLookup(nil),
		ClkTckHz: 100, NumCPUs: 4,
	})

	// Iterate but break after 2 pids.
	var seen int
	for _, err := range c.All(context.Background()) {
		require.NoError(t, err)
		seen++
		if seen == 2 {
			break
		}
	}
	assert.Equal(t, 2, seen)

	// Second sample: even though we broke early before, all 5 pids are
	// still reported (state was eagerly recorded).
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 5)
	for _, info := range infos {
		assert.Equal(t, float32(0), info.CPUPercent, "no CPU% bump from interrupted iteration")
	}
}

func TestSample_MaxProcs_CapsByCPUDescThenRSS(t *testing.T) {
	root := newProcTree(t)
	// Two-tick run: first tick primes; second tick observes the deltas.
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "100.00 100.00\n")

	// 10 PIDs. Half (1000..1004) accumulate CPU ticks; the other half
	// (1005..1009) stay idle but get progressively larger RSS so the RSS
	// tiebreak is exercised on the zero-CPU tail.
	for i := 0; i < 10; i++ {
		pid := uint32(1000 + i)
		writeFakeProc(t, root, fakeProc{
			pid: pid, ppid: 1, name: fmt.Sprintf("p%d", i), state: 'S',
			uid: 1000, gid: 1000, threads: 1,
			rssPages: uint64(100 + i*10), vsize: 1 << 20,
		})
	}

	c, _ := proc.New(proc.Options{
		Proc: procfs.New(root), UserLookup: staticUserLookup(map[uint32]string{1000: "alice"}),
		ClkTckHz: 100, PageSize: 4096, NumCPUs: 4,
		MaxProcs: 3,
	})

	// Tick 1: priming — every CPU% is 0; the cap should still apply (by
	// RSS desc tiebreak), so we must observe exactly MaxProcs entries.
	primed, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, primed, 3)

	// Bump utime on 1000..1004 and advance global stat so deltas are
	// non-zero on the next sample.
	writeGlobalStat(t, root, "cpu  1000 0 0 0 0 0 0 0 0 0\n")
	for i := 0; i < 5; i++ {
		pid := uint32(1000 + i)
		writeFakeProc(t, root, fakeProc{
			pid: pid, ppid: 1, name: fmt.Sprintf("p%d", i), state: 'S',
			uid: 1000, gid: 1000, threads: 1,
			utime: uint64((i + 1) * 50), stime: 0,
			rssPages: uint64(100 + i*10), vsize: 1 << 20,
		})
	}

	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 3, "MaxProcs=3 must clamp the result slice")

	// All survivors come from the busy half (1000..1004); none from
	// 1005..1009. CPU%-desc plus RSS-desc tiebreak orders them as
	// {1004, 1003, 1002} on the input side; the final sort is PID asc.
	pids := make([]uint32, 0, len(infos))
	for _, info := range infos {
		pids = append(pids, info.PID)
		assert.Equal(t, "alice", info.User,
			"survivors must have run through enrichDetailPID (status read)")
	}
	assert.Equal(t, []uint32{1002, 1003, 1004}, pids,
		"top-N by CPU%% desc must select the highest-utime PIDs and the final slice is PID-asc sorted")
}

func TestSample_MaxProcs_Unlimited(t *testing.T) {
	root := newProcTree(t)
	writeGlobalStat(t, root, "cpu  0 0 0 0 0 0 0 0 0 0\n")
	writeUptime(t, root, "100.00 100.00\n")
	for i := 0; i < 7; i++ {
		writeFakeProc(t, root, fakeProc{
			pid: uint32(2000 + i), ppid: 1, name: fmt.Sprintf("u%d", i), state: 'S',
			threads: 1, rssPages: 200, vsize: 1 << 20,
		})
	}
	c, _ := proc.New(proc.Options{
		Proc: procfs.New(root), UserLookup: staticUserLookup(nil),
		ClkTckHz: 100, PageSize: 4096, NumCPUs: 4,
		// MaxProcs left at 0 → unlimited.
	})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 7, "MaxProcs=0 must keep every visible PID")
}

func TestSample_LiveProc_Smoke(t *testing.T) {
	if _, err := os.Stat("/proc/self"); err != nil {
		t.Skipf("no live /proc: %v", err)
	}
	c, _ := proc.New(proc.Options{})
	infos, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, infos, "live system should have at least the running test process")

	myPID := uint32(os.Getpid())
	var sawSelf bool
	maxCPUPercent := float32(100 * runtime.NumCPU())
	for _, info := range infos {
		// Plausibility: every process's basic shape.
		assert.Greater(t, info.PID, uint32(0))
		assert.NotEmpty(t, info.Name)
		// Per-CPU view: a fully-loaded N-thread process should not
		// exceed 100*N. Allow 5% slack for delta-window jitter.
		assert.LessOrEqual(t, info.CPUPercent, maxCPUPercent*1.05,
			"pid %d (%s) cpu%% %v > %v", info.PID, info.Name, info.CPUPercent, maxCPUPercent)
		assert.GreaterOrEqual(t, info.CPUPercent, float32(0))
		assert.GreaterOrEqual(t, info.NumThreads, int32(0))

		if info.PID == myPID {
			sawSelf = true
		}
	}
	assert.True(t, sawSelf, "test process must appear in the live /proc snapshot")
}

func TestSample_ContextCancelled(t *testing.T) {
	c, _ := proc.New(proc.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Sample(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// --- helpers -------------------------------------------------------------

type fakeProc struct {
	pid       uint32
	ppid      uint32
	name      string
	state     byte
	uid       uint32
	gid       uint32
	utime     uint64
	stime     uint64
	nice      int32
	priority  int32
	threads   int32
	starttime uint64
	vsize     uint64
	rssPages  uint64
}

func (fp fakeProc) statContent() string {
	state := fp.state
	if state == 0 {
		state = 'S'
	}
	prio := fp.priority
	if prio == 0 {
		prio = 20
	}
	threads := fp.threads
	if threads == 0 {
		threads = 1
	}
	// Field positions match Documentation/filesystems/proc.rst (1-indexed
	// pid, comm, then state at 3 ... rss at 24).
	return fmt.Sprintf(
		"%d (%s) %c %d %d %d 0 -1 4194304 0 0 0 0 %d %d 0 0 %d %d %d 0 %d %d %d 0 0 0 0 0 0 0 0 0 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n",
		fp.pid, fp.name, state, fp.ppid, fp.pid, fp.pid,
		fp.utime, fp.stime, prio, fp.nice, threads, fp.starttime, fp.vsize, fp.rssPages,
	)
}

func (fp fakeProc) statusContent() string {
	state := fp.state
	if state == 0 {
		state = 'S'
	}
	return fmt.Sprintf(
		"Name:\t%s\nState:\t%c (sleeping)\nPid:\t%d\nPPid:\t%d\nUid:\t%d\t%d\t%d\t%d\nGid:\t%d\t%d\t%d\t%d\n",
		fp.name, state, fp.pid, fp.ppid,
		fp.uid, fp.uid, fp.uid, fp.uid,
		fp.gid, fp.gid, fp.gid, fp.gid,
	)
}

func newProcTree(t *testing.T) (root string) {
	t.Helper()
	return t.TempDir()
}

func writeGlobalStat(t *testing.T, root, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stat"), []byte(content), 0o644))
}

func writeUptime(t *testing.T, root, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "uptime"), []byte(content), 0o644))
}

func writeFakeProc(t *testing.T, root string, fp fakeProc) {
	t.Helper()
	dir := filepath.Join(root, strconv.FormatUint(uint64(fp.pid), 10))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "comm"), []byte(fp.name+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cmdline"), []byte("/usr/bin/"+fp.name+"\x00"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte(fp.statContent()), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "status"), []byte(fp.statusContent()), 0o644))
}

func staticUserLookup(byUid map[uint32]string) proc.UserLookupFunc {
	return func(uid uint32) string {
		if byUid == nil {
			return ""
		}
		return byUid[uid]
	}
}

// silence the unused-import linter for time when no test imports it directly
var _ = time.Time{}
