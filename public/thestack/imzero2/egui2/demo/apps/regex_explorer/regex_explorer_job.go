package regex_explorer

// ClickHouse query execution.
//
// Every query follows the same three steps: build SQL (regex_explorer_sql.go),
// execute it through the chlocalbroker and pull the single result cell out of
// the Arrow record (here), and hand the decoded value to a [queryLane]
// (regex_explorer_lane.go) which owns the async state around it.
//
// The functions here are the middle step only. They are synchronous, take
// everything they need by value, and touch no render-thread state — which is
// what makes them safe to call from a lane's worker goroutine.

import (
	"context"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// runQueryBlocking executes sql via the bus and hands the first column of
// the first result record to decode. label names the query in error
// messages.
func runQueryBlocking[T any](ctx context.Context, inst *App, label string, sql string, decode func(col arrow.Array) (out T, err error)) (out T, err error) {
	return runRecordBlocking(ctx, inst, label, sql, func(rec arrow.RecordBatch) (out T, err error) {
		return decode(rec.Column(0))
	})
}

// runRecordBlocking executes sql via the bus and hands the first result
// record — one row, any number of columns — to decode.
//
// A free function rather than a method because Go methods cannot take type
// parameters; inst is used only for the transport and the allocator, both
// of which are goroutine-safe.
func runRecordBlocking[T any](ctx context.Context, inst *App, label string, sql string, decode func(rec arrow.RecordBatch) (out T, err error)) (out T, err error) {
	rdr, closer, execErr := executeArrowStreamViaBus(ctx, inst.busSnapshot(), sql, inst.alloc)
	if execErr != nil {
		err = eb.Build().Str("label", label).Errorf("execute query: %w", execErr)
		return
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil && err == nil {
			err = eb.Build().Str("label", label).Errorf("close query: %w", cErr)
		}
	}()
	defer rdr.Release()

	if !rdr.Next() {
		readerErr := rdr.Err()
		if readerErr != nil {
			err = eb.Build().Str("label", label).Errorf("read result: %w", readerErr)
			return
		}
		err = eb.Build().Str("label", label).Errorf("query returned no records")
		return
	}
	rec := rdr.RecordBatch()
	if rec.NumRows() == 0 || rec.NumCols() == 0 {
		err = eb.Build().Str("label", label).Int64("rows", rec.NumRows()).Int64("cols", rec.NumCols()).Errorf("query returned an empty record")
		return
	}
	out, err = decode(rec)
	return
}

// listRowRange returns the [start, end) index range covering the first row
// of an Arrow list column.
//
// arrow-go's List.Offsets returns the raw offsets buffer without adjusting
// for the array's own offset, so this is only correct for an unsliced
// array — which is what the reader hands back for these single-row,
// single-column results. The length check makes the assumption fail loudly
// instead of panicking if that ever stops holding.
func listRowRange(label string, list *array.List) (start int, end int, err error) {
	offsets := list.Offsets()
	if len(offsets) < 2 {
		err = eb.Build().Str("label", label).Int("offsets", len(offsets)).Errorf("list column offset count is wrong; one row needs 2")
		return
	}
	start = int(offsets[0])
	end = int(offsets[1])
	return
}

// asList casts col to an Arrow list column, or reports what it got instead.
func asList(label string, col arrow.Array) (list *array.List, err error) {
	list, ok := col.(*array.List)
	if !ok {
		err = eb.Build().Str("label", label).Type("col", col).Errorf("query returned an unexpected column type (expected *array.List)")
	}
	return
}

// decodeStrings decodes an Array(String) cell.
func decodeStrings(label string) func(col arrow.Array) (out []string, err error) {
	return func(col arrow.Array) (out []string, err error) {
		list, err := asList(label, col)
		if err != nil {
			return
		}
		inner, ok := list.ListValues().(*array.String)
		if !ok {
			err = eb.Build().Str("label", label).Type("array", list.ListValues()).Errorf("inner column type (expected *array.String)")
			return
		}
		start, end, err := listRowRange(label, list)
		if err != nil {
			return
		}
		out = make([]string, 0, end-start)
		for i := start; i < end; i++ {
			out = append(out, inner.Value(i))
		}
		return
	}
}

// decodeString, decodeUint8 and decodeUint64 decode a scalar cell.
func decodeString(label string, col arrow.Array) (out string, err error) {
	s, ok := col.(*array.String)
	if !ok {
		err = eb.Build().Str("label", label).Type("col", col).Errorf("unexpected column type (expected *array.String)")
		return
	}
	out = s.Value(0)
	return
}

func decodeUint8(label string, col arrow.Array) (out uint8, err error) {
	u, ok := col.(*array.Uint8)
	if !ok {
		err = eb.Build().Str("label", label).Type("col", col).Errorf("unexpected column type (expected *array.Uint8)")
		return
	}
	out = u.Value(0)
	return
}

func decodeUint64(label string, col arrow.Array) (out uint64, err error) {
	u, ok := col.(*array.Uint64)
	if !ok {
		err = eb.Build().Str("label", label).Type("col", col).Errorf("unexpected column type (expected *array.Uint64)")
		return
	}
	out = u.Value(0)
	return
}

// runExtractAllBlocking evaluates extractAll(haystack, pattern) —
// Array(String). The SD1 tripwire's query; the Functions tab evaluates
// extractAll with the other pattern functions in [runPatternFnsBlocking].
func runExtractAllBlocking(ctx context.Context, inst *App, haystack string, pattern string) (matches []string, err error) {
	return runQueryBlocking(ctx, inst, "extractAll", buildExtractAllSQL(haystack, pattern), decodeStrings("extractAll"))
}

// decodeStringLists returns a decoder for an Array(Array(String)) cell —
// one inner list per outer element. label names the query in errors.
func decodeStringLists(label string) func(col arrow.Array) (out [][]string, err error) {
	return func(col arrow.Array) (out [][]string, err error) {
		outer, err := asList(label, col)
		if err != nil {
			return
		}
		inner, ok := outer.ListValues().(*array.List)
		if !ok {
			err = eb.Build().Str("label", label).Type("array", outer.ListValues()).Errorf("inner column type (expected *array.List)")
			return
		}
		leaf, ok := inner.ListValues().(*array.String)
		if !ok {
			err = eb.Build().Str("label", label).Type("array", inner.ListValues()).Errorf("leaf column type (expected *array.String)")
			return
		}
		matchStart, matchEnd, err := listRowRange(label, outer)
		if err != nil {
			return
		}
		innerOffsets := inner.Offsets()
		if len(innerOffsets) < matchEnd+1 {
			err = eb.Build().Str("label", label).Int("offsets", len(innerOffsets)).Int("elements", matchEnd-matchStart).Errorf("inner offset count does not match the element count")
			return
		}
		out = make([][]string, 0, matchEnd-matchStart)
		for m := matchStart; m < matchEnd; m++ {
			start := int(innerOffsets[m])
			end := int(innerOffsets[m+1])
			row := make([]string, 0, end-start)
			for i := start; i < end; i++ {
				row = append(row, leaf.Value(i))
			}
			out = append(out, row)
		}
		return
	}
}

// fnOutcome is what the pattern functions return for one input — from
// ClickHouse ([runPatternFnsBlocking]) or as the Go model predicts it
// ([predictFunctions]); one type, so the Functions tab compares like with
// like.
type fnOutcome struct {
	Match         bool
	Count         uint64
	Extract       string
	RegexpExtract string
	ExtractAll    []string
	// Groups is extractAllGroups' output — one row of capture-group values
	// per match — or nil when the pattern has no capture group (ClickHouse
	// rejects the call then, so it is not made).
	Groups [][]string
	// YieldsGroups records that the pattern captures, which makes
	// ExtractAll and Extract hold capture group 1 rather than whole
	// matches.
	YieldsGroups bool
}

// runPatternFnsBlocking evaluates every pattern function in one query.
// numGroups is the pattern's capture-group count, determined by the caller
// on the render thread; it decides whether extractAllGroups is asked at
// all.
func runPatternFnsBlocking(ctx context.Context, inst *App, haystack string, pattern string, numGroups int) (out fnOutcome, err error) {
	fns := patternFns
	if numGroups == 0 {
		fns = fns[:len(fns)-1] // drop extractAllGroups — see fnExtractAllGroups
	}
	const label = "pattern functions"
	return runRecordBlocking(ctx, inst, label, buildFnsSQL(haystack, pattern, "", fns), func(rec arrow.RecordBatch) (out fnOutcome, err error) {
		if int(rec.NumCols()) != len(fns) {
			err = eb.Build().Int64("cols", rec.NumCols()).Int("want", len(fns)).Errorf("pattern functions: column count")
			return
		}
		match, err := decodeUint8("match", rec.Column(int(fnMatch)))
		if err != nil {
			return
		}
		out.Match = match != 0
		if out.Count, err = decodeUint64("countMatches", rec.Column(int(fnCountMatches))); err != nil {
			return
		}
		if out.Extract, err = decodeString("extract", rec.Column(int(fnExtract))); err != nil {
			return
		}
		if out.RegexpExtract, err = decodeString("regexpExtract", rec.Column(int(fnRegexpExtract))); err != nil {
			return
		}
		if out.ExtractAll, err = decodeStrings("extractAll")(rec.Column(int(fnExtractAll))); err != nil {
			return
		}
		if numGroups > 0 {
			out.YieldsGroups = true
			out.Groups, err = decodeStringLists("extractAllGroups")(rec.Column(int(fnExtractAllGroups)))
		}
		return
	})
}

// replaceOutcome is what replaceRegexpOne and replaceRegexpAll return.
type replaceOutcome struct {
	One string
	All string
}

// runReplaceFnsBlocking evaluates the two replace functions in one query.
func runReplaceFnsBlocking(ctx context.Context, inst *App, haystack string, pattern string, replacement string) (out replaceOutcome, err error) {
	return runRecordBlocking(ctx, inst, "replace functions", buildFnsSQL(haystack, pattern, replacement, replaceFns), func(rec arrow.RecordBatch) (out replaceOutcome, err error) {
		if rec.NumCols() != 2 {
			err = eb.Build().Int64("cols", rec.NumCols()).Errorf("replace functions: column count")
			return
		}
		if out.One, err = decodeString("replaceRegexpOne", rec.Column(0)); err != nil {
			return
		}
		out.All, err = decodeString("replaceRegexpAll", rec.Column(1))
		return
	})
}

// runMultiMatchBlocking evaluates multiMatchAllIndices(haystack, [p...]) —
// Array(UInt64) of 1-based indices into the pattern array. The result is
// not sorted; callers key by index rather than position.
func runMultiMatchBlocking(ctx context.Context, inst *App, haystack string, patterns []string) (hits []uint64, err error) {
	return runQueryBlocking(ctx, inst, "multiMatchAllIndices", buildMultiMatchSQL(haystack, patterns), func(col arrow.Array) (out []uint64, err error) {
		list, err := asList("multiMatchAllIndices", col)
		if err != nil {
			return
		}
		inner, ok := list.ListValues().(*array.Uint64)
		if !ok {
			err = eb.Build().Type("array", list.ListValues()).Errorf("multiMatchAllIndices inner column type (expected *array.Uint64)")
			return
		}
		start, end, err := listRowRange("multiMatchAllIndices", list)
		if err != nil {
			return
		}
		out = make([]uint64, 0, end-start)
		for i := start; i < end; i++ {
			out = append(out, inner.Value(i))
		}
		return
	})
}

// runMultiLinesBlocking evaluates the multi-pattern input and maps the hits
// back onto its lines. sent holds the patterns sent, origIdx the line each
// came from.
//
// multiMatchAllIndices is one call over the whole set, so a single pattern
// VectorScan refuses fails every line. When that happens this asks again
// one pattern at a time to find the refused ones, marks them with
// ClickHouse's message, and re-runs the rest — so the user sees which
// line to fix and still gets hits for the others. A failure no single
// pattern reproduces is returned as it came.
func runMultiLinesBlocking(ctx context.Context, inst *App, haystack string, lines []multiLine, sent []string, origIdx []int) (out []multiLine, err error) {
	hits, err := runMultiMatchBlocking(ctx, inst, haystack, sent)
	if err == nil || !isEngineRejection(err) {
		if err == nil {
			out = applyMultiHits(lines, origIdx, hits)
		}
		return
	}
	setErr := err
	out = slices.Clone(lines)
	var keep []string
	var keepIdx []int
	for i, p := range sent {
		_, probeErr := runMultiMatchBlocking(ctx, inst, haystack, []string{p})
		switch {
		case probeErr == nil:
			keep = append(keep, p)
			keepIdx = append(keepIdx, origIdx[i])
		case isEngineRejection(probeErr):
			out[origIdx[i]].Rejected = clickHouseMessage(probeErr)
		default:
			err = probeErr
			return
		}
	}
	if len(keep) == len(sent) {
		err = setErr
		return
	}
	err = nil
	if len(keep) == 0 {
		return
	}
	hits, err = runMultiMatchBlocking(ctx, inst, haystack, keep)
	if err != nil {
		return
	}
	out = applyMultiHits(out, keepIdx, hits)
	return
}

// ---------------------------------------------------------------------------
// Lane reconciliation — the render thread's once-per-frame convergence step
// ---------------------------------------------------------------------------

// reconcileQueries points every lane at the inputs currently in the
// editors. Call once per frame from renderBody after the widgets have
// written this frame's values.
//
// There is no "did anything change" flag: the lanes compare keys
// themselves, so a frame where nothing changed costs three key builds and
// three string comparisons, and a frame where something changed cannot
// lose the change (see [queryLane]). An empty haystack is an input like
// any other — match(”, '^$') is 1.
func (inst *App) reconcileQueries() {
	inst.reconcileSingle()
	inst.reconcileMulti()
}

// reconcileSingle drives the two lanes off the single-pattern input. Both
// go idle when the pattern is empty or does not compile — a cleared or
// broken pattern must drop the previous answer, not keep showing it.
func (inst *App) reconcileSingle() {
	a := inst.analysis()
	if a.state != patternValid {
		inst.fnLane.reset()
		inst.replaceLane.reset()
		return
	}

	pattern := a.pattern
	haystack := a.haystack
	// Read here, on the render thread, because it needs the compiled
	// pattern — and because it decides whether extractAllGroups is legal.
	numGroups := a.re.NumSubexp()
	inst.fnLane.demand(inst.singleKey(), "regex_explorer.functions", func(ctx context.Context) (out fnOutcome, err error) {
		return runPatternFnsBlocking(ctx, inst, haystack, pattern, numGroups)
	})

	// The replacement feeds only this lane, so an edit to it must not
	// re-run the pattern functions — hence its own key.
	replacement := inst.replacement
	inst.replaceLane.demand(inst.replaceKey(), "regex_explorer.replace", func(ctx context.Context) (out replaceOutcome, err error) {
		return runReplaceFnsBlocking(ctx, inst, haystack, pattern, replacement)
	})
}

// reconcileMulti drives the VectorScan lane off the multi-pattern input.
//
// The pattern list is parsed on the render thread because
// parseAndValidatePatternList reads the flag toggles; the worker gets the
// parsed lines by value.
func (inst *App) reconcileMulti() {
	lines := inst.parseAndValidatePatternList(inst.patternList)
	if len(lines) == 0 {
		inst.multiLane.reset()
		return
	}
	key := inst.multiKey()

	var sent []string
	var origIdx []int
	for i, l := range lines {
		if l.Invalid {
			continue
		}
		sent = append(sent, inst.effectivePattern(l.Text))
		origIdx = append(origIdx, i)
	}
	if len(sent) == 0 {
		// multiMatchAllIndices rejects an empty pattern array with
		// ILLEGAL_TYPE_OF_ARGUMENT, and there is nothing to ask anyway:
		// no line can hit. Serve the parsed lines so the markers render
		// as answered rather than pending.
		inst.multiLane.serve(key, lines)
		return
	}

	haystack := inst.haystack
	inst.multiLane.demand(key, "regex_explorer.multiMatchAllIndices", func(ctx context.Context) (out []multiLine, err error) {
		return runMultiLinesBlocking(ctx, inst, haystack, lines, sent, origIdx)
	})
}

// cancelQueries abandons every lane's in-flight run and drops what the
// lanes hold. Called from Unmount; safe to call more than once.
func (inst *App) cancelQueries() {
	inst.fnLane.reset()
	inst.replaceLane.reset()
	inst.multiLane.reset()
}

// applyMultiHits maps ClickHouse's hit indices back onto line positions.
//
// The indices are 1-based and count only the patterns actually sent, so
// invalid lines — which are skipped when building the call — shift every
// subsequent index. validOrigIdx records where each sent pattern came
// from; this walks that mapping backwards.
//
// Out-of-range indices are ignored rather than trusted: they would mean
// ClickHouse answered about a pattern array we did not send.
func applyMultiHits(lines []multiLine, validOrigIdx []int, hits []uint64) (out []multiLine) {
	out = slices.Clone(lines)
	for _, chIdx := range hits {
		validIdx := int(chIdx) - 1
		if validIdx < 0 || validIdx >= len(validOrigIdx) {
			continue
		}
		out[validOrigIdx[validIdx]].Hit = true
	}
	return
}
