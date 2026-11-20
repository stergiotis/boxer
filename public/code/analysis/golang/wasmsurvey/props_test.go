package wasmsurvey

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/packageprops"
)

func TestTierToState(t *testing.T) {
	for tier, want := range map[Tier]packageprops.WASMState{
		TierGreen:   packageprops.WASMCompiles,
		TierRed:     packageprops.WASMBlocked,
		TierYellow:  packageprops.WASMUnknown,
		TierUnknown: packageprops.WASMUnknown,
	} {
		if got := tierToState(tier); got != want {
			t.Errorf("tierToState(%v) = %v, want %v", tier, got, want)
		}
	}
}

func TestPatternsToPrefixes(t *testing.T) {
	got := patternsToPrefixes("m", []string{"./public/math/...", "./public/functional/..."})
	want := []string{"m/public/math", "m/public/functional"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if p := patternsToPrefixes("m", []string{"./..."}); p != nil {
		t.Errorf("whole-module pattern should yield nil (match all), got %v", p)
	}
}

func TestRenderHarvestGo(t *testing.T) {
	rows := []HarvestRow{
		{ImportPath: "example.com/a", WASMWASI: packageprops.WASMCompiles, WASMJS: packageprops.WASMBlocked, WASMFreestanding: packageprops.WASMUnknown},
	}
	src, err := renderHarvestGo(rows, "proptable")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, want := range []string{
		"package proptable",
		"var Table = packageprops.Table{",
		`ImportPath: "example.com/a"`,
		"packageprops.WASMCompiles",
		"packageprops.WASMBlocked",
		"packageprops.WASMUnknown",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("emitted Go missing %q:\n%s", want, s)
		}
	}
}

func TestInScope(t *testing.T) {
	pre := []string{"m/public/math"}
	if !inScope("m/public/math", pre) || !inScope("m/public/math/numerical", pre) {
		t.Error("exact and sub-path should be in scope")
	}
	if inScope("m/public/mathx", pre) {
		t.Error("sibling sharing a prefix must NOT be in scope")
	}
	if !inScope("anything", nil) {
		t.Error("nil prefixes = match all")
	}
}

// TestPropsRenderParseRoundTrip renders a props file from a verdict and parses
// it back, exercising both the generator and the harvester's ast parse without
// TinyGo or touching the tree.
func TestPropsRenderParseRoundTrip(t *testing.T) {
	pr := PackageReport{
		ImportPath: "example.com/foo",
		Name:       "foo",
		Targets: []TargetVerdict{
			{Target: TargetWASI, Static: TierGreen},
			{Target: TargetJS, Static: TierRed},
			{Target: TargetWasmUnknown, Static: TierUnknown},
		},
	}
	src, err := renderPropsFile(pr, packageprops.KindUnspecified, packageprops.StabilityUnspecified)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "package foo") {
		t.Errorf("missing package clause:\n%s", src)
	}
	if strings.Contains(string(src), "Kind:") {
		t.Errorf("KindUnspecified must not emit a Kind field:\n%s", src)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, propsFileName)
	if err = os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	fields, err := parsePropsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{
		"WASMWASI":         "WASMCompiles",
		"WASMJS":           "WASMBlocked",
		"WASMFreestanding": "WASMUnknown",
	} {
		if fields[field] != want {
			t.Errorf("%s: parsed %q, want %q", field, fields[field], want)
		}
	}
}

func TestKindTokenRoundTrip(t *testing.T) {
	for _, k := range []packageprops.Kind{
		packageprops.KindUnspecified,
		packageprops.KindDemo,
		packageprops.KindExample,
		packageprops.KindIntegrationTest,
	} {
		if got := parseKindToken(kindToken(k)); got != k {
			t.Errorf("round-trip %v: token %q parsed back as %v", k, kindToken(k), got)
		}
	}
	if got := parseKindToken("bogus"); got != packageprops.KindUnspecified {
		t.Errorf("unknown token should parse to KindUnspecified, got %v", got)
	}
}

func TestHeuristicKind(t *testing.T) {
	for dir, want := range map[string]packageprops.Kind{
		"/x/demo":      packageprops.KindDemo,
		"/x/demos":     packageprops.KindDemo,
		"/x/example":   packageprops.KindExample,
		"/x/examples":  packageprops.KindExample,
		"/x/recordsto": packageprops.KindUnspecified,
		"/x/test":      packageprops.KindUnspecified, // integration tests are file-level, never seeded
	} {
		if got := heuristicKind(PackageReport{Dir: dir}); got != want {
			t.Errorf("heuristicKind(%q) = %v, want %v", dir, got, want)
		}
	}
}

// TestRenderPropsFileKind renders and re-parses a declaration carrying a Kind,
// covering the seed/preserve path (a curated Kind must survive a re-render).
func TestRenderPropsFileKind(t *testing.T) {
	pr := PackageReport{
		ImportPath: "example.com/foo",
		Name:       "foo",
		Targets:    []TargetVerdict{{Target: TargetWASI, Static: TierGreen}},
	}
	src, err := renderPropsFile(pr, packageprops.KindExample, packageprops.StabilityUnspecified)
	if err != nil {
		t.Fatal(err)
	}
	// gofmt aligns the struct's colons, so match the field and value loosely
	// (the re-parse below is the exact check).
	if !strings.Contains(string(src), "Kind:") || !strings.Contains(string(src), "packageprops.KindExample") {
		t.Errorf("missing Kind field:\n%s", src)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, propsFileName)
	if err = os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	fields, err := parsePropsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseKindToken(fields["Kind"]); got != packageprops.KindExample {
		t.Errorf("harvested Kind = %v, want KindExample", got)
	}
}

