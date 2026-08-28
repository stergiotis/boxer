package track

import (
	"context"
	"io"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/audio/pcm"
	"github.com/stergiotis/boxer/public/science/audio/pcm/pcmtest"
)

// scratchSource is a source shaped like a real decoder: the bytes of a read
// go through a scratch buffer held on the struct, and a read counter is
// bumped without synchronisation. Two unserialised concurrent reads through
// it are therefore a data race the detector reports, which is what makes a
// concurrency test over [lockedSource] mean anything — a [pcm.SynthSource]
// touches no mutable state and would pass with the lock removed.
//
// Sample values are a pure function of the absolute interleaved sample
// index, so a read is chunk-invariant as the contract requires.
type scratchSource struct {
	format  pcm.Format
	frames  int64
	scratch []byte
	reads   int64
	closes  int64
}

var _ pcm.SourceI = (*scratchSource)(nil)

func newScratchSource(format pcm.Format, frames int64) (inst *scratchSource) {
	return &scratchSource{format: format, frames: frames}
}

// scratchSample is the value scratchSource yields for one absolute
// interleaved sample index.
func scratchSample(index int64) (sample float32) {
	return float32(index%251)/125.5 - 1
}

func (inst *scratchSource) Format() (format pcm.Format) { return inst.format }
func (inst *scratchSource) Frames() (frames int64)      { return inst.frames }

func (inst *scratchSource) ReadFramesAtE(_ context.Context, frameOffset int64, dst []float32) (n int, err error) {
	n, err = pcm.ClampReadE(inst.format, inst.frames, frameOffset, dst)
	if err != nil || n == 0 {
		return n, err
	}
	ch := int(inst.format.Channels)
	need := n * ch
	if cap(inst.scratch) < need {
		inst.scratch = make([]byte, need)
	}
	raw := inst.scratch[:need]
	first := frameOffset * int64(ch)
	for i := range raw {
		raw[i] = byte((first + int64(i)) % 251)
	}
	for i, b := range raw {
		dst[i] = float32(b)/125.5 - 1
	}
	inst.reads++
	return n, nil
}

func (inst *scratchSource) CloseE() (err error) {
	inst.closes++
	return nil
}

func TestLockedSourceHonoursTheSourceContract(t *testing.T) {
	for _, channels := range []uint16{1, 2, 5} {
		format := pcm.Format{SampleRate: 48000, Channels: channels}
		for _, frames := range []int64{0, 1, 999, 40_000} {
			locked := newLockedSource(newScratchSource(format, frames))
			pcmtest.CheckSourceContract(t, locked, 2048)
			require.Equal(t, format, locked.Format())
			require.Equal(t, frames, locked.Frames())
		}
	}
}

// TestLockedSourceSerialisesConcurrentReads is the seam of ADR-0208 §SD1
// under the race detector: the frame thread's window reads and a sink
// callback's reads land on one source, and the adapter is the only thing
// keeping them apart.
func TestLockedSourceSerialisesConcurrentReads(t *testing.T) {
	format := pcm.Format{SampleRate: 48000, Channels: 2}
	const frames int64 = 50_000
	raw := newScratchSource(format, frames)
	locked := newLockedSource(raw)

	const readers = 4
	const readsPerGoroutine = 200
	ch := int(format.Channels)
	var wg sync.WaitGroup
	for g := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewPCG(0x7a11, uint64(g)))
			dst := make([]float32, 512*ch)
			for range readsPerGoroutine {
				from := r.Int64N(frames)
				want := 1 + r.IntN(512)
				n, err := locked.ReadFramesAtE(context.Background(), from, dst[:want*ch])
				if !assert.NoError(t, err) {
					return
				}
				if !assert.Positive(t, n) {
					return
				}
				for i := range n * ch {
					if !assert.Equal(t, scratchSample(from*int64(ch)+int64(i)), dst[i], "sample %d of a read from %d", i, from) {
						return
					}
				}
			}
		}()
	}
	wg.Wait()

	// Read under the lock, which is also the assertion that the counter the
	// readers bumped was never bumped concurrently.
	require.NoError(t, locked.CloseE())
	require.Equal(t, int64(readers*readsPerGoroutine), raw.reads)
}

func TestLockedSourceCloseIsIdempotentAndRefusesReads(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	raw := newScratchSource(format, 1000)
	locked := newLockedSource(raw)

	dst := make([]float32, 16)
	n, err := locked.ReadFramesAtE(context.Background(), 0, dst)
	require.NoError(t, err)
	require.Equal(t, 16, n)

	require.NoError(t, locked.CloseE())
	require.NoError(t, locked.CloseE())
	require.Equal(t, int64(1), raw.closes, "the wrapped source is closed once")

	n, err = locked.ReadFramesAtE(context.Background(), 0, dst)
	require.Zero(t, n)
	require.Error(t, err)
	require.NotErrorIs(t, err, io.EOF, "a closed source is not an ended one")
}

// TestLockedSourceClampsNegativeFrameCount guards the one value the adapter
// normalises rather than forwards: a source reporting a negative length would
// otherwise make every bound derived from it nonsense.
func TestLockedSourceClampsNegativeFrameCount(t *testing.T) {
	format := pcm.Format{SampleRate: 8000, Channels: 1}
	locked := newLockedSource(newScratchSource(format, -5))
	require.Equal(t, int64(0), locked.Frames())
}
