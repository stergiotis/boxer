package callsites

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureDir returns the absolute path of the hermetic fixture module
// (std-only imports plus a replace'd local dep, ADR-0107 Consequences).
func fixtureDir(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "fixmod"))
	require.NoError(t, err)
	return abs
}

// newFixtureService isolates the run from any enclosing go.work: the fixture
// module is not a workspace member of this repository.
func newFixtureService(t *testing.T, adjudicate bool) *AnalyzerService {
	t.Helper()
	t.Setenv("GOWORK", "off")
	return &AnalyzerService{
		Dir:        fixtureDir(t),
		Adjudicate: adjudicate,
	}
}

func collectSites(t *testing.T, svc *AnalyzerService) (sites []CallSite) {
	t.Helper()
	for site, err := range svc.All(t.Context()) {
		require.NoError(t, err)
		sites = append(sites, site)
	}
	return
}

// markersOf maps "@name" markers to their 1-based line in a fixture file.
func markersOf(t *testing.T, file string) map[string]int {
	t.Helper()
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	markers := make(map[string]int, 32)
	for i, line := range strings.Split(string(data), "\n") {
		idx := strings.Index(line, "// @")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[idx+len("// "):])
		require.NotContains(t, markers, name, "duplicate marker in fixture")
		markers[name] = i + 1
	}
	return markers
}

// findAtMarker returns the call site on the marked line, nil when absent.
// Fixture lines carry at most one call expression by construction.
func findAtMarker(t *testing.T, sites []CallSite, dir string, relFile string, marker string) *CallSite {
	t.Helper()
	file := filepath.Join(dir, relFile)
	line, ok := markersOf(t, file)[marker]
	require.True(t, ok, "marker %s not present in %s", marker, relFile)
	var found *CallSite
	for i := range sites {
		if sites[i].File == file && sites[i].Line == line {
			require.Nil(t, found, "more than one site on marked line %s", marker)
			found = &sites[i]
		}
	}
	return found
}

func siteAtMarker(t *testing.T, sites []CallSite, dir string, relFile string, marker string) CallSite {
	t.Helper()
	found := findAtMarker(t, sites, dir, relFile, marker)
	require.NotNil(t, found, "no call site at marker %s", marker)
	return *found
}

func shapesOf(args []TypeArgInfo) (shapes []ShapeClassE) {
	for _, a := range args {
		shapes = append(shapes, a.Shape)
	}
	return
}

