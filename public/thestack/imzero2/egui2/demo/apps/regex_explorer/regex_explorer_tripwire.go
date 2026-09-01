package regex_explorer

// SD1 engine-fidelity tripwire.
//
// Go's regexp and ClickHouse's libre2 both implement RE2 — same specification,
// independent implementations. The tripwire runs a small fixed corpus of
// (haystack, pattern, flags) cases through both engines on startup;
// divergences are logged and surfaced in the status bar. See ADR-0054
// section "Subsidiary design decisions" for the rationale.
//
// Disagreements are partitioned. A case carrying a [tripwireCase.KnownDrift]
// note is a difference we have already investigated and decided to live
// with — it is counted separately and does not light the DRIFT indicator.
// Everything else is unexpected and does. Without that split the indicator
// degrades into a permanent warning the moment the corpus covers a real
// engine difference, and a permanent warning conveys nothing.
//
// The tripwire is not gating: a drift does not block the app. Users can
// still type and run queries; the expectation is that unexpected divergence
// is rare and informational, and anyone who cares about an exact-match
// verification is looking for the status-bar indicator.

import (
	"context"
	"reflect"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// tripwireCase is one triple-tuple for engine-fidelity comparison: a
// pattern applied to a haystack must produce the same match list under
// Go's regexp.Regexp.FindAllString and ClickHouse's extractAll.
//
// Flags carries the RE2 inline-flag letters ("i", "m", "s", or a
// combination) the case is evaluated under. The corpus states them
// explicitly rather than reading the live UI toggles: SD1 compares two
// engines against a *fixed* corpus, so a case whose meaning changed with
// whatever the user last clicked would compare two moving targets.
// [App.effectivePattern] builds the same "(?ims)"-prefixed string for
// the interactive path, so the flag prefix itself is on both paths.
//
// KnownDrift documents an engine difference that is real, understood,
// and not a regression. A case with a non-empty KnownDrift is still
// evaluated and still logged when the engines disagree, but the
// disagreement is counted as *known* rather than reported as drift —
// otherwise every widened corpus permanently pins the status bar to
// "DRIFT" and the indicator stops carrying information. An empty
// KnownDrift means the engines must agree exactly.
type tripwireCase struct {
	Name       string
	Haystack   string
	Pattern    string
	Flags      string
	KnownDrift string
}

// effective returns the case's pattern with its inline-flag prefix
// applied — the same shape [App.effectivePattern] produces for the
// interactive path.
func (inst tripwireCase) effective() (pattern string) {
	pattern = inst.Pattern
	if inst.Flags != "" {
		pattern = "(?" + inst.Flags + ")" + pattern
	}
	return
}

// tripwireCorpus is the fixed set of cases the SD1 tripwire evaluates.
// Keep small and fast; covers character classes, anchors, groups,
// Unicode, the inline-flag prefix, and the empty-match enumeration
// policy — the places two independent RE2 implementations are most
// likely to drift apart.
var tripwireCorpus = []tripwireCase{
	{Name: "literal", Haystack: "foobar", Pattern: `foo.*`},
	{Name: "digits", Haystack: "a1 b22 c333", Pattern: `\d+`},
	{Name: "words", Haystack: "hello world", Pattern: `\w+`},
	{Name: "upper-class", Haystack: "UPPER lower Mixed", Pattern: `[A-Z]+`},
	{Name: "lower-class", Haystack: "abc123def456", Pattern: `[a-z]+`},
	{
		// ClickHouse's extractAll returns the first *capture group* when
		// the pattern has one, and the full match only when it does not.
		// Go's FindAllString always returns the full match. So for
		// `\(([^)]+)\)` over "(x) (yz)" Go says ["(x)" "(yz)"] and
		// ClickHouse says ['x','yz'] — same match count, different
		// strings.
		//
		// This case shipped in the original corpus without the note,
		// which pinned the status-bar indicator to DRIFT from the day it
		// was written and left it unable to signal anything else. It is
		// documented here rather than removed: it is the single most
		// load-bearing difference between the two engines for this app,
		// because the List tab renders extractAll output directly.
		Name:       "group-capture",
		Haystack:   "(x) (yz)",
		Pattern:    `\(([^)]+)\)`,
		KnownDrift: "ClickHouse extractAll yields capture group 1; Go FindAllString yields the full match",
	},
	{
		// The non-capturing counterpart, which must agree exactly —
		// it keeps grouping itself under test and pins the boundary of
		// the difference above to "has a capture group", not "has parens".
		Name:     "group-noncapture",
		Haystack: "(x) (yz)",
		Pattern:  `\((?:[^)]+)\)`,
	},
	{Name: "whitespace", Haystack: "a b  c", Pattern: `\s+`},
	{Name: "unicode-latin-ext", Haystack: "ümlaüts are fine", Pattern: `[äöüÄÖÜ]`},
	{Name: "anchor-start", Haystack: "foo bar", Pattern: `^foo`},
	{Name: "anchor-end", Haystack: "foo bar", Pattern: `bar$`},
	{Name: "alternation", Haystack: "cat dog cow", Pattern: `cat|cow`},
	{Name: "word-boundary", Haystack: "cat category cat", Pattern: `\bcat\b`},
	{Name: "nested-quantifier", Haystack: "aaa b aa", Pattern: `(?:a+)+`},
	{Name: "case-fold-flag", Haystack: "Foo FOO foo", Pattern: `foo`, Flags: "i"},
	{Name: "multiline-anchor-flag", Haystack: "foo\nbar\nfoo", Pattern: `^foo$`, Flags: "m"},
	{Name: "dotall-flag", Haystack: "a\nb", Pattern: `a.b`, Flags: "s"},
	{
		// The empty-match enumeration policy is where the two engines
		// genuinely part company: Go's FindAllString reports one
		// zero-width match at every position (4 for a 3-byte haystack),
		// ClickHouse's extractAll reports none. Neither is wrong —
		// RE2 specifies the match, not how a caller enumerates repeated
		// empty matches. The app sides with ClickHouse in the preview
		// (see nonEmptyMatches) because predicting ClickHouse is the
		// product; the corpus records the raw-engine difference so it
		// stays visible if either side ever changes its mind.
		Name:       "empty-matchable-star",
		Haystack:   "xyz",
		Pattern:    `a*`,
		KnownDrift: "Go enumerates a zero-width match per position; ClickHouse extractAll reports none",
	},
	{
		Name:       "empty-matchable-opt",
		Haystack:   "xyz",
		Pattern:    `q?`,
		KnownDrift: "zero-width matches, as empty-matchable-star",
	},
}

// TripwireState is a snapshot of the SD1 tripwire outcome rendered in the
// status bar. Zero value means "not yet started".
type TripwireState struct {
	Done   bool
	Drifts []int // corpus indices where the engines disagreed unexpectedly
	Known  []int // corpus indices where they disagreed as documented (KnownDrift)
	Err    error // the tripwire itself failed (e.g., ClickHouse unreachable)
}

// tripwireResult holds the SD1 outcome on the [App] once RunTripwire
// completes. Read under App.mu.RLock.
type tripwireResult struct {
	done   bool
	drifts []int
	known  []int
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
		drifts, known, err := inst.runTripwireBlocking(ctx)
		inst.mu.Lock()
		defer inst.mu.Unlock()
		inst.tripwire = tripwireResult{done: true, drifts: drifts, known: known, err: err}
		if err != nil {
			log.Warn().Err(err).Msg("regex_explorer: tripwire failed to complete")
			return
		}
		if len(drifts) > 0 {
			log.Warn().Ints("drifts", drifts).Ints("known", known).Msg("regex_explorer: Go/ClickHouse RE2 divergence detected")
		} else {
			log.Info().Int("cases", len(tripwireCorpus)).Ints("known", known).Msg("regex_explorer: tripwire passed — Go/ClickHouse RE2 agree outside the known-difference ledger")
		}
	}()
}

