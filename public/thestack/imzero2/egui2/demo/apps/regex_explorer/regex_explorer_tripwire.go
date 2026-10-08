package regex_explorer

// SD1 engine-fidelity tripwire.
//
// Go's regexp and ClickHouse's libre2 both implement RE2 — same specification,
// independent implementations. The tripwire runs a small fixed corpus of
// (haystack, pattern, flags) cases through both engines on startup;
// divergences are logged and surfaced in the status bar. See ADR-0054
// section "Subsidiary design decisions" for the rationale.
//
// Each case is checked twice against ClickHouse's extractAll. The check
// that gates is the *model*: what the app predicts extractAll returns
// (regex_explorer_chmodel.go — Go's matches, ClickHouse's enumeration and
// defaults) must equal what it returns. A miss there means the app is
// telling the user something false, and lights DRIFT. The second check is
// the raw engines — Go's FindAllString against the same output — and a
// difference there is expected only where a [tripwireCase.KnownDrift] note
// says so: a documented raw difference the model accounts for is counted
// as *known*, an undocumented one is drift. Without that split the
// indicator degrades into a permanent warning the moment the corpus covers
// a real engine difference, and a permanent warning conveys nothing.
//
// The tripwire is not gating: a drift does not block the app. Users can
// still type and run queries; the expectation is that unexpected divergence
// is rare and informational, and anyone who cares about an exact-match
// verification is looking for the status-bar indicator.

import (
	"context"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// tripwireCase is one triple-tuple for engine-fidelity comparison: a
// pattern applied to a haystack, whose ClickHouse extractAll output must
// equal the model's prediction and — unless KnownDrift says otherwise — Go's
// FindAllString.
//
// Flags carries the RE2 inline-flag letters ("i", "m", "s", or a
// combination) the case is evaluated under. The corpus states them
// explicitly rather than reading the live UI toggles: SD1 compares two
// engines against a *fixed* corpus, so a case whose meaning changed with
// whatever the user last clicked would compare two moving targets.
// Unlike the interactive path ([inlineFlags]), a case states only the
// flags it names and leaves the dot flag to each engine's default, so the
// corpus can pin those defaults too.
//
// KnownDrift documents a raw engine difference that is real, understood,
// and modelled. A case with a non-empty KnownDrift is still evaluated and
// logged, and its raw disagreement is counted as *known* rather than
// reported as drift — otherwise every widened corpus permanently pins the
// status bar to "DRIFT" and the indicator stops carrying information. An
// empty KnownDrift means the raw engines must agree exactly.
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
		// because extract and extractAll both return group 1.
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
		// Empty-match enumeration is where the two engines part company.
		// RE2 specifies the match, not how a caller walks repeated
		// matches: Go's FindAllString reports a zero-width match at every
		// position where one is found, ClickHouse's extractAll stops at
		// the first one (regex_explorer_chmodel.go). Here the first match
		// is already empty, so extractAll returns nothing.
		Name:       "empty-matchable-star",
		Haystack:   "xyz",
		Pattern:    `a*`,
		KnownDrift: "Go enumerates zero-width matches; ClickHouse extractAll stops at the first one",
	},
	{
		Name:       "empty-matchable-opt",
		Haystack:   "xyz",
		Pattern:    `q?`,
		KnownDrift: "zero-width matches, as empty-matchable-star",
	},
	{
		// The stop also drops real matches after it: Go reports "",
		// "aa", "", "" and extractAll stops on the first "" before it
		// reaches "aa".
		Name:       "empty-before-match",
		Haystack:   "xaay",
		Pattern:    `a*`,
		KnownDrift: "extractAll stops at the zero-width match at byte 0, before the real match",
	},
	{
		// The subtle half of the rule: the zero-width match right where
		// "ab" ends is one Go's FindAll skips, so Go reports "ab" and
		// "cd" with no empty match between them — and extractAll still
		// stops there. The model has to recover a match FindAll hid.
		Name:       "empty-abutting-match",
		Haystack:   "ab cd",
		Pattern:    `\w*`,
		KnownDrift: "extractAll stops at the zero-width match abutting \"ab\" that FindAll skips",
	},
	{
		// ClickHouse compiles RE2 with dot-matches-newline on; Go has it
		// off. The interactive path states the flag either way
		// ([inlineFlags]); the corpus sends the pattern bare so the default
		// itself stays pinned.
		Name:       "dot-newline-default",
		Haystack:   "a\nb",
		Pattern:    `a.b`,
		KnownDrift: "ClickHouse's dot matches a newline by default; Go's does not",
	},
	{
		// The explicit flag the interactive path sends when the dot-all
		// box is cleared: both engines must honour it.
		Name:     "dot-newline-off",
		Haystack: "a\nb",
		Pattern:  `a.b`,
		Flags:    "-s",
	},
}

