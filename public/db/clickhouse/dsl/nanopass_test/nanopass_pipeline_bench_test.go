package nanopass_test

// Benchmarks for the composed pre-execute pipeline — what a play Run pays
// between pressing the button and the statement leaving the process — over a
// real applet buffer rather than a synthetic one. Run with:
//
//	go test -bench BenchmarkPlayPipeline -benchmem -run xxx ./public/db/clickhouse/dsl/nanopass_test/
//
// # Why a real buffer
//
// The synthetic sizes in nanopass_bench_test.go bound the per-parse cost, but
// they do not show what that cost turns into for a consumer, because they are
// parsed once. The pipeline is a Sequence, and a Sequence hands each child the
// previous child's **output string** — so the statement is re-parsed once per
// pass. play's stage runs CanonicalizeFull (12 sub-passes, four of them fixed
// points, so ~16 parses on its own) plus the standard set — measured at about
// 34 parses of the statement for one Run.
//
// The fixture is where that stopped being theoretical. It is the ADR-0191
// event-timeline applet as it stood before §SD7 moved its extraction into a
// provider: ~9 KB, twelve membership kinds, a dozen LW_GET calls. Against a
// ClickHouse answering in 90 ms it cost seconds per Run, and the profile was
// flat — every sub-pass costing about one parse, whether or not it rewrote
// anything. Roughly eight of the twelve rewrote nothing at all.
//
// So this benchmark exists to make one number visible: what the pipeline costs
// on an input a person actually wrote, and how many parses that is. Divide
// BenchmarkPlayPipelineApply/applet_9kb by BenchmarkPlayPipelineReparseFloor
// — on the machine this was written on the ratio was about 34, against a
// statement that a person edits as one thing. A parse memoized on the input
// text is the obvious thing to try against it: consecutive passes are
// frequently handed byte-identical SQL, and a pass that rewrites nothing then
// costs nothing instead of a parse.
//
// # Hermetic on purpose
//
// The fixture's schema and membership ids are embedded, not read from the
// keelson packages the query belongs to. Two reasons: a dsl benchmark should
// not track an application's schema, and a benchmark wants a fixed input —
// a fixture that moved when a leeway aspect changed would report a
// performance regression that is really a schema edit.

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// runtimeTimelineAppletSQL is the applet buffer described above, with its four
// parameters bound to their defaults so the fixture is a complete statement.
//
//go:embed testdata/runtime_timeline_applet.sql
var runtimeTimelineAppletSQL string

// The physical column lists the buffer's leeway handles and LW_GET calls
// resolve against. Snapshots, so the benchmark neither needs a server nor
// moves when a schema does.
//
//go:embed testdata/boxer_facts_columns.txt
var boxerFactsColumns string

//go:embed testdata/boxer_persiststate_columns.txt
var boxerPersiststateColumns string

func splitColumns(raw string) (names []string) {
	for l := range strings.SplitSeq(strings.TrimSpace(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			names = append(names, l)
		}
	}
	return
}

// benchMembershipIds resolves the vocabulary names the fixture mentions.
//
// The ids are synthetic and sequential: what an expansion costs depends on
// the shape it expands into, not on which 19-digit number it embeds, and
// binding this to the real registry would make a dsl benchmark depend on a
// keelson vocabulary. Unknown names error, matching the real lookup — a
// silent zero would expand into a predicate that matches nothing and quietly
// change what is being measured.
type benchMembershipIds map[string]uint64

func (inst benchMembershipIds) LookupMembership(name string) (id uint64, err error) {
	id, ok := inst[name]
	if !ok {
		err = fmt.Errorf("bench: no such membership: %s", name)
	}
	return
}

// newBenchMembershipIds mints one id per name the fixture uses. Sorted first,
// so the assignment is the same on every run.
func newBenchMembershipIds() (ids benchMembershipIds) {
	names := []string{
		"runtimeApp", "runtimeAuditRequestSubject", "runtimeAuditResult",
		"runtimeColWidthScope", "runtimeColWidthTier",
		"runtimeKindAppLifecycle", "runtimeKindAudit", "runtimeKindColumnWidth",
		"runtimeKindEvent", "runtimeKindGrant", "runtimeKindLaunch",
		"runtimeKindLog", "runtimeKindQueryRun", "runtimeKindRuntimeHeartbeat",
		"runtimeKindRuntimeRun", "runtimeKindState", "runtimeKindWorkingset",
		"runtimeLaunchCaller", "runtimeLifecyclePhase",
		"runtimeLifecycleStopReason", "runtimeLifecycleTileKey",
		"runtimeLogLevel", "runtimeLogMessage", "runtimePersistKey",
		"runtimeQueryRunEventType", "runtimeRun", "runtimeRunHostname",
		"runtimeRunPid", "runtimeSubjectFilterDirection",
		"runtimeSubjectFilterPattern", "runtimeWorkingsetName",
	}
	sort.Strings(names)
	// The band real runtime ids occupy, so the emitted literals are the same
	// width as the ones a live expansion embeds.
	const base = uint64(9223372049739677696)
	ids = make(benchMembershipIds, len(names))
	for i, n := range names {
		ids[n] = base + uint64(i)
	}
	return
}

