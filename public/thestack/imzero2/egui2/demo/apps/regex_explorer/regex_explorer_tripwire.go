//go:build llm_generated_opus47

package regex_explorer

// SD1 engine-fidelity tripwire.
//
// Go's regexp and ClickHouse's libre2 both implement RE2 — same specification,
// independent implementations. The tripwire runs a small fixed corpus of
// (haystack, pattern, expected_matches) tuples through both engines on
// startup; divergences are logged via eh structured fields and surfaced in
// the status bar. See ADR-0054 section "Subsidiary design decisions" for
// the rationale.
//
// The tripwire is not gating: a drift does not block the app. Users can
// still type and run queries; the expectation is that divergence is rare
// and informational, and anyone who cares about an exact-match verification
// is looking for the status-bar indicator.

import (
	"context"
	"reflect"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// tripwireCase is one triple-tuple for engine-fidelity comparison: a
// pattern applied to a haystack must produce the same match list under
// Go's regexp.Regexp.FindAllString and ClickHouse's extractAll.
type tripwireCase struct {
	Name     string
	Haystack string
	Pattern  string
}

// tripwireCorpus is the fixed set of cases the SD1 tripwire evaluates.
// Keep small (~10 cases) and fast; covers character classes, anchors,
// groups, and Unicode to surface RE2-version drift early.
var tripwireCorpus = []tripwireCase{
	{Name: "literal", Haystack: "foobar", Pattern: `foo.*`},
	{Name: "digits", Haystack: "a1 b22 c333", Pattern: `\d+`},
	{Name: "words", Haystack: "hello world", Pattern: `\w+`},
	{Name: "upper-class", Haystack: "UPPER lower Mixed", Pattern: `[A-Z]+`},
	{Name: "lower-class", Haystack: "abc123def456", Pattern: `[a-z]+`},
	{Name: "group-capture", Haystack: "(x) (yz)", Pattern: `\(([^)]+)\)`},
	{Name: "whitespace", Haystack: "a b  c", Pattern: `\s+`},
	{Name: "unicode-latin-ext", Haystack: "ümlaüts are fine", Pattern: `[äöüÄÖÜ]`},
	{Name: "anchor-start", Haystack: "foo bar", Pattern: `^foo`},
	{Name: "anchor-end", Haystack: "foo bar", Pattern: `bar$`},
}

// TripwireState is a snapshot of the SD1 tripwire outcome rendered in the
// status bar. Zero value means "not yet started".
type TripwireState struct {
	Done   bool
	Drifts []int // indices into tripwireCorpus where Go and CH disagreed
	Err    error // the tripwire itself failed (e.g., ClickHouse unreachable)
}

// tripwireResult holds the SD1 outcome on the [App] once RunTripwire
// completes. Read under App.mu.RLock.
type tripwireResult struct {
	done   bool
	drifts []int
	err    error
}

// RunTripwire launches the SD1 engine-fidelity tripwire if it has not yet
// run this session. One-shot: subsequent calls are no-ops. The result is
// stored on the [App] and read by the status bar.
func (inst *App) RunTripwire(ctx context.Context) {
	if inst.tripwireRan.Swap(true) {
		return
	}
	go func() {
		drifts, err := inst.runTripwireBlocking(ctx)
		inst.mu.Lock()
		defer inst.mu.Unlock()
		inst.tripwire = tripwireResult{done: true, drifts: drifts, err: err}
		if err != nil {
			log.Warn().Err(err).Msg("regex_explorer: tripwire failed to complete")
			return
		}
		if len(drifts) > 0 {
			log.Warn().Ints("drifts", drifts).Msg("regex_explorer: Go/ClickHouse RE2 divergence detected")
		} else {
			log.Info().Int("cases", len(tripwireCorpus)).Msg("regex_explorer: tripwire passed — Go/ClickHouse RE2 agree")
		}
	}()
}

// runTripwireBlocking runs each corpus case through both engines and
// returns the indices of cases where the match lists differ. Short-circuits
// on the first ClickHouse transport error — if CH is unreachable the whole
// tripwire is considered un-run (err set, drifts empty).
func (inst *App) runTripwireBlocking(ctx context.Context) (drifts []int, err error) {
	alloc := memory.NewGoAllocator()
	for i, tc := range tripwireCorpus {
		goMatches, goErr := tripwireGoMatches(tc.Pattern, tc.Haystack)
		if goErr != nil {
			err = eh.Errorf("tripwire[%d=%s]: Go compile: %w", i, tc.Name, goErr)
			return
		}
		chMatches, chErr := tripwireCHMatches(ctx, inst, alloc, tc.Pattern, tc.Haystack)
		if chErr != nil {
			err = eh.Errorf("tripwire[%d=%s]: ClickHouse: %w", i, tc.Name, chErr)
			return
		}
		if !reflect.DeepEqual(goMatches, chMatches) {
			log.Warn().Str("case", tc.Name).Str("pattern", tc.Pattern).Str("haystack", tc.Haystack).Strs("go", goMatches).Strs("ch", chMatches).Msg("regex_explorer: tripwire divergence")
			drifts = append(drifts, i)
		}
	}
	return
}

// tripwireGoMatches is the Go-side reference for a tripwire case. Mirrors
// ClickHouse's extractAll semantics: all non-overlapping matches, full-match
// strings (not capture groups). Shares the [App] compile cache so patterns
// reused by the tripwire and the main loop are compiled only once.
func tripwireGoMatches(pattern string, haystack string) (matches []string, err error) {
	re, err := app.getCompiledRegexp(pattern)
	if err != nil {
		return
	}
	matches = re.FindAllString(haystack, -1)
	if matches == nil {
		matches = []string{}
	}
	return
}

// tripwireCHMatches runs ClickHouse's extractAll over a `clickhouse
// local` subprocess and returns the string matches, mirroring the shape
// of tripwireGoMatches. Allocates a fresh [memory.Allocator] so the
// tripwire does not share memory bookkeeping with live UI queries.
func tripwireCHMatches(ctx context.Context, inst *App, alloc memory.Allocator, pattern string, haystack string) (matches []string, err error) {
	sql := buildExtractAllSQL(haystack, pattern)
	rdr, closer, execErr := executeArrowStreamViaBus(ctx, inst.bus, sql, alloc)
	if execErr != nil {
		err = eh.Errorf("execute tripwire query: %w", execErr)
		return
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil && err == nil{
			err = eh.Errorf("close tripwire query: %w", cErr)
		}
	}()
	defer rdr.Release()
	if !rdr.Next() {
		rErr := rdr.Err()
		if rErr != nil{
			err = eh.Errorf("read tripwire result: %w", rErr)
			return
		}
		err = eh.Errorf("tripwire query returned no records")
		return
	}
	rec := rdr.Record()
	if rec.NumRows() == 0 || rec.NumCols() == 0 {
		err = eh.Errorf("tripwire query returned empty record")
		return
	}
	col := rec.Column(0)
	list, ok := col.(*array.List)
	if !ok {
		err = eh.Errorf("tripwire unexpected column type %T", col)
		return
	}
	inner, ok := list.ListValues().(*array.String)
	if !ok {
		err = eh.Errorf("tripwire inner column type %T", list.ListValues())
		return
	}
	offsets := list.Offsets()
	start := int(offsets[0])
	end := int(offsets[1])
	matches = make([]string, 0, end-start)
	for i := start; i < end; i++ {
		matches = append(matches, inner.Value(i))
	}
	return
}

// tripwireState exposes a thread-safe snapshot of the SD1 outcome.
func (inst *App) tripwireSnapshot() (state TripwireState) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	state.Done = inst.tripwire.done
	if len(inst.tripwire.drifts) > 0 {
		state.Drifts = make([]int, len(inst.tripwire.drifts))
		copy(state.Drifts, inst.tripwire.drifts)
	}
	state.Err = inst.tripwire.err
	return
}
