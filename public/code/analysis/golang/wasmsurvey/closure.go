package wasmsurvey

import (
	"context"

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
		Tags:     tags,
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
