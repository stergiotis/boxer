package sockets_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sockets"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

const tcpFixture = `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 12345 1 0000000000000000 100 0 0 10 0
   1: 0100007F:9C40 0200007F:1F90 01 00000000:00000000 00:00000000 00000000  1000        0 12346 1 0000000000000000 100 0 0 10 0
`

const udpFixture = `   sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops
  100: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000   999        0 22345 2 0000000000000000 0
  101: 0100007F:8125 0100007F:0035 01 00000000:00000000 00:00000000 00000000   999        0 22346 2 0000000000000000 0
`

const unixFixture = `Num       RefCount Protocol Flags    Type St Inode Path
0000000000000000: 00000002 00000000 00010000 0001 01 32345 /run/test.sock
0000000000000000: 00000003 00000000 00000000 0001 03 32346 /run/connected.sock
0000000000000000: 00000002 00000000 00010000 0001 01 32347 @abstract.sock
0000000000000000: 00000002 00000000 00010000 0001 01 32348
`

// writeFixtureRoot lays out a /proc-shaped tree: the net tables plus one
// pid (4711) whose fd table holds the tcp listener and the named unix
// socket. The udp and abstract-unix inodes belong to no visible pid and
// must stay unattributed.
func writeFixtureRoot(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "net"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(tcpFixture), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "net", "udp"), []byte(udpFixture), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "net", "unix"), []byte(unixFixture), 0o644))

	fdDir := filepath.Join(root, "4711", "fd")
	require.NoError(t, os.MkdirAll(fdDir, 0o755))
	require.NoError(t, os.Symlink("socket:[12345]", filepath.Join(fdDir, "4")))
	require.NoError(t, os.Symlink("socket:[32345]", filepath.Join(fdDir, "5")))
	require.NoError(t, os.Symlink("/dev/null", filepath.Join(fdDir, "0")))
	return
}

func findByInode(t *testing.T, rows []sysmsnap.SocketInfo, inode uint64) (row sysmsnap.SocketInfo) {
	t.Helper()
	for _, r := range rows {
		if r.Inode == inode {
			return r
		}
	}
	t.Fatalf("no row with inode %d in %+v", inode, rows)
	return
}

func TestSample_ParsesAndAttributes(t *testing.T) {
	root := writeFixtureRoot(t)
	c, err := sockets.New(sockets.Options{Proc: procfs.New(root)})
	require.NoError(t, err)

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.NotNil(t, snap)
	// tcp LISTEN + udp bound + 2 accepting named unix; the established
	// tcp/udp rows, the non-accepting unix row, and the pathless unix
	// row are filtered.
	require.Len(t, snap.Sockets, 4)

	tcp := findByInode(t, snap.Sockets, 12345)
	require.Equal(t, sysmsnap.SocketProtoTCP, tcp.Proto)
	require.Equal(t, "127.0.0.1", tcp.Addr)
	require.Equal(t, uint16(8080), tcp.Port)
	require.Equal(t, uint32(1000), tcp.UID)
	require.Equal(t, uint32(4711), tcp.PID)

	udp := findByInode(t, snap.Sockets, 22345)
	require.Equal(t, sysmsnap.SocketProtoUDP, udp.Proto)
	require.Equal(t, "0.0.0.0", udp.Addr)
	require.Equal(t, uint16(53), udp.Port)
	require.Equal(t, uint32(999), udp.UID)
	require.Equal(t, uint32(0), udp.PID) // no visible owner: partial over absent

	unixNamed := findByInode(t, snap.Sockets, 32345)
	require.Equal(t, sysmsnap.SocketProtoUnix, unixNamed.Proto)
	require.Equal(t, "/run/test.sock", unixNamed.Addr)
	require.Equal(t, uint16(0), unixNamed.Port)
	require.Equal(t, uint32(4711), unixNamed.PID)

	unixAbstract := findByInode(t, snap.Sockets, 32347)
	require.Equal(t, "@abstract.sock", unixAbstract.Addr)
	require.Equal(t, uint32(0), unixAbstract.PID)
}

// TestSample_MissingTablesArePartial proves one readable table suffices
// (no IPv6 tables here) and only a fully unreadable /proc/net errors.
func TestSample_MissingTablesArePartial(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "net"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "net", "tcp"), []byte(tcpFixture), 0o644))

	c, err := sockets.New(sockets.Options{Proc: procfs.New(root)})
	require.NoError(t, err)
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Sockets, 1)

	empty, err := sockets.New(sockets.Options{Proc: procfs.New(t.TempDir())})
	require.NoError(t, err)
	_, err = empty.Sample(context.Background())
	require.Error(t, err)
}

// TestSample_IntervalCaching proves the collector-owned cadence: within
// the interval the cached snapshot is served (fixture changes invisible),
// after it a fresh collection runs.
func TestSample_IntervalCaching(t *testing.T) {
	root := writeFixtureRoot(t)
	now := time.UnixMilli(1_000_000)
	c, err := sockets.New(sockets.Options{
		Proc:     procfs.New(root),
		Interval: 15 * time.Second,
		NowFunc:  func() time.Time { return now },
	})
	require.NoError(t, err)

	first, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, first.Sockets, 4)

	// Mutate the fixture: within the interval the cache must hide it.
	require.NoError(t, os.WriteFile(filepath.Join(root, "net", "udp"), []byte(udpFixture[:len(udpFixture)/2]), 0o644))
	now = now.Add(5 * time.Second)
	second, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Same(t, first, second)

	now = now.Add(15 * time.Second)
	third, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.NotSame(t, first, third)
	require.NotEqual(t, len(first.Sockets), len(third.Sockets))
	require.Equal(t, now.UnixMilli(), third.CollectedAtUnixMs)
}

// TestSample_RealProc_AttributesOwnListener is the end-to-end proof on a
// live /proc: a listener this test opens must come back attributed to
// this test's own pid (our own fd table is always readable).
func TestSample_RealProc_AttributesOwnListener(t *testing.T) {
	if _, statErr := os.Stat("/proc/net/tcp"); statErr != nil {
		t.Skipf("no live /proc/net: %v", statErr)
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)

	c, err := sockets.New(sockets.Options{})
	require.NoError(t, err)
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	own := os.Getpid()
	for _, r := range snap.Sockets {
		if r.Proto == sysmsnap.SocketProtoTCP && r.Port == port && r.Addr == "127.0.0.1" {
			require.Equal(t, uint32(own), r.PID, "own listener must attribute to our pid")
			return
		}
	}
	t.Fatalf("own listener 127.0.0.1:%d not found in %d sampled sockets", port, len(snap.Sockets))
}
