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
	src, err := renderPropsFile(pr, packageprops.KindUnspecified)
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
	src, err := renderPropsFile(pr, packageprops.KindExample)
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
