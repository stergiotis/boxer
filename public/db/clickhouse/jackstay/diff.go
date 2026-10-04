package jackstay

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LeafDiff is one leaf whose digests differ between the sides.
type LeafDiff struct {
	Leaf    uint32 `json:"leaf"`
	SrcRows uint64 `json:"srcRows"`
	DstRows uint64 `json:"dstRows"`
	// KeysDiffer is true when the key digests differ. False means the same
	// keys hold different values, with high probability.
	KeysDiffer bool `json:"keysDiffer"`
	// Resolved is true when Missing, Extra and Changed are exact: the leaf
	// was compared row by row, or one side holds no rows of it.
	Resolved bool   `json:"resolved"`
	Missing  uint64 `json:"missing,omitempty"`
	Extra    uint64 `json:"extra,omitempty"`
	Changed  uint64 `json:"changed,omitempty"`
}

// ChunkDiff is one chunk whose digests differ. A chunk one side lacks
// entirely is reported without leaves.
type ChunkDiff struct {
	Id      string `json:"id"`
	Display string `json:"display,omitempty"`
	// SrcPid and DstPid are each side's partition id of the chunk, when the
	// table is chunked by partition; they let a sync prune and drop.
	SrcPid         string     `json:"srcPid,omitempty"`
	DstPid         string     `json:"dstPid,omitempty"`
	SrcRows        uint64     `json:"srcRows"`
	DstRows        uint64     `json:"dstRows"`
	AbsentOnTarget bool       `json:"absentOnTarget,omitempty"`
	AbsentOnSource bool       `json:"absentOnSource,omitempty"`
	Leaves         []LeafDiff `json:"leaves,omitempty"`
}

// RowExample names one differing row by its key, for the operator.
type RowExample struct {
	Kind  string `json:"kind"`
	Chunk string `json:"chunk"`
	Key   string `json:"key"`
}

// TableDiff is the content diff of one table (ADR-0259 §SD4): which chunks and
// leaves differ, and, where they could be compared row by row, how.
type TableDiff struct {
	ComputedAt time.Time `json:"computedAt"`
	// Final records that the diff was asked for with FINAL. [PlanTable.DigestSpecs]
	// applies FINAL only to a side whose engine collapses rows, so the request
	// may have reached one side, both or neither; MaybeSpurious says whether a
	// merge engine was read without it.
	Final bool `json:"final"`
	// MaybeSpurious is set when a merge-semantics engine was read without
	// FINAL: differences may vanish once both sides are merged.
	MaybeSpurious   bool   `json:"maybeSpurious,omitempty"`
	SrcRows         uint64 `json:"srcRows"`
	DstRows         uint64 `json:"dstRows"`
	Chunks          uint64 `json:"chunks"`
	IdenticalChunks uint64 `json:"identicalChunks"`
	DifferingLeaves uint64 `json:"differingLeaves"`
	// UnresolvedLeaves differ, but were too large (or over budget) to compare
	// row by row; their rows count toward none of Missing, Extra, Changed.
	UnresolvedLeaves uint64       `json:"unresolvedLeaves"`
	Missing          uint64       `json:"missing"`
	Extra            uint64       `json:"extra"`
	Changed          uint64       `json:"changed"`
	Differing        []ChunkDiff  `json:"differing,omitempty"`
	Examples         []RowExample `json:"examples,omitempty"`
}

func (inst *TableDiff) IsIdentical() (ok bool) {
	return len(inst.Differing) == 0
}

// DiffOptions bound the row-by-row stage of a diff.
type DiffOptions struct {
	// PairThreshold is the largest leaf (by the larger side's row count)
	// compared row by row.
	PairThreshold uint64
	// PairBudget caps the rows fetched for row-by-row comparison per table
	// and side.
	PairBudget uint64
	// MaxExamples caps the example rows kept per table.
	MaxExamples int
}

