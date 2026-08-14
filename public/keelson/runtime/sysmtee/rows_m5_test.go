package sysmtee

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testProcs() []sysmsnap.ProcInfo {
	return []sysmsnap.ProcInfo{
		{PID: 1, PPID: 0, Name: "systemd", State: 'S', UID: 0, GID: 0, User: "root",
			Cmd: "/sbin/init splash", CPUPercent: 0.5, RSSBytes: 1 << 20, VMSizeBytes: 1 << 24,
			NumThreads: 1, Nice: 0, Priority: 20, StartedAtUnixMs: 1000,
			Component: "init", CgroupUnit: "init.scope"},
		{PID: 2, PPID: 0, Name: "kthreadd", State: 'S', KernelThread: true,
			Cmd: "", StartedAtUnixMs: 1001},
		{PID: 4242, PPID: 1, Name: "boxer", State: 'R', UID: 1000, GID: 1000, User: "someuser",
			Cmd: "boxer sysmetricsd --tee", CPUPercent: 250, RSSBytes: 512 << 20,
			NumThreads: 12, Priority: 20, StartedAtUnixMs: 2000, CgroupUnit: "boxer.service"},
	}
}

func TestProcRow_TransposesAndAligns(t *testing.T) {
	row, err := procRow("box1", testProcs(), testTs)
	require.NoError(t, err)

	require.Len(t, row.Pid, 3)
	for _, n := range []int{len(row.Ppid), len(row.Name), len(row.State), len(row.CPUPercent),
		len(row.RSSBytes), len(row.VMSizeBytes), len(row.NumThreads), len(row.Nice),
		len(row.Priority), len(row.KernelThread), len(row.StartedAtUnixMs),
		len(row.Component), len(row.CgroupUnit)} {
		assert.Equal(t, len(row.Pid), n, "every array must have one element per process")
	}

	assert.EqualValues(t, 1, row.Pid[0])
	assert.Equal(t, "systemd", row.Name[0])
	assert.Equal(t, "S", row.State[0], "the state is stored as its letter")
	assert.EqualValues(t, 0, row.KernelThread[0])
	assert.EqualValues(t, 1, row.KernelThread[1], "kthreadd is a kernel thread")
	assert.Equal(t, "boxer.service", row.CgroupUnit[2])

	// Per-CPU and unclamped: a process on 2.5 cores reads 250, and clamping to
	// 100 would erase exactly the process worth looking at.
	assert.EqualValues(t, 250, row.CPUPercent[2])
}

// The sensitive fields must not appear on the always-written kind. This is the
// assertion that stands between a default deployment and durably stored command
// lines.
func TestProcRow_CarriesNoCommandLineOrUser(t *testing.T) {
	row, err := procRow("box1", testProcs(), testTs)
	require.NoError(t, err)
	for _, name := range row.Name {
		assert.NotContains(t, name, "--tee", "comm is the truncated binary name, never the command line")
	}
	for _, c := range row.Component {
		assert.NotEqual(t, "someuser", c)
	}
}

func TestProcCmdRow_CarriesTheSensitiveFieldsAndJoinsOnPid(t *testing.T) {
	procs := testProcs()
	sample, err := procRow("box1", procs, testTs)
	require.NoError(t, err)
	cmd, err := procCmdRow("box1", procs, testTs)
	require.NoError(t, err)

	assert.NotEqual(t, sample.Id, cmd.Id, "the two kinds are separate series")
	require.Len(t, cmd.Pid, 3)
	// Pid repeats on this kind because a reader joins the two on it; the arrays
	// are aligned within a kind, not across kinds.
	assert.Equal(t, sample.Pid, cmd.Pid)
	assert.Equal(t, "boxer sysmetricsd --tee", cmd.Cmd[2])
	assert.Equal(t, "someuser", cmd.User[2])
	assert.EqualValues(t, 1000, cmd.Uid[2])
	assert.Empty(t, cmd.Cmd[1], "kernel threads have no command line")
}

// A zero state byte would otherwise reach a LowCardinality(String) column as a
// NUL, which is neither readable nor what the collector meant.
func TestProcStateLabel_RendersMissingAsEmpty(t *testing.T) {
	assert.Equal(t, "R", procStateLabel('R'))
	assert.Equal(t, "", procStateLabel(0))
}

func TestSocketRow_TransposesAndDatesByTheCollectionStamp(t *testing.T) {
	collected := time.Unix(1_700_000_500, 0).UTC()
	row, err := socketRow("box1", &sysmsnap.SocketsSnapshot{
		CollectedAtUnixMs: collected.UnixMilli(),
		Sockets: []sysmsnap.SocketInfo{
			{Proto: sysmsnap.SocketProtoTCP, Addr: "0.0.0.0", Port: 8123, Inode: 111, UID: 0, PID: 42},
			{Proto: sysmsnap.SocketProtoUnix, Addr: "@/tmp/x", Port: 0, Inode: 222, UID: 1000, PID: 0},
		},
	})
	require.NoError(t, err)

	// Dated by the collector, not by the bundle that carried it: the sockets
	// table is re-sent on every tick and only the stamp says when it was taken.
	assert.Equal(t, collected, row.Ts)

	require.Len(t, row.Proto, 2)
	assert.Equal(t, len(row.Proto), len(row.Pid))
	assert.Equal(t, "tcp", row.Proto[0])
	assert.EqualValues(t, 8123, row.Port[0])
	assert.EqualValues(t, 0, row.Port[1], "unix sockets have no port")
	// Zero means "not attributed" — the fd table was unreadable — never
	// "owned by pid 0".
	assert.EqualValues(t, 0, row.Pid[1])
	assert.EqualValues(t, 222, row.Inode[1])
}
