package regex_explorer

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// RunMatch dispatches an asynchronous ClickHouse query that evaluates
// match(haystack, pattern) through a `clickhouse local` subprocess.
// Coalesces concurrent calls via an atomic flag — if a query is already
// in flight the new call is dropped silently. Errors are stored on the
// app and surfaced through the status bar on the next frame.
func (inst *App) RunMatch(ctx context.Context) {
	if inst.matchRunning.Swap(true) {
		return
	}

	inst.mu.RLock()
	pattern := effectivePattern(inst.pattern)
	haystack := inst.haystack
	inst.mu.RUnlock()

	inst.mu.Lock()
	inst.lastMatchErr = nil
	inst.mu.Unlock()

	go func() {
		defer inst.matchRunning.Store(false)
		val, stats, err := inst.runMatchBlocking(ctx, haystack, pattern)
		inst.mu.Lock()
		defer inst.mu.Unlock()
		if err != nil {
			inst.lastMatchErr = err
			log.Warn().Err(err).Msg("regex_explorer: match query failed")
			return
		}
		inst.lastMatchResult = resultState{Valid: true, Value: val}
		inst.lastMatchStats = stats
	}()
}

// runMatchBlocking executes the SELECT match(...) query synchronously and
// extracts the single UInt8 cell. Intended to run on a goroutine.
func (inst *App) runMatchBlocking(ctx context.Context, haystack string, pattern string) (val bool, stats clStats, err error) {
	sql := buildMatchSQL(haystack, pattern)

	start := time.Now()
	rdr, closer, execErr := executeArrowStreamViaBus(ctx, inst.bus, sql, inst.alloc)
	if execErr != nil {
		err = eh.Errorf("execute match query: %w", execErr)
		return
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil && err == nil{
			err = eh.Errorf("close match query: %w", cErr)
		}
	}()
	defer rdr.Release()

	if !rdr.Next() {
		readerErr := rdr.Err()
		if readerErr != nil{
			err = eh.Errorf("read match result: %w", readerErr)
			return
		}
		err = eh.Errorf("match query returned no records")
		return
	}
	rec := rdr.Record()
	if rec.NumRows() == 0 || rec.NumCols() == 0 {
		err = eh.Errorf("match query returned empty record (rows=%d cols=%d)", rec.NumRows(), rec.NumCols())
		return
	}

	col := rec.Column(0)
	u8, ok := col.(*array.Uint8)
	if !ok {
		err = eh.Errorf("match query returned unexpected column type %T (expected *array.Uint8)", col)
		return
	}
	val = u8.Value(0) != 0
	stats.ElapsedNs = uint64(time.Since(start).Nanoseconds())
	return
}

// RunExtractAll dispatches an asynchronous ClickHouse query that evaluates
// extractAll(haystack, pattern), returning an Array(String) of matches.
// Coalesced and error-surfaced the same way as [App.RunMatch].
func (inst *App) RunExtractAll(ctx context.Context) {
	if inst.listRunning.Swap(true) {
		return
	}

	inst.mu.RLock()
	pattern := effectivePattern(inst.pattern)
	haystack := inst.haystack
	inst.mu.RUnlock()

	inst.mu.Lock()
	inst.listErr = nil
	inst.mu.Unlock()

	go func() {
		defer inst.listRunning.Store(false)
		matches, stats, err := inst.runExtractAllBlocking(ctx, haystack, pattern)
		inst.mu.Lock()
		defer inst.mu.Unlock()
		if err != nil {
			inst.listErr = err
			log.Warn().Err(err).Msg("regex_explorer: extractAll query failed")
			return
		}
		inst.listMatches = matches
		inst.listStats = stats
	}()
}

