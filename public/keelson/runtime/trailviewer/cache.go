package trailviewer

import (
	"context"
	"maps"
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
//     the end of what is held; with the rows, the rows the SQL exception
//     rules raise, the claim rule's judgments, the rows' review marks and,
//     for the entity case, the state as of the window's end (rules.go);
//   - the verdicts: one line per writer run among the rows, read once per
//     refill at most and at most every verdictEvery;
//   - the detail: the selected row's context, and its previous state.
//
// Everything here but the workers' reads runs on the render goroutine. The
// table read reports each statement as a step, so the progress row above
// the table is determinate; its lifecycle is projected onto one state
// machine in readstate.go, which never drives it.

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
	// resolvedAt is the instant the window was resolved at: to, for a last
	// window. at, when the read finished, is later.
	resolvedAt time.Time
	rows       []Row
	// more says the read was full, so older rows may follow.
	more bool
	// extend marks a cursor read whose rows follow the held ones.
	extend  bool
	buckets []Bucket
	step    time.Duration
	facets  [][]FacetValue
	at      time.Time
	// exceptions are the SQL rules' raised rows among rows, reviews their
	// latest marks, claims the claim rule's new judgments, asOf the entity
	// case's state as of the window's end.
	exceptions []Exception
	reviews    []Review
	claims     map[uint64]claimJudgment
	asOf       *AsOf
}

// tableCache is the rows a window holds. rows is replaced whole, never
// written in place, so a snapshot may share it.
type tableCache struct {
	runner bgjob.Runner[tableFill]

	rows     []Row
	more     bool
	scope    scope
	from, to time.Time
	// resolvedAt is the instant from and to were resolved at, the anchor a
	// question's last window resolves against to be compared with them.
	resolvedAt  time.Time
	buckets     []Bucket
	step        time.Duration
	facets      [][]FacetValue
	filled      bool
	refreshedAt time.Time
	gen         uint64
	lastErr     error
	exceptions  []Exception
	reviews     map[uint64]Review
	// claims is every judgment the claim rule made, kept across refills
	// so a row is read once.
	claims map[uint64]claimJudgment
	asOf   *AsOf

	inFlight scope
	started  time.Time
	// ended is when the last read finished, whatever its outcome; the
	// refresh interval counts from it, not from started, so a store slower
	// than the interval does not keep the window refreshing (ADR-0350
	// §SD12).
	ended      time.Time
	want       scope
	wantSince  time.Time
	refreshNow bool
	// extendNow asks for the rows after the held ones.
	extendNow bool
	// hold is a stop (ADR-0350 §SD12): tick starts nothing — no refill, no
	// interval refresh, no extension — until refresh or a scope change
	// clears it. A stop is not a failure, so lastErr is untouched.
	hold bool
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
				inst.exceptions = append(slices.Clip(inst.exceptions), res.exceptions...)
				inst.reviews = maps.Clone(inst.reviews)
				if inst.reviews == nil {
					inst.reviews = map[uint64]Review{}
				}
				for _, r := range res.reviews {
					inst.reviews[r.Id] = r
				}
				inst.mergeClaims(res.claims)
				inst.gen++
			}
		} else {
			inst.rows, inst.more, inst.scope, inst.from, inst.to = res.rows, res.more, res.scope, res.from, res.to
			inst.resolvedAt = res.resolvedAt
			inst.buckets, inst.step, inst.facets = res.buckets, res.step, res.facets
			inst.filled, inst.refreshedAt, inst.lastErr = true, res.at, nil
			inst.exceptions, inst.asOf = res.exceptions, res.asOf
			inst.reviews = make(map[uint64]Review, len(res.reviews))
			for _, r := range res.reviews {
				inst.reviews[r.Id] = r
			}
			inst.mergeClaims(res.claims)
			inst.gen++
		}
	}
	if snap := inst.runner.Snapshot(); snap.State == bgjob.StateFailed {
		inst.lastErr = snap.Err
	}
	if !inst.runner.Running() && inst.ended.Before(inst.started) {
		inst.ended = now
	}
	if !src.persisted() {
		return
	}
	if want != inst.want {
		inst.want, inst.wantSince, inst.hold = want, now, false
	}
	if inst.hold {
		return
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
	case inst.refreshNow || now.Sub(inst.ended) >= refreshInterval():
		inst.start(now, want, spec, false, newProbe)
	}
}

