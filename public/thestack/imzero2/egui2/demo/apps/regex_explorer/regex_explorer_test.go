package regex_explorer

// Unit tests for the regex explorer's testable surface. The tests that need
// a clickhouse-local are in regex_explorer_integration_test.go, behind the
// integration build tag.
//
// State is per-[App]: every test allocates its own via newTestApp, so flag
// state and the compile cache cannot leak between cases and nothing has to
// be reset on setup.

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// newTestApp returns a fresh [App]: no regex flags set, empty compile
// cache, no bus. Cheap enough to call per subtest, which is what keeps
// cases independent.
func newTestApp(t *testing.T) (inst *App) {
	t.Helper()
	inst = newApp()
	return
}

// ---------------------------------------------------------------------------
// Pure-Go helpers
// ---------------------------------------------------------------------------

func TestEffectivePattern(t *testing.T) {
	cases := []struct {
		name  string
		setup func(inst *App)
		base  string
		want  string
	}{
		// The dot flag is always stated — on by default, as in
		// ClickHouse — so both engines read one pattern (see inlineFlags).
		{name: "empty-base-no-flags", setup: func(*App) {}, base: "", want: ""},
		{name: "defaults", setup: func(*App) {}, base: "foo", want: "(?s)foo"},
		{name: "case-insensitive", setup: func(inst *App) { inst.caseInsensitive = true }, base: "foo", want: "(?is)foo"},
		{name: "multiline", setup: func(inst *App) { inst.multiline = true }, base: "^x$", want: "(?ms)^x$"},
		{name: "dotall-off", setup: func(inst *App) { inst.dotAll = false }, base: ".", want: "(?-s)."},
		{name: "case-insensitive-dotall-off", setup: func(inst *App) {
			inst.caseInsensitive = true
			inst.dotAll = false
		}, base: "foo", want: "(?i-s)foo"},
		{name: "all-three", setup: func(inst *App) {
			inst.caseInsensitive = true
			inst.multiline = true
		}, base: "foo", want: "(?ims)foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := newTestApp(t)
			tc.setup(inst)
			got := inst.effectivePattern(tc.base)
			if got != tc.want {
				t.Errorf("effectivePattern(%q) = %q; want %q", tc.base, got, tc.want)
			}
		})
	}
}

// TestPredictExtractAll pins the Go-side model of ClickHouse's extractAll
// against outputs read off clickhouse-local (26.9). Each case is one shape
// of the enumeration rule in regex_explorer_chmodel.go; the SD1 corpus
// re-checks the model against a live ClickHouse.
func TestPredictExtractAll(t *testing.T) {
	cases := []struct {
		pattern  string
		haystack string
		want     []string
	}{
		{`\d+`, "a1 b22 c333", []string{"1", "22", "333"}},
		// Stops at the first zero-width match …
		{`a*`, "xyz", []string{}},
		{`a*`, "xaay", []string{}},
		{`a*`, "aax", []string{"aa"}},
		// … including the one abutting a match, which FindAll skips.
		{`a*`, "aaxa", []string{"aa"}},
		{`\w*`, "ab cd", []string{"ab"}},
		{`a*b?`, "abxb", []string{"ab"}},
		{`\b\w*`, "ab cd", []string{"ab"}},
		// No empty match is possible, so nothing stops it.
		{`\Ba`, "aaa", []string{"a", "a"}},
		{`(?m)^a`, "a\na", []string{"a", "a"}},
		{`^a`, "aaa", []string{"a"}},
		// Capturing patterns yield group 1; an unset group is "".
		{`(a)*`, "aaxa", []string{"a"}},
		{`b|(c)`, "bc", []string{"", "c"}},
		{`(x)?b`, "ab", []string{""}},
		// ClickHouse's dot matches a newline unless told otherwise.
		{`.*`, "ab\ncd", []string{"ab\ncd"}},
		{`(?-s).*`, "ab\ncd", []string{"ab"}},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+"/"+tc.haystack, func(t *testing.T) {
			t.Parallel()
			re := regexp.MustCompile(clickHouseDefaults(tc.pattern))
			got := predictExtractAll(re, tc.haystack)
			if !slices.Equal(got, tc.want) {
				t.Errorf("predictExtractAll(%q, %q) = %q; want %q", tc.pattern, tc.haystack, got, tc.want)
			}
		})
	}
}