// tripwireState is the SD1 tripwire outcome the status bar renders. The
// zero value means "not finished"; [App.tripwireRan] says whether it has
// started.
type tripwireState struct {
	Done   bool
	Drifts []int // corpus indices where the engines disagreed unexpectedly
	Known  []int // corpus indices where they disagreed as documented (KnownDrift)
	Err    error // the tripwire itself failed (e.g., ClickHouse unreachable)
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
		inst.tripwire = tripwireState{Done: true, Drifts: drifts, Known: known, Err: err}
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
// partitions the disagreements: drifts are unexpected (the model mispredicts
// ClickHouse, or the raw engines differ where no KnownDrift says they do),
// known are the documented raw differences the model accounts for. Short-circuits on the first ClickHouse
// transport error — if CH is unreachable the whole tripwire is considered
// un-run (err set, both slices empty).
func (inst *App) runTripwireBlocking(ctx context.Context) (drifts []int, known []int, err error) {
	for i, tc := range tripwireCorpus {
		pattern := tc.effective()
		goMatches, predicted, goErr := inst.tripwireGoMatches(pattern, tc.Haystack)
		if goErr != nil {
			err = eb.Build().Int("tripwire", i).Str("name", tc.Name).Errorf("tripwire: Go compile: %w", goErr)
			return
		}
		chMatches, chErr := runExtractAllBlocking(ctx, inst, tc.Haystack, pattern)
		if chErr != nil {
			err = eb.Build().Int("tripwire", i).Str("name", tc.Name).Errorf("tripwire: ClickHouse: %w", chErr)
			return
		}

		// The multi-pattern input runs through VectorScan, not RE2, and
		// the app decides which lines to send there using Go's regexp —
		// a different engine's opinion. Checking that VectorScan at least
		// accepts every pattern Go accepts is what keeps that proxy
		// honest; a rejection here is a genuine engine disagreement, not a
		// mismatched result, so it is reported as a drift on its own.
		vsAccepted, vsErr := inst.tripwireVectorScanAccepts(ctx, pattern, tc.Haystack)
		if vsErr != nil {
			err = eb.Build().Int("tripwire", i).Str("name", tc.Name).Errorf("tripwire: VectorScan: %w", vsErr)
			return
		}
		if !vsAccepted {
			log.Warn().Str("case", tc.Name).Str("pattern", pattern).Msg("regex_explorer: VectorScan rejected a pattern Go accepts — the multi-pattern per-line validity marker is unreliable for it")
			drifts = append(drifts, i)
			continue
		}

		if !slices.Equal(predicted, chMatches) {
			log.Warn().Str("case", tc.Name).Str("pattern", pattern).Str("haystack", tc.Haystack).Strs("predicted", predicted).Strs("ch", chMatches).Msg("regex_explorer: tripwire model miss — the app predicts extractAll wrongly")
			drifts = append(drifts, i)
			continue
		}
		if slices.Equal(goMatches, chMatches) {
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

// tripwireGoMatches is the Go side of a tripwire case: raw is every match
// FindAllString reports, as full-match strings; predicted is what the app's
// model says ClickHouse's extractAll returns ([predictExtractAll], with
// ClickHouse's dot default applied).
//
// raw is deliberately unfiltered and unmodelled: it is the engine as it
// behaves, which is what makes the ledger's KnownDrift entries mean
// something. Comparing only the model would make the tripwire agree with
// itself wherever the model is right and say nothing about why.
//
// Shares the receiver's compile cache so patterns reused by the tripwire
// and the main loop are compiled only once. Reaching the cache through
// the receiver rather than a package-level pointer is what keeps this
// goroutine off the render thread's toes.
func (inst *App) tripwireGoMatches(pattern string, haystack string) (raw []string, predicted []string, err error) {
	re, err := inst.getCompiledRegexp(pattern)
	if err != nil {
		return
	}
	raw = re.FindAllString(haystack, -1)
	if raw == nil {
		raw = []string{}
	}
	chRe, err := inst.getCompiledRegexp(clickHouseDefaults(pattern))
	if err != nil {
		return
	}
	predicted = predictExtractAll(chRe, haystack)
	return
}

// tripwireVectorScanAccepts reports whether ClickHouse's VectorScan
// backend compiles pattern at all, by asking multiMatchAllIndices for a
// one-element pattern set. Only acceptance matters here, not the hits:
// the multi-pattern input's failure mode is a pattern that Go compiles and
// VectorScan refuses, which takes down the whole set's query with it.
//
// A transport failure is returned as an error (the tripwire is un-run); a
// query ClickHouse answers with a rejection returns accepted=false.
func (inst *App) tripwireVectorScanAccepts(ctx context.Context, pattern string, haystack string) (accepted bool, err error) {
	sql := buildMultiMatchSQL(haystack, []string{pattern})
	rdr, closer, execErr := executeArrowStreamViaBus(ctx, inst.busSnapshot(), sql, inst.alloc)
	if execErr != nil {
		if isVectorScanRejection(execErr) {
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

// isVectorScanRejection reports whether err is ClickHouse refusing to
// compile a pattern for VectorScan, rather than a transport failure or an
// unrelated exception. The shapes seen from clickhouse-local:
// BAD_ARGUMENTS "Pattern '…' failed with error '…'" for syntax VectorScan
// does not support (`\C`, `(?U)`), and HYPERSCAN_CANNOT_SCAN_TEXT for a
// pattern it deems too expensive (`a{1001}`); CANNOT_COMPILE_REGEXP is
// ClickHouse's dedicated regex-compile code. BAD_ARGUMENTS alone is not
// enough — it is ClickHouse's general-purpose code.
func isVectorScanRejection(err error) bool {
	if !isEngineRejection(err) {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "failed with error") ||
		strings.Contains(msg, "HYPERSCAN_CANNOT_SCAN_TEXT") ||
		strings.Contains(msg, "CANNOT_COMPILE_REGEXP")
}

// tripwireSnapshot exposes a thread-safe snapshot of the SD1 outcome.
func (inst *App) tripwireSnapshot() (state tripwireState) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	state = inst.tripwire
	state.Drifts = slices.Clone(state.Drifts)
	state.Known = slices.Clone(state.Known)
	return
}
