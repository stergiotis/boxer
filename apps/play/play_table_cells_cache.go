package play

import (
	"hash/fnv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog/log"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// masterCellsKey names everything the master table's cell bytes depend on.
// Two frames with equal keys emit byte-identical cells, so the second may
// splice the first's capture (EndETableFluid.SendWithRawCells) instead of
// walking the page again. Anything a cell reads that is not listed here is
// a bug in this list, not in the cache.
type masterCellsKey struct {
	result       ResultID
	schema       *arrow.Schema
	pageStart    int64
	pageEnd      int64
	rowLo        uint64
	rowHi        uint64
	selectedRow  int64
	refit        bool
	cellPadX     float32
	sortActive   bool
	sortCol      int
	sortDesc     bool
	identityDone bool
	opts         tableDisplayOpts
	// columns folds the visible Arrow columns and the synthetic ones, with
	// their positions and the host's per-position visibility.
	columns uint64
}

// masterCellsCache keeps the last capture of the master table's cells and
// the wire ids of the buttons inside it. The profile that motivated it: on a
// handheld the cells were 1.6 ms of a 2.9 ms Go frame while nothing on the
// page changed (keelson-wasm-frame-cost trial, M1), and under wasm that
// cost is multiplied by about four.
type masterCellsCache struct {
	key   masterCellsKey
	raw   []byte
	ids   map[uint64]struct{}
	valid bool
	// collect is set while the live path runs, so selectableCell records
	// the wire id of every cell button it emits.
	collect bool
	hits    uint64
	misses  uint64
}

// cellInteractionFlags are the responses a cell's live path acts on or that
// change how the host draws the cell. A frame in which any cell carries one
// of these takes the live path, which reads the response; Enabled,
// Highlighter and ClickedElsewhere are states every widget reports and
// no cell reads.
const cellInteractionFlags = c.PrimaryClickedResponseFlags | c.SecondaryClickedResponseFlags |
	c.LongTouchedResponseFlags | c.MiddleClickedResponseFlags | c.DoubleClickedResponseFlags |
	c.TripleClickedResponseFlags | c.HoveredResponseFlags | c.ContainsPointerResponseFlags |
	c.HasFocusResponseFlags | c.GainedFocusResponseFlags | c.LostFocusResponseFlags |
	c.DragStartedResponseFlags | c.DraggedResponseFlags | c.DragStoppedResponseFlags |
	c.IsPointerButtonDownResponseFlags | c.ChangedResponseFlags

// masterCellsCacheEnabled is the switch a measurement flips; the cache is
// the shipped behaviour.
const masterCellsCacheEnabled = true

// retained returns the cells bytes to replay for key, or nil when the live
// path must run: the key changed, nothing is cached, or a cell has a
// response the live path would read.
func (inst *masterCellsCache) retained(key masterCellsKey, sm *c.StateManager) []byte {
	if n := inst.hits + inst.misses; n > 0 && n%300 == 0 {
		log.Debug().Uint64("hits", inst.hits).Uint64("misses", inst.misses).Msg("play: master table cells cache")
	}
	if !masterCellsCacheEnabled || !inst.valid || inst.key != key {
		inst.misses++
		return nil
	}
	if sm.ResponseFlagsAny(cellInteractionFlags, func(id uint64) bool {
		_, ok := inst.ids[id]
		return ok
	}) {
		inst.misses++
		return nil
	}
	inst.hits++
	return inst.raw
}

// beginCapture starts recording the live path's cell ids for key.
func (inst *masterCellsCache) beginCapture(key masterCellsKey) {
	inst.key = key
	inst.valid = false
	inst.raw = nil
	if inst.ids == nil {
		inst.ids = make(map[uint64]struct{}, 4096)
	} else {
		clear(inst.ids)
	}
	inst.collect = true
}

// endCapture stores the bytes the live path produced.
func (inst *masterCellsCache) endCapture(raw []byte) {
	inst.raw = raw
	inst.valid = true
	inst.collect = false
}

// note records a cell button's wire id while the live path runs.
func (inst *masterCellsCache) note(id uint64) {
	if inst.collect {
		inst.ids[id] = struct{}{}
	}
}

// foldColumns hashes the column layout a cells capture was made for.
func foldColumns(visCols []int, synth []int, visible func(pos uint32) bool) uint64 {
	h := fnv.New64a()
	var b [8]byte
	put := func(v uint64) {
		for i := range b {
			b[i] = byte(v >> (8 * i))
		}
		_, _ = h.Write(b[:])
	}
	if visible(0) {
		put(1)
	}
	for pos, col := range visCols {
		if visible(uint32(pos + 1)) {
			put(uint64(pos+1)<<32 | uint64(col))
		}
	}
	for k, sentinel := range synth {
		pos := uint32(len(visCols) + 1 + k)
		if visible(pos) {
			put(uint64(pos)<<32 | uint64(sentinel))
		}
	}
	return h.Sum64()
}