// runListQueryBlocking executes sql via the bus, expects a single record whose
// first column is an Arrow List, and decodes that list's single row via decode.
// label names the query in error messages. Shared by runExtractAllBlocking and
// runMultiMatchBlocking, which differ only in the list's inner element type.
// (Free function, not a method: Go methods cannot take type parameters.)
func runListQueryBlocking[T any](ctx context.Context, inst *App, label string, sql string, decode func(list *array.List) (out []T, err error)) (out []T, stats clStats, err error) {
	start := time.Now()
	rdr, closer, execErr := executeArrowStreamViaBus(ctx, inst.bus, sql, inst.alloc)
	if execErr != nil {
		err = eh.Errorf("execute %s query: %w", label, execErr)
		return
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil && err == nil {
			err = eh.Errorf("close %s query: %w", label, cErr)
		}
	}()
	defer rdr.Release()

	if !rdr.Next() {
		readerErr := rdr.Err()
		if readerErr != nil {
			err = eh.Errorf("read %s result: %w", label, readerErr)
			return
		}
		err = eh.Errorf("%s query returned no records", label)
		return
	}
	rec := rdr.Record()
	if rec.NumRows() == 0 || rec.NumCols() == 0 {
		err = eh.Errorf("%s query returned empty record (rows=%d cols=%d)", label, rec.NumRows(), rec.NumCols())
		return
	}

	col := rec.Column(0)
	list, ok := col.(*array.List)
	if !ok {
		err = eh.Errorf("%s query returned unexpected column type %T (expected *array.List)", label, col)
		return
	}
	out, err = decode(list)
	if err != nil {
		return
	}
	stats.ElapsedNs = uint64(time.Since(start).Nanoseconds())
	return
}

// runExtractAllBlocking executes SELECT extractAll(...) and decodes the
// Array(String) column into a Go []string. The record has a single row
// whose first column is an Arrow List<String>; we iterate the inner string
// array over the list's offsets.
func (inst *App) runExtractAllBlocking(ctx context.Context, haystack string, pattern string) (matches []string, stats clStats, err error) {
	return runListQueryBlocking(ctx, inst, "extractAll", buildExtractAllSQL(haystack, pattern), func(list *array.List) (out []string, err error) {
		inner, ok := list.ListValues().(*array.String)
		if !ok {
			err = eh.Errorf("extractAll inner column type %T (expected *array.String)", list.ListValues())
			return
		}
		offsets := list.Offsets()
		startIdx := int(offsets[0])
		end := int(offsets[1])
		out = make([]string, 0, end-startIdx)
		for i := startIdx; i < end; i++ {
			out = append(out, inner.Value(i))
		}
		return
	})
}

// RunReplaceAll dispatches an asynchronous ClickHouse query that evaluates
// replaceRegexpAll(haystack, pattern, replacement). Coalesced and
// error-surfaced the same way as [App.RunMatch].
func (inst *App) RunReplaceAll(ctx context.Context) {
	if inst.replaceRunning.Swap(true) {
		return
	}

	inst.mu.RLock()
	pattern := effectivePattern(inst.pattern)
	haystack := inst.haystack
	replacement := inst.replacement
	inst.mu.RUnlock()

	inst.mu.Lock()
	inst.replaceErr = nil
	inst.mu.Unlock()

	go func() {
		defer inst.replaceRunning.Store(false)
		result, stats, err := inst.runReplaceAllBlocking(ctx, haystack, pattern, replacement)
		inst.mu.Lock()
		defer inst.mu.Unlock()
		if err != nil {
			inst.replaceErr = err
			log.Warn().Err(err).Msg("regex_explorer: replaceRegexpAll query failed")
			return
		}
		inst.replaceResult = result
		inst.replaceValid = true
		inst.replaceStats = stats
	}()
}

