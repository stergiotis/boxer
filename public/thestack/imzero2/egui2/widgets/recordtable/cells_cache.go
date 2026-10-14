package recordtable

import (
	"hash/fnv"

	"github.com/rs/zerolog/log"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// cellsKey names everything a grid's cell bytes depend on. Two frames with
// equal keys emit byte-identical cells, so the second may splice the first's
// capture (EndETableFluid.SendWithRawCells) instead of walking the page again.
// Anything a cell reads that is not listed here is a bug in this list, not in
// the cache. What the grid cannot see — the record, the order, whatever a
// Column.Cell reads — is the host's Input.CellsKey, carried as content.
type cellsKey struct {
	content    any
	pageStart  int64
	pageN      int64
	rowLo      int64
	rowHi      int64
	selected   int64
	refit      bool
	pad        float32
	sortActive bool
	sortCol    int
	sortDesc   bool
	// columns folds the column identities, their Arrow indices and the
	// host's per-position visibility.
	columns uint64
}

// cellsCache keeps the last capture of a grid's cells and the wire ids of the
// buttons inside it. The profile that motivated it, in play's Table pane: on a
// handheld the cells were 1.6 ms of a 2.9 ms Go frame while nothing on the
// page changed (keelson-wasm-frame-cost trial, M1), and under wasm that cost
// is multiplied by about four.
type cellsCache struct {
	key   cellsKey
	raw   []byte
	ids   map[uint64]struct{}
	valid bool
	// collect is set while the live path runs, so note records the wire id
	// of every cell button it emits.
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

// cellsCacheEnabled is the switch a measurement flips; the cache is the
// shipped behaviour for a host that passes a CellsKey.
const cellsCacheEnabled = true

// retained returns the cells bytes to replay for key, or nil when the live
// path must run: the key changed, nothing is cached, or a cell has a
// response the live path would read.
func (inst *cellsCache) retained(key cellsKey, sm *c.StateManager) []byte {
	if n := inst.hits + inst.misses; n > 0 && n%300 == 0 {
		log.Debug().Uint64("hits", inst.hits).Uint64("misses", inst.misses).Msg("recordtable: cells cache")
	}
	if !cellsCacheEnabled || !inst.valid || inst.key != key {
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
func (inst *cellsCache) beginCapture(key cellsKey) {
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
func (inst *cellsCache) endCapture(raw []byte) {
	inst.raw = raw
	inst.valid = true
	inst.collect = false
}

// drop forgets the capture: a frame that does not cache must not leave one
// for a later frame whose key happens to match.
func (inst *cellsCache) drop() {
	inst.valid = false
	inst.raw = nil
	inst.collect = false
}

// note records a cell button's wire id while the live path runs.
func (inst *cellsCache) note(id uint64) {
	if inst.collect {
		inst.ids[id] = struct{}{}
	}
}

// foldColumns hashes the column layout a cells capture was made for: each
// position's identity and Arrow index, for the positions the host draws.
func foldColumns(cols []colwidth.Column, arrowIdx func(pos int) int, visible func(pos uint32) bool) uint64 {
	h := fnv.New64a()
	var b [8]byte
	put := func(v uint64) {
		for i := range b {
			b[i] = byte(v >> (8 * i))
		}
		_, _ = h.Write(b[:])
	}
	for pos, col := range cols {
		if !visible(uint32(pos)) {
			continue
		}
		put(uint64(pos)<<32 | uint64(uint32(arrowIdx(pos))))
		_, _ = h.Write([]byte(col.Name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(col.Type))
		_, _ = h.Write([]byte{1})
	}
	return h.Sum64()
}
