package godep_test

import (
	"go/build"
	"strings"
	"testing"
)

// TestSeam_NoToolchainOrUIImports enforces ADR-0064's load-bearing
// invariant: the manifest package must not import the go-toolchain
// analysis library (golang.org/x/tools/...) or any imzero2/egui binding.
// Both the collector (godepcollect) and the app (apps/godepview) depend on
// godep; godep must depend on neither, so the collection<->visualization
// separation holds at build time rather than by convention.
//
// The check reads the package's non-test imports via go/build (stdlib, so
// the test itself adds no forbidden import). Test imports land in
// TestImports/XTestImports and are intentionally not inspected.
func TestSeam_NoToolchainOrUIImports(t *testing.T) {
	pkg, err := build.Default.ImportDir(".", 0)
	if err != nil {
		t.Fatalf("build.ImportDir(\".\"): %v", err)
	}
	forbidden := []string{
		"golang.org/x/tools",
		"github.com/stergiotis/boxer/public/thestack/imzero2",
	}
	for _, imp := range pkg.Imports {
		for _, bad := range forbidden {
			if strings.HasPrefix(imp, bad) {
				t.Errorf("godep imports %q — forbidden by the ADR-0064 seam (prefix %q)", imp, bad)
			}
		}
	}
}
