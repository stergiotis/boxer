package jackstay

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SyncModeE is what a sync copies of a table (ADR-0259 §SD5).
type SyncModeE uint8

const (
	// SyncModeFull copies every chunk.
	SyncModeFull SyncModeE = iota
	// SyncModeRepair makes the target's differing leaves equal to the
	// source's, and only those the plan's diff showed.
	SyncModeRepair
	// SyncModeSample copies SampleNum of every SampleDen keys.
	SyncModeSample
)

var AllSyncModes = []SyncModeE{SyncModeFull, SyncModeRepair, SyncModeSample}

func (inst SyncModeE) String() (s string) {
	switch inst {
	case SyncModeFull:
		return "full"
	case SyncModeRepair:
		return "repair"
	case SyncModeSample:
		return "sample"
	}
	return "invalid"
}

func (inst SyncModeE) MarshalText() (text []byte, err error) {
	s := inst.String()
	if s == "invalid" {
		err = eb.Build().Uint8("mode", uint8(inst)).Errorf("invalid sync mode")
		return
	}
	return []byte(s), nil
}

func (inst *SyncModeE) UnmarshalText(text []byte) (err error) {
	for _, m := range AllSyncModes {
		if m.String() == string(text) {
			*inst = m
			return
		}
	}
	return eb.Build().Str("mode", string(text)).Errorf("unknown sync mode")
}

// ExistingPolicyE is what a full or sample sync does with a target table that
// already holds rows when the run begins.
type ExistingPolicyE uint8

const (
	// ExistingPolicyRefuse stops before copying anything.
	ExistingPolicyRefuse ExistingPolicyE = iota
	// ExistingPolicyAppend inserts beside the existing rows. The run does not
	// own them, so a chunk that fails verification cannot be retried.
	ExistingPolicyAppend
	// ExistingPolicyReplace clears each target chunk before copying it.
	ExistingPolicyReplace
)

var AllExistingPolicies = []ExistingPolicyE{ExistingPolicyRefuse, ExistingPolicyAppend, ExistingPolicyReplace}

func (inst ExistingPolicyE) String() (s string) {
	switch inst {
	case ExistingPolicyRefuse:
		return "refuse"
	case ExistingPolicyAppend:
		return "append"
	case ExistingPolicyReplace:
		return "replace"
	}
	return "invalid"
}

func (inst ExistingPolicyE) MarshalText() (text []byte, err error) {
	s := inst.String()
	if s == "invalid" {
		err = eb.Build().Uint8("policy", uint8(inst)).Errorf("invalid existing-rows policy")
		return
	}
	return []byte(s), nil
}

func (inst *ExistingPolicyE) UnmarshalText(text []byte) (err error) {
	for _, p := range AllExistingPolicies {
		if p.String() == string(text) {
			*inst = p
			return
		}
	}
	return eb.Build().Str("policy", string(text)).Errorf("unknown existing-rows policy")
}

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

// ChunkStatusE is the outcome of one chunk.
type ChunkStatusE uint8

const (
	// ChunkStatusCopied: copied and verified in this call.
	ChunkStatusCopied ChunkStatusE = iota
	// ChunkStatusDone: verified by an earlier call of the same run, and the
	// source has not moved since.
	ChunkStatusDone
	// ChunkStatusIdentical: repair found nothing to do.
	ChunkStatusIdentical
	// ChunkStatusStale: repair found the target no longer as the plan's diff
	// showed it, and touched nothing.
	ChunkStatusStale
	// ChunkStatusFailed: the chunk could not be copied and verified.
	ChunkStatusFailed
)

var AllChunkStatuses = []ChunkStatusE{ChunkStatusCopied, ChunkStatusDone, ChunkStatusIdentical, ChunkStatusStale, ChunkStatusFailed}

func (inst ChunkStatusE) String() (s string) {
	switch inst {
	case ChunkStatusCopied:
		return "copied"
	case ChunkStatusDone:
		return "done"
	case ChunkStatusIdentical:
		return "identical"
	case ChunkStatusStale:
		return "stale"
	case ChunkStatusFailed:
		return "failed"
	}
	return "invalid"
}

// ChunkResult reports one chunk.
type ChunkResult struct {
	Table    string       `json:"table"`
	Chunk    string       `json:"chunk"`
	Display  string       `json:"display,omitempty"`
	Status   ChunkStatusE `json:"-"`
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
	// verification (ADR-0259 §SD6).
	Rows *atomic.Int64
	// PollPeriod is how often Rows is refreshed.
	PollPeriod time.Duration
	// Compression is the HTTP content encoding the relay asks the source
	// for ("zstd", "gzip"; empty for none). The compressed bytes are passed
	// to the target as they are, never decoded in between.
	Compression string
	// BeforeChunk, when set, runs before each chunk; the disk floor wait
	// lives here. An error stops the table's sync.
	BeforeChunk func(ctx context.Context, pt *PlanTable) (err error)
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
	ls = make(leafSet, 16)
	for _, c := range chunks {
		for l, d := range c.leaves {
			ls[l] = ls[l].plus(d)
		}
	}
	return
}

