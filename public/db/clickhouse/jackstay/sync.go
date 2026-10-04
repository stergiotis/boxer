package jackstay

import (
	"context"
	"io"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TableSync is the operator's choice for one table on the Sync step.
type TableSync struct {
	Mode      SyncModeE       `json:"mode"`
	Existing  ExistingPolicyE `json:"existing"`
	SampleNum uint32          `json:"sampleNum,omitempty"`
	SampleDen uint32          `json:"sampleDen,omitempty"`
}

// SyncRun identifies a sync run; the journal's lines carry its id.
type SyncRun struct {
	RunId     string    `json:"runId"`
	StartedAt time.Time `json:"startedAt"`
}

// ChunkResult reports one chunk.
type ChunkResult struct {
	Table    string       `json:"table"`
	Chunk    string       `json:"chunk"`
	Display  string       `json:"display,omitempty"`
	Status   ChunkStatusE `json:"status"`
	Rows     uint64       `json:"rows"`
	Bytes    uint64       `json:"bytes"`
	Cleared  uint64       `json:"cleared,omitempty"`
	Attempts int          `json:"attempts"`
	Note     string       `json:"note,omitempty"`
}

// TableSyncReport sums a table's chunk results.
type TableSyncReport struct {
	RunId      string    `json:"runId"`
	FinishedAt time.Time `json:"finishedAt"`
	Copied     int       `json:"copied"`
	Done       int       `json:"done"`
	Identical  int       `json:"identical"`
	Stale      int       `json:"stale"`
	Failed     int       `json:"failed"`
	Rows       uint64    `json:"rows"`
	Bytes      uint64    `json:"bytes"`
	Cleared    uint64    `json:"cleared"`
	// Problems are the notes of stale and failed chunks.
	Problems []string `json:"problems,omitempty"`
}

func (inst *TableSyncReport) add(r ChunkResult) {
	switch r.Status {
	case ChunkStatusCopied:
		inst.Copied++
	case ChunkStatusDone:
		inst.Done++
	case ChunkStatusIdentical:
		inst.Identical++
	case ChunkStatusStale:
		inst.Stale++
	case ChunkStatusFailed:
		inst.Failed++
	}
	inst.Rows += r.Rows
	inst.Bytes += r.Bytes
	inst.Cleared += r.Cleared
	if r.Status == ChunkStatusStale || r.Status == ChunkStatusFailed {
		label := r.Chunk
		if r.Display != "" {
			label = r.Display
		}
		inst.Problems = append(inst.Problems, r.Status.String()+" "+label+": "+r.Note)
	}
}

// ClientI is what a sync needs of each server;
// [github.com/stergiotis/boxer/public/keelson/data/chclient.Client] satisfies it.
type ClientI interface {
	QueryI
	ExecI
	QueryStream(ctx context.Context, sql string, opts chclient.StreamOptions) (body io.ReadCloser, contentEncoding string, err error)
	InsertStream(ctx context.Context, insertSQL string, body io.Reader, opts chclient.StreamOptions) (err error)
}

var _ ClientI = (*chclient.Client)(nil)

// SyncOptions tune a sync.
type SyncOptions struct {
	// MaxAttempts bounds the copies of one chunk the run owns.
	MaxAttempts int
	// Progress, when set, is called after each chunk.
	Progress func(r ChunkResult)
	// Bytes, when set, is advanced as bytes pass through the relay, for a
	// live transfer rate. With compression these are compressed bytes.
	Bytes *atomic.Int64
	// Rows, when set, is advanced as rows land on the target, read from the
	// INSERT's written_rows while it runs, and wound back when a copy fails
	// verification (ADR-0259 §SD6). A chunk an earlier call of the run
	// verified is credited with its rows when it is skipped.
	Rows *atomic.Int64
	// FreeFloor, when set, is waited for before each chunk, ahead of
	// BeforeChunk: the sync pauses while a target disk is below it
	// (ADR-0259 §SD6). OnLowDisk, when set, is told of the low disks on each
	// look.
	FreeFloor *FreeFloor
	OnLowDisk func(low []DiskInfo)
	// PollPeriod is how often Rows is refreshed.
	PollPeriod time.Duration
	// Compression is the HTTP content encoding the relay asks the source
	// for ("zstd", "gzip"; empty for none). The compressed bytes are passed
	// to the target as they are, never decoded in between.
	Compression string
	// BeforeChunk, when set, runs before each chunk. An error stops the
	// table's sync.
	BeforeChunk func(ctx context.Context, pt *PlanTable) (err error)
	// BeforeTable and AfterTable, when set, bracket each table of a run;
	// AfterTable runs once the table's report is stored in the plan.
	BeforeTable func(pt *PlanTable)
	AfterTable  func(pt *PlanTable)
}

func DefaultSyncOptions() (opts SyncOptions) {
	return SyncOptions{MaxAttempts: 3, PollPeriod: 500 * time.Millisecond, Compression: "zstd"}
}

// digest arithmetic: the wrapping sums are additive, so a target chunk after a
// copy must equal what it kept plus what was copied (ADR-0259 §SD5).
func (inst leafDigest) plus(o leafDigest) (s leafDigest) {
	return leafDigest{n: inst.n + o.n, kd: inst.kd + o.kd, rd: inst.rd + o.rd}
}

type leafSet map[uint32]leafDigest

func (inst leafSet) total() (t leafDigest) {
	for _, d := range inst {
		t = t.plus(d)
	}
	return
}

func (inst leafSet) plus(o leafSet) (s leafSet) {
	s = make(leafSet, len(inst)+len(o))
	for l, d := range inst {
		s[l] = d
	}
	for l, d := range o {
		s[l] = s[l].plus(d)
	}
	return
}

func (inst leafSet) equal(o leafSet) (eq bool) {
	for l, d := range inst {
		if d.n != 0 && o[l] != d {
			return false
		}
	}
	for l, d := range o {
		if d.n != 0 && inst[l] != d {
			return false
		}
	}
	return true
}

func (inst leafSet) differing(o leafSet) (leaves []uint32) {
	for l, d := range inst {
		if o[l] != d {
			leaves = append(leaves, l)
		}
	}
	for l := range o {
		if _, has := inst[l]; !has {
			leaves = append(leaves, l)
		}
	}
	slices.Sort(leaves)
	return
}

func (inst leafSet) restrict(leaves []uint32) (s leafSet) {
	s = make(leafSet, len(leaves))
	for _, l := range leaves {
		if d, has := inst[l]; has {
			s[l] = d
		}
	}
	return
}

func readLeafSet(ctx context.Context, q QueryI, spec *DigestSpec) (ls leafSet, err error) {
	var chunks map[string]*chunkDigests
	chunks, err = readDigests(ctx, q, spec)
	if err != nil {
		return
	}
	ls = leafSetOf(chunks)
	return
}

func leafSetOf(chunks map[string]*chunkDigests) (ls leafSet) {
	ls = make(leafSet, 16)
	for _, c := range chunks {
		for l, d := range c.leaves {
			ls[l] = ls[l].plus(d)
		}
	}
	return
}

func (inst *tableSyncer) unwind(counted int64) {
	if inst.opts.Rows != nil && counted != 0 {
		inst.opts.Rows.Add(-counted)
	}
}

// checkStart refuses to resume a table under settings other than those the
// run began it under: the mode, the existing-rows policy, the sample fraction
// and the row filter. A start entry without a fraction predates its recording
// and is taken as matching.
func checkStart(e JournalEntry, pt *PlanTable) (err error) {
	if e.Mode != pt.Sync.Mode.String() || e.Existing != pt.Sync.Existing.String() {
		return eb.Build().Str("table", pt.Source.String()).Str("begun", e.Mode+"/"+e.Existing).Str("now", pt.Sync.Mode.String()+"/"+pt.Sync.Existing.String()).
			Errorf("the run began this table under other settings; keep them, or restart the run")
	}
	if pt.Sync.Mode == SyncModeSample && e.SampleDen != 0 && (e.SampleNum != pt.Sync.SampleNum || e.SampleDen != pt.Sync.SampleDen) {
		return eb.Build().Str("table", pt.Source.String()).
			Str("begun", strconv.FormatUint(uint64(e.SampleNum), 10)+"/"+strconv.FormatUint(uint64(e.SampleDen), 10)).
			Str("now", strconv.FormatUint(uint64(pt.Sync.SampleNum), 10)+"/"+strconv.FormatUint(uint64(pt.Sync.SampleDen), 10)).
			Errorf("the run began this table under another sample; keep it, or restart the run")
	}
	if e.Filter != pt.Filter {
		return eb.Build().Str("table", pt.Source.String()).Str("begun", e.Filter).Str("now", pt.Filter).
			Errorf("the run began this table under another row filter; keep it, or restart the run")
	}
	return
}

// tableSyncer carries one table's sync state across its chunks.
type tableSyncer struct {
	src     SourceI
	dst     ClientI
	pt      *PlanTable
	srcSpec DigestSpec
	dstSpec DigestSpec
	sameKey bool
	owned   bool
	journal *Journal
	opts    SyncOptions
	now     func() time.Time
}

// SyncTable copies one plan table per its [TableSync] (ADR-0259 §SD5). Each
// chunk is copied, then its target digest is compared with what it kept plus
// what was copied; only a verified chunk enters the journal. A chunk the run
// owns is cleared and copied again on a mismatch, up to MaxAttempts.
//
// The table must be diffable (its DDL applied), have a chunk layout, and, for
// repair, a diff: repair touches only the leaves that diff showed, and only
// while the target still holds what it showed.
//
// A table with a row filter is synced as its slice (ADR-0271 §SD1): every
// read, count and clear on either side is restricted to the filter's rows.
func SyncTable(ctx context.Context, src SourceI, dst ClientI, pt *PlanTable, j *Journal, opts SyncOptions, now func() time.Time) (rep TableSyncReport, err error) {
	switch {
	case pt.Sync == nil:
		err = eb.Build().Str("table", pt.Source.String()).Errorf("table has no sync settings")
	case !pt.IsDiffable():
		err = eb.Build().Str("table", pt.Source.String()).Str("verdict", pt.Verdict.String()).Errorf("table is not ready to sync; apply its DDL first")
	case pt.Chunking == nil:
		err = eb.Build().Str("table", pt.Source.String()).Errorf("table has no chunk layout")
	case pt.Sync.Mode == SyncModeRepair && pt.Diff == nil:
		err = eb.Build().Str("table", pt.Source.String()).Errorf("repair needs a diff; run diff first")
	case pt.Sync.Mode == SyncModeSample && (pt.Sync.SampleDen == 0 || pt.Sync.SampleNum == 0 || pt.Sync.SampleNum > pt.Sync.SampleDen):
		err = eb.Build().Str("table", pt.Source.String()).Errorf("invalid sample fraction")
	case pt.Sync.Mode != SyncModeFull && src.limits().noSubsets:
		err = eb.Build().Str("table", pt.Source.String()).Str("mode", pt.Sync.Mode.String()).Errorf("%s holds whole chunks only; sync it in full", src.limits().what)
	}
	if err != nil {
		return
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 1
	}
	ts := &tableSyncer{src: src, dst: dst, pt: pt, journal: j, opts: opts, now: now,
		sameKey: pt.Chunking.Kind == ChunkingPartition && normalizeExpr(pt.PartitionKey) == normalizeExpr(pt.TargetPartitionKey)}
	ts.srcSpec, ts.dstSpec, _ = pt.DigestSpecs(false)
	table := pt.Source.String()
	rep.RunId = j.run

	if e, started := j.Started(table); started {
		if err = checkStart(e, pt); err != nil {
			return
		}
		ts.owned = e.Owned
	} else {
		ts.owned, err = ts.claim(ctx)
		if err != nil {
			return
		}
		err = j.RecordStart(table, ts.owned, *pt.Sync, pt.Filter, now())
		if err != nil {
			return
		}
	}

	var chunks []func() ChunkResult
	if pt.Sync.Mode == SyncModeRepair {
		for i := range pt.Diff.Differing {
			cd := &pt.Diff.Differing[i]
			chunks = append(chunks, func() ChunkResult { return ts.repairChunk(ctx, cd) })
		}
	} else {
		var srcChunks []chunkListRow
		srcChunks, err = src.chunkList(ctx, &ts.srcSpec)
		if err != nil {
			err = eb.Build().Str("table", table).Errorf("unable to list source chunks: %w", err)
			return
		}
		for _, c := range srcChunks {
			chunks = append(chunks, func() ChunkResult { return ts.copyChunk(ctx, c) })
		}
		if pt.Sync.Existing == ExistingPolicyReplace {
			// Replace makes the target equal to the source, so chunks the
			// target holds and the source does not are cleared too.
			var extra []chunkListRow
			extra, err = ts.targetOnlyChunks(ctx, srcChunks)
			if err != nil {
				return
			}
			for _, c := range extra {
				chunks = append(chunks, func() ChunkResult { return ts.clearChunk(ctx, c) })
			}
		}
	}
	for _, chunk := range chunks {
		if err = ctx.Err(); err != nil {
			return
		}
		if opts.FreeFloor != nil {
			if err = opts.FreeFloor.WaitForFree(ctx, dst, pt.Target, opts.OnLowDisk); err != nil {
				return
			}
		}
		if opts.BeforeChunk != nil {
			if err = opts.BeforeChunk(ctx, pt); err != nil {
				return
			}
		}
		r := chunk()
		if r.Status == ChunkStatusDone && opts.Rows != nil {
			// The relay never counted a chunk an earlier call verified.
			opts.Rows.Add(int64(doneRows(pt, r)))
		}
		rep.add(r)
		if opts.Progress != nil {
			opts.Progress(r)
		}
	}
	rep.FinishedAt = now().UTC()
	return
}

type countRow struct {
	N uint64 `json:"n"`
}

type chunkListRow struct {
	Chunk   string `json:"chunk"`
	Pid     string `json:"pid"`
	Display string `json:"display"`
}

// claim decides, when a run first reaches a table, whether it owns the
// target's rows: it does when the target is empty or the policy is replace. A
// full or sample sync into a non-empty target is refused unless the policy
// says otherwise. Repair owns only the leaves it was shown, which is decided
// per chunk against the diff, so it claims nothing here.
func (inst *tableSyncer) claim(ctx context.Context) (owned bool, err error) {
	if inst.pt.Sync.Mode == SyncModeRepair {
		return false, nil
	}
	// Under a row filter the run owns the target's slice, and only the
	// slice is counted (ADR-0271 §SD1).
	where := ""
	if inst.dstSpec.Filter != "" {
		where = " WHERE " + inst.dstSpec.Filter
	}
	var rows []countRow
	rows, err = queryRows[countRow](ctx, inst.dst, "SELECT count() AS n FROM "+QuoteRef(inst.pt.Target)+where+jsonSettings)
	if err != nil {
		err = eb.Build().Str("table", inst.pt.Target.String()).Errorf("unable to count target rows: %w", err)
		return
	}
	empty := len(rows) == 1 && rows[0].N == 0
	switch inst.pt.Sync.Existing {
	case ExistingPolicyReplace:
		return true, nil
	case ExistingPolicyAppend:
		return empty, nil
	}
	if !empty {
		err = eb.Build().Str("table", inst.pt.Target.String()).Uint64("rows", rows[0].N).
			Errorf("target table is not empty; choose append or replace")
	}
	return empty, err
}

// targetPid is the target partition id of a chunk: dstPid when known, else
// the source's. A partition id names the same rows on both sides only when
// both tables partition by the same key; otherwise there is none.
func (inst *tableSyncer) targetPid(srcPid string, dstPid string) (pid string) {
	switch {
	case !inst.sameKey:
		return ""
	case dstPid != "":
		return dstPid
	}
	return srcPid
}

// targetOnlyChunks lists the target's chunks that the source's list lacks.
func (inst *tableSyncer) targetOnlyChunks(ctx context.Context, srcChunks []chunkListRow) (extra []chunkListRow, err error) {
	var dstChunks []chunkListRow
	dstChunks, err = queryRows[chunkListRow](ctx, inst.dst, inst.dstSpec.ChunkListQuery())
	if err != nil {
		err = eb.Build().Str("table", inst.pt.Target.String()).Errorf("unable to list target chunks: %w", err)
		return
	}
	have := make(map[string]bool, len(srcChunks))
	for _, c := range srcChunks {
		have[c.Chunk] = true
	}
	for _, c := range dstChunks {
		if !have[c.Chunk] {
			extra = append(extra, c)
		}
	}
	return
}

// clearChunk removes a target-only chunk under replace. It is journaled as a
// chunk copied at an empty source digest, so a resumed run skips it while the
// source still lacks it.
func (inst *tableSyncer) clearChunk(ctx context.Context, c chunkListRow) (r ChunkResult) {
	table := inst.pt.Source.String()
	r = ChunkResult{Table: table, Chunk: c.Chunk, Display: c.Display, Note: "target-only chunk cleared", Attempts: 1}
	if r.Display == "" {
		r.Display = inst.pt.Chunking.RangeDisplay(c.Chunk)
	}
	if e, done := inst.journal.Done(table, c.Chunk); done && e.N == 0 {
		r.Status, r.Note = ChunkStatusDone, "target-only chunk cleared earlier"
		return
	}
	dstSpec := inst.dstSpec.ForChunk(c.Chunk, inst.targetPid(c.Pid, ""))
	before, err := readLeafSet(ctx, inst.dst, &dstSpec)
	if err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return
	}
	if n := before.total().n; n > 0 {
		if err = inst.clear(ctx, c.Chunk, inst.targetPid(c.Pid, ""), nil); err != nil {
			r.Status, r.Note = ChunkStatusFailed, "unable to clear the target chunk: "+err.Error()
			return
		}
		r.Cleared = n
	}
	if err = inst.journal.RecordChunk(table, c.Chunk, inst.pt.Sync.Mode, leafDigest{}, inst.now()); err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return
	}
	r.Status = ChunkStatusCopied
	return
}

