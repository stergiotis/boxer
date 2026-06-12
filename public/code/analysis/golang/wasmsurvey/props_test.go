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
	src, err := renderPropsFile(pr)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "package foo") {
		t.Errorf("missing package clause:\n%s", src)
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