func TestClassification(t *testing.T) {
	svc := newFixtureService(t, false)
	dir := svc.Dir
	sites := collectSites(t, svc)

	type siteExpect struct {
		file       string
		marker     string
		callType   CallTypeE
		origin     OriginE
		funcIs     string // exact match when non-empty
		funcHas    string // substring match when non-empty
		argShapes  []ShapeClassE
		recvShapes []ShapeClassE
	}
	expectations := []siteExpect{
		{file: "main.go", marker: "@stdlib-mono", callType: CallTypeMonomorphic, origin: OriginStdLib, funcIs: "fmt.Println"},
		{file: "main.go", marker: "@local-mono", callType: CallTypeMonomorphic, origin: OriginLocal, funcHas: "LocalMono"},
		{file: "main.go", marker: "@conv-slice", callType: CallTypeConversion, origin: OriginUnknown, funcIs: "[]byte"},
		{file: "main.go", marker: "@conv-paren-pointer", callType: CallTypeConversion, origin: OriginUnknown, funcIs: "*S"},
		{file: "main.go", marker: "@conv-ident", callType: CallTypeConversion, origin: OriginUnknown, funcIs: "int64"},
		{file: "main.go", marker: "@funclit", callType: CallTypeMonomorphic, origin: OriginLocal, funcIs: "(func literal)"},
		{file: "main.go", marker: "@paren-generic", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: "ParenGen", argShapes: []ShapeClassE{ShapeClassStenciled}},
		{file: "main.go", marker: "@generic-value-recv", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: ").Val", recvShapes: []ShapeClassE{ShapeClassStenciled}},
		{file: "main.go", marker: "@generic-pointer-recv", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: ").Set", recvShapes: []ShapeClassE{ShapeClassStenciled}},
		{file: "main.go", marker: "@iface-devirt", callType: CallTypeDynamicPolymorphic, origin: OriginLocal, funcHas: ").MDyn"},
		{file: "main.go", marker: "@method-expr", callType: CallTypeDynamicPolymorphic, origin: OriginLocal, funcHas: ").MDyn"},
		{file: "main.go", marker: "@embedded-iface", callType: CallTypeDynamicPolymorphic, origin: OriginLocal, funcHas: ").MDyn"},
		{file: "main.go", marker: "@generic-iface-recv", callType: CallTypeDynamicPolymorphic, origin: OriginLocal, funcHas: ").MIG"},
		{file: "main.go", marker: "@mono-arg-pass", callType: CallTypeMonomorphic, origin: OriginLocal, funcHas: "RunDynamic"},
		{file: "main.go", marker: "@generic-struct-arg", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: "UseGeneric", argShapes: []ShapeClassE{ShapeClassStenciled}},
		{file: "main.go", marker: "@alias-arg", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: "Gen", argShapes: []ShapeClassE{ShapeClassStenciled}},
		{file: "main.go", marker: "@pointer-arg", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: "Gen", argShapes: []ShapeClassE{ShapeClassPointer}},
		{file: "main.go", marker: "@interface-arg", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: "Gen", argShapes: []ShapeClassE{ShapeClassInterface}},
		{file: "main.go", marker: "@same-module", callType: CallTypeMonomorphic, origin: OriginLocal, funcHas: "SameModuleFunc"},
		{file: "main.go", marker: "@third-party", callType: CallTypeMonomorphic, origin: Origin3rdParty, funcHas: "extdep.ExtFunc"},
		{file: "main.go", marker: "@func-value", callType: CallTypeDynamicPolymorphic, origin: OriginLocal, funcIs: "fns"},
		{file: "main.go", marker: "@builtin", callType: CallTypeBuiltin, origin: OriginStdLib, funcIs: "make"},
		{file: "main.go", marker: "@universe-method", callType: CallTypeDynamicPolymorphic, origin: OriginStdLib, funcHas: "Error"},
		{file: "main.go", marker: "@typeparam-recv", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: ").MDyn", recvShapes: []ShapeClassE{ShapeClassTypeParam}},
		{file: "main.go", marker: "@typeparam-passthrough", callType: CallTypeStaticPolymorphic, origin: OriginLocal, funcHas: "Gen", argShapes: []ShapeClassE{ShapeClassTypeParam}},
		{file: "dynamic.go", marker: "@true-dynamic", callType: CallTypeDynamicPolymorphic, origin: OriginLocal, funcHas: ").MDyn"},
		{file: "lib/lib.go", marker: "@lib-internal", callType: CallTypeMonomorphic, origin: OriginLocal, funcHas: "helper"},
	}
	for _, e := range expectations {
		site := siteAtMarker(t, sites, dir, e.file, e.marker)
		assert.Equal(t, e.callType, site.Type, "%s: call type of %s", e.marker, site)
		assert.Equal(t, e.origin, site.Origin, "%s: origin of %s", e.marker, site)
		if e.funcIs != "" {
			assert.Equal(t, e.funcIs, site.Func, "%s: func", e.marker)
		}
		if e.funcHas != "" {
			assert.Contains(t, site.Func, e.funcHas, "%s: func", e.marker)
		}
		assert.Equal(t, e.argShapes, shapesOf(site.TypeArgs), "%s: type args of %s", e.marker, site)
		assert.Equal(t, e.recvShapes, shapesOf(site.RecvTypeArgs), "%s: recv type args of %s", e.marker, site)
	}

	// The alias argument renders under its alias name and stays classified.
	alias := siteAtMarker(t, sites, dir, "main.go", "@alias-arg")
	require.Len(t, alias.TypeArgs, 1)
	assert.Equal(t, "IntSlice", alias.TypeArgs[0].Type)

	// Position is the opening parenthesis: "\ti.MDyn()" puts it at column 8.
	devirt := siteAtMarker(t, sites, dir, "main.go", "@iface-devirt")
	assert.Equal(t, 8, devirt.Col)

	// Test files are out of scope by default.
	assert.Nil(t, findAtMarker(t, sites, dir, "main_test.go", "@test-file-call"))

	// Nothing in the fixture is unresolvable, and nothing is fabricated.
	var summary Summary
	for _, s := range sites {
		summary.Add(s)
	}
	assert.Equal(t, uint64(len(sites)), summary.Total)
	assert.Zero(t, summary.Unknown)
	assert.Equal(t, uint64(3), summary.Conversions)
	assert.Equal(t, uint64(1), summary.Builtins)
	assert.GreaterOrEqual(t, summary.InterfaceArgs, uint64(1))
	assert.GreaterOrEqual(t, summary.PointerArgs, uint64(1))

	// The one shipped --fail-on predicate matches exactly the interface arg.
	assert.True(t, siteHasInterfaceTypeArg(siteAtMarker(t, sites, dir, "main.go", "@interface-arg")))
	assert.False(t, siteHasInterfaceTypeArg(siteAtMarker(t, sites, dir, "main.go", "@pointer-arg")))
}