// clear removes rows of a target chunk: all of it (leaves nil) or the given
// leaves. A whole partition chunk whose partition key matches the source's is
// dropped as a partition; anything else is a lightweight DELETE, and a table
// outside the MergeTree family can only be truncated whole. Under a row
// filter only the slice may go, so the clear is always a DELETE restricted
// by the filter (ADR-0271 §SD1).
func (inst *tableSyncer) clear(ctx context.Context, chunk string, pid string, leaves []uint32) (err error) {
	target := QuoteRef(inst.pt.Target)
	filtered := inst.dstSpec.Filter != ""
	if !isMergeTreeEngine(inst.pt.TargetEngine) {
		if filtered || leaves != nil || inst.pt.Chunking.Kind != ChunkingSingle {
			return eb.Build().Str("engine", inst.pt.TargetEngine).Errorf("target engine cannot delete a subset of rows")
		}
		return inst.dst.Exec(ctx, "TRUNCATE TABLE "+target)
	}
	if !filtered && leaves == nil && inst.pt.Chunking.Kind == ChunkingPartition && inst.sameKey && pid != "" {
		return inst.dst.Exec(ctx, "ALTER TABLE "+target+" DROP PARTITION ID "+QuoteString(pid))
	}
	if !filtered && leaves == nil && inst.pt.Chunking.Kind == ChunkingSingle {
		return inst.dst.Exec(ctx, "TRUNCATE TABLE "+target)
	}
	return inst.dst.Exec(ctx, deleteQuery(inst.pt.Target, clearPredicate(&inst.dstSpec, chunk, leaves)))
}