// TestRenderHarvestGoKind checks the static Table emits Kind only when set, so
// the overwhelmingly-common unspecified rows stay unchanged.
func TestRenderHarvestGoKind(t *testing.T) {
	rows := []HarvestRow{
		{ImportPath: "example.com/plain", WASMWASI: packageprops.WASMCompiles},
		{ImportPath: "example.com/ex", WASMWASI: packageprops.WASMBlocked, Kind: packageprops.KindExample},
	}
	src, err := renderHarvestGo(rows, "proptable")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, `ImportPath: "example.com/ex"`) || !strings.Contains(s, "Kind: packageprops.KindExample") {
		t.Errorf("classified row must carry its Kind:\n%s", s)
	}
	// The plain row's segment must not carry a Kind field.
	for line := range strings.SplitSeq(s, "\n") {
		if strings.Contains(line, `"example.com/plain"`) && strings.Contains(line, "Kind:") {
			t.Errorf("unspecified row must not emit Kind:\n%s", line)
		}
	}
}

// TestRenderPropsFileStability round-trips a declared tier through render and
// parse, and checks harvest emits Stability only when set (ADR-0389).
func TestRenderPropsFileStability(t *testing.T) {
	pr := PackageReport{ImportPath: "example.com/foo", Name: "foo"}
	src, err := renderPropsFile(pr, packageprops.KindTestSupport, packageprops.StabilityStable)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, propsFileName)
	if err = os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	fields, err := parsePropsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := parseStabilityToken(fields["Stability"]); got != packageprops.StabilityStable {
		t.Errorf("harvested Stability = %v, want stable", got)
	}
	if got := parseKindToken(fields["Kind"]); got != packageprops.KindTestSupport {
		t.Errorf("harvested Kind = %v, want test-support", got)
	}
	for _, st := range packageprops.AllStabilities {
		if got := parseStabilityToken(stabilityToken(st)); got != st {
			t.Errorf("stability token round trip: %v -> %q -> %v", st, stabilityToken(st), got)
		}
	}

	rows := []HarvestRow{
		{ImportPath: "example.com/plain"},
		{ImportPath: "example.com/st", Stability: packageprops.StabilityExperimental},
	}
	out, err := renderHarvestGo(rows, "proptable")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(line, `"example.com/plain"`) && strings.Contains(line, "Stability:") {
			t.Errorf("unspecified row must not emit Stability:\n%s", line)
		}
		if strings.Contains(line, `"example.com/st"`) && !strings.Contains(line, "Stability: packageprops.StabilityExperimental") {
			t.Errorf("tiered row must carry its Stability:\n%s", line)
		}
	}
}

func TestPackageSynopsis(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package foo\n")
	if got := packageSynopsis(dir); got != "" {
		t.Errorf("no doc comment: synopsis = %q, want empty", got)
	}
	write("a_test.go", "// Package foo is documented only in a test file.\npackage foo\n")
	if got := packageSynopsis(dir); got != "" {
		t.Errorf("a test file's doc must not count: synopsis = %q", got)
	}
	write("b.go", "// Package foo does b. More text.\npackage foo\n")
	write("doc.go", "// Package foo does things. A second sentence.\npackage foo\n")
	if got := packageSynopsis(dir); got != "Package foo does things." {
		t.Errorf("synopsis = %q, want the first sentence of doc.go", got)
	}
}

// TestRenderSurface checks every row lands in exactly one section, under its
// area, with its synopsis, and that rendering is deterministic.
func TestRenderSurface(t *testing.T) {
	const mod = "example.com/m"
	rows := []HarvestRow{
		{ImportPath: mod + "/public/a/x", Stability: packageprops.StabilityStable, Synopsis: "Package x does x."},
		{ImportPath: mod + "/public/b/y", Stability: packageprops.StabilityExperimental},
		{ImportPath: mod + "/public/a/xtest", Kind: packageprops.KindTestSupport, Stability: packageprops.StabilityStable},
		{ImportPath: mod + "/public/c/tool", Kind: packageprops.KindTool},
		{ImportPath: mod + "/apps/z", Kind: packageprops.KindUnspecified},
	}
	src := string(RenderSurface(rows, mod))
	for _, want := range []string{
		"## Stable\n", "### a\n", "- `public/a/x` — Package x does x.\n",
		"- `public/a/xtest` (test-support)\n",
		"## Experimental\n", "- `public/b/y`\n",
		"## Tools\n", "- `public/c/tool`\n",
		"## Unclassified\n", "### apps\n", "- `apps/z`\n",
		"| [Stable](#stable) | 2 |",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("surface document lacks %q:\n%s", want, src)
		}
	}
	if strings.Count(src, "`public/c/tool`") != 1 {
		t.Errorf("a row must appear in exactly one section:\n%s", src)
	}
	if again := string(RenderSurface(rows, mod)); again != src {
		t.Error("rendering is not deterministic")
	}
}
