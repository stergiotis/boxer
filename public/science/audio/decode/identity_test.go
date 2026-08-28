package decode

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeFixture writes size deterministic bytes to a file in dir and pins its
// modification time, so a rewrite that keeps the size also keeps the mtime and
// only the content differs.
func writeFixture(t *testing.T, dir string, name string, size int64) (path string) {
	t.Helper()
	path = filepath.Join(dir, name)
	rng := rand.New(rand.NewPCG(0x5eed, uint64(size)))
	body := make([]byte, size)
	for i := range body {
		body[i] = byte(rng.UintN(256))
	}
	require.NoError(t, os.WriteFile(path, body, 0o644))
	pinMTime(t, path)
	return path
}

func pinMTime(t *testing.T, path string) {
	t.Helper()
	fixed := time.Unix(1_700_000_000, 123_456_789)
	require.NoError(t, os.Chtimes(path, fixed, fixed))
}

func patchByte(t *testing.T, path string, off int64) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	require.NoError(t, err)
	var one [1]byte
	_, err = f.ReadAt(one[:], off)
	require.NoError(t, err)
	one[0] ^= 0xFF
	_, err = f.WriteAt(one[:], off)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	pinMTime(t, path)
}

func TestIdentityIsStableForTheSameFile(t *testing.T) {
	path := writeFixture(t, t.TempDir(), "a.bin", 3<<20)
	first, err := IdentityE(path)
	require.NoError(t, err)
	second, err := IdentityE(path)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, int64(3<<20), first.SizeBytes)
	require.NotZero(t, first.ModTimeUnixNano)
	require.NotEqual(t, [32]byte{}, first.Hash)
}

func TestIdentityDetectsAChangedEnd(t *testing.T) {
	const large int64 = 4 << 20
	const small int64 = 1024
	cases := []struct {
		name string
		size int64
		off  int64
	}{
		{"first byte", large, 0},
		{"inside head", large, identityEdgeBytes - 1},
		{"inside tail", large, large - identityEdgeBytes},
		{"last byte", large, large - 1},
		{"middle of a short file", small, small / 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := writeFixture(t, t.TempDir(), "a.bin", c.size)
			before, err := IdentityE(path)
			require.NoError(t, err)
			patchByte(t, path, c.off)
			after, err := IdentityE(path)
			require.NoError(t, err)
			require.Equal(t, before.SizeBytes, after.SizeBytes)
			require.Equal(t, before.ModTimeUnixNano, after.ModTimeUnixNano)
			require.NotEqual(t, before.Hash, after.Hash)
		})
	}
}

func TestIdentityMissesAChangeInTheMiddleOfALargeFile(t *testing.T) {
	// By design (ADR-0208 §SD4): the identity is a fingerprint over the size,
	// the mtime and 1 MiB of each end, not a checksum. An edit confined to the
	// middle of a file whose size and mtime are unchanged is invisible to it,
	// and the cheap open of a twelve-hour recording is what that buys. The
	// case is asserted rather than left implied so the limitation is not
	// mistaken for a defect.
	const size = 8 << 20
	path := writeFixture(t, t.TempDir(), "a.bin", size)
	before, err := IdentityE(path)
	require.NoError(t, err)
	patchByte(t, path, size/2)
	after, err := IdentityE(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestIdentityDetectsSizeAndMTime(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "a.bin", 2048)
	before, err := IdentityE(path)
	require.NoError(t, err)

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	require.NoError(t, err)
	_, err = f.Write([]byte{0})
	require.NoError(t, err)
	require.NoError(t, f.Close())
	pinMTime(t, path)

	grown, err := IdentityE(path)
	require.NoError(t, err)
	require.Equal(t, before.SizeBytes+1, grown.SizeBytes)
	require.NotEqual(t, before.Hash, grown.Hash)

	touched := time.Unix(1_700_000_001, 0)
	require.NoError(t, os.Chtimes(path, touched, touched))
	retouched, err := IdentityE(path)
	require.NoError(t, err)
	require.Equal(t, grown.SizeBytes, retouched.SizeBytes)
	require.NotEqual(t, grown.ModTimeUnixNano, retouched.ModTimeUnixNano)
	require.NotEqual(t, grown.Hash, retouched.Hash)
}

func TestIdentityOfAMissingFileErrors(t *testing.T) {
	_, err := IdentityE(filepath.Join(t.TempDir(), "absent.bin"))
	require.Error(t, err)
}