func TestParseAndValidatePatternList(t *testing.T) {
	cases := []struct {
		name        string
		input       string
		wantTexts   []string
		wantInvalid []bool
	}{
		{
			name:      "empty",
			input:     "",
			wantTexts: nil,
		},
		{
			name:        "two-trivial",
			input:       "foo\nbar",
			wantTexts:   []string{"foo", "bar"},
			wantInvalid: []bool{false, false},
		},
		{
			name:        "trailing-newline",
			input:       "foo\nbar\n",
			wantTexts:   []string{"foo", "bar"},
			wantInvalid: []bool{false, false},
		},
		{
			name:        "blank-line-in-middle",
			input:       "foo\n\nbar",
			wantTexts:   []string{"foo", "bar"},
			wantInvalid: []bool{false, false},
		},
		{
			name:        "whitespace-only-line-dropped",
			input:       "foo\n   \nbar",
			wantTexts:   []string{"foo", "bar"},
			wantInvalid: []bool{false, false},
		},
		{
			name:        "one-invalid",
			input:       "foo\n(unclosed\nbar",
			wantTexts:   []string{"foo", "(unclosed", "bar"},
			wantInvalid: []bool{false, true, false},
		},
		{
			name:        "all-invalid",
			input:       "(bad\n[unclosed",
			wantTexts:   []string{"(bad", "[unclosed"},
			wantInvalid: []bool{true, true},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := newTestApp(t).parseAndValidatePatternList(tc.input)
			if len(got) != len(tc.wantTexts) {
				t.Fatalf("line count = %d; want %d (got=%v)", len(got), len(tc.wantTexts), got)
			}
			for i, line := range got {
				if line.Text != tc.wantTexts[i] {
					t.Errorf("line %d text = %q; want %q", i, line.Text, tc.wantTexts[i])
				}
				if line.Invalid != tc.wantInvalid[i] {
					t.Errorf("line %d invalid = %v; want %v (err=%q)", i, line.Invalid, tc.wantInvalid[i], line.Text)
				}
				if line.Hit {
					t.Errorf("line %d hit should start false", i)
				}
			}
		})
	}
}