func DefaultDiffOptions() (opts DiffOptions) {
	return DiffOptions{PairThreshold: 4096, PairBudget: 200_000, MaxExamples: 10}
}

type leafDigest struct {
	n, kd, rd uint64
}

type chunkDigests struct {
	display string
	pid     string
	rows    uint64
	leaves  map[uint32]leafDigest
}

func readDigests(ctx context.Context, q QueryI, spec *DigestSpec) (chunks map[string]*chunkDigests, err error) {
	var rows []leafDigestRow
	rows, err = queryRows[leafDigestRow](ctx, q, spec.LeafDigestQuery())
	if err != nil {
		err = eh.Errorf("unable to read leaf digests: %w", err)
		return
	}
	chunks = make(map[string]*chunkDigests, 16)
	for _, r := range rows {
		c := chunks[r.Chunk]
		if c == nil {
			c = &chunkDigests{display: r.Display, pid: r.Pid, leaves: make(map[uint32]leafDigest, 16)}
			chunks[r.Chunk] = c
		}
		c.rows += r.N
		c.leaves[r.Leaf] = leafDigest{n: r.N, kd: r.Kd, rd: r.Rd}
	}
	return
}

// both runs f for the source and the target at once; the scans are independent
// and each is bound by its own server. The first error cancels the other
// side, so a failure on one server is not reported only after the other's
// scan has run to its end. The error returned is the first side's: the other
// side's error is dropped when it is only the cancellation that failure caused
// (it would name a server that did nothing wrong), and joined to it otherwise.
func both[T any](ctx context.Context, f func(ctx context.Context, side int) (T, error)) (src T, dst T, err error) {
	type result struct {
		v   T
		err error
	}
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	var first atomic.Int32 // 0: no side has failed; otherwise side+1
	fail := func(side int) {
		if first.CompareAndSwap(0, int32(side)+1) {
			cancel(errOtherSideFailed)
		}
	}
	ch := make(chan result, 1)
	go func() {
		v, e := f(ctx, 1)
		if e != nil {
			fail(1)
		}
		ch <- result{v, e}
	}()
	var srcErr error
	src, srcErr = f(ctx, 0)
	if srcErr != nil {
		fail(0)
	}
	r := <-ch
	dst = r.v
	firstErr, laterErr := srcErr, r.err
	if first.Load() == 2 {
		firstErr, laterErr = r.err, srcErr
	}
	switch {
	case laterErr == nil:
		err = firstErr
	case errors.Is(laterErr, context.Canceled) && context.Cause(ctx) == errOtherSideFailed:
		err = firstErr
	default:
		err = errors.Join(firstErr, laterErr)
	}
	return
}

// errOtherSideFailed is the cause [both] cancels the second side with.
var errOtherSideFailed = errors.New("the other server failed")

