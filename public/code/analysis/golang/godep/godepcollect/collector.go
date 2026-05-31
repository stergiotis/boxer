package godepcollect

import (
	"context"
	"hash/fnv"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/tools/go/packages"
)

// Config parameterizes a LiveCollector.
type Config struct {
	// Dir is the directory packages.Load runs in (the module to explore).
	// Empty means the process working directory.
	Dir string
	// Patterns are the load patterns; empty defaults to []string{"./..."}.
	Patterns []string
	// Tags are build tags forwarded as -tags=<csv> to the underlying
	// `go list`. Empty relies on the inherited GOFLAGS (the boxer launcher
	// exports the repo's tags there). The collected graph reflects whichever
	// files these tags select — for this repo the tags are load-bearing, so
	// an empty Tags with no GOFLAGS yields a degraded graph.
	Tags []string
}

// LiveCollector loads the Go package closure with
// golang.org/x/tools/go/packages and builds a godep.Manifest. It is the
// live SourceI adapter (ADR-0064 SD3).
type LiveCollector struct {
	cfg Config
}

var _ godep.SourceI = (*LiveCollector)(nil)

// New returns a LiveCollector for cfg.
func New(cfg Config) (inst *LiveCollector) {
	inst = &LiveCollector{cfg: cfg}
	return
}

// Load collects the transitive package closure rooted at cfg.Patterns and
// returns a godep.Manifest. Per-package load errors are non-fatal: a
// package that failed to fully resolve still contributes a node from
// whatever metadata loaded. Load fails only when packages.Load itself
// fails, when nothing matches, or on an (astronomically unlikely) id
// collision.
func (inst *LiveCollector) Load(ctx context.Context) (m godep.Manifest, err error) {
	patterns := inst.cfg.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedModule,
		Context: ctx,
		Dir:     inst.cfg.Dir,
	}
	if len(inst.cfg.Tags) > 0 {
		cfg.BuildFlags = []string{"-tags=" + strings.Join(inst.cfg.Tags, ",")}
	}

	var roots []*packages.Package
	roots, err = packages.Load(cfg, patterns...)
	if err != nil {
		err = eb.Build().Str("dir", inst.cfg.Dir).Strs("patterns", patterns).Errorf("load packages: %w", err)
		return
	}
	if len(roots) == 0 {
		err = eb.Build().Str("dir", inst.cfg.Dir).Strs("patterns", patterns).Errorf("no packages matched")
		return
	}

	// The main (root) module, used to classify packages as internal.
	rootModule := ""
	for _, p := range roots {
		if p.Module != nil && p.Module.Main {
			rootModule = p.Module.Path
			break
		}
	}

	// Collect every package in the transitive closure (roots + all deps).
	uniq := make(map[string]*packages.Package, 1024)
	packages.Visit(roots, func(p *packages.Package) bool {
		if p.PkgPath != "" {
			uniq[p.PkgPath] = p
		}
		return true
	}, nil)

	// Deterministic order so repeated runs over the same module state
	// produce identical manifests.
	paths := make([]string, 0, len(uniq))
	for path := range uniq {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	ts := time.Now().UnixNano()

	seenID := make(map[uint64]string, len(paths))
	nodes := make([]godep.PackageNode, 0, len(paths))
	var numEdges uint64

	for _, path := range paths {
		p := uniq[path]
		id := hashPath(path)
		if prev, ok := seenID[id]; ok && prev != path {
			err = eb.Build().Str("pathA", prev).Str("pathB", path).Uint64("id", id).Errorf("FNV-1a-64 import-path hash collision")
			return
		}
		seenID[id] = path

		imports := make([]uint64, 0, len(p.Imports))
		for _, imp := range p.Imports {
			if imp.PkgPath == "" || imp.PkgPath == path {
				continue
			}
			imports = append(imports, hashPath(imp.PkgPath))
		}
		slices.Sort(imports)
		imports = dedupSortedU64(imports)
		numEdges += uint64(len(imports))

		dir := ""
		if len(p.GoFiles) > 0 {
			dir = filepath.Dir(p.GoFiles[0])
		} else if len(p.OtherFiles) > 0 {
			dir = filepath.Dir(p.OtherFiles[0])
		}

		modPath := "std"
		if p.Module != nil {
			modPath = p.Module.Path
		}

		nodes = append(nodes, godep.PackageNode{
			Id:         id,
			NaturalKey: []byte(path),
			Ts:         ts,
			ImportPath: path,
			Name:       p.Name,
			Dir:        dir,
			ModulePath: modPath,
			Class:      classify(p, rootModule),
			NumGoFiles: uint32(len(p.GoFiles)),
			NumImports: uint32(len(imports)),
			Imports:    imports,
		})
	}

	// Reverse pass: in-degree (NumImportedBy) — the one count that cannot be
	// derived from a single row in isolation (ADR-0064 SD4).
	indeg := make(map[uint64]uint32, len(nodes))
	for i := range nodes {
		for _, to := range nodes[i].Imports {
			indeg[to]++
		}
	}
	for i := range nodes {
		nodes[i].NumImportedBy = indeg[nodes[i].Id]
	}

	m = godep.Manifest{
		Run: godep.CollectionRun{
			Id:             runID(rootModule, ts),
			NaturalKey:     []byte(rootModule),
			Ts:             ts,
			RootModulePath: rootModule,
			GoVersion:      runtime.Version(),
			Scope:          godep.ScopeTransitive,
			NumPackages:    uint32(len(nodes)),
			NumEdges:       uint32(numEdges),
			BuildTags:      append([]string(nil), inst.cfg.Tags...),
			Roots:          append([]string(nil), patterns...),
		},
		Packages: nodes,
	}
	return
}

// classify assigns a package's provenance relative to the root module.
// go/packages reports Module == nil for standard-library packages.
func classify(p *packages.Package, rootModule string) (class string) {
	if p.Module == nil {
		return godep.ClassStdlib
	}
	if rootModule != "" && p.Module.Path == rootModule {
		return godep.ClassInternal
	}
	return godep.ClassExternal
}

func hashPath(path string) (id uint64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(path))
	return h.Sum64()
}

func runID(rootModule string, ts int64) (id uint64) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(rootModule))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.FormatInt(ts, 10)))
	return h.Sum64()
}

// dedupSortedU64 removes adjacent duplicates from a sorted slice in place.
func dedupSortedU64(in []uint64) (out []uint64) {
	out = in[:0]
	var last uint64
	for i, v := range in {
		if i == 0 || v != last {
			out = append(out, v)
			last = v
		}
	}
	return
}
