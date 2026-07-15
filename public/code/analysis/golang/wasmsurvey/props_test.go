package wasmsurvey

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/code/analysis/golang/propsfile"
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
		{ImportPath: "example.com/a", Props: packageprops.Props{WASMWASI: packageprops.WASMCompiles, WASMJS: packageprops.WASMBlocked, WASMFreestanding: packageprops.WASMUnknown}},
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

// TestStateFor pins the wasm-specific half of generation: turning a package's
// per-target verdicts into the declared WASM* states. The file rendering and
// parsing it feeds are propsfile's, and tested there.
func TestStateFor(t *testing.T) {
	pr := PackageReport{
		ImportPath: "example.com/foo",
		Name:       "foo",
		Targets: []TargetVerdict{
			{Target: TargetWASI, Static: TierGreen},
			{Target: TargetJS, Static: TierRed},
		},
	}
	for target, want := range map[TargetID]packageprops.WASMState{
		TargetWASI:        packageprops.WASMCompiles,
		TargetJS:          packageprops.WASMBlocked,
		TargetWasmUnknown: packageprops.WASMUnknown, // no verdict recorded: asserts nothing
	} {
		if got := stateFor(pr, target); got != want {
			t.Errorf("stateFor(%v) = %v, want %v", target, got, want)
		}
	}
}

// TestGenerateRoundTripsThroughPropsfile is the seam check: the states this
// survey computes survive a render/parse cycle, so a re-seed reads back what it
// wrote rather than silently dropping a verdict.
func TestGenerateRoundTripsThroughPropsfile(t *testing.T) {
	pr := PackageReport{
		ImportPath: "example.com/foo",
		Name:       "foo",
		Targets: []TargetVerdict{
			{Target: TargetWASI, Static: TierGreen},
			{Target: TargetJS, Static: TierRed},
		},
	}
	want := packageprops.Props{
		WASMWASI:         stateFor(pr, TargetWASI),
		WASMJS:           stateFor(pr, TargetJS),
		WASMFreestanding: stateFor(pr, TargetWasmUnknown),
		Kind:             packageprops.KindExample,
	}
	src, err := propsfile.Render(pr.Name, pr.ImportPath, want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "package foo") {
		t.Errorf("missing package clause:\n%s", src)
	}
	path := filepath.Join(t.TempDir(), propsFileName)
	if err = os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := propsfile.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("round trip: got %+v, want %+v", got, want)
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

// TestRenderHarvestGoKind checks the static Table emits Kind only when set, so
// the overwhelmingly-common unspecified rows stay unchanged.
func TestRenderHarvestGoKind(t *testing.T) {
	rows := []HarvestRow{
		{ImportPath: "example.com/plain", Props: packageprops.Props{WASMWASI: packageprops.WASMCompiles}},
		{ImportPath: "example.com/ex", Props: packageprops.Props{WASMWASI: packageprops.WASMBlocked, Kind: packageprops.KindExample}},
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
