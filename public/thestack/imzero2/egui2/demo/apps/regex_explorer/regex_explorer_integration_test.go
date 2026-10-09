//go:build integration

package regex_explorer

// Tests against a real clickhouse-local, behind the integration build tag
// (run by scripts/ci/gotest-integration.sh): they stand up an in-proc bus
// and a chlocalbroker per test and skip when the binary is not on PATH.

import (
	"context"
	"reflect"
	"regexp"
	"slices"
	"strings"
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
)

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

	// The Multi tab's wording of a real refusal: the pattern as typed, and
	// the index pointing into it.
	_, err := runMultiMatchBlocking(ctx, inst, "xa", []string{inst.effectivePattern(`(?U)a+`)})
	if err == nil {
		t.Fatalf("VectorScan accepted (?U)a+")
	}
	if got := rejectionText(clickHouseMessage(err)); !strings.Contains(got, "Pattern '(?U)a+'") || !strings.Contains(got, "at index 0") {
		t.Errorf("rejection worded as %q; want the typed pattern and an index into it", got)
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
