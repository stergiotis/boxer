package trailviewer

import (
	"context"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/trailviewer/verdict"
)

// The window's caches (ADR-0350 §SD3, proposed), each filled by a bgjob
// worker and adopted by a later frame, which never waits:
//
//   - the table: the case's rows, the strip's buckets and the facets,
//     refilled when the case, the window or the filter change and on the
//     refresh interval, and extended by the cursor when the person reaches
//     the end of what is held;
//   - the verdicts: one line per writer run among the rows, read once per
//     refill at most and at most every verdictEvery;
//   - the detail: the selected row's context, and its previous state.
//
// Everything here but the workers' reads runs on the render goroutine.

const (
	readTimeout = 20 * time.Second
	scopeSettle = 300 * time.Millisecond
	// verdictRuns bounds how many writer runs get a verdict line.
	verdictRuns  = 8
	verdictEvery = 30 * time.Second
)

// tableFill is one read of the table.
type tableFill struct {
	scope    scope
	from, to time.Time
	rows     []Row
	// more says the read was full, so older rows may follow.
	more bool
	// extend marks a cursor read whose rows follow the held ones.
	extend  bool
	buckets []Bucket
	step    time.Duration
	facets  [][]FacetValue
	at      time.Time
}

// tableCache is the rows a window holds. rows is replaced whole, never
// written in place, so a snapshot may share it.
type tableCache struct {
	runner bgjob.Runner[tableFill]

	rows        []Row
	more        bool
	scope       scope
	from, to    time.Time
	buckets     []Bucket
	step        time.Duration
	facets      [][]FacetValue
	filled      bool
	refreshedAt time.Time
	gen         uint64
	lastErr     error

	inFlight   scope
	started    time.Time
	want       scope
	wantSince  time.Time
	refreshNow bool
	// extendNow asks for the rows after the held ones.
	extendNow bool
}

// readSpec is what the worker needs besides the scope.
type readSpec struct {
	c       Case
	filter  Filter
	buckets int
}

// tick adopts a finished read and starts the next one.
func (inst *tableCache) tick(now time.Time, src *Source, want scope, spec readSpec, newProbe func() (*probe, error)) {
	if res, _, ok := inst.runner.TakeResult(); ok && res.scope == want {
		if res.extend {
			if inst.scope == want {
				inst.rows = appendNew(inst.rows, res.rows)
				inst.more = res.more
				inst.gen++
			}
		} else {
			inst.rows, inst.more, inst.scope, inst.from, inst.to = res.rows, res.more, res.scope, res.from, res.to
			inst.buckets, inst.step, inst.facets = res.buckets, res.step, res.facets
			inst.filled, inst.refreshedAt, inst.lastErr = true, res.at, nil
			inst.gen++
		}
	}
	if snap := inst.runner.Snapshot(); snap.State == bgjob.StateFailed {
		inst.lastErr = snap.Err
	}
	if !src.persisted() {
		return
	}
	if want != inst.want {
		inst.want, inst.wantSince = want, now
	}
	running := inst.runner.Running()
	switch {
	case !inst.filled || inst.scope != want:
		if running && inst.inFlight == want {
			return
		}
		if inst.lastErr != nil && inst.inFlight == want && now.Sub(inst.started) < refreshInterval() {
			return
		}
		if inst.filled && now.Sub(inst.wantSince) < scopeSettle {
			return
		}
		inst.start(now, want, spec, false, newProbe)
	case running:
	case inst.extendNow && inst.more:
		inst.start(now, want, spec, true, newProbe)
	case inst.refreshNow || now.Sub(inst.started) >= refreshInterval():
		inst.start(now, want, spec, false, newProbe)
	}
}