// benchPassBinding is what play hands ApplyBestEffortBound: one value the
// late-bound factories each assert their own seam off. Without it the
// LW_GET expansion and the handle resolution decline, and the benchmark
// measures a pipeline missing the two passes the fixture most exercises.
type benchPassBinding struct {
	*lwsql.Resolver
	passes.SchemaProviderI
	benchMembershipIds
}

func newBenchPassBinding() *benchPassBinding {
	provider := passes.NewStaticSchemaProvider(map[string][]string{
		"boxer.facts":        splitColumns(boxerFactsColumns),
		"boxer.persiststate": splitColumns(boxerPersiststateColumns),
	})
	return &benchPassBinding{
		Resolver:           lwsql.NewResolver(provider),
		SchemaProviderI:    provider,
		benchMembershipIds: newBenchMembershipIds(),
	}
}

// newPlayStagedRegistry composes the pre-execute stage play runs: the standard
// set, plus CanonicalizeFull ordered ahead of it.
//
// It mirrors play.RegisterPasses rather than calling it — importing an app
// from a dsl benchmark would invert the dependency and drag the whole UI stack
// into this package. The two must move together; the maxIter and order below
// are play's.
func newPlayStagedRegistry(tb testing.TB) *passreg.Registry {
	tb.Helper()
	reg := passreg.NewRegistry()
	if err := passregdefaults.RegisterStandard(reg); err != nil {
		tb.Fatal(err)
	}
	if err := reg.Register(passreg.Entry{
		Pass:        passes.CanonicalizeFull(100),
		Stage:       passreg.StagePreExecute,
		Order:       50,
		Description: "rewrite the statement into canonical form",
		Provenance:  "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes",
	}); err != nil {
		tb.Fatal(err)
	}
	return reg
}

// pipelineInputs are the fixture beside the synthetic sizes, so one run shows
// how the cost scales from a query somebody types to one somebody generates.
func pipelineInputs() []struct {
	name string
	sql  string
} {
	return []struct {
		name string
		sql  string
	}{
		{"small", benchSmallSQL},
		{"medium", benchMediumSQL},
		{"applet_9kb", runtimeTimelineAppletSQL},
	}
}

// BenchmarkPlayPipelineApply is the headline: one full pre-execute stage, the
// unit a Run pays.
func BenchmarkPlayPipelineApply(b *testing.B) {
	reg := newPlayStagedRegistry(b)
	binding := newBenchPassBinding()
	logger := zerolog.Nop()
	for _, in := range pipelineInputs() {
		b.Run(in.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				out := reg.ApplyBestEffortBound(passreg.StagePreExecute, in.sql, binding, logger)
				if out == "" {
					b.Fatal("pipeline produced an empty statement")
				}
			}
		})
	}
}

// BenchmarkPlayPipelineCanonicalizeSubPasses breaks the dominant half open.
//
// Each sub-pass is timed on the SAME input rather than on its predecessor's
// output, which is what makes the shape visible: the times come out roughly
// equal, because each is paying for one parse of the statement and the
// rewriting is noise beside it. Constructors is the exception that shows the
// rule — it is the one fixed point that finds something to rewrite here, so it
// runs a second iteration and costs about two parses.
//
// The finding is that cost is per pass, not per rewrite: on this fixture most
// of these produce byte-identical output and still pay a full parse for it.
func BenchmarkPlayPipelineCanonicalizeSubPasses(b *testing.B) {
	sql := runtimeTimelineAppletSQL
	for _, sub := range []struct {
		name string
		pass nanopass.Pass
	}{
		{"InsertWrapper", passes.CanonicalizeInsertWrapper},
		{"WhitespaceSingleLine", passes.CanonicalizeWhitespaceSingleLine},
		{"Equals", passes.CanonicalizeEquals},
		{"Sugar", passes.CanonicalizeSugar},
		{"Constructors", nanopass.FixedPoint(passes.CanonicalizeConstructors(passes.ConstructorFormFunction), 100)},
		{"CaseConditionals", nanopass.FixedPoint(passes.CanonicalizeCaseConditionals, 100)},
		{"MultiIf", passes.CanonicalizeMultiIf},
		{"Casts", nanopass.FixedPoint(passes.CanonicalizeCasts, 100)},
		{"Join", passes.CanonicalizeJoin},
		{"Ternary", nanopass.FixedPoint(passes.CanonicalizeTernary, 100)},
		{"KeywordCase", passes.CanonicalizeKeywordCase},
		{"Identifiers", passes.CanonicalizeIdentifiers},
	} {
		b.Run(sub.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := sub.pass.Run(sql); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkPlayPipelineReparseFloor is the lower bound the pipeline cannot go
// under as it stands: one bare parse of the fixture. Compare it against
// BenchmarkPlayPipelineApply/applet_9kb — the ratio is how many times the
// statement is parsed, and it is the number a shared-parse change would move.
func BenchmarkPlayPipelineReparseFloor(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := nanopass.Parse(runtimeTimelineAppletSQL); err != nil {
			b.Fatal(err)
		}
	}
}
