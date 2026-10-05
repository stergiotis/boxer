package play

import "time"

// The raster memo (ADR-0096 2026-10-02 Update, from the map-tile-addressing
// trial): the lane remembers one result, so a pan back, a zoom out and in, or
// a return to an earlier view re-ran its whole ladder. The panel now keeps
// the last packed rasters it drew, keyed by the node key — the SQL and every
// param, so table, level, render, viewport and time window are all in it —
// and serves a revisit from memory: the ladder starts at the finest level it
// holds, draws it without a query, and climbs on from there if that was not
// the last level. Entries expire after mapMemoTTL, the memo is bounded by
// mapMemoBytes, and Refresh empties it, so a revisit never shows data older
// than a few minutes unless nothing asked for newer.

const (
	mapMemoBytes = 64 << 20
	mapMemoTTL   = 5 * time.Minute
)

// mapMemoEntry is one landed raster, as packed.
type mapMemoEntry struct {
	pixels  []uint32
	w, h    uint32
	bounds  [4]float64
	level   mapLevel
	readout mapReadout
	elapsed time.Duration // the run's own, for the ladder's budget
	at      time.Time
}

func (inst *mapMemoEntry) bytes() int { return 4*len(inst.pixels) + inst.readout.bytes() }

// mapMemo is a small LRU of landed rasters; render-thread only.
type mapMemo struct {
	entries map[string]*mapMemoEntry
	order   []string // least recent first
	total   int
	now     func() time.Time // nil = time.Now; tests pin it
	budget  int              // 0 = mapMemoBytes; tests shrink it
}

func (inst *mapMemo) limit() int {
	if inst.budget > 0 {
		return inst.budget
	}
	return mapMemoBytes
}

func (inst *mapMemo) clock() time.Time {
	if inst.now != nil {
		return inst.now()
	}
	return time.Now()
}

// get returns the entry for key when it is held and not expired, and marks
// it most recent.
func (inst *mapMemo) get(key string) (e *mapMemoEntry, ok bool) {
	e, ok = inst.entries[key]
	if !ok {
		return
	}
	if inst.clock().Sub(e.at) >= mapMemoTTL {
		inst.remove(key)
		return nil, false
	}
	inst.touch(key)
	return
}

// has is get without the recency update.
func (inst *mapMemo) has(key string) bool {
	e, ok := inst.entries[key]
	return ok && inst.clock().Sub(e.at) < mapMemoTTL
}

// put stores e under key and evicts the least recent entries past the
// budget. An entry larger than the whole budget is not kept.
func (inst *mapMemo) put(key string, e *mapMemoEntry) {
	if e.bytes() > inst.limit() {
		return
	}
	if inst.entries == nil {
		inst.entries = make(map[string]*mapMemoEntry)
	}
	if _, held := inst.entries[key]; held {
		inst.remove(key)
	}
	e.at = inst.clock()
	inst.entries[key] = e
	inst.order = append(inst.order, key)
	inst.total += e.bytes()
	for inst.total > inst.limit() && len(inst.order) > 0 {
		inst.remove(inst.order[0])
	}
}

func (inst *mapMemo) clear() {
	inst.entries, inst.order, inst.total = nil, nil, 0
}

func (inst *mapMemo) remove(key string) {
	e, ok := inst.entries[key]
	if !ok {
		return
	}
	delete(inst.entries, key)
	inst.total -= e.bytes()
	for i, k := range inst.order {
		if k == key {
			inst.order = append(inst.order[:i], inst.order[i+1:]...)
			break
		}
	}
}

func (inst *mapMemo) touch(key string) {
	for i, k := range inst.order {
		if k == key {
			inst.order = append(append(inst.order[:i:i], inst.order[i+1:]...), key)
			return
		}
	}
}

// jumpToMemo moves a just-restarted ladder to the finest level the memo
// holds for these params, so a revisit starts where it left off.
func (inst *MapDriver) jumpToMemo(params map[string]string) {
	for i := len(inst.ladder.levels) - 1; i > inst.ladder.level; i-- {
		lv := inst.ladder.levels[i]
		key := compiledNode{SQL: rasterTemplateSQLWith(lv.table, lv.sampling, inst.colorSQL, inst.extraWhere, inst.readoutOn, inst.readoutSQL, inst.templateDPR()), Params: params}.key()
		if inst.memo.has(key) {
			inst.ladder.level = i
			inst.rebuildLevelTemplate()
			return
		}
	}
}

// serveFromMemo draws the current level from memory when the memo holds it,
// stopping whatever the lane was still running for a view the reader left,
// and moves the ladder as a landed result would. It reports whether it
// served.
func (inst *MapDriver) serveFromMemo(key string) (served bool) {
	e, ok := inst.memo.get(key)
	if !ok {
		inst.memoShown = ""
		return false
	}
	if inst.memoShown != key {
		inst.lane.abort()
		inst.pixels, inst.packW, inst.packH, inst.packBounds = e.pixels, e.w, e.h, e.bounds
		inst.readout = e.readout
		inst.version++
		inst.memoShown = key
	}
	inst.loading, inst.laneErr, inst.packErr, inst.cancelled = false, nil, nil, false
	// No run produced this raster now: the accounting beside Refresh would be
	// the view the reader left.
	inst.stats = laneStats{}
	inst.packLevel = e.level
	if inst.ladder.served(e.elapsed) {
		inst.rebuildLevelTemplate()
	}
	return true
}

// remember keeps the raster on screen as key's landed result.
func (inst *MapDriver) remember(key string, elapsed time.Duration) {
	inst.memo.put(key, &mapMemoEntry{
		pixels: inst.pixels, w: inst.packW, h: inst.packH, bounds: inst.packBounds,
		level: inst.ladder.current(), readout: inst.readout, elapsed: elapsed,
	})
}
