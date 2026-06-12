package wasmsurvey

import (
	"context"
	"slices"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// targetClosure is the package graph loaded for one wasm target. The graph is
// GOOS-specific: a package's selected files (and therefore its imports) differ
// under GOOS=wasip1 vs GOOS=js, so each target gets its own collection.
type targetClosure struct {
	target   TargetID
	manifest godep.Manifest
	index    *godep.Index
}

// loadClosureE collects the transitive package closure for one target. It
// drives the existing godepcollect.LiveCollector (ADR-0064) under the
// target's GOOS/GOARCH=wasm, so build-constraint file selection matches what
// TinyGo would compile. tags are the load-bearing repo build tags.
func loadClosureE(ctx context.Context, dir string, patterns []string, tags []string, target TargetID) (tc targetClosure, err error) {
	cfg := godepcollect.Config{
		Dir:      dir,
		Patterns: patterns,
		// TinyGo always defines the `tinygo` build tag, so the static closure
		// must model it too — otherwise build-tag seams (e.g. eh's tinygo-vs-
		// native split that drops os/exec and zerolog) are invisible to the
		// triage, which would falsely keep the seam's beneficiaries Red and
		// never probe them.
		Tags: appendTag(tags, "tinygo"),
		// Re-collect under the wasm target's GOOS so files gated by
		// //go:build js / wasip1 are selected as TinyGo would see them.
		Env: []string{
			"GOOS=" + target.GOOS(),
			"GOARCH=wasm",
		},
	}
	var m godep.Manifest
	m, err = godepcollect.New(cfg).Load(ctx)
	if err != nil {
		err = eb.Build().Str("target", target.String()).Str("goos", target.GOOS()).Errorf("collect wasm closure: %w", err)
		return
	}
	tc.target = target
	tc.manifest = m
	// Build the index against the stored manifest's backing array (BuildIndex
	// borrows pointers into manifest.Packages; we never reorder it after).
	tc.index = tc.manifest.BuildIndex()
	return
}

// appendTag returns tags with tag appended if not already present.
func appendTag(tags []string, tag string) (out []string) {
	if slices.Contains(tags, tag) {
		return tags
	}
	out = append(out, tags...)
	return append(out, tag)
}
