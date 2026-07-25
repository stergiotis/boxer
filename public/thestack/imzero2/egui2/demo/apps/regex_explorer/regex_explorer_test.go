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
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

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
// clickhouse-local binary is absent — avoids hard-failing on machines
// that do not have ClickHouse installed.
func skipIfNoClickHouseLocal(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("clickhouse-local"); err != nil {
		t.Skipf("clickhouse-local not on PATH: %v", err)
	}
}

// setupTestBus stands up an in-proc bus + chlocalbroker.Service and
// returns a bus client with the regex_explorer cap. The broker (and
// its pool) is torn down on test cleanup. Skips if clickhouse-local
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
		{Pattern: chLocalCapPattern, Direction: runtimeapp.CapDirectionPub, Reason: "test"},
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
		{name: "empty-base-no-flags", setup: func(*App) {}, base: "", want: ""},
		{name: "no-flags", setup: func(*App) {}, base: "foo", want: "foo"},
		{name: "case-insensitive", setup: func(inst *App) { inst.caseInsensitive = true }, base: "foo", want: "(?i)foo"},
		{name: "multiline", setup: func(inst *App) { inst.multiline = true }, base: "^x$", want: "(?m)^x$"},
		{name: "dotall", setup: func(inst *App) { inst.dotAll = true }, base: ".", want: "(?s)."},
		{name: "all-three", setup: func(inst *App) {
			inst.caseInsensitive = true
			inst.multiline = true
			inst.dotAll = true
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

func TestCountMatches(t *testing.T) {
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
		// Zero-width matches are not counted — ClickHouse's extractAll
		// reports none for these, and the preview follows ClickHouse so
		// the status bar and the List tab tell one story (ADR-0054 SD1
		// known-difference ledger, case empty-matchable-star).
		{"empty-matchable-star", `a*`, "xyz", 0, false},
		{"empty-matchable-opt", `q?`, "xyz", 0, false},
		{"mixed-empty-and-real", `a*`, "xayz", 1, false},
		{"boundary-is-zero-width", `\b`, "hi there", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, err := newTestApp(t).countMatches(tc.pattern, tc.haystack)
			if (err != nil) != tc.wantErr {
				t.Errorf("countMatches err=%v; wantErr=%v", err, tc.wantErr)
			}
			if n != tc.wantN {
				t.Errorf("countMatches n=%d; want %d", n, tc.wantN)
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
// contract: a lane that failed must not spin re-issuing the same doomed
// query every frame, but must try again as soon as the input changes.
func TestQueryLane_FailureIsNotRetriedForSameInput(t *testing.T) {
	t.Parallel()

	var lane queryLane[string]
	var ran atomic.Int32
	fail := func(key queryKey) {
		lane.demand(key, "test", func(ctx context.Context) (out string, err error) {
			ran.Add(1)
			err = eh.Errorf("nope")
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

func TestBuildMatchSQL(t *testing.T) {
	got := buildMatchSQL("hello", `h\w+`)
	want := `SELECT match('hello', 'h\\w+')`
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

func TestBuildExtractAllGroupsSQL(t *testing.T) {
	got := buildExtractAllGroupsSQL("a@b.c", `(\w+)@([\w.]+)`)
	want := `SELECT extractAllGroups('a@b.c', '(\\w+)@([\\w.]+)')`
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// TestRunListOutcomeBlocking_CaptureGroups pins the two behaviours that
// make the List tab honest about capture groups:
//
//   - extractAll returns capture group 1, not the full match, whenever the
//     pattern captures — so YieldsGroups must be set and the tab must say
//     so, or it silently contradicts the Preview tab's full-match
//     highlighting;
//   - extractAllGroups is only asked for when the pattern actually
//     captures, because ClickHouse rejects it outright otherwise, and
//     interactive typing produces group-less patterns constantly.
func TestRunListOutcomeBlocking_CaptureGroups(t *testing.T) {
	bus := setupTestBus(t)
	inst := newTestApp(t)
	inst.setBus(bus)
	ctx := context.Background()

	t.Run("with-groups", func(t *testing.T) {
		out, err := runListOutcomeBlocking(ctx, inst, "alice@example.com bob@test.org", `(\w+)@([\w.]+)`, 2)
		if err != nil {
			t.Fatalf("runListOutcomeBlocking: %v", err)
		}
		if !out.YieldsGroups {
			t.Errorf("YieldsGroups = false; extractAll returns group 1 for a capturing pattern")
		}
		// This is the divergence the tab has to explain: extractAll gives
		// the local parts, while Go highlights the whole addresses.
		if want := []string{"alice", "bob"}; !reflect.DeepEqual(out.Matches, want) {
			t.Errorf("Matches = %q; want %q", out.Matches, want)
		}
		want := [][]string{{"alice", "example.com"}, {"bob", "test.org"}}
		if !reflect.DeepEqual(out.Groups, want) {
			t.Errorf("Groups = %q; want %q", out.Groups, want)
		}
	})

	t.Run("without-groups", func(t *testing.T) {
		out, err := runListOutcomeBlocking(ctx, inst, "a1 b22", `\d+`, 0)
		if err != nil {
			t.Fatalf("runListOutcomeBlocking: %v", err)
		}
		if out.YieldsGroups || out.Groups != nil {
			t.Errorf("group-less pattern reported groups: YieldsGroups=%v Groups=%v", out.YieldsGroups, out.Groups)
		}
		if want := []string{"1", "22"}; !reflect.DeepEqual(out.Matches, want) {
			t.Errorf("Matches = %q; want %q", out.Matches, want)
		}
	})

	t.Run("extractAllGroups-rejects-group-less-pattern", func(t *testing.T) {
		// The reason numGroups gates the call rather than the app just
		// always asking. If this ever starts succeeding, the gate can go.
		_, err := runExtractAllGroupsBlocking(ctx, inst, "abc", `a`)
		if err == nil {
			t.Fatalf("expected ClickHouse to reject extractAllGroups on a group-less pattern")
		}
		if !strings.Contains(err.Error(), "no groups in regexp") {
			t.Errorf("err = %v; expected the BAD_ARGUMENTS 'no groups in regexp' text", err)
		}
	})
}

func TestBuildReplaceAllSQL(t *testing.T) {
	got := buildReplaceAllSQL("hello", `l+`, "L")
	want := `SELECT replaceRegexpAll('hello', 'l+', 'L')`
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

func TestExecuteArrowStreamViaBus_Match(t *testing.T) {
	bus := setupTestBus(t)
	ctx := context.Background()
	alloc := memory.NewGoAllocator()

	rdr, closer, err := executeArrowStreamViaBus(ctx, bus, buildMatchSQL("foobar", "foo.*"), alloc)
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

	sql := buildMatchSQL("foo", "bad(regex")
	_, _, err := executeArrowStreamViaBus(ctx, bus, sql, alloc)
	if err == nil {
		t.Fatalf("expected an error for invalid regex; got nil")
	}
	if !strings.Contains(err.Error(), "CANNOT_COMPILE_REGEXP") && !strings.Contains(err.Error(), "OptimizedRegularExpression") {
		t.Errorf("err = %v; expected CH regex-compile error text in the message", err)
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