// DiffTable compares one table's content on both servers without moving rows
// (ADR-0259 §SD4): one leaf-digest scan per side, then one row-pair scan per
// side for the differing leaves small enough to compare row by row. Final is
// set when either spec reads with FINAL; [DiffPlanTable] overwrites it with
// the operator's request.
//
// A source that serves no row pairs (a pack) leaves every differing leaf that
// both sides hold unresolved.
func DiffTable(ctx context.Context, src SourceI, dst QueryI, srcSpec *DigestSpec, dstSpec *DigestSpec, opts DiffOptions, now time.Time) (d TableDiff, err error) {
	d = TableDiff{ComputedAt: now.UTC(), Final: srcSpec.Final || dstSpec.Final}
	if src.limits().noPairs {
		opts.PairBudget = 0
	}
	var sc, dc map[string]*chunkDigests
	sc, dc, err = both(ctx, func(ctx context.Context, side int) (map[string]*chunkDigests, error) {
		if side == 0 {
			return src.digests(ctx, srcSpec)
		}
		return readDigests(ctx, dst, dstSpec)
	})
	if err != nil {
		return
	}

	ids := make([]string, 0, len(sc)+len(dc))
	for id := range sc {
		ids = append(ids, id)
	}
	for id := range dc {
		if _, has := sc[id]; !has {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, srcSpec.Chunking.compareChunkIds)
	d.Chunks = uint64(len(ids))

	var toResolve []ChunkLeaf
	var budget uint64
	for _, id := range ids {
		s, t := sc[id], dc[id]
		var cd ChunkDiff
		cd.Id = id
		if s != nil {
			cd.SrcPid = s.pid
		}
		if t != nil {
			cd.DstPid = t.pid
		}
		switch {
		case t == nil:
			d.SrcRows += s.rows
			cd.Display, cd.SrcRows, cd.AbsentOnTarget = s.display, s.rows, true
			d.Missing += s.rows
			d.DifferingLeaves += uint64(len(s.leaves))
		case s == nil:
			d.DstRows += t.rows
			cd.Display, cd.DstRows, cd.AbsentOnSource = t.display, t.rows, true
			d.Extra += t.rows
			d.DifferingLeaves += uint64(len(t.leaves))
		default:
			d.SrcRows += s.rows
			d.DstRows += t.rows
			cd.Display, cd.SrcRows, cd.DstRows = s.display, s.rows, t.rows
			cd.Leaves = diffLeaves(s, t)
			for i := range cd.Leaves {
				ld := &cd.Leaves[i]
				d.DifferingLeaves++
				switch {
				case ld.DstRows == 0:
					ld.Resolved, ld.Missing = true, ld.SrcRows
				case ld.SrcRows == 0:
					ld.Resolved, ld.Extra = true, ld.DstRows
				case opts.PairBudget > 0 && max(ld.SrcRows, ld.DstRows) <= opts.PairThreshold && budget+max(ld.SrcRows, ld.DstRows) <= opts.PairBudget:
					budget += max(ld.SrcRows, ld.DstRows)
					toResolve = append(toResolve, ChunkLeaf{Chunk: id, Leaf: ld.Leaf})
				}
			}
		}
		if cd.Display == "" {
			cd.Display = srcSpec.Chunking.RangeDisplay(id)
		}
		if cd.AbsentOnTarget || cd.AbsentOnSource || len(cd.Leaves) > 0 {
			d.Differing = append(d.Differing, cd)
		} else {
			d.IdenticalChunks++
		}
	}

	if len(toResolve) > 0 {
		err = resolveLeaves(ctx, src, dst, srcSpec, dstSpec, toResolve, opts, &d)
		if err != nil {
			return
		}
	}
	for _, cd := range d.Differing {
		for _, ld := range cd.Leaves {
			if !ld.Resolved {
				d.UnresolvedLeaves++
				continue
			}
			d.Missing += ld.Missing
			d.Extra += ld.Extra
			d.Changed += ld.Changed
		}
	}
	return
}

// compareChunkIds orders chunk ids of a layout: range chunk ids (decimal
// indexes) numerically, with any id that is not an index after them, and
// partition chunk ids (hex) as strings. Telling the kinds apart by the layout,
// not by an id's shape, keeps the order transitive: a hex id such as "1000"
// also reads as an index.
func (inst *Chunking) compareChunkIds(a string, b string) (r int) {
	if inst.Kind == ChunkingRange {
		ia, oka := rangeIndex(a)
		ib, okb := rangeIndex(b)
		switch {
		case oka && okb:
			return cmp.Compare(ia, ib)
		case oka:
			return -1
		case okb:
			return 1
		}
	}
	return cmp.Compare(a, b)
}

func diffLeaves(s *chunkDigests, t *chunkDigests) (out []LeafDiff) {
	leaves := make([]uint32, 0, len(s.leaves)+len(t.leaves))
	for l := range s.leaves {
		leaves = append(leaves, l)
	}
	for l := range t.leaves {
		if _, has := s.leaves[l]; !has {
			leaves = append(leaves, l)
		}
	}
	slices.Sort(leaves)
	for _, l := range leaves {
		a, b := s.leaves[l], t.leaves[l]
		if a == b {
			continue
		}
		out = append(out, LeafDiff{Leaf: l, SrcRows: a.n, DstRows: b.n, KeysDiffer: a.n != b.n || a.kd != b.kd})
	}
	return
}

type pairKey struct {
	cl ChunkLeaf
	kh uint64
}

type pairSide struct {
	rh   []uint64
	text string
}

func readPairs(ctx context.Context, q QueryI, spec *DigestSpec, leaves []ChunkLeaf) (pairs map[pairKey]*pairSide, err error) {
	var rows []pairRow
	rows, err = queryRows[pairRow](ctx, q, spec.PairQuery(leaves))
	if err != nil {
		err = eh.Errorf("unable to read row pairs: %w", err)
		return
	}
	pairs = make(map[pairKey]*pairSide, len(rows))
	for _, r := range rows {
		k := pairKey{cl: ChunkLeaf{Chunk: r.Chunk, Leaf: r.Leaf}, kh: r.Kh}
		p := pairs[k]
		if p == nil {
			p = &pairSide{text: r.KeyText}
			pairs[k] = p
		}
		p.rh = append(p.rh, r.Rh)
	}
	return
}

// resolveLeaves compares the given leaves row by row. Per key, rows present on
// both sides with equal row hashes match; unmatched rows under a key both sides
// hold are changed, pairing one source row with one target row, and the rest
// are missing or extra. Duplicate rows are counted, not collapsed. A leaf for
// which neither side returned a row (the table moved between the scans) is
// left unresolved rather than reported as zero differences.
func resolveLeaves(ctx context.Context, src SourceI, dst QueryI, srcSpec *DigestSpec, dstSpec *DigestSpec, leaves []ChunkLeaf, opts DiffOptions, d *TableDiff) (err error) {
	var sp, dp map[pairKey]*pairSide
	sp, dp, err = both(ctx, func(ctx context.Context, side int) (map[pairKey]*pairSide, error) {
		if side == 0 {
			return src.pairs(ctx, srcSpec, leaves)
		}
		return readPairs(ctx, dst, dstSpec, leaves)
	})
	if err != nil {
		return
	}
	type counts struct {
		missing, extra, changed uint64
		// seen is set once a side returned a row of the leaf.
		seen bool
	}
	per := make(map[ChunkLeaf]*counts, len(leaves))
	for _, cl := range leaves {
		per[cl] = &counts{}
	}
	example := func(kind string, chunk string, key string) {
		if len(d.Examples) < opts.MaxExamples {
			d.Examples = append(d.Examples, RowExample{Kind: kind, Chunk: chunk, Key: key})
		}
	}
	keys := make([]pairKey, 0, len(sp)+len(dp))
	for k := range sp {
		keys = append(keys, k)
	}
	for k := range dp {
		if _, has := sp[k]; !has {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a pairKey, b pairKey) int {
		if r := srcSpec.Chunking.compareChunkIds(a.cl.Chunk, b.cl.Chunk); r != 0 {
			return r
		}
		if r := cmp.Compare(a.cl.Leaf, b.cl.Leaf); r != 0 {
			return r
		}
		return cmp.Compare(a.kh, b.kh)
	})
	for _, k := range keys {
		c := per[k.cl]
		if c == nil {
			continue
		}
		c.seen = true
		s, t := sp[k], dp[k]
		switch {
		case t == nil:
			c.missing += uint64(len(s.rh))
			example("missing", k.cl.Chunk, s.text)
		case s == nil:
			c.extra += uint64(len(t.rh))
			example("extra", k.cl.Chunk, t.text)
		default:
			us, ut := unmatched(s.rh, t.rh)
			changed := min(us, ut)
			c.changed += changed
			c.missing += us - changed
			c.extra += ut - changed
			if us+ut > 0 {
				kind := "changed"
				if changed == 0 {
					kind = "missing"
					if us == 0 {
						kind = "extra"
					}
				}
				example(kind, k.cl.Chunk, s.text)
			}
		}
	}
	for i := range d.Differing {
		cd := &d.Differing[i]
		for j := range cd.Leaves {
			ld := &cd.Leaves[j]
			if c := per[ChunkLeaf{Chunk: cd.Id, Leaf: ld.Leaf}]; c != nil && c.seen {
				ld.Resolved = true
				ld.Missing, ld.Extra, ld.Changed = c.missing, c.extra, c.changed
			}
		}
	}
	return
}

// unmatched counts the elements of a and b left over after removing their
// multiset intersection.
func unmatched(a []uint64, b []uint64) (ua uint64, ub uint64) {
	slices.Sort(a)
	slices.Sort(b)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			ua++
			i++
		case a[i] > b[j]:
			ub++
			j++
		default:
			i++
			j++
		}
	}
	ua += uint64(len(a) - i)
	ub += uint64(len(b) - j)
	return
}

