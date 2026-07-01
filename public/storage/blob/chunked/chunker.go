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
		break
	case stateChunkFirstBuffered:
		inst.state = stateChunkFirstAndLast
		break
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

func (inst *wrapper) Write(p []byte) (n int, err error) {
	if p == nil || len(p) == 0 {
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
			p = inst.buf.Bytes()
			n, err = inst.cw.WriteFirstAndLastChunk(inst.id, p)
			inst.state = stateChunkCompleted
			break
		case stateChunkLast:
			p = inst.buf.Bytes()
			n, err = inst.cw.WriteLastChunk(inst.id, p, inst.nextIdx+1, inst.total+int64(len(p)))
			inst.state = stateChunkCompleted
			break
		default:
			// skipping empty chunk
			return
		}
	} else {
		switch inst.state {
		case stateChunkInitial:
			n, err = inst.buf.Write(p)
			inst.state = stateChunkFirstBuffered
			break
		case stateChunkFirstBuffered:
			err = inst.ensureId()
			if err != nil {
				err = eh.Errorf("unable to generate id: %w", err)
				return
			}
			_, err = inst.cw.WriteFirstChunk(inst.id, inst.buf.Bytes())
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to flush buffered chunk: %w", err)
				inst.err = err
				return
			}

			inst.buf.Reset() // the buffered chunk was just emitted; stage only the next one
			n, err = inst.buf.Write(p)
			inst.state = stateChunkIntermediate
			break
		case stateChunkIntermediate:
			n, err = inst.cw.WriteIntermediateChunk(inst.id, inst.buf.Bytes(), inst.nextIdx, inst.total)
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to write intermediate chunk: %w", err)
				inst.err = err
				return
			}

			inst.buf.Reset() // the buffered chunk was just emitted; stage only the next one
			n, err = inst.buf.Write(p)
			break
		case stateChunkLast:
			n, err = inst.cw.WriteIntermediateChunk(inst.id, inst.buf.Bytes(), inst.nextIdx, inst.total)
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to write intermediate chunk: %w", err)
				inst.err = err
				return
			}

			n, err = inst.cw.WriteLastChunk(inst.id, p, inst.nextIdx+1, inst.total+int64(len(p)))
			inst.state = stateChunkCompleted
			break
		case stateChunkFirstAndLast:
			err = inst.ensureId()
			if err != nil {
				err = eh.Errorf("unable to generate id: %w", err)
				return
			}
			n, err = inst.cw.WriteFirstChunk(inst.id, inst.buf.Bytes())
			if err != nil {
				inst.state = stateChunkError
				err = eh.Errorf("unable to write first chunk: %w", err)
				inst.err = err
				return
			}

			n, err = inst.cw.WriteLastChunk(inst.id, p, inst.nextIdx+1, inst.total+int64(len(p)))
			inst.state = stateChunkCompleted
			break
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
			break
		case stateChunkCompleted:
			err = eh.Errorf("write to completed chunk stream")
			return
		case stateChunkError:
			err = eh.Errorf("chunk writer is in errneous state: %w", inst.err)
			return
		}
	}
	inst.total += int64(n)
	inst.nextIdx++
	if err != nil {
		inst.state = stateChunkError
		err = eh.Errorf("unable to write intermediate chunk: %w", err)
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
