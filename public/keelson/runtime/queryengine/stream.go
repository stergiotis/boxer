package queryengine

import (
	"io"

	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// stream.go — the two stream shapes boxer's own adapters are built from,
// exported because an engine implemented elsewhere needs them just as much.
//
// They exist so the sequencing discipline lives in ONE place. Frame numbers
// start at 1 and strictly increase, exactly one terminal frame ends a
// stream, and nothing follows it — [runstream.Collector] rejects a producer
// that gets any of that wrong, and an adapter open-coding its own counter
// is a producer waiting to get it wrong.

// DefaultChunkSize is how much of a streaming body one data frame carries.
// Big enough that framing overhead is noise against the transfer, small
// enough that a consumer rendering as it goes is not waiting on a megabyte.
const DefaultChunkSize = 64 << 10

// sliceStream delivers frames prepared up front. It is what a buffered
// engine yields: a one-shot worker that hands back a whole result has
// nothing to stream, and saying so with a single data frame is more honest
// than chopping an in-memory buffer into pieces to look like a stream.
type sliceStream struct {
	frames []runstream.Frame[[]byte]
	pos    int
	closer io.Closer
	closed bool
}

var _ StreamI = (*sliceStream)(nil)

// NewSliceStream returns a stream over frames already in hand. The caller
// numbers nothing: sequence numbers are assigned here, in arrival order.
//
// closer may be nil. The frames' payloads are handed to the consumer as
// they are, so they must not be reused after this call.
func NewSliceStream(data [][]byte, final runstream.Terminal, closer io.Closer) (st StreamI) {
	frames := make([]runstream.Frame[[]byte], 0, len(data)+1)
	var seq runstream.Seq
	for _, d := range data {
		seq++
		frames = append(frames, runstream.DataFrame(seq, d))
	}
	seq++
	frames = append(frames, runstream.TerminalFrame[[]byte](seq, final))
	st = &sliceStream{frames: frames, closer: closer}
	return
}

func (inst *sliceStream) Next() (f runstream.Frame[[]byte], ok bool) {
	if inst.pos >= len(inst.frames) {
		return
	}
	f = inst.frames[inst.pos]
	inst.pos++
	ok = true
	return
}

func (inst *sliceStream) Err() (err error) {
	return
}

func (inst *sliceStream) Close() (err error) {
	if inst.closed {
		return
	}
	inst.closed = true
	if inst.closer != nil {
		err = inst.closer.Close()
	}
	return
}

// readerStream chunks a body as the consumer pulls it, then ends with a
// terminal frame.
type readerStream struct {
	r         io.Reader
	closer    io.Closer
	final     runstream.Terminal
	chunkSize int

	seq        runstream.Seq
	err        error
	terminated bool
	closed     bool
}

var _ StreamI = (*readerStream)(nil)

// NewReaderStream returns a stream that reads r one chunk per frame and
// then yields final.
//
// A read failure part-way does NOT silently shorten the result: the stream
// yields a failed terminal naming the cause and reports it from Err. That
// is a stronger statement than the contract's fallback — a consumer whose
// producer dies outright still sees no terminal at all, and reads that as
// incomplete — and it is available because this adapter is alive to make
// it.
//
// closer may be nil; chunkSize defaults to [DefaultChunkSize]. Each frame's
// bytes are freshly allocated and belong to the consumer.
func NewReaderStream(r io.Reader, final runstream.Terminal, closer io.Closer, chunkSize int) (st StreamI) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	st = &readerStream{r: r, closer: closer, final: final, chunkSize: chunkSize}
	return
}

func (inst *readerStream) Next() (f runstream.Frame[[]byte], ok bool) {
	if inst.terminated {
		return
	}
	if inst.r != nil {
		buf := make([]byte, inst.chunkSize)
		// ReadFull rather than Read: a body arriving in dribs would
		// otherwise become one frame per drib, and the framing would say
		// something about the network that it does not mean.
		n, rerr := io.ReadFull(inst.r, buf)
		if n > 0 {
			inst.seq++
			f = runstream.DataFrame(inst.seq, buf[:n])
			ok = true
			if rerr != nil {
				// Short read: the body ended (or broke) inside this chunk.
				// Deliver the bytes now and settle the outcome next call.
				inst.r = nil
				inst.recordReadErr(rerr)
			}
			return
		}
		inst.r = nil
		inst.recordReadErr(rerr)
	}
	inst.seq++
	inst.terminated = true
	f = runstream.TerminalFrame[[]byte](inst.seq, inst.final)
	ok = true
	return
}

// recordReadErr folds a read outcome into the terminal. EOF is how a body
// ends, not a failure; anything else replaces the caller's terminal, since
// a result whose transfer broke is not the result the caller was promised.
func (inst *readerStream) recordReadErr(rerr error) {
	if rerr == nil || rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
		return
	}
	inst.err = eh.Errorf("queryengine: read result body: %w", rerr)
	inst.final = runstream.Failed(inst.err)
}

func (inst *readerStream) Err() (err error) {
	err = inst.err
	return
}

func (inst *readerStream) Close() (err error) {
	if inst.closed {
		return
	}
	inst.closed = true
	inst.r = nil
	if inst.closer != nil {
		err = inst.closer.Close()
	}
	return
}

// Collect drains st into a single body and reports how the run ended.
//
// It is the convenience for the many consumers that want the whole result
// — an HTTP relay, a snapshot, a test — and it is deliberately the only
// thing in this package that buffers. term is authoritative: a stream that
// ended without a terminal frame returns [runstream.ErrIncomplete] and a
// body that is a PREFIX, not an answer.
func Collect(st StreamI) (body []byte, term runstream.Terminal, err error) {
	var col runstream.Collector[[]byte]
	for {
		f, ok := st.Next()
		if !ok {
			break
		}
		err = col.Push(f)
		if err != nil {
			return
		}
	}
	term, err = col.Terminal()
	chunks := col.Data()
	if len(chunks) == 1 {
		// The buffered engines' common case: one frame, no copy.
		body = chunks[0]
		return
	}
	n := 0
	for _, c := range chunks {
		n += len(c)
	}
	body = make([]byte, 0, n)
	for _, c := range chunks {
		body = append(body, c...)
	}
	return
}