func TestAdjudication(t *testing.T) {
	svc := newFixtureService(t, true)
	dir := svc.Dir
	sites := collectSites(t, svc)
	require.NotEmpty(t, sites)

	// Every non-test site was covered by the adjudication build.
	for _, s := range sites {
		assert.True(t, s.Compiler.Checked, "unchecked site %s", s.String())
	}

	// The locally-provable interface call is devirtualized (and inlined);
	// the opaque-parameter call in dynamic.go stays dynamic. If a toolchain
	// bump changes either verdict or the -m line format, this test is the
	// tripwire (ADR-0107 Consequences).
	devirt := siteAtMarker(t, sites, dir, "main.go", "@iface-devirt")
	assert.True(t, devirt.Compiler.Devirtualized, "%s", devirt)
	trueDynamic := siteAtMarker(t, sites, dir, "dynamic.go", "@true-dynamic")
	assert.False(t, trueDynamic.Compiler.Devirtualized, "%s", trueDynamic)

	// A parenthesized instantiated generic still joins by Lparen position.
	paren := siteAtMarker(t, sites, dir, "main.go", "@paren-generic")
	assert.True(t, paren.Compiler.InlinedCall, "%s", paren)
}

// TestAdjudicationRootShapes covers the two `go build` output regimes: a
// library-only pattern (no -o allowed) and a single main root (-o diverted
// to a temp directory so no binary lands in Dir).
func TestAdjudicationRootShapes(t *testing.T) {
	libOnly := newFixtureService(t, true)
	libOnly.Patterns = []string{"./lib"}
	dir := libOnly.Dir
	sites := collectSites(t, libOnly)
	site := siteAtMarker(t, sites, dir, "lib/lib.go", "@lib-internal")
	assert.True(t, site.Compiler.Checked)
	assert.True(t, site.Compiler.InlinedCall, "%s", site)

	singleMain := newFixtureService(t, true)
	singleMain.Patterns = []string{"."}
	sites = collectSites(t, singleMain)
	require.NotEmpty(t, sites)
	assert.NoFileExists(t, filepath.Join(dir, "fixmod"), "single-main build must not drop a binary into Dir")
}