// clearPredicate selects the target rows a DELETE of a chunk (or of its
// leaves) removes: within the row filter, always.
func clearPredicate(dstSpec *DigestSpec, chunk string, leaves []uint32) (sql string) {
	s := *dstSpec
	s.Where = ""
	s = s.with(s.Chunking.ChunkPredicate(chunk, ""))
	if leaves != nil {
		s = s.with(s.LeafPredicate(leaves))
	}
	sql = s.SlicePredicate()
	if sql == "" {
		sql = "1"
	}
	return
}

// verify compares a target chunk with its expected digest. A merge-semantics
// target may already have collapsed rows the copy delivered, so on a mismatch
// both sides are compared once more with FINAL, which is valid only when the
// target chunk holds nothing but the source's rows.
func (inst *tableSyncer) verify(ctx context.Context, srcSpec *DigestSpec, dstSpec *DigestSpec, expected leafSet, exclusive bool) (ok bool, note string, err error) {
	var after leafSet
	after, err = readLeafSet(ctx, inst.dst, dstSpec)
	if err != nil {
		return
	}
	if after.equal(expected) {
		return true, "", nil
	}
	note = "target digest after the copy does not match (" + strconv.FormatUint(after.total().n, 10) + " rows, expected " + strconv.FormatUint(expected.total().n, 10) + ")"
	if !exclusive || !(IsMergeEngine(inst.pt.Engine) || IsMergeEngine(inst.pt.TargetEngine)) {
		return
	}
	if inst.src.limits().noFinal {
		note += "; " + inst.src.limits().what + " cannot be read with FINAL to check merged rows"
		return
	}
	sf, df := *srcSpec, *dstSpec
	sf.Final = IsMergeEngine(inst.pt.Engine)
	df.Final = IsMergeEngine(inst.pt.TargetEngine)
	var s, d leafSet
	s, d, err = both(ctx, func(ctx context.Context, side int) (leafSet, error) {
		if side == 0 {
			return sourceLeafSet(ctx, inst.src, &sf)
		}
		return readLeafSet(ctx, inst.dst, &df)
	})
	if err != nil {
		return
	}
	if s.equal(d) {
		return true, "verified with FINAL (the target merged rows during the copy)", nil
	}
	return
}

