package colwidth

import (
	"slices"
	"sort"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// DefaultDebounce is how long a captured width is held before it is
// written. A drag emits a new width every frame it moves, so writing on
// each observation would put a row per frame into the facts table; waiting
// for the motion to stop collapses one drag into one row.
const DefaultDebounce = 700 * time.Millisecond

// DefaultMaxEntries bounds the in-memory override set. It is not a
// retention policy: rows already written stay written, and pruning the
// durable trail is a retention question over the facts table (the ADR's
// Update moved it there when the storage stopped being one document). The
// cap only stops a very long-lived process from growing its working set
// without limit.
const DefaultMaxEntries = 512

// Opts configures a Resolver. Only AppId is required.
type Opts struct {
	// AppId scopes every read and write. Overrides never cross apps.
	AppId app.AppIdT
	// Debounce overrides DefaultDebounce. Negative is rejected; zero
	// takes the default.
	Debounce time.Duration
	// MaxEntries overrides DefaultMaxEntries.
	MaxEntries int
	// MinPoints and MaxPoints clamp both resolved and captured widths.
	// A zero MaxPoints means unbounded. They exist so a corrupt or absurd
	// stored value cannot render a table unusable — the user would have no
	// way to drag a 100000pt column back.
	MinPoints float64
	MaxPoints float64
}

// entry is one override held in memory.
type entry struct {
	points    float64
	fontSize  float64
	updatedAt time.Time
	// dirty marks an entry captured from a drag and not yet written.
	dirty bool
	// changedAt is when the pending capture last moved; the debounce is
	// measured from it.
	changedAt time.Time
}

// reseedSettleFrames is how many width reports following a re-seed are
// read as the crate's own doing rather than the user's.
//
// Two, and the two are different reports. The read-back lags a frame, so
// the first one to arrive after Resolve bumps the epoch was produced
// *before* the seed landed and describes the columns as they were — match
// it up positionally against the new ones and a column acquires whatever
// its predecessor in that slot was wearing. The second is the seeded
// frame's own result, which is the baseline capture detection needs. Only
// from the third does a difference mean a person moved something.
const reseedSettleFrames = 2

// tableState is the per-table apply bookkeeping.
type tableState struct {
	epoch uint32
	cols  map[string]colState
	// order is the column keys as Resolve last returned them. The wire is
	// positional — the EtColumn sequence, the seed, and the read-back all
	// go by slot — so a reordering changes what every slot means even when
	// the set and the widths are identical, and has to bump the epoch like
	// any other change.
	order []string
	// settle counts the reports still owed to the last re-seed.
	settle int
}

// colState separates the two widths a column carries between frames. They
// start equal and are only ever told apart by the crate: `sent` is what Go
// handed the binding, `settled` what the binding reported back.
//
// Keeping one value for both roles conflates "my resolved widths changed"
// with "the crate ended up somewhere else". A table whose crate-side width
// legitimately differs from the resolved one — the layout grew a column to
// fit its content, say — would then bump its epoch every frame and re-seed
// the crate against its own layout, which is the per-frame re-assertion the
// ADR's Q4 rejects.
type colState struct {
	// sent is the width Resolve last returned for this column. The epoch
	// bumps when it changes, and only then.
	sent float64
	// settled is the width the crate last reported. Capture detection
	// compares a fetched width against this — equal means the crate is
	// echoing its own state back, not reporting a drag.
	settled float64
}

// Resolver resolves and captures column widths for one app.
//
// It is not safe for concurrent use: it is designed to be owned by a
// single app instance and driven from its render loop, the same
// single-threaded discipline every other imzero2 widget state follows.
type Resolver struct {
	store  StoreI
	opts   Opts
	byKey  map[factsstore.ColumnWidthKey]*entry
	tables map[string]*tableState
}

// New constructs a Resolver. It does not read the store — call Load once
// storage is available, which for an app is after Mount rather than at
// construction.
func New(store StoreI, opts Opts) (inst *Resolver, err error) {
	if store == nil {
		err = eh.Errorf("colwidth: New: nil store")
		return
	}
	if opts.AppId == "" {
		err = eh.Errorf("colwidth: New: empty AppId")
		return
	}
	if opts.Debounce < 0 {
		err = eh.Errorf("colwidth: New: negative debounce %s", opts.Debounce)
		return
	}
	if opts.Debounce == 0 {
		opts.Debounce = DefaultDebounce
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = DefaultMaxEntries
	}
	if opts.MaxPoints != 0 && opts.MaxPoints < opts.MinPoints {
		err = eh.Errorf("colwidth: New: MaxPoints %v below MinPoints %v", opts.MaxPoints, opts.MinPoints)
		return
	}
	inst = &Resolver{
		store:  store,
		opts:   opts,
		byKey:  make(map[factsstore.ColumnWidthKey]*entry, 32),
		tables: make(map[string]*tableState, 8),
	}
	return
}

// Load reads the app's override set. Pending unwritten captures are kept:
// a Load racing a drag must not silently discard the user's adjustment.
func (inst *Resolver) Load() (err error) {
	rows, err := inst.store.ListColumnWidths(inst.opts.AppId)
	if err != nil {
		err = eh.Errorf("colwidth: load: %w", err)
		return
	}
	for _, row := range rows {
		k := row.Key()
		if e, ok := inst.byKey[k]; ok && e.dirty {
			continue
		}
		inst.byKey[k] = &entry{
			points:    row.Points,
			fontSize:  row.FontSize,
			updatedAt: row.Ts,
		}
	}
	inst.evict()
	return
}

// Resolve returns one width per column: the most specific override that
// matches, else the caller's default. defaults may be nil or shorter than
// cols, in which case the missing entries resolve to 0 — the call site's
// signal to let the crate autofit.
//
// Resolve also records what it returned as the applied width for the
// table and bumps the table's epoch when that differs from last time, so
// the binding knows a re-apply is due. Calling it repeatedly with an
// unchanged result is cheap and does not bump the epoch.
func (inst *Resolver) Resolve(tableTag string, cols []Column, fontSize float64, defaults []float64) (widths []float64) {
	widths = make([]float64, len(cols))
	shape := ShapeHash(cols)
	st := inst.tableFor(tableTag)
	changed := false
	for i, c := range cols {
		key := c.Key()
		w, ok := inst.lookup(tableTag, shape, key, fontSize)
		if !ok {
			if i < len(defaults) {
				w = defaults[i]
			}
		}
		w = inst.clamp(w)
		widths[i] = w
		if prev, seen := st.cols[key]; !seen || prev.sent != w {
			changed = true
		}
	}
	// Rebuild rather than mutate: a column removed from the table should
	// drop out, or a later table with the same tag and fewer columns would
	// compare against a stale entry.
	//
	// A column whose sent width is unchanged keeps whatever the crate had
	// settled on; one that moved has its settled width reset to the new
	// value, because the epoch bump below is about to seed the crate with
	// exactly that and anything it settled on before is gone.
	next := make(map[string]colState, len(cols))
	order := make([]string, len(cols))
	for i, c := range cols {
		key := c.Key()
		order[i] = key
		cs := colState{sent: widths[i], settled: widths[i]}
		if prev, seen := st.cols[key]; seen && prev.sent == widths[i] {
			cs.settled = prev.settled
		}
		next[key] = cs
	}
	// The order comparison also covers a column added or removed, and one
	// case a map-size comparison would miss: a table showing the same
	// column twice has fewer map entries than slots.
	if !slices.Equal(order, st.order) {
		changed = true
	}
	st.cols = next
	st.order = order
	if changed {
		st.epoch++
		// A re-seed is on its way to the crate; the next reports describe
		// what came before it. See reseedSettleFrames.
		st.settle = reseedSettleFrames
	}
	return
}

// lookup walks the tiers most-specific-first.
func (inst *Resolver) lookup(tableTag, shape, columnKey string, fontSize float64) (points float64, ok bool) {
	for _, k := range []factsstore.ColumnWidthKey{
		{Tier: factsstore.ColWidthTierInstance, Scope: tableTag, ColumnKey: columnKey},
		{Tier: factsstore.ColWidthTierShape, Scope: shape, ColumnKey: columnKey},
		{Tier: factsstore.ColWidthTierColumn, ColumnKey: columnKey},
	} {
		e, found := inst.byKey[k]
		if !found {
			continue
		}
		points = rescale(e.points, e.fontSize, fontSize)
		ok = true
		return
	}
	return
}

// rescale adjusts a stored width for a font-size change. A width captured
// at one font size is wrong at another, and scaling proportionally is a
// better guess than either ignoring the change or discarding the override.
// A zero captured size means "no font reference"; nothing is scaled then.
func rescale(points, capturedAt, current float64) (out float64) {
	out = points
	if capturedAt <= 0 || current <= 0 || capturedAt == current {
		return
	}
	out = points * current / capturedAt
	return
}

// Epoch is the table's apply generation. The binding writes Go's widths
// into the crate's state only when this changes; between bumps the crate's
// own state — the user's live drag — wins.
func (inst *Resolver) Epoch(tableTag string) (epoch uint32) {
	epoch = inst.tableFor(tableTag).epoch
	return
}

// Observe reports the widths the binding read back after a frame.
//
// A fetched width that differs from what the crate last settled on for that
// column is a user adjustment, and is captured as an override on the
// instance and column tiers (§SD1). Two things are deliberately not
// captures: the first-show frame, where the crate force-autofits and the
// widths are its idea rather than the user's, and a value equal to the
// settled one, which is the crate echoing itself back.
//
// A capture updates the sent width too, without bumping the epoch. That is
// the echo suppression the ADR calls for: the crate already holds the
// width, so re-applying it would fight the very drag that produced it.
//
// firstShow *adopts* rather than ignores, and this is the whole of its
// effect. Skipping the frame outright left the settled width holding the
// default the call site supplied, so the very widths this is meant to
// disown read as a change on the next frame and were captured then — a
// one-frame delay, not a suppression, and enough for every column of a
// table nobody touched to acquire a durable override. Taking them as the
// baseline instead means only what moves *after* the crate settled counts
// as the user's. Since Resolve now opens the same window itself whenever
// it re-seeds, a call site that cannot tell a first show from any other
// frame may pass false throughout.
//
// A report is matched to cols by position, so one whose length differs is
// not about these columns at all — it was produced under a different set,
// or arrived truncated — and is dropped rather than lined up anyway.
func (inst *Resolver) Observe(tableTag string, cols []Column, fetched []float64, fontSize float64, firstShow bool, now time.Time) {
	st := inst.tableFor(tableTag)
	if len(fetched) != len(cols) {
		// Deliberately without spending a settle frame: the report we are
		// waiting for has not arrived yet, and this is evidence of that
		// rather than the thing itself.
		return
	}
	adopt := firstShow || st.settle > 0
	if st.settle > 0 {
		st.settle--
	}
	for i := range cols {
		key := cols[i].Key()
		cs, seen := st.cols[key]
		if !seen {
			continue
		}
		w := inst.clamp(fetched[i])
		if adopt {
			cs.settled = w
			st.cols[key] = cs
			continue
		}
		if w == cs.settled {
			continue
		}
		inst.capture(tableTag, key, w, fontSize, now)
		cs.sent, cs.settled = w, w
		st.cols[key] = cs
	}
}

// capture records a pending override on the instance and column tiers. The
// shape tier is read-only in v1 (§SD1): it exists so a table under a new
// tag can inherit widths, and writing it from a single table's drag would
// claim more than the user expressed.
func (inst *Resolver) capture(tableTag, columnKey string, points, fontSize float64, now time.Time) {
	for _, k := range []factsstore.ColumnWidthKey{
		{Tier: factsstore.ColWidthTierInstance, Scope: tableTag, ColumnKey: columnKey},
		{Tier: factsstore.ColWidthTierColumn, ColumnKey: columnKey},
	} {
		e, ok := inst.byKey[k]
		if !ok {
			e = &entry{}
			inst.byKey[k] = e
		}
		e.points = points
		e.fontSize = fontSize
		e.updatedAt = now
		e.changedAt = now
		e.dirty = true
	}
	inst.evict()
}

// Flush writes captures whose motion stopped at least Debounce ago. It is
// safe to call every frame; entries still moving are left pending.
//
// A write failure leaves the entry dirty so the next Flush retries: losing
// a width the user set because one insert failed is worse than writing a
// second row for it.
func (inst *Resolver) Flush(now time.Time) (written int, err error) {
	for k, e := range inst.byKey {
		if !e.dirty || now.Sub(e.changedAt) < inst.opts.Debounce {
			continue
		}
		_, werr := inst.store.WriteColumnWidth(factsstore.ColumnWidthRow{
			AppId:     inst.opts.AppId,
			Tier:      k.Tier,
			Scope:     k.Scope,
			ColumnKey: k.ColumnKey,
			Points:    e.points,
			FontSize:  e.fontSize,
			Ts:        now,
		})
		if werr != nil {
			// Report the first failure and stop; the rest stay dirty and
			// are retried, so nothing is dropped on the floor.
			err = eh.Errorf("colwidth: flush %s/%s: %w", k.Tier, k.ColumnKey, werr)
			return
		}
		e.dirty = false
		written++
	}
	return
}

// Clear removes the instance- and column-tier overrides for one column and
// returns it to defaults (§SD5's "clear override" affordance). The next
// Resolve for the table sees different widths and bumps its epoch, so the
// crate is re-seeded without the caller doing anything.
//
// The pending capture is dropped too: clearing an override that a drag has
// just set, but that has not been flushed yet, must not have the drag
// write it back a moment later.
func (inst *Resolver) Clear(tableTag string, col Column) (err error) {
	key := col.Key()
	for _, k := range []factsstore.ColumnWidthKey{
		{Tier: factsstore.ColWidthTierInstance, Scope: tableTag, ColumnKey: key},
		{Tier: factsstore.ColWidthTierColumn, ColumnKey: key},
	} {
		delete(inst.byKey, k)
		if derr := inst.store.DeleteColumnWidth(inst.opts.AppId, k.Tier, k.Scope, k.ColumnKey); derr != nil {
			err = eh.Errorf("colwidth: clear %s/%s: %w", k.Tier, k.ColumnKey, derr)
			return
		}
	}
	// Drop the table's per-column state so the next Resolve reports a
	// change and re-seeds the crate. The recorded order goes with it:
	// leaving it behind would describe a set of widths that no longer
	// exists, and the next Resolve would compare against a shape it never
	// actually sent.
	if st, ok := inst.tables[tableTag]; ok {
		st.cols = map[string]colState{}
		st.order = nil
	}
	return
}

// ClearAll returns every column of one table to defaults — the
// "reset this table's widths" gesture that pairs with per-column Clear.
//
// Unlike Clear it does not stop at the first failure. A partial clear is
// the worst outcome for this gesture: the user asked for a uniform table
// and would get some columns reset and others not, with no way to tell
// which. So every column is attempted and the first error is returned
// once all of them have been.
func (inst *Resolver) ClearAll(tableTag string, cols []Column) (err error) {
	for _, col := range cols {
		if cerr := inst.Clear(tableTag, col); cerr != nil && err == nil {
			err = cerr
		}
	}
	return
}

// PendingCount reports how many captures are waiting to be written. Tests
// and diagnostics use it; the render path does not.
func (inst *Resolver) PendingCount() (n int) {
	for _, e := range inst.byKey {
		if e.dirty {
			n++
		}
	}
	return
}

// Len reports the number of overrides held in memory.
func (inst *Resolver) Len() (n int) {
	n = len(inst.byKey)
	return
}

func (inst *Resolver) tableFor(tableTag string) (st *tableState) {
	st, ok := inst.tables[tableTag]
	if !ok {
		st = &tableState{cols: map[string]colState{}}
		inst.tables[tableTag] = st
	}
	return
}

func (inst *Resolver) clamp(w float64) (out float64) {
	out = w
	if out < inst.opts.MinPoints {
		out = inst.opts.MinPoints
	}
	if inst.opts.MaxPoints > 0 && out > inst.opts.MaxPoints {
		out = inst.opts.MaxPoints
	}
	return
}

// evict drops the least recently updated clean entries once the in-memory
// set exceeds MaxEntries. Dirty entries are never evicted — they are the
// user's unsaved adjustments — and nothing here deletes a stored row, so
// an evicted override comes back on the next Load.
func (inst *Resolver) evict() {
	over := len(inst.byKey) - inst.opts.MaxEntries
	if over <= 0 {
		return
	}
	cands := make([]evictCand, 0, len(inst.byKey))
	for k, e := range inst.byKey {
		if e.dirty {
			continue
		}
		cands = append(cands, evictCand{key: k, at: e.updatedAt})
	}
	// Ties break on the key so eviction is deterministic: entries loaded
	// from one read share a timestamp, and leaving the tiebreak to map
	// order would evict different entries on different runs.
	sort.Slice(cands, func(i, j int) bool {
		if !cands[i].at.Equal(cands[j].at) {
			return cands[i].at.Before(cands[j].at)
		}
		a, b := cands[i].key, cands[j].key
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		return a.ColumnKey < b.ColumnKey
	})
	for i := 0; i < len(cands) && over > 0; i++ {
		delete(inst.byKey, cands[i].key)
		over--
	}
}

type evictCand struct {
	key factsstore.ColumnWidthKey
	at  time.Time
}