// start supersedes any read in flight. A refill reads at least as many
// rows as are held, so a refresh does not take back what the cursor added;
// an extension reads the next cap rows after the oldest held.
func (inst *tableCache) start(now time.Time, want scope, spec readSpec, extend bool, newProbe func() (*probe, error)) {
	if inst.runner.Running() {
		inst.runner.Invalidate()
	}
	var from, to time.Time
	q := listQuery{Case: spec.c, Filter: spec.filter}
	if extend {
		from, to = inst.from, inst.to
		last := inst.rows[len(inst.rows)-1]
		q.Cursor = &Cursor{TsNs: last.TsNs, Id: last.Id}
		q.Limit = rowCap()
	} else {
		from, to = want.Window.Resolve(now)
		q.Limit = rowCap()
		if inst.scope == want {
			q.Limit = min(max(q.Limit, len(inst.rows)), maxRows)
		}
	}
	q.From, q.To = from, to
	inst.inFlight, inst.started, inst.refreshNow, inst.extendNow = want, now, false, false
	buckets, by := spec.buckets, want.Strip
	inst.runner.Start(nil, bgjob.Spec{Kind: "trailviewer.read", Title: "Trail viewer read"},
		func(ctx context.Context) (out *tableFill, err error) {
			ctx, cancel := context.WithTimeout(ctx, readTimeout)
			defer cancel()
			p, err := newProbe()
			if err != nil {
				return
			}
			rows, err := p.listRows(ctx, q)
			if err != nil {
				return
			}
			out = &tableFill{scope: want, from: from, to: to, rows: rows, more: len(rows) == q.Limit, extend: extend, at: time.Now()}
			if extend {
				return
			}
			out.step = stepFor(to.Sub(from), buckets)
			if out.buckets, err = p.strip(ctx, q, out.step, by); err != nil {
				return nil, err
			}
			out.facets = make([][]FacetValue, len(AllFacets))
			for i, f := range AllFacets {
				if out.facets[i], err = p.facet(ctx, f, q.Case, from, to); err != nil {
					return nil, err
				}
			}
			return
		})
}

// appendNew appends the rows of more that held does not already have, by
// facts id, keeping held's order.
func appendNew(held []Row, more []Row) (out []Row) {
	seen := make(map[uint64]bool, len(held))
	for i := range held {
		seen[held[i].Id] = true
	}
	out = slices.Clone(held)
	for i := range more {
		if !seen[more[i].Id] {
			out = append(out, more[i])
		}
	}
	return
}

func (inst *tableCache) close() { inst.runner.Invalidate() }

// verdictFill is one read of the verdict lines.
type verdictFill struct {
	runs  []string
	lines []verdict.Line
	at    time.Time
}

type verdictCache struct {
	runner  bgjob.Runner[verdictFill]
	runs    []string
	lines   []verdict.Line
	at      time.Time
	started time.Time
	asked   []string
}

// runsOf is the distinct writer runs among rows, in order of appearance,
// at most verdictRuns.
func runsOf(rows []Row) (runs []string) {
	for i := range rows {
		r := rows[i].Run
		if !slices.Contains(runs, r) {
			runs = append(runs, r)
			if len(runs) == verdictRuns {
				return
			}
		}
	}
	return
}

// tick adopts a finished read and starts one when the runs changed or the
// lines are older than verdictEvery.
func (inst *verdictCache) tick(now time.Time, src *Source, runs []string) {
	if res, _, ok := inst.runner.TakeResult(); ok {
		inst.runs, inst.lines, inst.at = res.runs, res.lines, res.at
	}
	if !src.persisted() || inst.runner.Running() || len(runs) == 0 {
		return
	}
	if slices.Equal(runs, inst.asked) && now.Sub(inst.started) < verdictEvery {
		return
	}
	inst.asked, inst.started = slices.Clone(runs), now
	asked := inst.asked
	inst.runner.Start(nil, bgjob.Spec{Kind: "trailviewer.verdict", Title: "Trail viewer verdicts"},
		func(ctx context.Context) (out *verdictFill, err error) {
			ctx, cancel := context.WithTimeout(ctx, readTimeout)
			defer cancel()
			out = &verdictFill{runs: asked, lines: readVerdicts(ctx, src, asked), at: time.Now()}
			return
		})
}

// line is the run's verdict, unverified when none was read.
func (inst *verdictCache) line(run string) (l verdict.Line) {
	for _, x := range inst.lines {
		if x.Run == run {
			return x
		}
	}
	return verdict.Line{Run: run, Reason: "not read yet"}
}

func (inst *verdictCache) close() { inst.runner.Invalidate() }