// runTripwireBlocking runs each corpus case through both engines and
// partitions the disagreements: drifts are unexpected (the case declares no
// KnownDrift and the engines still differ), known are the documented
// differences from the ledger. Short-circuits on the first ClickHouse
// transport error — if CH is unreachable the whole tripwire is considered
// un-run (err set, both slices empty).
func (inst *App) runTripwireBlocking(ctx context.Context) (drifts []int, known []int, err error) {
	alloc := memory.NewGoAllocator()
	for i, tc := range tripwireCorpus {
		pattern := tc.effective()
		goMatches, goErr := inst.tripwireGoMatches(pattern, tc.Haystack)
		if goErr != nil {
			err = eb.Build().Int("tripwire", i).Str("name", tc.Name).Errorf("tripwire: Go compile: %w", goErr)
			return
		}
		chMatches, chErr := inst.tripwireCHMatches(ctx, alloc, pattern, tc.Haystack)
		if chErr != nil {
			err = eb.Build().Int("tripwire", i).Str("name", tc.Name).Errorf("tripwire: ClickHouse: %w", chErr)
			return
		}

		// The Multi tab runs this pattern through VectorScan, not RE2, and
		// the app decides which lines to send there using Go's regexp —
		// a different engine's opinion. Checking that VectorScan at least
		// accepts every pattern Go accepts is what keeps that proxy
		// honest; a rejection here is a genuine engine disagreement, not a
		// mismatched result, so it is reported as a drift on its own.
		vsAccepted, vsErr := inst.tripwireVectorScanAccepts(ctx, alloc, pattern, tc.Haystack)
		if vsErr != nil {
			err = eb.Build().Int("tripwire", i).Str("name", tc.Name).Errorf("tripwire: VectorScan: %w", vsErr)
			return
		}
		if !vsAccepted {
			log.Warn().Str("case", tc.Name).Str("pattern", pattern).Msg("regex_explorer: VectorScan rejected a pattern Go accepts — the Multi tab's per-line validity marker is unreliable for it")
			drifts = append(drifts, i)
			continue
		}

		if reflect.DeepEqual(goMatches, chMatches) {
			continue
		}
		if tc.KnownDrift != "" {
			// Info, not warn: a documented difference is data, not an alarm.
			log.Info().Str("case", tc.Name).Str("pattern", pattern).Str("haystack", tc.Haystack).Str("reason", tc.KnownDrift).Strs("go", goMatches).Strs("ch", chMatches).Msg("regex_explorer: tripwire known engine difference")
			known = append(known, i)
			continue
		}
		log.Warn().Str("case", tc.Name).Str("pattern", pattern).Str("haystack", tc.Haystack).Strs("go", goMatches).Strs("ch", chMatches).Msg("regex_explorer: tripwire divergence")
		drifts = append(drifts, i)
	}
	return
}

