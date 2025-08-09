package ea

import (
	"io"
	"math/rand/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// TransferDataWithSplitReadAndWrites copies nBytesToRead from r to dest by randomly using io.Reader and io.ByteReader interface. The number of bytes transferred by each method is roughly balanced.
func TransferDataWithSplitReadAndWrites(dest ByteBlockWriter, nBytesToRead int, r ByteReadReader, maxConsecutiveBytesToRead int, ra *rand.Rand) (singleByteReads int, blockReads int, bytesBlockReads int, n int, err error) {
	if maxConsecutiveBytesToRead < 2 {
		err = eh.Errorf("maxConsecutiveBytesToRead has to be larger or equal to 2")
		return
	}
	tmp := make([]byte, 0, maxConsecutiveBytesToRead)
	avgBytesReadBlock := maxConsecutiveBytesToRead / 2
	for n < nBytesToRead {
		// half probability: read block or read byte
		if ra.IntN(avgBytesReadBlock+1) == 0 {
			// read block
			blockReads++
			l := ra.IntN(maxConsecutiveBytesToRead-1) + 1
			t := tmp[:l]
			var u int
			u, err = io.ReadFull(r, t)
			n += u
			if u > 0 {
				bytesBlockReads += u
				_, err = dest.Write(t[:u])
				if err != nil {
					err = eh.Errorf("unable to write to buffer: %w", err)
					return
				}
			}
			if err != nil {
				return
			}
		} else {
			// byte read
			singleByteReads++
			var b byte
			b, err = r.ReadByte()
			if err != nil {
				return
			}
			n++
			err = dest.WriteByte(b)
			if err != nil {
				err = eh.Errorf("unable to write to buffer: %w", err)
				return
			}
		}
	}
	return
}