// runReplaceAllBlocking executes SELECT replaceRegexpAll(...) and returns
// the single String result. Intended to run on a goroutine.
func (inst *App) runReplaceAllBlocking(ctx context.Context, haystack string, pattern string, replacement string) (result string, stats clStats, err error) {
	sql := buildReplaceAllSQL(haystack, pattern, replacement)

	start := time.Now()
	rdr, closer, execErr := executeArrowStreamViaBus(ctx, inst.bus, sql, inst.alloc)
	if execErr != nil {
		err = eh.Errorf("execute replaceRegexpAll query: %w", execErr)
		return
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil && err == nil{
			err = eh.Errorf("close replaceRegexpAll query: %w", cErr)
		}
	}()
	defer rdr.Release()

	if !rdr.Next() {
		readerErr := rdr.Err()
		if readerErr != nil{
			err = eh.Errorf("read replaceRegexpAll result: %w", readerErr)
			return
		}
		err = eh.Errorf("replaceRegexpAll query returned no records")
		return
	}
	rec := rdr.Record()
	if rec.NumRows() == 0 || rec.NumCols() == 0 {
		err = eh.Errorf("replaceRegexpAll query returned empty record (rows=%d cols=%d)", rec.NumRows(), rec.NumCols())
		return
	}

	col := rec.Column(0)
	strCol, ok := col.(*array.String)
	if !ok {
		err = eh.Errorf("replaceRegexpAll returned unexpected column type %T (expected *array.String)", col)
		return
	}
	result = strCol.Value(0)
	stats.ElapsedNs = uint64(time.Since(start).Nanoseconds())
	return
}

// RunMultiMatch dispatches an asynchronous ClickHouse query that evaluates
// multiMatchAllIndices(haystack, [<valid patterns>]) and stores the
// outcome as a [multiSnapshot] — one entry per non-empty line of
// patternListText, tagged with Invalid and (if CH accepted it) Hit.
// Invalid lines are skipped when building the CH call; CH's 1-based hit
// indices are mapped back onto the original line positions so the
// render code can key by visible line number.
//
// Coalesced via inst.multiRunning. If all lines are invalid (or the
// patternListText has no non-empty lines) the snapshot is updated
// synchronously without spawning clickhouse local.
func (inst *App) RunMultiMatch(ctx context.Context, patternListText string) {
	if inst.multiRunning.Swap(true) {
		return
	}

	lines := parseAndValidatePatternList(patternListText)

	var validPatterns []string
	var validOrigIdx []int
	for i, l := range lines {
		if l.Invalid {
			continue
		}
		validPatterns = append(validPatterns, effectivePattern(l.Text))
		validOrigIdx = append(validOrigIdx, i)
	}

	inst.mu.RLock()
	haystack := inst.haystack
	inst.mu.RUnlock()

	if len(validPatterns) == 0 {
		inst.mu.Lock()
		inst.multiSnapshot = multiSnapshot{patternListText: patternListText, lines: lines}
		inst.multiErr = nil
		inst.mu.Unlock()
		inst.multiRunning.Store(false)
		return
	}

	go func() {
		defer inst.multiRunning.Store(false)
		hits, stats, err := inst.runMultiMatchBlocking(ctx, haystack, validPatterns)
		inst.mu.Lock()
		defer inst.mu.Unlock()
		if err != nil {
			inst.multiErr = err
			inst.multiSnapshot = multiSnapshot{patternListText: patternListText, lines: lines}
			log.Warn().Err(err).Msg("regex_explorer: multiMatchAllIndices query failed")
			return
		}
		for _, chIdx := range hits {
			validIdx := int(chIdx) - 1
			if validIdx >= 0 && validIdx < len(validOrigIdx) {
				lines[validOrigIdx[validIdx]].Hit = true
			}
		}
		inst.multiSnapshot = multiSnapshot{patternListText: patternListText, lines: lines}
		inst.multiStats = stats
		inst.multiErr = nil
	}()
}

// runMultiMatchBlocking executes SELECT multiMatchAllIndices(...) and
// decodes the Array(UInt64) column into a Go []uint64. Intended to run on
// a goroutine.
func (inst *App) runMultiMatchBlocking(ctx context.Context, haystack string, patterns []string) (hits []uint64, stats clStats, err error) {
	return runListQueryBlocking(ctx, inst, "multiMatchAllIndices", buildMultiMatchSQL(haystack, patterns), func(list *array.List) (out []uint64, err error) {
		inner, ok := list.ListValues().(*array.Uint64)
		if !ok {
			err = eh.Errorf("multiMatchAllIndices inner column type %T (expected *array.Uint64)", list.ListValues())
			return
		}
		offsets := list.Offsets()
		startIdx := int(offsets[0])
		end := int(offsets[1])
		out = make([]uint64, 0, end-startIdx)
		for i := startIdx; i < end; i++ {
			out = append(out, inner.Value(i))
		}
		return
	})
}