// DigestSpecs builds the two sides' digest specs of a plan table. FINAL is
// applied to a side only when final is asked for and that side's engine
// collapses rows; maybeSpurious reports a merge engine read without it. A
// target outside the MergeTree family has no partition ids, so its partition
// chunks are read by the chunk expression.
func (inst *PlanTable) DigestSpecs(final bool) (src DigestSpec, dst DigestSpec, maybeSpurious bool) {
	keys := SplitKeyExprs(inst.SortingKey)
	var c Chunking
	if inst.Chunking != nil {
		c = *inst.Chunking
	}
	src = DigestSpec{Ref: inst.Source, KeyExprs: keys, CopyColumns: inst.CopyColumns, HashAsText: inst.HashAsText, Chunking: c, Final: final && IsMergeEngine(inst.Engine), Filter: inst.Filter}
	dst = DigestSpec{Ref: inst.Target, KeyExprs: keys, CopyColumns: inst.CopyColumns, HashAsText: inst.HashAsText, Chunking: c, Final: final && IsMergeEngine(inst.TargetEngine), Filter: inst.Filter,
		NoPartitionIds: !isMergeTreeEngine(inst.TargetEngine)}
	maybeSpurious = (IsMergeEngine(inst.Engine) && !src.Final) || (IsMergeEngine(inst.TargetEngine) && !dst.Final)
	return
}