// readVerdicts is one line per run: the current run's from
// keelson('evidence_checks'), every other run's from the recorder's seal
// verification.
func readVerdicts(ctx context.Context, src *Source, runs []string) (lines []verdict.Line) {
	current := src.run()
	var checks []verdict.Check
	var checksOk bool
	var checksErr error
	for _, run := range runs {
		if run == "" {
			lines = append(lines, verdict.Line{Run: run, Reason: "the rows carry no run"})
			continue
		}
		if run == current {
			if checks == nil && checksErr == nil {
				checks, checksOk, checksErr = src.evidenceChecks()
			}
			if checksErr != nil {
				lines = append(lines, verdict.Line{Run: run, Reason: "evidence_checks: " + checksErr.Error()})
				continue
			}
			if checksOk {
				if l, ok := verdict.FromChecks(run, checks); ok {
					lines = append(lines, l)
					continue
				}
			}
		}
		if src.Trail == nil {
			lines = append(lines, verdict.Line{Run: run, Reason: "the host registered no recorder"})
			continue
		}
		if why := src.Trail.NotDurable(); why != "" {
			lines = append(lines, verdict.Line{Run: run, Reason: why})
			continue
		}
		broken, unnamed, unanchored, err := src.Trail.VerifySeals(ctx, run)
		lines = append(lines, verdict.FromSeals(run, broken, unnamed, unanchored, err))
	}
	return
}

// detailFill is the selected row's per-row reads.
type detailFill struct {
	id       uint64
	before   []Row
	after    []Row
	previous *Previous
	err      error
}

type detailCache struct {
	runner bgjob.Runner[detailFill]
	id     uint64
	fill   *detailFill
	asked  uint64
	has    bool
}

// tick adopts a finished read and starts one for the selected row.
func (inst *detailCache) tick(src *Source, sel *Row, newProbe func() (*probe, error)) {
	if res, _, ok := inst.runner.TakeResult(); ok && sel != nil && res.id == sel.Id {
		inst.fill, inst.id, inst.has = res, res.id, true
	}
	if sel == nil {
		inst.has, inst.fill, inst.asked = false, nil, 0
		return
	}
	if !src.persisted() || inst.asked == sel.Id {
		return
	}
	if inst.runner.Running() {
		inst.runner.Invalidate()
	}
	inst.asked = sel.Id
	row := *sel
	look := lookback()
	inst.runner.Start(nil, bgjob.Spec{Kind: "trailviewer.detail", Title: "Trail viewer row"},
		func(ctx context.Context) (out *detailFill, err error) {
			ctx, cancel := context.WithTimeout(ctx, readTimeout)
			defer cancel()
			out = &detailFill{id: row.Id}
			p, perr := newProbe()
			if perr != nil {
				out.err = perr
				return
			}
			out.before, out.after, out.err = p.contextOf(ctx, &row, look)
			if out.err != nil {
				return
			}
			out.previous, out.err = p.previousOf(ctx, &row, look)
			return
		})
}

func (inst *detailCache) current(id uint64) (f *detailFill) {
	if inst.has && inst.id == id {
		f = inst.fill
	}
	return
}

func (inst *detailCache) close() { inst.runner.Invalidate() }

// previousOf is SD5's read for one row: an audit event with an entity, or
// an agent grant with a task; nil for any other row.
func (inst *probe) previousOf(ctx context.Context, r *Row, look time.Duration) (p *Previous, err error) {
	switch {
	case r.Kind == kindAuditEvent && r.Audit != nil && r.Audit.Entity != "":
		cur := r.Audit
		var prev []AuditDetail
		prev, err = queryRows[AuditDetail](ctx, inst, inst.marts.previousAuditSQL(cur, look))
		if err != nil {
			return
		}
		since := cur.Ts().Add(-look)
		var prevRow *AuditDetail
		if len(prev) > 0 {
			prevRow = &prev[0]
			since = prevRow.Ts()
		}
		var gaps []AuditDetail
		gaps, err = queryRows[AuditDetail](ctx, inst, inst.marts.gapsSQL(cur.Run, since, cur.Ts().Add(look)))
		if err != nil {
			return
		}
		v := judgeAudit(cur, prevRow, gaps, look)
		p = &v
	case r.Kind == kindAgentGrant && r.Task != "":
		var pair []GrantDetail
		pair, err = queryRows[GrantDetail](ctx, inst, inst.marts.grantPairSQL(r, look))
		if err != nil || len(pair) == 0 || pair[0].Id != r.Id {
			return
		}
		var prev *GrantDetail
		if len(pair) > 1 {
			prev = &pair[1]
		}
		v := judgeGrant(&pair[0], prev, look)
		p = &v
	}
	return
}
