//go:build llm_generated_opus47

package regex_explorer

// Unit + integration tests for the regex explorer's testable surface.
//
// The package currently uses a package-global [App] (var app). Tests that
// depend on flag state or cached compiles reset the relevant fields on
// setup — a richer future refactor would take the App as a parameter so
// tests could run t.Parallel, but for the current scope (single-user
// interactive demo) the global is fine and the setup hooks are small.
//
// Integration tests that shell out to `clickhouse local` skip when the
// binary is not on PATH, so the suite stays usable on machines without
// ClickHouse installed.

import (
	"context"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// resetAppFlags restores the package-global [app] to the flag state
// shared across tests — no regex flags, empty compile cache entry for
// patterns we're about to exercise.
func resetAppFlags(t *testing.T) {
	t.Helper()
	app.caseInsensitive = false
	app.multiline = false
	app.dotAll = false
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
	resetAppFlags(t)

	cases := []struct {
		name  string
		setup func()
		base  string
		want  string
	}{
		{name: "empty-base-no-flags", setup: func() {}, base: "", want: ""},
		{name: "no-flags", setup: func() {}, base: "foo", want: "foo"},
		{name: "case-insensitive", setup: func() { app.caseInsensitive = true }, base: "foo", want: "(?i)foo"},
		{name: "multiline", setup: func() { app.multiline = true }, base: "^x$", want: "(?m)^x$"},
		{name: "dotall", setup: func() { app.dotAll = true }, base: ".", want: "(?s)."},
		{name: "all-three", setup: func() {
			app.caseInsensitive = true
			app.multiline = true
			app.dotAll = true
		}, base: "foo", want: "(?ims)foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetAppFlags(t)
			tc.setup()
			got := effectivePattern(tc.base)
			if got != tc.want {
				t.Errorf("effectivePattern(%q) = %q; want %q", tc.base, got, tc.want)
			}
		})
	}
}

func TestParseAndValidatePatternList(t *testing.T) {
	resetAppFlags(t)

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
			resetAppFlags(t)
			got := parseAndValidatePatternList(tc.input)
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
			if got != tc.want{
				t.Errorf("countValidMultiLines(%v) = %d; want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestCountMatches(t *testing.T) {
	resetAppFlags(t)

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
		{"invalid-pattern", `\d(+`, "text", -1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetAppFlags(t)
			n, err := countMatches(tc.pattern, tc.haystack)
			if (err != nil) != tc.wantErr {
				t.Errorf("countMatches err=%v; wantErr=%v", err, tc.wantErr)
			}
			if n != tc.wantN {
				t.Errorf("countMatches n=%d; want %d", n, tc.wantN)
			}
		})
	}
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
		if cErr != nil{
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
		if cErr != nil{
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
		if cErr != nil{
			t.Errorf("closer.Close: %v", cErr)
		}
	}()
	defer rdr.Release()

	if !rdr.Next() {
		t.Fatalf("rdr.Next returned false: err=%v", rdr.Err())
	}
}