// tripwireGoMatches is the Go-side reference for a tripwire case: all
// non-overlapping matches as full-match strings (not capture groups).
//
// Deliberately raw — this is the one place that must NOT go through
// nonEmptyMatches. The preview mirrors ClickHouse's empty-match policy so
// the UI tells one story; the tripwire compares the engines as they
// actually behave, which is what makes the ledger's KnownDrift entries
// mean something. Filtering here would make the tripwire agree with
// itself by construction.
//
// Shares the receiver's compile cache so patterns reused by the tripwire
// and the main loop are compiled only once. Reaching the cache through
// the receiver rather than a package-level pointer is what keeps this
// goroutine off the render thread's toes.
func (inst *App) tripwireGoMatches(pattern string, haystack string) (matches []string, err error) {
	re, err := inst.getCompiledRegexp(pattern)
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
func (inst *App) tripwireCHMatches(ctx context.Context, alloc memory.Allocator, pattern string, haystack string) (matches []string, err error) {
	sql := buildExtractAllSQL(haystack, pattern)
	rdr, closer, execErr := executeArrowStreamViaBus(ctx, inst.busSnapshot(), sql, alloc)
	if execErr != nil {
		err = eh.Errorf("execute tripwire query: %w", execErr)
		return
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil && err == nil {
			err = eh.Errorf("close tripwire query: %w", cErr)
		}
	}()
	defer rdr.Release()
	if !rdr.Next() {
		rErr := rdr.Err()
		if rErr != nil {
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
		err = eb.Build().Type("col", col).Errorf("tripwire unexpected column type")
		return
	}
	inner, ok := list.ListValues().(*array.String)
	if !ok {
		err = eb.Build().Type("array", list.ListValues()).Errorf("tripwire inner column type")
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

// tripwireVectorScanAccepts reports whether ClickHouse's VectorScan
// backend compiles pattern at all, by asking multiMatchAllIndices for a
// one-element pattern set. Only acceptance matters here, not the hits:
// the Multi tab's failure mode is a pattern that Go compiles and
// VectorScan refuses, which takes down the whole set's query with it.
//
// A transport failure is returned as an error (the tripwire is un-run); a
// query ClickHouse answers with a rejection returns accepted=false.
func (inst *App) tripwireVectorScanAccepts(ctx context.Context, alloc memory.Allocator, pattern string, haystack string) (accepted bool, err error) {
	sql := buildMultiMatchSQL(haystack, []string{pattern})
	rdr, closer, execErr := executeArrowStreamViaBus(ctx, inst.busSnapshot(), sql, alloc)
	if execErr != nil {
		// The broker surfaces a ClickHouse-side rejection through the same
		// channel as a transport failure, so tell them apart by whether
		// the message names the pattern compiler.
		if isRegexRejection(execErr) {
			return
		}
		err = eh.Errorf("execute VectorScan probe: %w", execErr)
		return
	}
	cErr := closer.Close()
	rdr.Release()
	if cErr != nil {
		err = eh.Errorf("close VectorScan probe: %w", cErr)
		return
	}
	accepted = true
	return
}

// isRegexRejection reports whether err is ClickHouse refusing to compile a
// pattern rather than a transport or pool failure.
func isRegexRejection(err error) bool {
	msg := err.Error()
	for _, marker := range []string{
		"CANNOT_COMPILE_REGEXP",
		"OptimizedRegularExpression",
		"Hyperscan",
		"hyperscan",
		"BAD_ARGUMENTS",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// tripwireSnapshot exposes a thread-safe snapshot of the SD1 outcome.
func (inst *App) tripwireSnapshot() (state TripwireState) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	state.Done = inst.tripwire.done
	if len(inst.tripwire.drifts) > 0 {
		state.Drifts = make([]int, len(inst.tripwire.drifts))
		copy(state.Drifts, inst.tripwire.drifts)
	}
	if len(inst.tripwire.known) > 0 {
		state.Known = make([]int, len(inst.tripwire.known))
		copy(state.Known, inst.tripwire.known)
	}
	state.Err = inst.tripwire.err
	return
}
