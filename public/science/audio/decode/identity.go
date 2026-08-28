package decode

import (
	"encoding/binary"
	"io"
	"os"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/audio/peaks"
)

// identityEdgeBytes is how much of each end of a file the identity hash
// covers. A file of at most twice this is hashed whole, so the read is bounded
// by 2 * identityEdgeBytes for any input.
const identityEdgeBytes int64 = 1 << 20

// identityHashBytes is the blake3 digest width, matching peaks.Identity.Hash.
const identityHashBytes int = 32

// IdentityE computes the peaks-cache identity of the file at path (ADR-0208
// §SD4): its size in bytes, its modification time in unix nanoseconds, and a
// blake3-256 over the size, the mtime and the file's first and last
// identityEdgeBytes — the whole file when it is short enough that those
// overlap.
//
// It is a fingerprint, not a checksum: two files agreeing on size, mtime and
// both ends are one file as far as the cache is concerned, so an edit confined
// to the middle of a large recording that also preserves size and mtime goes
// undetected. Hashing a twelve-hour file in full would cost more than
// rebuilding the pyramid the cache exists to skip.
func IdentityE(path string) (id peaks.Identity, err error) {
	f, err := os.Open(path)
	if err != nil {
		return id, eb.Build().Str("path", path).Errorf("open recording for identity: %w", err)
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return id, eb.Build().Str("path", path).Errorf("stat recording for identity: %w", err)
	}
	id.SizeBytes = st.Size()
	id.ModTimeUnixNano = st.ModTime().UnixNano()

	hasher := blake3.New(identityHashBytes, nil)
	var meta [16]byte
	binary.LittleEndian.PutUint64(meta[0:8], uint64(id.SizeBytes))
	binary.LittleEndian.PutUint64(meta[8:16], uint64(id.ModTimeUnixNano))
	_, err = hasher.Write(meta[:])
	if err != nil {
		return id, eb.Build().Str("path", path).Errorf("hash recording identity: %w", err)
	}

	if id.SizeBytes <= 2*identityEdgeBytes {
		_, err = io.Copy(hasher, f)
	} else {
		_, err = io.CopyN(hasher, f, identityEdgeBytes)
		if err == nil {
			_, err = f.Seek(id.SizeBytes-identityEdgeBytes, io.SeekStart)
		}
		if err == nil {
			_, err = io.CopyN(hasher, f, identityEdgeBytes)
		}
	}
	if err != nil {
		return id, eb.Build().
			Str("path", path).
			Int64("sizeBytes", id.SizeBytes).
			Errorf("read recording for identity: %w", err)
	}

	id.Hash = [32]byte(hasher.Sum(make([]byte, 0, identityHashBytes)))
	return id, nil
}