func (inst *tableSyncer) copyChunk(ctx context.Context, c chunkListRow) (r ChunkResult) {
	table := inst.pt.Source.String()
	r = ChunkResult{Table: table, Chunk: c.Chunk, Display: c.Display}
	if r.Display == "" {
		r.Display = inst.pt.Chunking.RangeDisplay(c.Chunk)
	}
	srcSpec := inst.srcSpec.ForChunk(c.Chunk, c.Pid)
	if inst.pt.Sync.Mode == SyncModeSample {
		srcSpec = srcSpec.ForSample(inst.pt.Sync.SampleNum, inst.pt.Sync.SampleDen)
	}
	dstPid := inst.targetPid(c.Pid, "")
	dstSpec := inst.dstSpec.ForChunk(c.Chunk, dstPid)

	srcSet, err := sourceLeafSet(ctx, inst.src, &srcSpec)
	if err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return
	}
	srcTotal := srcSet.total()
	if e, done := inst.journal.Done(table, c.Chunk); done {
		if e.isAt(srcTotal) {
			r.Status, r.Rows = ChunkStatusDone, srcTotal.n
			return
		}
		if !inst.owned {
			r.Status, r.Note = ChunkStatusFailed, "source changed since this chunk was copied, and the run does not own the target's rows"
			return
		}
	}
	if !inst.owned && inst.journal.Attempted(table, c.Chunk) {
		r.Status, r.Note = ChunkStatusFailed, "an earlier attempt may have left rows in this chunk, and the run does not own the target's rows"
		return
	}
	if err = inst.src.precheck(ctx, &srcSpec); err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return
	}
	for attempt := 1; attempt <= inst.opts.MaxAttempts; attempt++ {
		r.Attempts = attempt
		if attempt > 1 {
			// The source may have moved since the digest the last attempt
			// verified against; a copy is verified against the source it
			// read from.
			srcSet, err = sourceLeafSet(ctx, inst.src, &srcSpec)
			if err != nil {
				r.Status, r.Note = ChunkStatusFailed, err.Error()
				return
			}
			srcTotal = srcSet.total()
		}
		var counted int64
		before, err := readLeafSet(ctx, inst.dst, &dstSpec)
		if err != nil {
			r.Status, r.Note = ChunkStatusFailed, err.Error()
			return
		}
		if inst.owned && before.total().n > 0 {
			err = inst.clear(ctx, c.Chunk, dstPid, nil)
			if err != nil {
				r.Status, r.Note = ChunkStatusFailed, "unable to clear the target chunk: "+err.Error()
				return
			}
			if attempt == 1 {
				// Later attempts clear the run's own failed copy, not
				// rows the operator had.
				r.Cleared += before.total().n
			}
			before = leafSet{}
		}
		if srcTotal.n > 0 {
			if !inst.owned {
				err = inst.journal.RecordAttempt(table, c.Chunk, inst.now())
				if err != nil {
					r.Status, r.Note = ChunkStatusFailed, err.Error()
					return
				}
			}
			var bytes uint64
			bytes, counted, err = relay(ctx, inst.src, inst.dst, &srcSpec, &inst.dstSpec, &inst.opts, srcTotal.n)
			r.Bytes += bytes
			if err != nil {
				r.Note = err.Error()
				if !inst.owned {
					break
				}
				continue
			}
		}
		if inst.settle(ctx, &r, c.Chunk, &srcSpec, &dstSpec, before.plus(srcSet), before.total().n == 0, counted, srcTotal, srcTotal.n) {
			return
		}
		if !inst.owned {
			r.Note += "; the run does not own the target's rows, so it cannot retry"
			break
		}
	}
	r.Status = ChunkStatusFailed
	return
}