// TestEarlyTermination locks the ADR-0107 §SD6 contract: breaking out of the
// stream must not panic the range-over-func machinery.
func TestEarlyTermination(t *testing.T) {
	svc := newFixtureService(t, false)
	n := 0
	for _, err := range svc.All(t.Context()) {
		require.NoError(t, err)
		n++
		break
	}
	require.Equal(t, 1, n)
}

func TestBuildTags(t *testing.T) {
	svc := newFixtureService(t, false)
	var stats LoadStats
	svc.OnLoadStats = func(s LoadStats) { stats = s }
	dir := svc.Dir
	base := collectSites(t, svc)
	assert.Nil(t, findAtMarker(t, base, dir, "tagged.go", "@tagged-call"))
	// The constraint-excluded file is visible as coverage loss, and test
	// files do not pollute the metric (ADR-0107 §SD5).
	assert.Equal(t, LoadStats{Packages: 2, IgnoredFiles: 1}, stats)

	tagged := newFixtureService(t, false)
	tagged.OnLoadStats = func(s LoadStats) { stats = s }
	tagged.BuildTags = []string{"fixtag"}
	sites := collectSites(t, tagged)
	site := siteAtMarker(t, sites, dir, "tagged.go", "@tagged-call")
	assert.Equal(t, CallTypeMonomorphic, site.Type)
	assert.Equal(t, OriginLocal, site.Origin)
	assert.Equal(t, LoadStats{Packages: 2, IgnoredFiles: 0}, stats)
}

func TestPatterns(t *testing.T) {
	svc := newFixtureService(t, false)
	svc.Patterns = []string{"./lib"}
	dir := svc.Dir
	sites := collectSites(t, svc)
	require.Len(t, sites, 1)
	libFile := filepath.Join(dir, "lib", "lib.go")
	assert.Equal(t, libFile, sites[0].File)
	assert.Contains(t, sites[0].Func, "helper")
}

func TestIncludeTests(t *testing.T) {
	svc := newFixtureService(t, true)
	svc.IncludeTests = true
	dir := svc.Dir
	sites := collectSites(t, svc)

	// Test files are scanned but stay outside the adjudication build.
	testSite := siteAtMarker(t, sites, dir, "main_test.go", "@test-file-call")
	assert.False(t, testSite.Compiler.Checked)
	prodSite := siteAtMarker(t, sites, dir, "main.go", "@local-mono")
	assert.True(t, prodSite.Compiler.Checked)

	// Test-variant packages repeat the production files; each file is
	// scanned once (no duplicate positions).
	type position struct {
		file string
		line int
		col  int
	}
	seen := make(map[position]struct{}, len(sites))
	for _, s := range sites {
		p := position{file: s.File, line: s.Line, col: s.Col}
		_, dup := seen[p]
		require.False(t, dup, "duplicate site %s", s.String())
		seen[p] = struct{}{}
	}
}

func TestParseCompilerDecisions(t *testing.T) {
	input := strings.Join([]string{
		"# example.com/fixmod",
		"./main.go:42:5: devirtualizing i.M to S",
		"./main.go:42:5: inlining call to S.M",
		"lib/lib.go:3:9: inlining call to helper",
		"<autogenerated>:1: inlining call to S.M",
		"./main.go:15:6: can inline Gen[go.shape.int]",
		"./main.go:26:14: zero-copy string->[]byte conversion",
		"/abs/other.go:7:3: inlining call to F",
	}, "\n")
	decisions := parseCompilerDecisions(bytes.NewBufferString(input), "/base")
	require.Len(t, decisions, 3)
	assert.Equal(t,
		CompilerDecision{Devirtualized: true, InlinedCall: true},
		decisions[posKey{file: "/base/main.go", line: 42, col: 5}])
	assert.Equal(t,
		CompilerDecision{InlinedCall: true},
		decisions[posKey{file: "/base/lib/lib.go", line: 3, col: 9}])
	assert.Equal(t,
		CompilerDecision{InlinedCall: true},
		decisions[posKey{file: "/abs/other.go", line: 7, col: 3}])
}