func TestCountValidMultiLines(t *testing.T) {
	cases := []struct {
		name string
		in   []multiLine
		want int
	}{
		{"empty", nil, 0},
		{"all-valid", []multiLine{{Text: "a"}, {Text: "b"}, {Text: "c"}}, 3},
		{"some-invalid", []multiLine{{Text: "a"}, {Text: "b", Invalid: true}, {Text: "c"}}, 2},
		{"all-invalid", []multiLine{{Text: "a", Invalid: true}, {Text: "b", Invalid: true}}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := countValidMultiLines(tc.in)
			if got != tc.want {
				t.Errorf("countValidMultiLines(%v) = %d; want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestAnalysisMatchCount pins the match list every surface reads (status
// bar, preview, capture groups, hand-off) — see [App.analysis].
func TestAnalysisMatchCount(t *testing.T) {
	cases := []struct {
		name     string
		pattern  string
		haystack string
		wantN    int
		wantErr  bool
	}{
		{"empty-both", "", "", 0, false},
		{"empty-pattern", "", "hello", 0, false},
		{"empty-haystack", `\d+`, "", 0, false},
		{"digits", `\d+`, "a1 b22 c333", 3, false},
		{"no-match", `\d+`, "no digits", 0, false},
		// A compile failure is reported through err with a zero count.
		// There is no negative sentinel: the caller distinguishes
		// "couldn't compile" from "compiled, matched nothing" by err.
		{"invalid-pattern", `\d(+`, "text", 0, true},
		// Zero-width matches are not counted, as ClickHouse's
		// countMatches does not count them: there is nothing to
		// highlight. Which matches extractAll returns is the model's
		// question (TestPredictExtractAll), not this count's.
		{"empty-matchable-star", `a*`, "xyz", 0, false},
		{"empty-matchable-opt", `q?`, "xyz", 0, false},
		{"mixed-empty-and-real", `a*`, "xayz", 1, false},
		{"boundary-is-zero-width", `\b`, "hi there", 0, false},
		// The capture-group breakdown reads the same match list, so a
		// capturing empty-matchable pattern must not grow rows the
		// status bar does not count.
		{"capturing-empty-matchable", `(a)*`, "xyz", 0, false},
		{"capturing-mixed-empty-and-real", `(a)*`, "xaay", 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := newTestApp(t)
			inst.pattern, inst.haystack = tc.pattern, tc.haystack
			a := inst.analysis()
			if (a.err != nil) != tc.wantErr {
				t.Errorf("analysis err=%v; wantErr=%v", a.err, tc.wantErr)
			}
			if n := len(a.matches); n != tc.wantN {
				t.Errorf("analysis matches=%d; want %d", n, tc.wantN)
			}
		})
	}
}

// TestApplyMultiHits covers the index remap, which is the one place in the
// app where a silent wrong answer can hide: ClickHouse's indices are
// 1-based and count only the patterns actually sent, so every invalid line
// shifts the mapping.
func TestApplyMultiHits(t *testing.T) {
	t.Parallel()

	valid := func(texts ...string) (lines []multiLine) {
		for _, s := range texts {
			lines = append(lines, multiLine{Text: s})
		}
		return
	}

	cases := []struct {
		name         string
		lines        []multiLine
		validOrigIdx []int
		hits         []uint64
		wantHit      []bool
	}{
		{
			name:         "all-valid-first-hits",
			lines:        valid("a", "b", "c"),
			validOrigIdx: []int{0, 1, 2},
			hits:         []uint64{1},
			wantHit:      []bool{true, false, false},
		},
		{
			name:         "unsorted-indices",
			lines:        valid("a", "b", "c"),
			validOrigIdx: []int{0, 1, 2},
			hits:         []uint64{3, 1},
			wantHit:      []bool{true, false, true},
		},
		{
			// The case the remap exists for: line 1 is invalid and never
			// sent, so CH index 1 means line 0 and index 2 means line 2.
			name:         "invalid-line-shifts-indices",
			lines:        []multiLine{{Text: "a"}, {Text: "(bad", Invalid: true}, {Text: "c"}},
			validOrigIdx: []int{0, 2},
			hits:         []uint64{2},
			wantHit:      []bool{false, false, true},
		},
		{
			name:         "leading-invalid",
			lines:        []multiLine{{Text: "(bad", Invalid: true}, {Text: "b"}},
			validOrigIdx: []int{1},
			hits:         []uint64{1},
			wantHit:      []bool{false, true},
		},
		{
			name:         "no-hits",
			lines:        valid("a", "b"),
			validOrigIdx: []int{0, 1},
			hits:         nil,
			wantHit:      []bool{false, false},
		},
		{
			// A reply about patterns we did not send is ignored rather
			// than panicking or marking an arbitrary line.
			name:         "out-of-range-indices-ignored",
			lines:        valid("a", "b"),
			validOrigIdx: []int{0, 1},
			hits:         []uint64{0, 3, 99},
			wantHit:      []bool{false, false},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := applyMultiHits(tc.lines, tc.validOrigIdx, tc.hits)
			if len(got) != len(tc.wantHit) {
				t.Fatalf("line count = %d; want %d", len(got), len(tc.wantHit))
			}
			for i, want := range tc.wantHit {
				if got[i].Hit != want {
					t.Errorf("line %d (%q) hit = %v; want %v", i, got[i].Text, got[i].Hit, want)
				}
			}
			// The input must not be mutated: the render thread reuses its
			// own parse of the same lines in the frame that dispatched.
			for i := range tc.lines {
				if tc.lines[i].Hit {
					t.Errorf("input line %d was mutated", i)
				}
			}
		})
	}
}

func TestMakeQueryKey(t *testing.T) {
	t.Parallel()

	// Quoting each part is what stops different input tuples from
	// colliding. Without it, ("a\x1fb", "") and ("a", "b") would produce
	// the same key under a raw separator join, and one lane would serve
	// the other's result.
	cases := []struct {
		name string
		a    []string
		b    []string
		same bool
	}{
		{"identical", []string{"foo", "bar"}, []string{"foo", "bar"}, true},
		{"different-value", []string{"foo", "bar"}, []string{"foo", "baz"}, false},
		{"boundary-shift", []string{"foobar", ""}, []string{"foo", "bar"}, false},
		{"separator-injection", []string{"a\x1fb", ""}, []string{"a", "b"}, false},
		{"quote-injection", []string{`a"b`, ""}, []string{"a", "b"}, false},
		{"empty-vs-absent", []string{"a", ""}, []string{"a"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ka, kb := makeQueryKey(tc.a...), makeQueryKey(tc.b...)
			if (ka == kb) != tc.same {
				t.Errorf("makeQueryKey(%q)=%q vs makeQueryKey(%q)=%q; same=%v want %v",
					tc.a, ka, tc.b, kb, ka == kb, tc.same)
			}
		})
	}
}

// TestQueryLane_Convergence is the regression test for the stale-result
// bug: an input that changes while a query is in flight must still be
// queried, and the lane must never report a result for older inputs as
// describing the current ones.
func TestQueryLane_Convergence(t *testing.T) {
	t.Parallel()

	var lane queryLane[string]
	release := make(chan struct{})
	var ran atomic.Int32

	start := func(key queryKey, value string) {
		lane.demand(key, "test", func(ctx context.Context) (out string, err error) {
			ran.Add(1)
			<-release
			out = value
			return
		})
	}

	keyA, keyB := makeQueryKey("A"), makeQueryKey("B")

	// Frame 1: ask for A. Nothing served yet.
	start(keyA, "resultA")
	if v := lane.view(keyA); v.Has || !v.Running {
		t.Fatalf("after first demand: Has=%v Running=%v; want false/true", v.Has, v.Running)
	}
	waitFor(t, func() bool { return ran.Load() == 1 }, "A's run to reach the worker")

	// Frames 2..N: the input moves to B while A is still running. The
	// old edge-triggered code dropped this edit permanently.
	for range 5 {
		start(keyB, "resultB")
	}
	if got := ran.Load(); got != 1 {
		t.Fatalf("runs started while busy = %d; want 1 (the lane must coalesce)", got)
	}

	// A lands. It is a real result, but it describes inputs that are no
	// longer on screen, so it must not read as fresh for B.
	close(release)
	waitFor(t, func() bool {
		lane.drain()
		return lane.servedFor(keyA)
	}, "lane to take A's result")

	if v := lane.view(keyB); v.Fresh {
		t.Errorf("A's result reported as fresh for input B")
	}

	// The next frame re-observes the mismatch and queries for B.
	release = make(chan struct{})
	close(release)
	start(keyB, "resultB")
	waitFor(t, func() bool {
		lane.drain()
		return lane.servedFor(keyB)
	}, "lane to converge on B")

	v := lane.view(keyB)
	if !v.Fresh || v.Value != "resultB" {
		t.Errorf("converged view = %+v; want fresh resultB", v)
	}
}

// TestQueryLane_FailureIsNotRetriedForSameInput pins the other half of the
// contract: a lane that ClickHouse refused must not spin re-issuing the
// same doomed query, but must try again as soon as the input changes.
func TestQueryLane_FailureIsNotRetriedForSameInput(t *testing.T) {
	t.Parallel()

	var lane queryLane[string]
	var ran atomic.Int32
	fail := func(key queryKey) {
		lane.demand(key, "test", func(ctx context.Context) (out string, err error) {
			ran.Add(1)
			err = eh.Errorf("Code: 427. DB::Exception: cannot compile regexp")
			return
		})
	}

	keyA := makeQueryKey("A")
	fail(keyA)
	waitFor(t, func() bool {
		lane.drain()
		return lane.failedFor(keyA)
	}, "lane to record the failure")

	for range 10 {
		fail(keyA)
	}
	if got := ran.Load(); got != 1 {
		t.Errorf("runs for an already-failed input = %d; want 1", got)
	}
	if v := lane.view(keyA); v.Err == nil {
		t.Errorf("view for the failed input carries no error")
	}

	keyB := makeQueryKey("B")
	fail(keyB)
	waitFor(t, func() bool { return ran.Load() == 2 }, "a changed input to retry")

	// Final means final: an old rejection is still not re-run.
	waitFor(t, func() bool {
		lane.drain()
		return lane.failedFor(keyB)
	}, "lane to record B's failure")
	lane.errAt = time.Now().Add(-time.Hour)
	fail(keyB)
	if got := ran.Load(); got != 2 {
		t.Errorf("runs after an old final failure = %d; want 2", got)
	}
}

// TestQueryLane_TransientFailureIsRetried covers a failure that says
// nothing about the input — no bus, a refused capability, a pool still
// warming up. It is held for transientRetryDelay, so the lane does not
// spin, and then re-run for the same input without the user editing.
func TestQueryLane_TransientFailureIsRetried(t *testing.T) {
	t.Parallel()

	var lane queryLane[string]
	var ran atomic.Int32
	demand := func(key queryKey) {
		lane.demand(key, "test", func(ctx context.Context) (out string, err error) {
			if ran.Add(1) == 1 {
				err = eh.Errorf("chlocalbroker: bus request timed out")
				return
			}
			out = "ok"
			return
		})
	}

	keyA := makeQueryKey("A")
	demand(keyA)
	waitFor(t, func() bool {
		lane.drain()
		return lane.failedFor(keyA)
	}, "lane to record the failure")
	if lane.errFinal {
		t.Fatalf("a transport failure was classified as final")
	}

	for range 10 {
		demand(keyA)
	}
	if got := ran.Load(); got != 1 {
		t.Errorf("runs inside the retry delay = %d; want 1", got)
	}

	lane.errAt = time.Now().Add(-transientRetryDelay)
	demand(keyA)
	waitFor(t, func() bool {
		lane.drain()
		return lane.servedFor(keyA)
	}, "the retry to succeed")
	if v := lane.view(keyA); v.Err != nil || v.Value != "ok" {
		t.Errorf("view after retry = %+v; want ok and no error", v)
	}
}

// waitFor polls cond until it holds or the test times out. The lane is
// render-thread state driven by a worker goroutine, so tests advance it
// the way the render loop does — by calling drain and re-checking.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ---------------------------------------------------------------------------
// SQL builders — pure string composition, exact-match tests
// ---------------------------------------------------------------------------

func TestBuildFnsSQL(t *testing.T) {
	got := buildFnsSQL("it's", `h\w+`, "", []chFnE{fnMatch, fnRegexpExtract, fnExtractAll})
	want := `WITH 'it\'s' AS haystack SELECT match(haystack, 'h\\w+'), regexpExtract(haystack, 'h\\w+', 0), extractAll(haystack, 'h\\w+')`
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
	got = buildFnsSQL("hello", `(l+)`, `[\1]`, replaceFns)
	want = `WITH 'hello' AS haystack SELECT replaceRegexpOne(haystack, '(l+)', '[\\1]'), replaceRegexpAll(haystack, '(l+)', '[\\1]')`
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

func TestBuildExtractAllSQL(t *testing.T) {
	got := buildExtractAllSQL("a1 b22", `\d+`)
	want := `SELECT extractAll('a1 b22', '\\d+')`
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

func TestBuildMultiMatchSQL(t *testing.T) {
	cases := []struct {
		name     string
		haystack string
		patterns []string
		want     string
	}{
		{"two-patterns", "foo bar", []string{"foo", "bar"}, `SELECT multiMatchAllIndices('foo bar', ['foo', 'bar'])`},
		{"single", "foo", []string{"f.*"}, `SELECT multiMatchAllIndices('foo', ['f.*'])`},
		{"with-quotes", "it's", []string{"'", "t"}, `SELECT multiMatchAllIndices('it\'s', ['\'', 't'])`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildMultiMatchSQL(tc.haystack, tc.patterns)
			if got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

// TestTripwireRunsOncePerProcess pins the SD1 run's sharing and retry
// rule: a second explorer does not repeat a run, and a failed run — here
// the no-bus failure an embedded explorer meets before its host attaches
// one — is retried only once tripwireRetryDelay has passed.
//
// Not parallel: it resets the process-wide run.
func TestTripwireRunsOncePerProcess(t *testing.T) {
	sharedTripwire = tripwireRun{}
	t.Cleanup(func() { sharedTripwire = tripwireRun{} })
	finished := func() bool {
		tw, started, running := tripwireSnapshot()
		return started && !running && tw.Done
	}

	newTestApp(t).RunTripwire(context.Background()) // no bus: fails fast
	waitFor(t, finished, "the first run to finish")
	if tw, _, _ := tripwireSnapshot(); tw.Err == nil {
		t.Fatalf("a run without a bus reported no error")
	}
	firstAt := sharedTripwire.at

	newTestApp(t).RunTripwire(context.Background())
	if _, _, running := tripwireSnapshot(); running || sharedTripwire.at != firstAt {
		t.Fatalf("a second explorer re-ran a failed check inside the retry delay")
	}

	sharedTripwire.mu.Lock()
	sharedTripwire.at = time.Now().Add(-tripwireRetryDelay)
	sharedTripwire.mu.Unlock()
	newTestApp(t).RunTripwire(context.Background())
	waitFor(t, func() bool {
		sharedTripwire.mu.Lock()
		defer sharedTripwire.mu.Unlock()
		return !sharedTripwire.running && sharedTripwire.at.After(firstAt)
	}, "a retry once the delay passed")
}

// TestCompileCacheIsBounded pins the cap on the compile cache: typing
// leaves every prefix behind, so it must not grow with the session.
func TestCompileCacheIsBounded(t *testing.T) {
	t.Parallel()
	inst := newTestApp(t)
	for i := range maxCompileCache + 50 {
		_, _ = inst.getCompiledRegexp(strconv.Itoa(i) + "x")
	}
	if n := len(inst.compileCache); n > maxCompileCache {
		t.Errorf("compile cache holds %d entries; cap is %d", n, maxCompileCache)
	}
	if _, err := inst.getCompiledRegexp(`\d+`); err != nil {
		t.Errorf("compile after a reset: %v", err)
	}
}

// TestClickHouseMessageNamesARefusedCapability pins the wording an
// explorer embedded in a host without the ch.local grant shows: the
// broker's transport chain reduced to what the user can act on.
func TestClickHouseMessageNamesARefusedCapability(t *testing.T) {
	t.Parallel()
	bus := inprocbus.NewInst(zerolog.Nop())
	caller := bus.NewClient("test.no-grant", nil)
	_, _, err := executeArrowStreamViaBus(context.Background(), caller, "SELECT 1", memory.NewGoAllocator())
	if err == nil {
		t.Fatalf("a client without the grant reached the broker")
	}
	if got := clickHouseMessage(err); !strings.Contains(got, ChLocalCapPattern) || strings.Contains(got, "chlocalbroker") {
		t.Errorf("clickHouseMessage(%v) = %q; want the capability named and the transport chain gone", err, got)
	}
}