// settle verifies an attempt's copy against expected and, when it holds,
// journals the chunk at srcTotal and reports rows copied. A copy that fails
// verification has the rows it counted wound back. final reports that r is
// the chunk's result; otherwise r.Note says why the attempt failed.
func (inst *tableSyncer) settle(ctx context.Context, r *ChunkResult, chunk string, srcSpec *DigestSpec, dstSpec *DigestSpec, expected leafSet, exclusive bool, counted int64, srcTotal leafDigest, rows uint64) (final bool) {
	ok, note, err := inst.verify(ctx, srcSpec, dstSpec, expected, exclusive)
	r.Note = note
	if err != nil || !ok {
		inst.unwind(counted)
	}
	switch {
	case err != nil:
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return true
	case !ok:
		return false
	}
	err = inst.journal.RecordChunk(r.Table, chunk, inst.pt.Sync.Mode, srcTotal, inst.now())
	if err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return true
	}
	r.Status, r.Rows = ChunkStatusCopied, rows
	return true
}

// repairChunk makes a target chunk's differing leaves equal to the source's.
// It first checks the target against the plan's diff: every leaf it would
// clear must be one the diff listed, holding the row count the diff showed.
// Otherwise it touches nothing and reports the chunk stale.
func (inst *tableSyncer) repairChunk(ctx context.Context, cd *ChunkDiff) (r ChunkResult) {
	table := inst.pt.Source.String()
	r = ChunkResult{Table: table, Chunk: cd.Id, Display: cd.Display}
	srcSpec := inst.srcSpec.ForChunk(cd.Id, cd.SrcPid)
	dstPid := inst.targetPid(cd.SrcPid, cd.DstPid)
	dstSpec := inst.dstSpec.ForChunk(cd.Id, dstPid)

	s, d, err := both(ctx, func(ctx context.Context, side int) (leafSet, error) {
		if side == 0 {
			return sourceLeafSet(ctx, inst.src, &srcSpec)
		}
		return readLeafSet(ctx, inst.dst, &dstSpec)
	})
	if err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return
	}
	srcTotal := s.total()
	if e, done := inst.journal.Done(table, cd.Id); done && e.isAt(srcTotal) && s.equal(d) {
		r.Status, r.Rows = ChunkStatusDone, srcTotal.n
		return
	}
	shown := make(map[uint32]uint64, len(cd.Leaves))
	for _, ld := range cd.Leaves {
		shown[ld.Leaf] = ld.DstRows
	}
	allowed := inst.differingAllowed(cd, shown, s, d)
	if allowed == nil {
		if s.equal(d) {
			r.Status = ChunkStatusIdentical
			return
		}
		r.Status, r.Note = ChunkStatusStale, "the target no longer holds what the diff showed; run diff again"
		return
	}

	for attempt := 1; attempt <= inst.opts.MaxAttempts; attempt++ {
		r.Attempts = attempt
		var counted int64
		leaves := d.differing(s)
		if len(leaves) == 0 {
			break
		}
		for _, l := range leaves {
			if !slices.Contains(allowed, l) {
				r.Status, r.Note = ChunkStatusStale, "a leaf the diff did not show now differs; run diff again"
				return
			}
		}
		dstRows := d.restrict(leaves).total().n
		if dstRows > 0 {
			var clearLeaves []uint32
			if !clearsWholeChunk(cd, leaves, s, d) {
				clearLeaves = leaves
			}
			err = inst.clear(ctx, cd.Id, dstPid, clearLeaves)
			if err != nil {
				r.Status, r.Note = ChunkStatusFailed, "unable to clear leaves on the target: "+err.Error()
				return
			}
			r.Cleared += dstRows
		}
		copied := s.restrict(leaves).total().n
		if copied > 0 {
			leafSpec := srcSpec
			if !coversAll(leaves, s, d) {
				leafSpec = srcSpec.ForLeaves(leaves)
			}
			var bytes uint64
			bytes, counted, err = relay(ctx, inst.src, inst.dst, &leafSpec, &inst.dstSpec, &inst.opts, copied)
			r.Bytes += bytes
			if err != nil {
				// The relay's error is what the operator needs to see; the
				// verification that would follow could only say the target
				// does not match.
				r.Note = err.Error()
				d, err = readLeafSet(ctx, inst.dst, &dstSpec)
				if err != nil {
					r.Status, r.Note = ChunkStatusFailed, err.Error()
					return
				}
				continue
			}
		}
		if inst.settle(ctx, &r, cd.Id, &srcSpec, &dstSpec, s, true, counted, srcTotal, copied) {
			return
		}
		d, err = readLeafSet(ctx, inst.dst, &dstSpec)
		if err != nil {
			r.Status, r.Note = ChunkStatusFailed, err.Error()
			return
		}
	}
	r.Status = ChunkStatusFailed
	return
}