// stop cancels the read in flight and holds the cache: nothing starts
// until refresh or a scope change. It sets and never toggles. The runner
// finishes a cancelled compute as idle, not failed (bgjob's
// finishCancelled), so lastErr stays as it was and the state reads
// stopped.
func (inst *tableCache) stop() {
	inst.hold, inst.refreshNow, inst.extendNow = true, false, false
	inst.runner.Cancel()
}

// refresh asks for the next read at once and clears a hold.
func (inst *tableCache) refresh() {
	inst.refreshNow, inst.hold = true, false
}

// readPlan is one read's parameters, fixed when it starts: what the worker
// needs besides the probe.
type readPlan struct {
	want                 scope
	q                    listQuery
	from, to, resolvedAt time.Time
	extend               bool
	buckets              int
	by                   StripByE
	look                 time.Duration
	judged               map[uint64]bool
}

// The notes the read reports its steps under, one per statement.
const (
	stepRows       = "rows"
	stepExceptions = "exceptions"
	stepReviews    = "reviews"
	stepClaims     = "claims"
	stepAsOf       = "as-of"
	stepStrip      = "strip"
	stepFacet      = "facet: "
)

// steps is how many statements the read runs at most: an extension reads
// the rows and what the rules say of them, a refill the strip, the as-of
// state and the facets too.
func (inst readPlan) steps() (n uint64) {
	n = 4
	if !inst.extend {
		n += 2 + uint64(len(AllFacets))
	}
	return
}

// run is the read: each statement a step reported before it runs, so the
// progress bar is determinate by step and the ETA follows. Steps a read
// has nothing for — the rules over no rows — count as done without a
// report.
func (inst readPlan) run(ctx context.Context, report bgjob.Reporter, p *probe) (out *tableFill, err error) {
	q, total, done := inst.q, inst.steps(), uint64(0)
	step := func(note string) {
		report(done, total, note)
		done++
	}
	step(stepRows)
	rows, err := p.listRows(ctx, q)
	if err != nil {
		return
	}
	out = &tableFill{scope: inst.want, from: inst.from, to: inst.to, resolvedAt: inst.resolvedAt, rows: rows, more: len(rows) == q.Limit, extend: inst.extend, at: time.Now()}
	if len(rows) > 0 {
		// The rules and the marks cover the rows read: older than the
		// cursor, and no older than the last row when more follow.
		var lower *Cursor
		if out.more {
			last := rows[len(rows)-1]
			lower = &Cursor{TsNs: last.TsNs, Id: last.Id}
		}
		step(stepExceptions)
		if out.exceptions, err = queryRows[Exception](ctx, p, p.marts.exceptionsSQL(q, lower, inst.look)); err != nil {
			return nil, err
		}
		step(stepReviews)
		if out.reviews, err = queryRows[Review](ctx, p, p.marts.reviewsSQL(q, lower)); err != nil {
			return nil, err
		}
		step(stepClaims)
		if out.claims, err = p.judgeClaims(ctx, rows, inst.judged, inst.look); err != nil {
			return nil, err
		}
	} else {
		done += 3
	}
	if !inst.extend {
		step(stepAsOf)
		if out.asOf, err = p.readAsOf(ctx, q, inst.look); err != nil {
			return nil, err
		}
		out.step = stepFor(inst.to.Sub(inst.from), inst.buckets)
		step(stepStrip)
		if out.buckets, err = p.strip(ctx, q, out.step, inst.by); err != nil {
			return nil, err
		}
		out.facets = make([][]FacetValue, len(AllFacets))
		for i, f := range AllFacets {
			step(stepFacet + f.String())
			if out.facets[i], err = p.facet(ctx, f, q.Case, inst.from, inst.to); err != nil {
				return nil, err
			}
		}
	}
	report(total, total, "")
	return
}

