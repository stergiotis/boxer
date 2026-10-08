package regex_explorer

// Unit + integration tests for the regex explorer's testable surface.
//
// State is per-[App]: every test allocates its own via newTestApp, so flag
// state and the compile cache cannot leak between cases and nothing has to
// be reset on setup.
//
// Integration tests that shell out to `clickhouse local` skip when the
// binary is not on PATH, so the suite stays usable on machines without
// ClickHouse installed.

import (
	"context"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
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

// skipIfNoClickHouseLocal short-circuits integration tests when the
// clickhouse binary is absent — avoids hard-failing on machines
// that do not have ClickHouse installed.
func skipIfNoClickHouseLocal(t *testing.T) {
	t.Helper()
	if _, ok := extbin.ClickHouseLocal.Resolve(); !ok {
		t.Skip("clickhouse not on PATH")
	}
}

// setupTestBus stands up an in-proc bus + chlocalbroker.Service and
// returns a bus client with the regex_explorer cap. The broker (and
// its pool) is torn down on test cleanup. Skips if clickhouse
// is not on PATH.
func setupTestBus(t *testing.T) (caller runtimeapp.BusI) {
	t.Helper()
	skipIfNoClickHouseLocal(t)
	logger := zerolog.New(zerolog.NewTestWriter(t))
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(15 * time.Second)

	poolCfg := chlocalpool.Config{
		BaseTmpDir:       t.TempDir(),
		MinIdle:          1,
		MaxConcurrent:    2,
		SpawnConcurrency: 1,
		SpawnTimeout:     5 * time.Second,
	}
	svc, err := chlocalbroker.NewService(bus, poolCfg, logger)
	if err != nil {
		t.Fatalf("chlocalbroker.NewService: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})

	caller = bus.NewClient("test.regex_explorer", []runtimeapp.SubjectFilter{
		{Pattern: ChLocalCapPattern, Direction: runtimeapp.CapDirectionPub, Reason: "test"},
	})
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

// TestRunPatternFnsBlocking pins what the pattern-functions query returns
// and the gate in front of extractAllGroups, which ClickHouse rejects for
// a pattern without a capture group — and interactive typing produces
// group-less patterns constantly.
func TestRunPatternFnsBlocking(t *testing.T) {
	bus := setupTestBus(t)
	inst := newTestApp(t)
	inst.setBus(bus)
	ctx := context.Background()

	t.Run("with-groups", func(t *testing.T) {
		out, err := runPatternFnsBlocking(ctx, inst, "alice@example.com bob@test.org", `(\w+)@([\w.]+)`, 2)
		if err != nil {
			t.Fatalf("runPatternFnsBlocking: %v", err)
		}
		want := fnOutcome{
			Match:         true,
			Count:         2,
			Extract:       "alice",
			RegexpExtract: "alice@example.com",
			ExtractAll:    []string{"alice", "bob"},
			Groups:        [][]string{{"alice", "example.com"}, {"bob", "test.org"}},
			YieldsGroups:  true,
		}
		if !reflect.DeepEqual(out, want) {
			t.Errorf("got %+v; want %+v", out, want)
		}
	})

	t.Run("without-groups", func(t *testing.T) {
		out, err := runPatternFnsBlocking(ctx, inst, "a1 b22", `\d+`, 0)
		if err != nil {
			t.Fatalf("runPatternFnsBlocking: %v", err)
		}
		if out.YieldsGroups || out.Groups != nil {
			t.Errorf("group-less pattern reported groups: %+v", out)
		}
		if want := []string{"1", "22"}; !reflect.DeepEqual(out.ExtractAll, want) {
			t.Errorf("ExtractAll = %q; want %q", out.ExtractAll, want)
		}
	})

	t.Run("extractAllGroups-rejects-group-less-pattern", func(t *testing.T) {
		// The reason numGroups gates the call rather than the app just
		// always asking. If this ever starts succeeding, the gate can go.
		_, err := runQueryBlocking(ctx, inst, "groups", buildFnsSQL("abc", `a`, "", []chFnE{fnExtractAllGroups}), decodeStringLists("groups"))
		if err == nil {
			t.Fatalf("expected ClickHouse to reject extractAllGroups on a group-less pattern")
		}
		if !strings.Contains(err.Error(), "no groups in regexp") {
			t.Errorf("err = %v; expected the BAD_ARGUMENTS 'no groups in regexp' text", err)
		}
	})

	t.Run("replace", func(t *testing.T) {
		out, err := runReplaceFnsBlocking(ctx, inst, "hello", `l`, `[\0]`)
		if err != nil {
			t.Fatalf("runReplaceFnsBlocking: %v", err)
		}
		if want := (replaceOutcome{One: "he[l]lo", All: "he[l][l]o"}); out != want {
			t.Errorf("got %+v; want %+v", out, want)
		}
	})
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

// ---------------------------------------------------------------------------
// Integration tests against `clickhouse local`
//
// These tests stand up an in-proc bus + chlocalbroker.Service per test
// and exercise the production path (executeArrowStreamViaBus). They
// skip automatically if the binary is not on PATH.
// ---------------------------------------------------------------------------

// TestRunTripwireBlocking_Ledger is SD1 run for real against
// clickhouse-local. It asserts the ledger in both directions:
//
//   - no corpus case diverges unless it says it will — an unexpected
//     entry in drifts means Go and ClickHouse have actually parted
//     company somewhere we assumed they agreed;
//   - every case that says it will diverge still does — a KnownDrift
//     note whose difference has since been fixed upstream is stale, and
//     stale ledger entries silently shrink the tripwire's coverage.
func TestRunTripwireBlocking_Ledger(t *testing.T) {
	bus := setupTestBus(t)
	inst := newTestApp(t)
	inst.setBus(bus)

	drifts, known, err := inst.runTripwireBlocking(context.Background())
	if err != nil {
		t.Fatalf("runTripwireBlocking: %v", err)
	}

	for _, i := range drifts {
		tc := tripwireCorpus[i]
		t.Errorf("unexpected Go/ClickHouse divergence: case %q, pattern %q, haystack %q",
			tc.Name, tc.effective(), tc.Haystack)
	}

	inKnown := make(map[int]bool, len(known))
	for _, i := range known {
		inKnown[i] = true
	}
	for i, tc := range tripwireCorpus {
		if tc.KnownDrift == "" {
			continue
		}
		if !inKnown[i] {
			t.Errorf("stale ledger entry: case %q declares KnownDrift (%s) but the engines now agree — drop the note",
				tc.Name, tc.KnownDrift)
		}
	}
}

// TestPredictFunctions_AgainstClickHouse checks the Go model of every
// modelled function — and the way the Functions tab prints its values —
// over a grid of patterns that can match the empty string, or lean on the
// context around a match, against haystacks chosen to put empty matches
// first, last, between and abutting real ones. Each cell asks ClickHouse
// to print its own value with toString, so one query carries the grid,
// and a cell agrees only if the model's value and its rendering both do.
func TestPredictFunctions_AgainstClickHouse(t *testing.T) {
	inst := newTestApp(t)
	inst.setBus(setupTestBus(t))

	patterns := []string{
		`a*`, `a*?`, `a+`, `\w*`, `\s*`, `[^a]*`, `.?`, `a?b?`, `(?:ab)*`,
		`\b`, `\B`, `\b\w*`, `\w*\b`, `^`, `$`, `^a*`, `a*$`,
		`(?m)^`, `(?m)$`, `(?m)^\w*`, `x*|a`, `a|`, `(a)*`, `(a*)(b*)`, `(b)|a*`, `(x)?a`,
	}
	haystacks := []string{
		"", "a", "aa", "aax", "xaa", "aaxa", "a a", "ab cd", "ab\ncd", "aba", "\n\n", "ü a", "b", "xyz", "it's",
	}
	type cell struct {
		pattern, haystack string
		fn                chFnE
	}
	var cells []cell
	var sql strings.Builder
	sql.WriteString("SELECT [")
	for _, p := range patterns {
		groups := regexp.MustCompile(p).NumSubexp() > 0
		for _, h := range haystacks {
			for _, fn := range patternFns {
				if fn == fnExtractAllGroups && !groups {
					continue
				}
				if len(cells) > 0 {
					sql.WriteString(", ")
				}
				cells = append(cells, cell{p, h, fn})
				expr := strings.ReplaceAll(fn.expr(p, ""), "haystack", marshalling.EscapeString(h))
				switch fn {
				case fnExtract, fnRegexpExtract:
					expr = "[" + expr + "]" // so the string prints quoted
				}
				sql.WriteString("toString(" + expr + ")")
			}
		}
	}
	sql.WriteString("]")

	got, err := runQueryBlocking(context.Background(), inst, "grid", sql.String(), decodeStrings("grid"))
	if err != nil {
		t.Fatalf("grid query: %v", err)
	}
	if len(got) != len(cells) {
		t.Fatalf("grid returned %d results for %d cells", len(got), len(cells))
	}
	type prediction struct {
		out            fnOutcome
		groupsModelled bool
	}
	analyses := map[[2]string]prediction{}
	compared := 0
	for i, c := range cells {
		k := [2]string{c.pattern, c.haystack}
		pr, ok := analyses[k]
		if !ok {
			a := newTestApp(t)
			a.pattern, a.haystack = c.pattern, c.haystack
			// The interactive path states the dot flag; ClickHouse here
			// reads the bare pattern under its own default, which is on.
			an := a.analysis()
			pr = prediction{predictFunctions(an), an.groupsModelled()}
			analyses[k] = pr
		}
		if c.fn == fnExtractAllGroups && !pr.groupsModelled {
			continue // the model declines here, and the tab says so
		}
		compared++
		predicted := pr.out
		want := fnValue(c.fn, predicted)
		switch c.fn {
		case fnExtract, fnRegexpExtract:
			want = "[" + want + "]"
		}
		if got[i] != want {
			t.Errorf("%s over %q: ClickHouse %s, model %s", c.fn.expr(c.pattern, ""), c.haystack, got[i], want)
		}
	}
	t.Logf("%d of %d cells compared", compared, len(cells))
}

func TestExecuteArrowStreamViaBus_Match(t *testing.T) {
	bus := setupTestBus(t)
	ctx := context.Background()
	alloc := memory.NewGoAllocator()

	rdr, closer, err := executeArrowStreamViaBus(ctx, bus, "SELECT match('foobar', 'foo.*')", alloc)
	if err != nil {
		t.Fatalf("executeArrowStreamViaBus: %v", err)
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil {
			t.Errorf("closer.Close: %v", cErr)
		}
	}()
	defer rdr.Release()

	if !rdr.Next() {
		t.Fatalf("rdr.Next returned false: err=%v", rdr.Err())
	}
	rec := rdr.Record()
	u8, ok := rec.Column(0).(*array.Uint8)
	if !ok {
		t.Fatalf("unexpected column type %T", rec.Column(0))
	}
	if u8.Value(0) != 1 {
		t.Errorf("match('foobar', 'foo.*') = %d; want 1", u8.Value(0))
	}
}

func TestExecuteArrowStreamViaBus_MultiMatch_TwoTrivial(t *testing.T) {
	// Reproduces the reported case: two trivial patterns should not
	// produce a ClickHouse error. Uses the exact SQL the UI would build.
	bus := setupTestBus(t)
	ctx := context.Background()
	alloc := memory.NewGoAllocator()

	sql := buildMultiMatchSQL("foo bar baz", []string{"foo", "bar"})
	rdr, closer, err := executeArrowStreamViaBus(ctx, bus, sql, alloc)
	if err != nil {
		t.Fatalf("executeArrowStreamViaBus: %v\nsql: %s", err, sql)
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil {
			t.Errorf("closer.Close: %v\nsql: %s", cErr, sql)
		}
	}()
	defer rdr.Release()

	if !rdr.Next() {
		t.Fatalf("rdr.Next returned false: err=%v", rdr.Err())
	}
	rec := rdr.Record()
	list, ok := rec.Column(0).(*array.List)
	if !ok {
		t.Fatalf("unexpected column type %T", rec.Column(0))
	}
	inner, ok := list.ListValues().(*array.Uint64)
	if !ok {
		t.Fatalf("unexpected inner type %T", list.ListValues())
	}
	offsets := list.Offsets()
	var hits []uint64
	for i := int(offsets[0]); i < int(offsets[1]); i++ {
		hits = append(hits, inner.Value(i))
	}
	// multiMatchAllIndices does not promise sorted output — VectorScan
	// reports hits in match order, so ['^foo$','f.o'] over "foo" comes
	// back as [2,1]. The UI keys hits by index rather than position, so
	// sort before comparing instead of asserting an accidental order.
	slices.Sort(hits)
	wantHits := []uint64{1, 2}
	if !reflect.DeepEqual(hits, wantHits) {
		t.Errorf("multiMatchAllIndices hits = %v; want %v", hits, wantHits)
	}
}

func TestExecuteArrowStreamViaBus_InvalidRegex(t *testing.T) {
	// ClickHouse should reject `bad(regex`. With the bus path, the
	// worker's stderr is captured by the broker and surfaced via
	// ExecOnPool's reply.Err(); executeArrowStreamViaBus wraps that
	// into a single error before the Arrow reader is constructed.
	bus := setupTestBus(t)
	ctx := context.Background()
	alloc := memory.NewGoAllocator()

	sql := "SELECT match('foo', 'bad(regex')"
	_, _, err := executeArrowStreamViaBus(ctx, bus, sql, alloc)
	if err == nil {
		t.Fatalf("expected an error for invalid regex; got nil")
	}
	if !strings.Contains(err.Error(), "CANNOT_COMPILE_REGEXP") && !strings.Contains(err.Error(), "OptimizedRegularExpression") {
		t.Errorf("err = %v; expected CH regex-compile error text in the message", err)
	}
	// The lane's retry policy keys on this: a rejection is final for its
	// input, so it must classify as one through the real broker.
	if !isEngineRejection(err) {
		t.Errorf("isEngineRejection(%v) = false; want true", err)
	}
}

// TestIsVectorScanRejection_RealBroker pins the text-based classifier the
// SD1 VectorScan probe uses against what the broker actually returns: a
// pattern Go accepts but VectorScan refuses is a rejection (accepted=false,
// no error), and a missing bus is a transport failure, not a rejection.
func TestIsVectorScanRejection_RealBroker(t *testing.T) {
	inst := newTestApp(t)
	inst.setBus(setupTestBus(t))
	ctx := context.Background()

	for _, pattern := range []string{`(?U)a+`, `a{1001}`} {
		accepted, err := inst.tripwireVectorScanAccepts(ctx, pattern, "xa")
		if err != nil || accepted {
			t.Errorf("tripwireVectorScanAccepts(%q) = %v, %v; want false, nil", pattern, accepted, err)
		}
	}
	if accepted, err := inst.tripwireVectorScanAccepts(ctx, `a+`, "xa"); err != nil || !accepted {
		t.Errorf("tripwireVectorScanAccepts(a+) = %v, %v; want true, nil", accepted, err)
	}

	inst.setBus(nil)
	if _, err := inst.tripwireVectorScanAccepts(ctx, `(?U)a+`, "xa"); err == nil {
		t.Errorf("no bus: want a transport error, got nil")
	}
}

func TestExecuteArrowStreamViaBus_EmptyHaystack(t *testing.T) {
	// Hypothesis check: empty haystack with a non-empty pattern list is
	// a common UI state while the user is still typing. Must not error.
	bus := setupTestBus(t)
	ctx := context.Background()
	alloc := memory.NewGoAllocator()

	sql := buildMultiMatchSQL("", []string{"foo", "bar"})
	rdr, closer, err := executeArrowStreamViaBus(ctx, bus, sql, alloc)
	if err != nil {
		t.Fatalf("executeArrowStreamViaBus: %v\nsql: %s", err, sql)
	}
	defer func() {
		cErr := closer.Close()
		if cErr != nil {
			t.Errorf("closer.Close: %v", cErr)
		}
	}()
	defer rdr.Release()

	if !rdr.Next() {
		t.Fatalf("rdr.Next returned false: err=%v", rdr.Err())
	}
}
