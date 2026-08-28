//go:build integration

package decode

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// memfdInput is a recording held in anonymous memory, the shape a host that
// must not put plaintext on disk stages into. Every OpenE re-opens the
// descriptor through procfs, so each decoder process gets a file offset of
// its own.
type memfdInput struct {
	name string
	f    *os.File
}

func newMemfdInput(t *testing.T, path string) (in *memfdInput) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	fd, err := unix.MemfdCreate("decode-test", unix.MFD_CLOEXEC)
	require.NoError(t, err)
	f := os.NewFile(uintptr(fd), "memfd:decode-test")
	_, err = f.Write(data)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return &memfdInput{name: "decode-test", f: f}
}

func (inst *memfdInput) Name() (s string) { return inst.name }

func (inst *memfdInput) OpenE() (f *os.File, err error) {
	return os.Open(childFdPath(int(inst.f.Fd())))
}

// TestFfmpegOverAnInheritedFdMatchesThePath is the whole claim of the fd
// seam: ffprobe determines the same length and ffmpeg decodes the same
// samples whether the recording is named on the command line or handed over
// as a descriptor — including after a backward seek, which restarts the
// process and so needs a second handle that has not been read.
func TestFfmpegOverAnInheritedFdMatchesThePath(t *testing.T) {
	requireFfmpeg(t)
	set := newFixtureSet(t)

	byPath := openFfmpegFixtureE(t, set.flac)
	byFd, err := OpenFfmpegFdE(context.Background(), newMemfdInput(t, set.flac))
	require.NoError(t, err)
	defer func() { require.NoError(t, byFd.CloseE()) }()

	require.Equal(t, byPath.Format(), byFd.Format())
	require.Equal(t, byPath.Frames(), byFd.Frames(), "ffprobe seeks an inherited fd as it seeks a path")

	shared := min(byPath.Frames(), byFd.Frames())
	want := readAll(t, byPath, shared, 4096)
	got := readAll(t, byFd, shared, 4096)
	require.Equal(t, want, got)

	// Backward: a restart with -ss over the fd. The source must land on the
	// same samples, which it cannot if the child inherited a handle another
	// process had already read through.
	restarts := byFd.Restarts()
	channels := int64(byFd.Format().Channels)
	const at, span int64 = 12000, 2048
	buf := make([]float32, span*channels)
	n, err := byFd.ReadFramesAtE(context.Background(), at, buf)
	require.NoError(t, err)
	require.Equal(t, int(span), n)
	require.Greater(t, byFd.Restarts(), restarts, "a backward read restarts the decoder")
	require.Equal(t, want[at*channels:(at+span)*channels], buf)
}