// start supersedes any read in flight. A refill reads at least as many
// rows as are held, so a refresh does not take back what the cursor added;
// an extension reads the next cap rows after the oldest held.
func (inst *tableCache) start(now time.Time, want scope, spec readSpec, extend bool, newProbe func() (*probe, error)) {
	if inst.runner.Running() {
		inst.runner.Invalidate()
	}
	pl := readPlan{want: want, extend: extend, buckets: spec.buckets, by: want.Strip, look: lookback()}
	pl.q = listQuery{Case: spec.c, Filter: spec.filter}
	if extend {
		pl.from, pl.to, pl.resolvedAt = inst.from, inst.to, inst.resolvedAt
		last := inst.rows[len(inst.rows)-1]
		pl.q.Cursor = &Cursor{TsNs: last.TsNs, Id: last.Id}
		pl.q.Limit = rowCap()
	} else {
		pl.from, pl.to = want.Window.Resolve(now)
		pl.resolvedAt = now
		pl.q.Limit = rowCap()
		if inst.scope == want {
			pl.q.Limit = min(max(pl.q.Limit, len(inst.rows)), maxRows)
		}
	}
	pl.q.From, pl.q.To = pl.from, pl.to
	inst.inFlight, inst.started, inst.refreshNow, inst.extendNow = want, now, false, false
	pl.judged = make(map[uint64]bool, len(inst.claims))
	for id := range inst.claims {
		pl.judged[id] = true
	}
	inst.runner.StartReporting(nil, bgjob.Spec{Kind: "trailviewer.read", Title: "Trail viewer read"},
		func(ctx context.Context, report bgjob.Reporter) (out *tableFill, err error) {
			ctx, cancel := context.WithTimeout(ctx, readTimeout)
			defer cancel()
			p, err := newProbe()
			if err != nil {
				return
			}
			return pl.run(ctx, report, p)
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

// mergeClaims adds a fill's judgments to the ones held, forgetting them
// all past maxJudged.
func (inst *tableCache) mergeClaims(add map[uint64]claimJudgment) {
	if len(add) == 0 {
		return
	}
	if inst.claims == nil || len(inst.claims)+len(add) > maxJudged {
		inst.claims = make(map[uint64]claimJudgment, len(add))
	} else {
		inst.claims = maps.Clone(inst.claims)
	}
	maps.Copy(inst.claims, add)
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
	// retryAt is when a read of asked that failed is started again; zero
	// while none failed. The runner reports a failed read as done, with
	// the error in the fill, so nothing else would start it again while
	// the row stays selected.
	retryAt time.Time
	has     bool
}

// tick adopts a finished read and starts one for the selected row, and
// again one refresh interval after a read of it failed.
func (inst *detailCache) tick(now time.Time, src *Source, sel *Row, newProbe func() (*probe, error)) {
	if res, _, ok := inst.runner.TakeResult(); ok && sel != nil && res.id == sel.Id {
		inst.fill, inst.id, inst.has = res, res.id, true
		inst.retryAt = time.Time{}
		if res.err != nil {
			inst.retryAt = now.Add(refreshInterval())
		}
	}
	if sel == nil {
		inst.has, inst.fill, inst.asked, inst.retryAt = false, nil, 0, time.Time{}
		return
	}
	if !src.persisted() {
		return
	}
	if inst.asked == sel.Id && (inst.retryAt.IsZero() || now.Before(inst.retryAt)) {
		return
	}
	if inst.runner.Running() {
		inst.runner.Invalidate()
	}
	inst.asked, inst.retryAt = sel.Id, time.Time{}
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
