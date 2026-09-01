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
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// runQueryBlocking executes sql via the bus and hands the first column of
// the first result record to decode. label names the query in error
// messages.
//
// A free function rather than a method because Go methods cannot take type
// parameters; inst is used only for the transport and the allocator, both
// of which are goroutine-safe.
func runQueryBlocking[T any](ctx context.Context, inst *App, label string, sql string, decode func(col arrow.Array) (out T, err error)) (out T, err error) {
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
	rec := rdr.Record()
	if rec.NumRows() == 0 || rec.NumCols() == 0 {
		err = eb.Build().Str("label", label).Int64("rows", rec.NumRows()).Int64("cols", rec.NumCols()).Errorf("query returned an empty record")
		return
	}
	out, err = decode(rec.Column(0))
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

// runMatchBlocking evaluates match(haystack, pattern) — UInt8, 0 or 1.
func runMatchBlocking(ctx context.Context, inst *App, haystack string, pattern string) (val bool, err error) {
	return runQueryBlocking(ctx, inst, "match", buildMatchSQL(haystack, pattern), func(col arrow.Array) (out bool, err error) {
		u8, ok := col.(*array.Uint8)
		if !ok {
			err = eb.Build().Type("col", col).Errorf("match query returned unexpected column type (expected *array.Uint8)")
			return
		}
		out = u8.Value(0) != 0
		return
	})
}

// runReplaceAllBlocking evaluates replaceRegexpAll(haystack, pattern,
// replacement) — the haystack with every match replaced.
func runReplaceAllBlocking(ctx context.Context, inst *App, haystack string, pattern string, replacement string) (result string, err error) {
	return runQueryBlocking(ctx, inst, "replaceRegexpAll", buildReplaceAllSQL(haystack, pattern, replacement), func(col arrow.Array) (out string, err error) {
		strCol, ok := col.(*array.String)
		if !ok {
			err = eb.Build().Type("col", col).Errorf("replaceRegexpAll returned unexpected column type (expected *array.String)")
			return
		}
		out = strCol.Value(0)
		return
	})
}

// runExtractAllBlocking evaluates extractAll(haystack, pattern) —
// Array(String).
//
// Note that ClickHouse returns capture group 1 rather than the full match
// when the pattern has a capture group; see [listOutcome.YieldsGroups] for
// how the UI accounts for that.
func runExtractAllBlocking(ctx context.Context, inst *App, haystack string, pattern string) (matches []string, err error) {
	return runQueryBlocking(ctx, inst, "extractAll", buildExtractAllSQL(haystack, pattern), func(col arrow.Array) (out []string, err error) {
		list, err := asList("extractAll", col)
		if err != nil {
			return
		}
		inner, ok := list.ListValues().(*array.String)
		if !ok {
			err = eb.Build().Type("array", list.ListValues()).Errorf("extractAll inner column type (expected *array.String)")
			return
		}
		start, end, err := listRowRange("extractAll", list)
		if err != nil {
			return
		}
		out = make([]string, 0, end-start)
		for i := start; i < end; i++ {
			out = append(out, inner.Value(i))
		}
		return
	})
}

// runExtractAllGroupsBlocking evaluates extractAllGroups(haystack, pattern)
// — Array(Array(String)), one inner array of capture-group values per
// match. The full match is not included; extractAllGroups reports groups
// only.
//
// ClickHouse rejects a pattern with no capture group outright (BAD_ARGUMENTS
// "There are no groups in regexp"), so callers must check that the pattern
// has at least one before asking.
func runExtractAllGroupsBlocking(ctx context.Context, inst *App, haystack string, pattern string) (groups [][]string, err error) {
	return runQueryBlocking(ctx, inst, "extractAllGroups", buildExtractAllGroupsSQL(haystack, pattern), func(col arrow.Array) (out [][]string, err error) {
		outer, err := asList("extractAllGroups", col)
		if err != nil {
			return
		}
		inner, ok := outer.ListValues().(*array.List)
		if !ok {
			err = eb.Build().Type("array", outer.ListValues()).Errorf("extractAllGroups inner column type (expected *array.List)")
			return
		}
		leaf, ok := inner.ListValues().(*array.String)
		if !ok {
			err = eb.Build().Type("array", inner.ListValues()).Errorf("extractAllGroups leaf column type (expected *array.String)")
			return
		}
		matchStart, matchEnd, err := listRowRange("extractAllGroups", outer)
		if err != nil {
			return
		}
		innerOffsets := inner.Offsets()
		if len(innerOffsets) < matchEnd+1 {
			err = eb.Build().Int("offsets", len(innerOffsets)).Int("matches", matchEnd-matchStart).Errorf("extractAllGroups: inner offset count does not match the match count")
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
	})
}

// listOutcome is everything the List tab draws for one input.
type listOutcome struct {
	// Matches is extractAll's output, verbatim.
	Matches []string
	// Groups is extractAllGroups' output — one row of capture-group
	// values per match — or nil when the pattern has no capture group.
	Groups [][]string
	// YieldsGroups records that Matches holds capture-group-1 values
	// rather than full matches, which is what extractAll returns whenever
	// the pattern has a capture group. The tab says so rather than
	// letting the reader assume otherwise: the Test tab highlights full
	// matches, so without the caveat the two tabs look like they
	// disagree.
	YieldsGroups bool
}

// runListOutcomeBlocking gathers the List tab's results. numGroups is the
// pattern's capture-group count, determined by the caller on the render
// thread — it decides whether the extractAllGroups call is legal at all
// (ClickHouse rejects it outright for a group-less pattern).
func runListOutcomeBlocking(ctx context.Context, inst *App, haystack string, pattern string, numGroups int) (out listOutcome, err error) {
	out.Matches, err = runExtractAllBlocking(ctx, inst, haystack, pattern)
	if err != nil {
		return
	}
	if numGroups == 0 {
		return
	}
	out.YieldsGroups = true
	out.Groups, err = runExtractAllGroupsBlocking(ctx, inst, haystack, pattern)
	return
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

// ---------------------------------------------------------------------------
// Lane reconciliation — the render thread's once-per-frame convergence step
// ---------------------------------------------------------------------------

// reconcileQueries points every lane at the inputs currently in the
// editors. Call once per frame from renderBody after the widgets have
// written this frame's values.
//
// There is no "did anything change" flag: the lanes compare keys
// themselves, so a frame where nothing changed costs four key builds and
// four string comparisons, and a frame where something changed cannot lose
// the change (see [queryLane]).
func (inst *App) reconcileQueries() {
	inst.reconcileSingle()
	inst.reconcileMulti()
}

// reconcileSingle drives the three RE2-backed lanes off the single-pattern
// input. All three go idle when there is nothing dispatchable — a cleared
// or broken pattern must drop the previous answer, not keep showing it.
func (inst *App) reconcileSingle() {
	if inst.haystack == "" || !inst.isPatternValid() {
		inst.matchLane.reset()
		inst.listLane.reset()
		inst.replaceLane.reset()
		return
	}

	pattern := inst.effectivePattern(inst.pattern)
	haystack := inst.haystack
	singleKey := makeQueryKey(pattern, haystack)

	inst.matchLane.demand(singleKey, "regex_explorer.match", func(ctx context.Context) (out bool, err error) {
		return runMatchBlocking(ctx, inst, haystack, pattern)
	})

	// The capture-group count is read here, on the render thread, because
	// it needs the compiled pattern — and because it decides whether the
	// extractAllGroups call is legal at all.
	numGroups := inst.patternNumSubexp()
	inst.listLane.demand(singleKey, "regex_explorer.extractAll", func(ctx context.Context) (out listOutcome, err error) {
		return runListOutcomeBlocking(ctx, inst, haystack, pattern, numGroups)
	})

	// The replacement text feeds only this lane, so an edit to it must
	// not re-run match and extractAll — hence its own key rather than a
	// shared "something changed" trigger.
	replacement := inst.replacement
	replaceKey := makeQueryKey(pattern, haystack, replacement)
	inst.replaceLane.demand(replaceKey, "regex_explorer.replaceRegexpAll", func(ctx context.Context) (out string, err error) {
		return runReplaceAllBlocking(ctx, inst, haystack, pattern, replacement)
	})
}

// reconcileMulti drives the VectorScan lane off the multi-pattern input.
//
// The pattern list is parsed on the render thread because
// parseAndValidatePatternList reads the flag toggles; the worker gets the
// parsed lines by value.
func (inst *App) reconcileMulti() {
	if inst.haystack == "" || strings.TrimSpace(inst.patternList) == "" {
		inst.multiLane.reset()
		return
	}
	lines := inst.parseAndValidatePatternList(inst.patternList)
	if len(lines) == 0 {
		inst.multiLane.reset()
		return
	}

	// The haystack and the flags are part of the key, not just the
	// pattern-list text: editing either changes which patterns hit, and
	// keying on the text alone would present the old hits as current.
	key := makeQueryKey(inst.patternList, inst.haystack, inst.flagPrefix())

	var validPatterns []string
	var validOrigIdx []int
	for i, l := range lines {
		if l.Invalid {
			continue
		}
		validPatterns = append(validPatterns, inst.effectivePattern(l.Text))
		validOrigIdx = append(validOrigIdx, i)
	}
	if len(validPatterns) == 0 {
		// multiMatchAllIndices rejects an empty pattern array with
		// ILLEGAL_TYPE_OF_ARGUMENT, and there is nothing to ask anyway:
		// no line can hit. Serve the parsed lines so the markers render
		// as answered rather than pending.
		inst.multiLane.serve(key, lines)
		return
	}

	haystack := inst.haystack
	inst.multiLane.demand(key, "regex_explorer.multiMatchAllIndices", func(ctx context.Context) (out []multiLine, err error) {
		hits, hitErr := runMultiMatchBlocking(ctx, inst, haystack, validPatterns)
		if hitErr != nil {
			err = hitErr
			return
		}
		out = applyMultiHits(lines, validOrigIdx, hits)
		return
	})
}

// cancelQueries abandons every lane's in-flight run and drops what the
// lanes hold. Called from Unmount; safe to call more than once.
func (inst *App) cancelQueries() {
	inst.matchLane.reset()
	inst.listLane.reset()
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
