package wire

import (
	"encoding/binary"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ErrBadParts is wrapped by DeserializeParts when the bytes are not a part
// archive: a truncated count, a length past the end, a part count past the
// bytes that could hold it.
var ErrBadParts = eh.Errorf("bytes are not a part archive")

const partsIntLen = 4

// SerializedPartsLen is the size SerializeParts produces for parts.
func SerializedPartsLen(parts [][]byte) (n int) {
	n = partsIntLen * (len(parts) + 1)
	for _, p := range parts {
		n += len(p)
	}
	return
}

// AppendParts appends the archive of parts to dst and returns the extended
// slice: a big-endian 32-bit part count, then each part as a big-endian 32-bit
// length and its bytes.
func AppendParts(dst []byte, parts [][]byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(parts)))
	for _, p := range parts {
		dst = binary.BigEndian.AppendUint32(dst, uint32(len(p)))
		dst = append(dst, p...)
	}
	return dst
}

// SerializeParts is AppendParts into a fresh slice of exactly the right size.
func SerializeParts(parts [][]byte) []byte {
	return AppendParts(make([]byte, 0, SerializedPartsLen(parts)), parts)
}

// DeserializeParts splits an archive into its parts. The parts alias b; a
// caller that keeps them past b's lifetime copies them.
func DeserializeParts(b []byte) (parts [][]byte, err error) {
	if len(b) < partsIntLen {
		err = eb.Build().Int("len", len(b)).Errorf("part count: %w", ErrBadParts)
		return
	}
	count := binary.BigEndian.Uint32(b)
	b = b[partsIntLen:]
	// Every part costs at least its length prefix, so a count the remaining
	// bytes cannot hold is refused before anything is allocated for it.
	if uint64(count)*partsIntLen > uint64(len(b)) {
		err = eb.Build().Uint32("count", count).Int("len", len(b)).Errorf("part count exceeds the bytes: %w", ErrBadParts)
		return
	}
	parts = make([][]byte, count)
	for i := range parts {
		if len(b) < partsIntLen {
			err = eb.Build().Int("part", i).Errorf("part length: %w", ErrBadParts)
			return nil, err
		}
		n := binary.BigEndian.Uint32(b)
		b = b[partsIntLen:]
		if uint64(n) > uint64(len(b)) {
			err = eb.Build().Int("part", i).Uint32("len", n).Int("remaining", len(b)).Errorf("part runs past the end: %w", ErrBadParts)
			return nil, err
		}
		parts[i] = b[:n:n]
		b = b[n:]
	}
	if len(b) != 0 {
		err = eb.Build().Int("trailing", len(b)).Errorf("bytes follow the last part: %w", ErrBadParts)
		return nil, err
	}
	return
}