type countingReader struct {
	r     io.Reader
	n     int64
	total *atomic.Int64
}

func (inst *countingReader) Read(p []byte) (n int, err error) {
	n, err = inst.r.Read(p)
	inst.n += int64(n)
	if inst.total != nil {
		inst.total.Add(int64(n))
	}
	return
}

// relay streams the Native rows of srcSpec from src into dstSpec's table on
// dst, undecoded and, when the source compressed them, still compressed. It
// returns the bytes that crossed and the rows it counted into opts.Rows, so a
// caller whose verification fails can wind them back.
func relay(ctx context.Context, src ClientI, dst ClientI, srcSpec *DigestSpec, dstSpec *DigestSpec, opts *SyncOptions, expectedRows uint64) (bytes uint64, counted int64, err error) {
	var queryId string
	queryId, err = gonanoid.New()
	if err != nil {
		err = eh.Errorf("unable to mint a query id: %w", err)
		return
	}
	queryId = "jackstay-" + queryId
	var body io.ReadCloser
	var encoding string
	body, encoding, err = src.QueryStream(ctx, srcSpec.SelectNative(), chclient.StreamOptions{AcceptEncoding: opts.Compression})
	if err != nil {
		err = eh.Errorf("unable to read source rows: %w", err)
		return
	}
	defer func() { _ = body.Close() }()
	stop := make(chan struct{})
	seenCh := make(chan int64, 1)
	go func() {
		seenCh <- pollWrittenRows(ctx, dst, queryId, max(opts.PollPeriod, 100*time.Millisecond), opts.Rows, stop)
	}()
	cr := &countingReader{r: body, total: opts.Bytes}
	err = dst.InsertStream(ctx, dstSpec.InsertNative(), cr, chclient.StreamOptions{QueryId: queryId, ContentEncoding: encoding})
	close(stop)
	counted = <-seenCh
	bytes = uint64(cr.n)
	if err != nil {
		if opts.Rows != nil {
			opts.Rows.Add(-counted)
		}
		counted = 0
		err = eh.Errorf("unable to insert rows on the target: %w", err)
		return
	}
	if opts.Rows != nil {
		opts.Rows.Add(int64(expectedRows) - counted)
	}
	counted = int64(expectedRows)
	return
}

func (inst *tableSyncer) unwind(counted int64) {
	if inst.opts.Rows != nil && counted != 0 {
		inst.opts.Rows.Add(-counted)
	}
}

func isMergeTreeEngine(engine string) (ok bool) {
	return strings.HasSuffix(engine, "MergeTree")
}

// tableSyncer carries one table's sync state across its chunks.
type tableSyncer struct {
	src, dst     ClientI
	pt           *PlanTable
	srcSpec      DigestSpec
	dstSpec      DigestSpec
	sameKey      bool
	owned        bool
	journal      *Journal
	opts         SyncOptions
	now          func() time.Time
	sampleFilter string
}