// DiffPlanTable runs the content diff of one plan table and stores it in the
// table: the chunk layout is derived from the source first when the plan has
// none. The table must be diffable ([PlanTable.IsDiffable]).
func DiffPlanTable(ctx context.Context, src SourceI, dst QueryI, pt *PlanTable, final bool, chunkOpts ChunkingOptions, diffOpts DiffOptions, now time.Time) (err error) {
	if !pt.IsDiffable() {
		err = eb.Build().Str("table", pt.Source.String()).Str("verdict", pt.Verdict.String()).Errorf("table is not diffable")
		return
	}
	if final && src.limits().noFinal {
		err = eb.Build().Str("table", pt.Source.String()).Errorf("%s cannot be read with FINAL", src.limits().what)
		return
	}
	if pt.Chunking == nil {
		var c Chunking
		c, err = src.deriveChunking(ctx, pt, chunkOpts)
		if err != nil {
			return
		}
		pt.Chunking = &c
	}
	srcSpec, dstSpec, maybeSpurious := pt.DigestSpecs(final)
	var d TableDiff
	d, err = DiffTable(ctx, src, dst, &srcSpec, &dstSpec, diffOpts, now)
	if err != nil {
		err = eb.Build().Str("table", pt.Source.String()).Errorf("unable to diff table: %w", err)
		return
	}
	d.Final = final
	d.MaybeSpurious = maybeSpurious && !d.IsIdentical()
	pt.Diff = &d
	return
}
