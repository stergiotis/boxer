package chunked

import (
	"time"

	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Reassembler is the consumer side of the chunk protocol (ADR-0252 §SD4): it
// takes the chunks one [ChunkedWriterI] emitted for many ids, in any order,
// with duplicates, and yields each id's bytes once its chunk set is whole.
//
// A set is whole when its last chunk has arrived, so the count is known, and
// every index below the count is present. What is held is bounded twice:
// MaxBytes across every open set, and MaxAge per set from its first chunk;
// [Reassembler.Expire] returns the ids whose sets outlived the age so a caller
// can report them, and releases their bytes. A chunk redelivered after its
// set completed is dropped, not re-opened: the last MaxRecent completed ids
// are remembered for that. The zero value is usable and unbounded.
type Reassembler struct {
	// MaxBytes bounds the bytes held across every open set; zero is unbounded.
	MaxBytes int64
	// MaxAge bounds how long a set may stay open; zero is unbounded.
	MaxAge time.Duration
	// MaxRecent bounds how many completed ids are remembered so a late
	// duplicate is dropped; zero takes DefaultMaxRecent.
	MaxRecent int
	held      int64
	open      map[identifier.TaggedId]*openSet
	recent    map[identifier.TaggedId]struct{}
	ring      []identifier.TaggedId
	ringNext  int
	now       func() time.Time
}

// DefaultMaxRecent is how many completed ids are remembered when MaxRecent
// is zero.
const DefaultMaxRecent = 4096

type openSet struct {
	chunks   map[uint32][]byte
	total    uint32 // zero until the last chunk arrived
	bytes    int64
	openedAt time.Time
}

// ErrReassemblyFull is wrapped when a chunk would push the held bytes past
// MaxBytes; the chunk is not kept and its set stays open.
var ErrReassemblyFull = eh.Errorf("reassembler holds its byte bound")

// ErrChunkDisagrees is wrapped when a chunk repeats an index with different
// bytes, or claims a total another chunk of the set contradicts.
var ErrChunkDisagrees = eh.Errorf("chunk disagrees with the set it joins")

func (inst *Reassembler) clock() time.Time {
	if inst.now != nil {
		return inst.now()
	}
	return time.Now()
}

// Add records one chunk. index counts from zero; last marks the final chunk
// and total is then the set's chunk count, which last-and-only chunks give
// as one. When the chunk completes its set, done is true and data is the
// set's bytes in index order, and the set is released.
func (inst *Reassembler) Add(id identifier.TaggedId, index uint32, last bool, total uint32, chunk []byte) (data []byte, done bool, err error) {
	if last && total == 0 {
		err = eb.Build().Uint64("id", id.Value()).Errorf("a last chunk must carry the set's total")
		return
	}
	if last && index != total-1 {
		err = eb.Build().Uint64("id", id.Value()).Uint32("index", index).Uint32("total", total).Errorf("last chunk's index is not total-1: %w", ErrChunkDisagrees)
		return
	}
	if _, completed := inst.recent[id]; completed {
		return // a chunk of a set already yielded: a redelivery, dropped
	}
	if inst.open == nil {
		inst.open = make(map[identifier.TaggedId]*openSet, 16)
	}
	set := inst.open[id]
	if set == nil {
		set = &openSet{chunks: make(map[uint32][]byte, 8), openedAt: inst.clock()}
		inst.open[id] = set
	}
	if prev, dup := set.chunks[index]; dup {
		if string(prev) != string(chunk) {
			err = eb.Build().Uint64("id", id.Value()).Uint32("index", index).Errorf("repeated index with other bytes: %w", ErrChunkDisagrees)
		}
		return // a faithful duplicate is a redelivery; nothing changes
	}
	if last {
		if set.total != 0 && set.total != total {
			err = eb.Build().Uint64("id", id.Value()).Uint32("total", total).Uint32("known", set.total).Errorf("total: %w", ErrChunkDisagrees)
			return
		}
		set.total = total
	}
	if set.total != 0 && index >= set.total {
		err = eb.Build().Uint64("id", id.Value()).Uint32("index", index).Uint32("total", set.total).Errorf("index past the total: %w", ErrChunkDisagrees)
		return
	}
	if inst.MaxBytes > 0 && inst.held+int64(len(chunk)) > inst.MaxBytes {
		err = eb.Build().Uint64("id", id.Value()).Int64("held", inst.held).Int64("max", inst.MaxBytes).Errorf("add chunk: %w", ErrReassemblyFull)
		return
	}
	kept := make([]byte, len(chunk))
	copy(kept, chunk)
	set.chunks[index] = kept
	set.bytes += int64(len(kept))
	inst.held += int64(len(kept))

	if set.total == 0 || uint32(len(set.chunks)) < set.total {
		return
	}
	data = make([]byte, 0, set.bytes)
	for i := uint32(0); i < set.total; i++ {
		data = append(data, set.chunks[i]...)
	}
	inst.release(id, set)
	inst.remember(id)
	return data, true, nil
}

// remember records a completed id in a ring of MaxRecent, evicting the
// oldest.
func (inst *Reassembler) remember(id identifier.TaggedId) {
	capacity := inst.MaxRecent
	if capacity <= 0 {
		capacity = DefaultMaxRecent
	}
	if inst.recent == nil {
		inst.recent = make(map[identifier.TaggedId]struct{}, capacity)
		inst.ring = make([]identifier.TaggedId, capacity)
	}
	old := inst.ring[inst.ringNext]
	if old != 0 {
		delete(inst.recent, old)
	}
	inst.ring[inst.ringNext] = id
	inst.ringNext = (inst.ringNext + 1) % capacity
	inst.recent[id] = struct{}{}
}

// Expire releases every set older than MaxAge and returns their ids, in no
// particular order. With no MaxAge it releases nothing.
func (inst *Reassembler) Expire() (expired []identifier.TaggedId) {
	if inst.MaxAge <= 0 || len(inst.open) == 0 {
		return
	}
	cutoff := inst.clock().Add(-inst.MaxAge)
	for id, set := range inst.open {
		if set.openedAt.Before(cutoff) {
			expired = append(expired, id)
			inst.release(id, set)
		}
	}
	return
}

// Open is the number of sets waiting for chunks.
func (inst *Reassembler) Open() int { return len(inst.open) }

// Held is the bytes waiting across every open set.
func (inst *Reassembler) Held() int64 { return inst.held }

// Drop releases one set without yielding it.
func (inst *Reassembler) Drop(id identifier.TaggedId) {
	set := inst.open[id]
	if set != nil {
		inst.release(id, set)
	}
}

func (inst *Reassembler) release(id identifier.TaggedId, set *openSet) {
	inst.held -= set.bytes
	delete(inst.open, id)
}

// Collector adapts a [Reassembler] to [ChunkedWriterI], so a Chunker can be
// wired straight into it; what completes is handed to OnComplete. It is the
// round-trip both sides are tested against.
type Collector struct {
	Reassembler *Reassembler
	OnComplete  func(id identifier.TaggedId, data []byte) error
}

var _ ChunkedWriterI = (*Collector)(nil)

func (inst *Collector) add(id identifier.TaggedId, index uint32, last bool, total uint32, p []byte) (n int, err error) {
	data, done, err := inst.Reassembler.Add(id, index, last, total, p)
	if err != nil {
		return 0, err
	}
	if done && inst.OnComplete != nil {
		err = inst.OnComplete(id, data)
		if err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// WriteFirstChunk implements ChunkedWriterI.
func (inst *Collector) WriteFirstChunk(id identifier.TaggedId, p []byte) (int, error) {
	return inst.add(id, 0, false, 0, p)
}

// WriteIntermediateChunk implements ChunkedWriterI.
func (inst *Collector) WriteIntermediateChunk(id identifier.TaggedId, p []byte, chunkIndex uint32, _ int64) (int, error) {
	return inst.add(id, chunkIndex, false, 0, p)
}

// WriteLastChunk implements ChunkedWriterI.
func (inst *Collector) WriteLastChunk(id identifier.TaggedId, p []byte, totalChunks uint32, _ int64) (int, error) {
	return inst.add(id, totalChunks-1, true, totalChunks, p)
}

// WriteFirstAndLastChunk implements ChunkedWriterI.
func (inst *Collector) WriteFirstAndLastChunk(id identifier.TaggedId, p []byte) (int, error) {
	return inst.add(id, 0, true, 1, p)
}

// SetClockForTest replaces the clock Expire and Add read. Tests only.
func (inst *Reassembler) SetClockForTest(now func() time.Time) { inst.now = now }