// SyncTable copies one plan table per its [TableSync] (ADR-0259 §SD5). Each
// chunk is copied, then its target digest is compared with what it kept plus
// what was copied; only a verified chunk enters the journal. A chunk the run
// owns is cleared and copied again on a mismatch, up to MaxAttempts.
//
// The table must be diffable (its DDL applied), have a chunk layout, and, for
// repair, a diff: repair touches only the leaves that diff showed, and only
// while the target still holds what it showed.
func SyncTable(ctx context.Context, src ClientI, dst ClientI, pt *PlanTable, j *Journal, opts SyncOptions, now func() time.Time) (rep TableSyncReport, err error) {
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
	if pt.Sync.Mode == SyncModeSample {
		ts.sampleFilter = ts.srcSpec.SamplePredicate(pt.Sync.SampleNum, pt.Sync.SampleDen)
	}
	table := pt.Source.String()
	rep.RunId = j.run

	owned, started := j.Started(table)
	if !started {
		owned, err = ts.claim(ctx)
		if err != nil {
			return
		}
		err = j.RecordStart(table, owned, now())
		if err != nil {
			return
		}
	}
	ts.owned = owned

	if pt.Sync.Mode == SyncModeRepair {
		for i := range pt.Diff.Differing {
			if err = ctx.Err(); err != nil {
				return
			}
			if opts.BeforeChunk != nil {
				if err = opts.BeforeChunk(ctx, pt); err != nil {
					return
				}
			}
			r := ts.repairChunk(ctx, &pt.Diff.Differing[i])
			rep.add(r)
			if opts.Progress != nil {
				opts.Progress(r)
			}
		}
	} else {
		var chunks []chunkListRow
		chunks, err = queryRows[chunkListRow](ctx, src, ts.srcSpec.ChunkListQuery())
		if err != nil {
			err = eb.Build().Str("table", table).Errorf("unable to list source chunks: %w", err)
			return
		}
		for _, c := range chunks {
			if err = ctx.Err(); err != nil {
				return
			}
			if opts.BeforeChunk != nil {
				if err = opts.BeforeChunk(ctx, pt); err != nil {
					return
				}
			}
			r := ts.copyChunk(ctx, c)
			rep.add(r)
			if opts.Progress != nil {
				opts.Progress(r)
			}
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
	N       uint64 `json:"n"`
}

// claim decides, when a run first reaches a table, whether it owns the
// target's rows: it does when the target is empty or the policy is replace. A
// full or sample sync into a non-empty target is refused unless the policy
// says otherwise; repair owns only the leaves it was shown.
func (inst *tableSyncer) claim(ctx context.Context) (owned bool, err error) {
	var rows []countRow
	rows, err = queryRows[countRow](ctx, inst.dst, "SELECT count() AS n FROM "+QuoteRef(inst.pt.Target)+jsonSettings)
	if err != nil {
		err = eb.Build().Str("table", inst.pt.Target.String()).Errorf("unable to count target rows: %w", err)
		return
	}
	empty := len(rows) == 1 && rows[0].N == 0
	if inst.pt.Sync.Mode == SyncModeRepair {
		return empty, nil
	}
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

func (inst *tableSyncer) targetPid(srcPid string) (pid string) {
	if inst.sameKey {
		return srcPid
	}
	return ""
}

// clear removes rows of a target chunk: all of it (leaves nil) or the given
// leaves. A whole partition chunk whose partition key matches the source's is
// dropped as a partition; anything else is a lightweight DELETE, and a table
// outside the MergeTree family can only be truncated whole.
func (inst *tableSyncer) clear(ctx context.Context, chunk string, pid string, leaves []uint32) (err error) {
	target := QuoteRef(inst.pt.Target)
	if !isMergeTreeEngine(inst.pt.TargetEngine) {
		if leaves != nil || inst.pt.Chunking.Kind != ChunkingSingle {
			return eb.Build().Str("engine", inst.pt.TargetEngine).Errorf("target engine cannot delete a subset of rows")
		}
		return inst.dst.Exec(ctx, "TRUNCATE TABLE "+target)
	}
	if leaves == nil && inst.pt.Chunking.Kind == ChunkingPartition && inst.sameKey && pid != "" {
		return inst.dst.Exec(ctx, "ALTER TABLE "+target+" DROP PARTITION ID "+QuoteString(pid))
	}
	if leaves == nil && inst.pt.Chunking.Kind == ChunkingSingle {
		return inst.dst.Exec(ctx, "TRUNCATE TABLE "+target)
	}
	where := inst.pt.Chunking.ChunkPredicate(chunk, "")
	if leaves != nil {
		where = "(" + where + ") AND " + inst.dstSpec.LeafPredicate(leaves)
	}
	return inst.dst.Exec(ctx, "DELETE FROM "+target+" WHERE "+where+" SETTINGS lightweight_deletes_sync = 2")
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
	sf, df := *srcSpec, *dstSpec
	sf.Final = IsMergeEngine(inst.pt.Engine)
	df.Final = IsMergeEngine(inst.pt.TargetEngine)
	var s, d leafSet
	s, d, err = both(func(side int) (leafSet, error) {
		if side == 0 {
			return readLeafSet(ctx, inst.src, &sf)
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
	srcSpec := inst.srcSpec.With(inst.pt.Chunking.ChunkPredicate(c.Chunk, c.Pid)).With(inst.sampleFilter)
	dstSpec := inst.dstSpec.With(inst.pt.Chunking.ChunkPredicate(c.Chunk, inst.targetPid(c.Pid)))

	srcSet, err := readLeafSet(ctx, inst.src, &srcSpec)
	if err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return
	}
	srcTotal := srcSet.total()
	if e, done := inst.journal.Done(table, c.Chunk); done {
		if e.N == srcTotal.n && e.Kd == srcTotal.kd && e.Rd == srcTotal.rd {
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
	for attempt := 1; attempt <= inst.opts.MaxAttempts; attempt++ {
		r.Attempts = attempt
		var counted int64
		before, err := readLeafSet(ctx, inst.dst, &dstSpec)
		if err != nil {
			r.Status, r.Note = ChunkStatusFailed, err.Error()
			return
		}
		if inst.owned && before.total().n > 0 {
			err = inst.clear(ctx, c.Chunk, c.Pid, nil)
			if err != nil {
				r.Status, r.Note = ChunkStatusFailed, "unable to clear the target chunk: "+err.Error()
				return
			}
			r.Cleared += before.total().n
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
		var ok bool
		ok, r.Note, err = inst.verify(ctx, &srcSpec, &dstSpec, before.plus(srcSet), before.total().n == 0)
		if err != nil || !ok {
			inst.unwind(counted)
			counted = 0
		}
		if err != nil {
			r.Status, r.Note = ChunkStatusFailed, err.Error()
			return
		}
		if ok {
			err = inst.journal.RecordChunk(table, c.Chunk, inst.pt.Sync.Mode, srcTotal, inst.now())
			if err != nil {
				r.Status, r.Note = ChunkStatusFailed, err.Error()
				return
			}
			r.Status, r.Rows = ChunkStatusCopied, srcTotal.n
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

// repairChunk makes a target chunk's differing leaves equal to the source's.
// It first checks the target against the plan's diff: every leaf it would
// clear must be one the diff listed, holding the row count the diff showed.
// Otherwise it touches nothing and reports the chunk stale.
func (inst *tableSyncer) repairChunk(ctx context.Context, cd *ChunkDiff) (r ChunkResult) {
	table := inst.pt.Source.String()
	r = ChunkResult{Table: table, Chunk: cd.Id, Display: cd.Display}
	srcSpec := inst.srcSpec.With(inst.pt.Chunking.ChunkPredicate(cd.Id, cd.SrcPid))
	// A target partition id names the same rows as the source's chunk only
	// when both tables partition by the same key.
	dstPid := ""
	if inst.sameKey {
		dstPid = cd.DstPid
		if dstPid == "" {
			dstPid = cd.SrcPid
		}
	}
	dstSpec := inst.dstSpec.With(inst.pt.Chunking.ChunkPredicate(cd.Id, dstPid))

	s, d, err := both(func(side int) (leafSet, error) {
		if side == 0 {
			return readLeafSet(ctx, inst.src, &srcSpec)
		}
		return readLeafSet(ctx, inst.dst, &dstSpec)
	})
	if err != nil {
		r.Status, r.Note = ChunkStatusFailed, err.Error()
		return
	}
	srcTotal := s.total()
	if e, done := inst.journal.Done(table, cd.Id); done && e.N == srcTotal.n && e.Kd == srcTotal.kd && e.Rd == srcTotal.rd && s.equal(d) {
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
			if !cd.AbsentOnSource || len(leaves) < len(d) {
				clearLeaves = leaves
			}
			err = inst.clear(ctx, cd.Id, dstPid, clearLeaves)
			if err != nil {
				r.Status, r.Note = ChunkStatusFailed, "unable to clear leaves on the target: "+err.Error()
				return
			}
			r.Cleared += dstRows
		}
		if s.restrict(leaves).total().n > 0 {
			leafSpec := srcSpec
			if !coversAll(leaves, s, d) {
				leafSpec = srcSpec.With(inst.srcSpec.LeafPredicate(leaves))
			}
			var bytes uint64
			bytes, counted, err = relay(ctx, inst.src, inst.dst, &leafSpec, &inst.dstSpec, &inst.opts, s.restrict(leaves).total().n)
			r.Bytes += bytes
			if err != nil {
				r.Note = err.Error()
			}
			r.Rows += s.restrict(leaves).total().n
		}
		var ok bool
		ok, r.Note, err = inst.verify(ctx, &srcSpec, &dstSpec, s, true)
		if err != nil || !ok {
			inst.unwind(counted)
			counted = 0
		}
		if err != nil {
			r.Status, r.Note = ChunkStatusFailed, err.Error()
			return
		}
		if ok {
			err = inst.journal.RecordChunk(table, cd.Id, SyncModeRepair, srcTotal, inst.now())
			if err != nil {
				r.Status, r.Note = ChunkStatusFailed, err.Error()
				return
			}
			r.Status = ChunkStatusCopied
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
// the target is not as the diff showed it. A chunk absent on either side in
// the diff allows every leaf; otherwise each currently differing leaf must be
// a listed one with the target row count the diff recorded.
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
		if d.total().n != cd.DstRows {
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