// clearsWholeChunk reports whether repair may clear the chunk whole rather
// than by leaf: the diff showed it absent on the source, the source still
// holds none of it, and the leaves to clear cover every leaf the target holds
// rows in. A whole chunk can be dropped as a partition or truncated; a leaf
// left out of the clear would be lost, since only the listed leaves are
// copied back.
func clearsWholeChunk(cd *ChunkDiff, leaves []uint32, s leafSet, d leafSet) (whole bool) {
	return cd.AbsentOnSource && s.total().n == 0 && coversAll(leaves, s, d)
}

// coversAll reports whether leaves holds every leaf either side has rows in, so
// a leaf predicate would select the whole chunk anyway.
func coversAll(leaves []uint32, s leafSet, d leafSet) (all bool) {
	for l, v := range s {
		if v.n > 0 && !slices.Contains(leaves, l) {
			return false
		}
	}
	for l, v := range d {
		if v.n > 0 && !slices.Contains(leaves, l) {
			return false
		}
	}
	return true
}

// differingAllowed returns the leaves repair may clear in a chunk, or nil when
// the chunk is not as the diff showed it. A chunk the diff showed absent on
// the target allows every leaf while the target still holds none of it; one
// shown absent on the source, while the source still holds none and the
// target the row count the diff recorded. Otherwise each currently differing
// leaf must be a listed one with the target row count the diff recorded.
func (inst *tableSyncer) differingAllowed(cd *ChunkDiff, shown map[uint32]uint64, s leafSet, d leafSet) (allowed []uint32) {
	leaves := d.differing(s)
	if len(leaves) == 0 {
		return nil
	}
	switch {
	case cd.AbsentOnTarget:
		if d.total().n != 0 {
			return nil
		}
		return leaves
	case cd.AbsentOnSource:
		if s.total().n != 0 || d.total().n != cd.DstRows {
			return nil
		}
		return leaves
	}
	for _, l := range leaves {
		rows, listed := shown[l]
		if !listed || d[l].n != rows {
			return nil
		}
	}
	return leaves
}

// doneRows is what a chunk an earlier call verified adds to the progress
// count, in the measure [ExpectedRows] counts: a repair expects only the
// rows of the differing leaves, not the whole chunk the result reports.
func doneRows(pt *PlanTable, r ChunkResult) (rows uint64) {
	if pt.Sync == nil || pt.Sync.Mode != SyncModeRepair || pt.Diff == nil {
		return r.Rows
	}
	for i := range pt.Diff.Differing {
		if cd := &pt.Diff.Differing[i]; cd.Id == r.Chunk {
			return cd.repairCopyRows()
		}
	}
	return 0
}
