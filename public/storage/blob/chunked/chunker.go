package chunked

import (
	"bufio"
	"bytes"
	"io"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
)

type stateE uint8

const (
	stateChunkInitial       stateE = 0
	stateChunkFirstBuffered stateE = 1
	stateChunkIntermediate  stateE = 2
	stateChunkLast          stateE = 3
	stateChunkFirstIsLast   stateE = 4
	stateChunkFirstAndLast  stateE = 5
	stateChunkCompleted     stateE = 6
	stateChunkError         stateE = 7
)

type wrapper struct {
	cw         ChunkedWriterI
	id         identifier.TaggedId
	idGen      identifier.IdGeneratorI
	naturalKey []byte
	nextIdx    uint32
	state      stateE
	total      int64
	buf        *bytes.Buffer
	err        error
}

func (inst *wrapper) prepare(idGen identifier.IdGeneratorI, naturalKey []byte, cw ChunkedWriterI) {
	inst.cw = cw
	inst.id = 0
	inst.idGen = idGen
	inst.naturalKey = naturalKey
	inst.nextIdx = 0
	inst.state = stateChunkInitial
	inst.total = 0
	inst.buf.Reset()
	inst.err = nil
}

func (inst *wrapper) nextWriteIsLastWrite() {
	switch inst.state {
	case stateChunkInitial:
		inst.state = stateChunkFirstIsLast
	case stateChunkFirstBuffered:
		inst.state = stateChunkFirstAndLast
	case stateChunkIntermediate:
		inst.state = stateChunkLast
	default:
		log.Panic().Uint8("previous", uint8(inst.state)).Msg("can not transition to last write state")
	}
}

func (inst *wrapper) ready() bool {
	return inst.cw != nil
}

func (inst *wrapper) ensureId() (err error) {
	inst.id, _, err = inst.idGen.GetId(inst.naturalKey)
	if err != nil {
		err = eh.Errorf("unable to generate id: %w", err)
		return
	}
	return
}

// Write drives the chunk state machine. nextIdx and total track chunks actually
// emitted (not Write calls): nextIdx is the 0-based index of the next chunk to
// emit, total the cumulative bytes of chunks already emitted. Both advance only
// at an emit site, so the chunkIndex / sizeSoFar / totalChunks / totalSize
// reported to the ChunkedWriterI are exact.
func (inst *wrapper) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		switch inst.state {
		case stateChunkFirstIsLast:
			log.Debug().Uint64("id", uint64(inst.id)).Msg("suppressed completely empty chunk set")
			return
		case stateChunkFirstAndLast:
			err = inst.ensureId()
			if err != nil {
				err = eh.Errorf("unable to generate id: %w", err)
				return
			}
			n, err = inst.cw.WriteFirstAndLastChunk(inst.id, inst.buf.Bytes())
			inst.state = stateChunkCompleted
		case stateChunkLast:
			chunk := inst.buf.Bytes()
			n, err = inst.cw.WriteLastChunk(inst.id, chunk, inst.nextIdx+1, inst.total+int64(len(chunk)))
			inst.state = stateChunkCompleted
		default:
			// skipping empty chunk
			return
		}
	} else {
		switch inst.state {
		case stateChunkInitial:
			n, err = inst.buf.Write(p)
			inst.state = stateChunkFirstBuffered
		case stateChunkFirstBuffered:
			err = inst.ensureId()
			if err != nil {
				err = eh.Errorf("unable to generate id: %w", err)
				return
			}
			first := inst.buf.Bytes()
			_, err = inst.cw.WriteFirstChunk(inst.id, first)
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to flush buffered chunk: %w", err)
				inst.err = err
				return
			}
			inst.nextIdx++
			inst.total += int64(len(first))

			inst.buf.Reset() // the buffered chunk was just emitted; stage only the next one
			n, err = inst.buf.Write(p)
			inst.state = stateChunkIntermediate
		case stateChunkIntermediate:
			chunk := inst.buf.Bytes()
			sizeSoFar := inst.total + int64(len(chunk))
			_, err = inst.cw.WriteIntermediateChunk(inst.id, chunk, inst.nextIdx, sizeSoFar)
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to write intermediate chunk: %w", err)
				inst.err = err
				return
			}
			inst.nextIdx++
			inst.total = sizeSoFar

			inst.buf.Reset() // the buffered chunk was just emitted; stage only the next one
			n, err = inst.buf.Write(p)
		case stateChunkLast:
			chunk := inst.buf.Bytes()
			sizeSoFar := inst.total + int64(len(chunk))
			_, err = inst.cw.WriteIntermediateChunk(inst.id, chunk, inst.nextIdx, sizeSoFar)
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to write intermediate chunk: %w", err)
				inst.err = err
				return
			}
			inst.nextIdx++
			inst.total = sizeSoFar

			n, err = inst.cw.WriteLastChunk(inst.id, p, inst.nextIdx+1, inst.total+int64(len(p)))
			inst.state = stateChunkCompleted
		case stateChunkFirstAndLast:
			err = inst.ensureId()
			if err != nil {
				err = eh.Errorf("unable to generate id: %w", err)
				return
			}
			first := inst.buf.Bytes()
			_, err = inst.cw.WriteFirstChunk(inst.id, first)
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to write first chunk: %w", err)
				inst.err = err
				return
			}
			inst.nextIdx++
			inst.total += int64(len(first))

			n, err = inst.cw.WriteLastChunk(inst.id, p, inst.nextIdx+1, inst.total+int64(len(p)))
			inst.state = stateChunkCompleted
		case stateChunkFirstIsLast:
			err = inst.ensureId()
			if err != nil {
				err = eh.Errorf("unable to generate id: %w", err)
				return
			}
			n, err = inst.cw.WriteFirstAndLastChunk(inst.id, p)
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to write first chunk: %w", err)
				inst.err = err
				return
			}
			inst.state = stateChunkCompleted
		case stateChunkCompleted:
			err = eh.Errorf("write to completed chunk stream")
			return
		case stateChunkError:
			err = eh.Errorf("chunk writer is in erroneous state: %w", inst.err)
			return
		}
	}
	if err != nil {
		inst.state = stateChunkError
		err = eh.Errorf("unable to write chunk: %w", err)
		inst.err = err
		return
	}
	return
}

