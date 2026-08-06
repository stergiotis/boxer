package providersgodep

import (
	"context"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/stergiotis/boxer/public/code/analysis/golang/codevol"
)

// volumeBudget bounds the line-counting pass. Measured on this repository the
// pass costs about 1.8 s over ~7,000 files on top of a ~0.4 s load; the budget
// exists so a pathological tree leaves an empty column rather than a stalled
// query.
const volumeBudget = 60 * time.Second

// volumeCache holds the per-package line tallies behind the volume columns of
// go_packages (ADR-0173 §SD3).
//
// It is deliberately a *separate* cache from the graph collection, and it is
// populated by its own packages.Load. Two reasons, in order of importance:
//
//   - Cost isolation. Counting lines costs roughly 1.8 s. Folding it into the
//     graph collection would push that from ~1.4 s to ~3.2 s against a 5 s
//     first-wait budget, so every `SELECT import_path FROM go_packages` would
//     pay for a column it does not read. Introspect projections skip the
//     getters of unselected columns, so with a separate cache the cost falls
//     only on a query that actually asks for a volume column.
//   - The graph manifest is destined for boxer.facts, and counting needs
//     absolute on-disk file paths. Those are machine-local and must not end
//     up in a stored fact, so they stay here and never enter the DTO.
//
// The duplicate load is the honest price of that separation.
type volumeCache struct {
	cfg  Config
	once sync.Once
	// byPkg is nil until the first volume column is read. A package missing
	// from the map counts as zero, which is also what an unbuilt or failed
	// pass yields.
	byPkg map[string]codevol.Volume

	// load is a field so tests can supply tallies without running the
	// toolchain.
	load func(ctx context.Context, cfg Config) map[string]codevol.Volume
}

func newVolumeCache(cfg Config) (inst *volumeCache) {
	return &volumeCache{cfg: cfg, load: loadVolumes}
}

// get returns the tally for one package, populating the whole cache on the
// first call. Callers reach this from a column getter, so the first row of the
// first volume query pays for every package and the rest are free.
func (inst *volumeCache) get(importPath string) (v codevol.Volume) {
	if inst == nil {
		return
	}
	inst.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), volumeBudget)
		defer cancel()
		inst.byPkg = inst.load(ctx, inst.cfg)
	})
	return inst.byPkg[importPath]
}

// loadVolumes walks the same closure the graph collection walks and counts the
// files the toolchain says are compiled — not every .go file in the directory,
// which would silently include build-tag-excluded and _test.go files.
func loadVolumes(ctx context.Context, cfg Config) (out map[string]codevol.Volume) {
	c := &packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps,
		Context: ctx,
		Dir:     cfg.Root,
	}
	if len(cfg.Tags) > 0 {
		c.BuildFlags = []string{"-tags=" + strings.Join(cfg.Tags, ",")}
	}
	roots, err := packages.Load(c, "./...")
	if err != nil || len(roots) == 0 {
		// Off-repo, or no toolchain. The columns read zero, matching the
		// empty-not-absent behaviour the rest of these tables have.
		return
	}
	out = make(map[string]codevol.Volume, 1024)
	packages.Visit(roots, func(p *packages.Package) bool {
		if p.PkgPath == "" {
			return true
		}
		// CompiledGoFiles includes cgo-generated sources, which are machine
		// output rather than anything anyone wrote; GoFiles is the honest
		// answer to "how much source is this package".
		out[p.PkgPath] = codevol.CountFiles(p.GoFiles, p.OtherFiles)
		return true
	}, nil)
	return
}
