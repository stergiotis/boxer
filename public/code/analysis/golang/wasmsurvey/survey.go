package wasmsurvey

import (
	"context"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Mode selects how far the survey goes: static triage only, empirical probe
// only, or both (triage then confirm the non-Red survivors).
type Mode uint8

const (
	ModeBoth      Mode = iota // static triage, then probe everything not statically Red
	ModeStatic                // graph-only; no TinyGo needed
	ModeEmpirical             // probe every candidate (still uses static triage to pick candidates)
)

// String renders the mode for reports/flags.
func (m Mode) String() (s string) {
	switch m {
	case ModeStatic:
		s = "static"
	case ModeEmpirical:
		s = "empirical"
	default:
		s = "both"
	}
	return
}

// ParseModeE resolves a flag string to a Mode. ok is false for an unknown one.
func ParseModeE(s string) (m Mode, ok bool) {
	switch s {
	case "both", "":
		return ModeBoth, true
	case "static":
		return ModeStatic, true
	case "empirical":
		return ModeEmpirical, true
	default:
		return ModeBoth, false
	}
}

func (m Mode) probes() (b bool) { return m == ModeBoth || m == ModeEmpirical }

// Options configures a Run.
type Options struct {
	Dir             string        // module dir to survey; "" → process working dir
	Patterns        []string      // load patterns; nil → ["./..."]
	Tags            []string      // build tags (load-bearing for this repo)
	Targets         []TargetID    // wasm targets; nil → AllTargets
	Mode            Mode          // static | empirical | both
	IncludeExternal bool          // also verdict external packages (default: internal-only)
	Jobs            int           // empirical probe parallelism; <=0 → GOMAXPROCS
	ProbeTimeout    time.Duration // per-package tinygo build timeout; <=0 → 180s
	// AssumeClean is a counterfactual lever: import-path prefixes treated as
	// wasm-clean Green sinks during static triage, so the report shows what
	// would go green/yellow if those packages were fixed. It is a static-only
	// hypothesis — the empirical probe still builds the real (unfixed) code.
	AssumeClean []string
}

// probeOutcome is one empirical result for a package on a target.
type probeOutcome struct {
	tier   Tier
	reason Reason
	millis int64
	probed bool // false when the probe could not run (harness error) → keep static
}

// aggregate collects a package's metadata and per-target verdicts as the run
// sweeps each target.
type aggregate struct {
	class      string
	numExports int
	exportsSet bool
	verdicts   map[TargetID]TargetVerdict
}

// Run surveys the package closure for every requested target and returns the
// assembled Survey. The closure is collected once per target (under that
// target's GOOS), statically triaged, and — in a probing mode with TinyGo
// available — the non-Red survivors are confirmed with `tinygo build`.
func Run(ctx context.Context, opts Options) (survey Survey, err error) {
	if len(opts.Targets) == 0 {
		opts.Targets = AllTargets
	}
	if opts.Jobs <= 0 {
		opts.Jobs = runtime.GOMAXPROCS(0)
	}
	if opts.ProbeTimeout <= 0 {
		opts.ProbeTimeout = 180 * time.Second
	}

	root := opts.Dir
	if root == "" {
		if wd, e := os.Getwd(); e == nil {
			root = wd
		}
	}

	// want selects which packages earn a reported verdict. A main package
	// cannot be imported by the probe, so it is excluded.
	want := func(p *godep.PackageNode) bool {
		if p.Name == "main" {
			return false // a main package cannot be imported by the probe
		}
		if p.Name == "" || p.NumGoFiles == 0 {
			return false // test-only/unbuildable dir: no importable package to survey
		}
		if strings.Contains(p.ImportPath, "/internal/") || strings.HasSuffix(p.ImportPath, "/internal") {
			return false // internal packages aren't importable by the probe's external main
		}
		if opts.IncludeExternal {
			return p.Class != godep.ClassStdlib
		}
		return p.Class == godep.ClassInternal
	}

	survey.Mode = opts.Mode.String()
	survey.Tags = opts.Tags
	for _, t := range opts.Targets {
		survey.Targets = append(survey.Targets, t.String())
	}

	doProbe := opts.Mode.probes()
	if doProbe {
		if !tinygoAvailable() {
			doProbe = false
			survey.Warnings = append(survey.Warnings,
				"empirical probe skipped: no `tinygo` on PATH — reporting static verdicts only (install tinygo to confirm)")
		} else {
			survey.TinyGoVer = tinygoVersion(ctx)
			// Preflight once: if tinygo can't even build an empty main (most
			// likely a Go-version ceiling — TinyGo 0.39 supports Go ≤1.25 while
			// this repo is on Go 1.26), skip probing rather than report a wall
			// of identical failures.
			if ok, detail := tinygoPreflightE(ctx, root, opts.Targets[0], opts.Tags); !ok {
				doProbe = false
				survey.Warnings = append(survey.Warnings,
					"empirical probe skipped: tinygo cannot build for this toolchain — "+detail+
						" (TinyGo 0.39 supports Go ≤1.25; this repo is on Go 1.26). Reporting static verdicts only.")
			}
		}
	}

	aggs := make(map[string]*aggregate)

	for _, target := range opts.Targets {
		var tc targetClosure
		tc, err = loadClosureE(ctx, opts.Dir, opts.Patterns, opts.Tags, target)
		if err != nil {
			return
		}
		if survey.RootModule == "" {
			survey.RootModule = tc.manifest.Run.RootModulePath
		}

		staticV := classifyStatic(tc, want, opts.AssumeClean)

		// Pick probe candidates (everything not statically Red) and record
		// per-package metadata.
		type candidate struct {
			id         uint64
			importPath string
			dir        string
		}
		var candidates []candidate
		for id, v := range staticV {
			node, ok := tc.index.Node(id)
			if !ok {
				continue
			}
			ag := aggs[node.ImportPath]
			if ag == nil {
				ag = &aggregate{class: node.Class, verdicts: make(map[TargetID]TargetVerdict, len(opts.Targets))}
				aggs[node.ImportPath] = ag
			}
			if !ag.exportsSet {
				if names, e := exportedFuncs(node.Dir, target.GOOS(), opts.Tags); e == nil {
					ag.numExports = len(names)
					ag.exportsSet = true
				}
			}
			if doProbe && v.Static != TierRed {
				candidates = append(candidates, candidate{id: id, importPath: node.ImportPath, dir: node.Dir})
			}
		}

		// Empirical confirm of the survivors, bounded by Jobs.
		var probed map[uint64]probeOutcome
		if doProbe && len(candidates) > 0 {
			probed = make(map[uint64]probeOutcome, len(candidates))
			var mu sync.Mutex
			var wg sync.WaitGroup
			sem := make(chan struct{}, opts.Jobs)
			for _, cand := range candidates {
				wg.Add(1)
				sem <- struct{}{}
				go func(cd candidate) {
					defer wg.Done()
					defer func() { <-sem }()
					tier, reason, millis, perr := probePackageE(ctx, root, cd.importPath, cd.dir, target, opts.Tags, opts.ProbeTimeout)
					out := probeOutcome{tier: tier, reason: reason, millis: millis, probed: perr == nil}
					if perr != nil {
						out.reason = Reason{Kind: ReasonProbeOther, Leaf: cd.importPath, Detail: "probe harness error: " + perr.Error()}
					}
					mu.Lock()
					probed[cd.id] = out
					mu.Unlock()
				}(cand)
			}
			wg.Wait()
		}

		// Merge static + empirical into the per-target verdict.
		for id, v := range staticV {
			node, ok := tc.index.Node(id)
			if !ok {
				continue
			}
			if po, ok := probed[id]; ok && po.probed {
				v.Probed = true
				v.Empirical = po.tier
				v.BuildMillis = po.millis
				if po.reason.Kind != ReasonNone {
					v.Reasons = append(v.Reasons, po.reason)
				}
			} else if po, ok := probed[id]; ok && !po.probed {
				// harness error: keep static verdict but surface the note
				v.Reasons = append(v.Reasons, po.reason)
			}
			aggs[node.ImportPath].verdicts[target] = v
		}
	}

	// Flatten to a sorted, deterministic report.
	paths := make([]string, 0, len(aggs))
	for p := range aggs {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	survey.Packages = make([]PackageReport, 0, len(paths))
	for _, p := range paths {
		ag := aggs[p]
		pr := PackageReport{ImportPath: p, Class: ag.class, NumExports: ag.numExports}
		for _, t := range opts.Targets {
			if v, ok := ag.verdicts[t]; ok {
				pr.Targets = append(pr.Targets, v)
			}
		}
		survey.Packages = append(survey.Packages, pr)
	}

	if len(survey.Packages) == 0 {
		err = eb.Build().Strs("patterns", opts.Patterns).Errorf("survey produced no packages (check patterns/tags)")
		return
	}
	return
}