func (inst *wrapper) forceFlush() {
	switch inst.state {
	case stateChunkCompleted, stateChunkError:
		break
	default:
		_, _ = inst.Write(nil)
	}
}

var _ io.Writer = (*wrapper)(nil)

type Chunker struct {
	bw      *bufio.Writer
	wrapper *wrapper
}

func (inst *Chunker) WriteString(s string) (n int, err error) {
	if inst.wrapper.ready() {
		n, err = inst.bw.WriteString(s)
		if err != nil {
			err = eh.Errorf("unable to write string: %w", err)
			return
		}
		return
	} else {
		err = eh.Errorf("no writer available")
		return
	}
}

func NewChunker(chunkSize int) *Chunker {
	bw := bufio.NewWriterSize(nil, chunkSize)
	wra := &wrapper{buf: &bytes.Buffer{}}
	wra.prepare(nil, nil, nil)
	return &Chunker{
		bw:      bw,
		wrapper: wra,
	}
}

// Close will flush and prepare for a writer reset
func (inst *Chunker) Close() error {
	wra := inst.wrapper
	if wra.ready() {
		wra.nextWriteIsLastWrite()
		err := inst.bw.Flush()
		if err != nil {
			return eh.Errorf("unable to flush to writer: %w", err)
		}
		wra.forceFlush()
		wra.prepare(nil, nil, nil)
	}
	return nil
}

func (inst *Chunker) Write(p []byte) (n int, err error) {
	if inst.wrapper.ready() {
		n, err = inst.bw.Write(p)
		if err != nil {
			err = eh.Errorf("unable to write bytes: %w", err)
			return
		}
		return
	} else {
		err = eh.Errorf("no writer available")
		return
	}
}

func (inst *Chunker) Prepare(idGen identifier.IdGeneratorI, naturalKey []byte, chunkRecv ChunkedWriterI) (err error) {
	if inst.wrapper.ready() {
		err = inst.bw.Flush()
		if err != nil {
			err = eh.Errorf("unable to flush current writer before initializing next: %w", err)
		}
	}
	inst.wrapper.prepare(idGen, naturalKey, chunkRecv)
	inst.bw.Reset(inst.wrapper)
	return
}

var _ io.Writer = (*Chunker)(nil)

var _ io.StringWriter = (*Chunker)(nil)

var _ io.Closer = (*Chunker)(nil)
