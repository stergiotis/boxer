package capsurvey

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/packageprops"
)

// repoRoot locates the module root so the survey runs against the real tree
// regardless of the test's working directory.
func repoRoot(t *testing.T) (root string) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root, ok := godepcollect.ModuleRoot(wd)
	if !ok {
		t.Fatalf("no module root above %s", wd)
	}
	return
}

// tags reads the repo's load-bearing build tags, mirroring what a real build
// passes. Without them the survey sees a different set of files.
func tags(t *testing.T, root string) (out []string) {
	t.Helper()
	b, err := os.ReadFile(root + "/tags")
	if err != nil {
		t.Fatalf("read tags: %v", err)
	}
	for s := range strings.SplitSeq(strings.TrimSpace(string(b)), ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return
}

func runSurvey(t *testing.T, patterns ...string) (s Survey) {
	t.Helper()
	root := repoRoot(t)
	s, err := Run(context.Background(), Options{
		Dir:      root,
		Patterns: patterns,
		Tags:     tags(t, root),
	})
	if err != nil {
		t.Fatalf("survey: %v", err)
	}
	if len(s.Unknown) > 0 {
		t.Errorf("capslock reported capabilities this vocabulary lacks: %v — extend packageprops.Capability", s.Unknown)
	}
	if len(s.Failed) > 0 {
		t.Logf("packages that failed to load (unsurveyed): %v", s.Failed)
	}
	return
}

func find(s Survey, importPath string) (r PackageReport, ok bool) {
	for _, p := range s.Packages {
		if p.ImportPath == importPath {
			return p, true
		}
	}
	return
}

// TestRunExtbin pins the survey against a package whose capabilities are known
// from its source: extbin resolves and runs external binaries, so it must hold
// exec directly. This is the end-to-end check that the capslock wiring, the
// enum mapping and the direct/reachable split all work.
func TestRunExtbin(t *testing.T) {
	s := runSurvey(t, "./public/extbin/...")
	r, ok := find(s, "github.com/stergiotis/boxer/public/extbin")
	if !ok {
		t.Fatalf("extbin missing from survey; got %d packages", len(s.Packages))
	}
	if !r.Direct.Has(packageprops.CapabilityExec) {
		t.Errorf("extbin direct = %v, want it to include exec", r.Direct)
	}
	if !r.Direct.Has(packageprops.CapabilityFiles) {
		t.Errorf("extbin direct = %v, want it to include files", r.Direct)
	}
	if r.Direct.Safe() {
		t.Errorf("extbin direct = %v, must not be marked safe", r.Direct)
	}
	if !r.Direct.Surveyed() || !r.Reachable.Surveyed() {
		t.Errorf("extbin verdict must be marked surveyed, got direct=%v reachable=%v", r.Direct, r.Reachable)
	}
	// Reachable is the closure, so a package that execs directly must also read
	// as able to exec at all. Before the closure was stored, capslock's raw
	// TRANSITIVE ("reachable only via a dependency") left this false and made
	// extbin's reachable set read as "safe".
	if !r.Reachable.Has(packageprops.CapabilityExec) {
		t.Errorf("extbin reachable = %v, want it to include exec", r.Reachable)
	}
	if r.Reachable.Safe() {
		t.Errorf("extbin reachable = %v, must not be marked safe", r.Reachable)
	}
}

// TestDirectSubsetOfReachable pins the invariant the whole representation rests
// on (ADR-0120 SD5): Reachable is a closure, so anything a package does directly
// it can necessarily do at all. Without this, "can package X touch the network?"
// would have to consult both sets.
func TestDirectSubsetOfReachable(t *testing.T) {
	s := runSurvey(t, "./public/extbin/...", "./public/functional/...", "./public/code/analysis/golang/...")
	if len(s.Packages) == 0 {
		t.Fatal("survey returned no packages")
	}
	for _, pr := range s.Packages {
		if !pr.Direct.Subset(pr.Reachable) {
			t.Errorf("%s: direct %v is not a subset of reachable %v", pr.ImportPath, pr.Direct, pr.Reachable)
		}
		if !pr.Direct.Surveyed() || !pr.Reachable.Surveyed() {
			t.Errorf("%s: a surveyed package must carry a verdict in both fields, got direct=%v reachable=%v",
				pr.ImportPath, pr.Direct, pr.Reachable)
		}
		// The encoding invariant: the safe marker is set exactly when nothing
		// privileged is.
		for _, set := range []packageprops.CapabilitySet{pr.Direct, pr.Reachable} {
			if set.Safe() != (set.Privileged() == 0) {
				t.Errorf("%s: safe marker disagrees with content: %v", pr.ImportPath, set)
			}
		}
	}
	t.Logf("checked the subset invariant over %d packages", len(s.Packages))
}

// TestRunPureLeaf pins the other end of the range: a pure functional package
// reaches nothing privileged, and must be recorded as explicitly safe rather
// than left at the zero value, which would read as "never surveyed"
// (ADR-0120 SD4).
func TestRunPureLeaf(t *testing.T) {
	s := runSurvey(t, "./public/functional/option/...")
	r, ok := find(s, "github.com/stergiotis/boxer/public/functional/option")
	if !ok {
		t.Fatalf("option missing from survey; got %d packages", len(s.Packages))
	}
	if !r.Direct.Safe() {
		t.Errorf("option direct = %v, want safe", r.Direct)
	}
	if !r.Direct.Surveyed() {
		t.Errorf("option direct = %v, want it to count as surveyed", r.Direct)
	}
	if r.Direct != packageprops.Caps(packageprops.CapabilitySafe) {
		t.Errorf("option direct = %#x, want exactly the safe bit", uint32(r.Direct))
	}
}

// TestRunCoversMainPackages is the ADR-0120 SD6 scope decision: unlike the wasm
// survey, the capability survey reaches main packages — where the capability
// set of a whole binary lives.
func TestRunCoversMainPackages(t *testing.T) {
	s := runSurvey(t, "./apps/godepview/...")
	r, ok := find(s, "github.com/stergiotis/boxer/apps/godepview")
	if !ok {
		t.Fatalf("main package godepview missing from survey; got %d packages", len(s.Packages))
	}
	if !r.Reachable.Surveyed() {
		t.Errorf("godepview reachable = %v, want a verdict", r.Reachable)
	}
}
