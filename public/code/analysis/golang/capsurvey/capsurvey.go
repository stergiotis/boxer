package capsurvey

import (
	"context"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/capslock/analyzer"
	cpb "github.com/google/capslock/proto"
	"golang.org/x/tools/go/packages"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/packageprops"
)

// Options configures a Run.
type Options struct {
	// Dir is the module directory to survey; "" uses the process working
	// directory. Set it rather than chdir-ing: capslock's own
	// analyzer.LoadPackages has no Dir and loads from the working directory,
	// which is why this package loads packages itself (ADR-0120 SD2).
	Dir string

	// Patterns are go list patterns; nil means ["./..."].
	Patterns []string

	// Tags are build tags forwarded as -tags=<csv>. They are load-bearing in
	// this repo: an empty value relies on the inherited GOFLAGS, and without
	// either the survey sees a different (smaller) set of files than a real
	// build does.
	Tags []string
}

// PackageReport is one package's capability verdict.
type PackageReport struct {
	ImportPath string
	Name       string // package clause name, needed to render a declaration
	Dir        string // package directory, where a declaration is written

	// Direct is what the package's own code exercises. Reachable is the closure:
	// everything it can reach once dependencies are followed, so Direct is always
	// a subset of it.
	//
	// Note this is not capslock's raw shape. capslock reports one record per
	// (package, capability) tagged DIRECT or TRANSITIVE, where TRANSITIVE means
	// "reachable only through a dependency" — a package that execs directly gets
	// no TRANSITIVE exec record even though it also reaches exec through its
	// deps. Storing that verbatim makes "can this package exec at all?" a
	// two-set question with a subtlety attached, so the closure is stored
	// instead (ADR-0120 SD5).
	Direct    packageprops.CapabilitySet
	Reachable packageprops.CapabilitySet
}

// Survey is the result of a Run.
type Survey struct {
	RootModule string
	Packages   []PackageReport // sorted by ImportPath

	// Failed lists packages that did not load cleanly. Their verdicts are
	// absent from Packages rather than guessed, so an unbuildable package stays
	// unsurveyed (the zero value asserts nothing) instead of being recorded as
	// safe.
	Failed []string

	// Unknown lists capability tokens capslock reported that this vocabulary
	// does not know — non-empty only when capslock has gained a capability that
	// public/packageprops has not caught up with. The bits are dropped, so a
	// non-empty Unknown means the survey is silently incomplete and
	// packageprops.Capability needs extending.
	Unknown []string
}

// Run surveys the capabilities of the packages matched by opts.
//
// It is CPU- and memory-hungry: a whole-module run builds SSA for every package
// and its dependencies. Expect multiple GB of peak RSS and tens of seconds
// (ADR-0120 Consequences). ctx cancels package loading; capslock's own analysis
// is not cancellable, so a cancel during it takes effect only once it returns.
func Run(ctx context.Context, opts Options) (s Survey, err error) {
	patterns := opts.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	cfg := &packages.Config{
		// capslock exports the exact LoadMode its analysis needs; using the
		// constant rather than a hand-copied bit list keeps us correct across
		// upgrades.
		Mode:    analyzer.PackagesLoadModeNeeded,
		Context: ctx,
		Dir:     opts.Dir,
	}
	if len(opts.Tags) > 0 {
		cfg.BuildFlags = []string{"-tags=" + strings.Join(opts.Tags, ",")}
	}

	var pkgs []*packages.Package
	pkgs, err = packages.Load(cfg, patterns...)
	if err != nil {
		err = eb.Build().Strs("patterns", patterns).Errorf("load packages for capability survey: %w", err)
		return
	}
	if len(pkgs) == 0 {
		err = eb.Build().Strs("patterns", patterns).Errorf("capability survey matched no packages")
		return
	}

	// Roots that failed to type-check yield unusable capability verdicts. Record
	// them and drop them from the queried set rather than reporting a verdict
	// derived from incomplete type information.
	ok := make([]*packages.Package, 0, len(pkgs))
	for _, p := range pkgs {
		if len(p.Errors) > 0 || p.PkgPath == "" {
			if p.PkgPath != "" {
				s.Failed = append(s.Failed, p.PkgPath)
			}
			continue
		}
		if s.RootModule == "" && p.Module != nil {
			s.RootModule = p.Module.Path
		}
		ok = append(ok, p)
	}
	sort.Strings(s.Failed)
	if len(ok) == 0 {
		err = eb.Build().Strs("failed", s.Failed).Errorf("every matched package failed to load")
		return
	}

	queried := analyzer.GetQueriedPackages(ok)
	cil := analyzer.GetCapabilityInfo(ok, queried, &analyzer.Config{
		// excludeUnanalyzed mirrors the capslock CLI's default (its -noisy flag
		// is what turns unanalyzed calls back on).
		Classifier:  analyzer.GetClassifier(true),
		Granularity: analyzer.GranularityPackage,
		// Call paths are the bulk of the output and this survey records only the
		// verdict; omitting them saves the memory of materialising every chain.
		OmitPaths: true,
	})

	// Seed every successfully queried package so that "surveyed, reaches
	// nothing" is recorded as CapabilitySafe rather than left at the zero value,
	// which would be indistinguishable from "never surveyed" (ADR-0120 SD4).
	direct := make(map[string]packageprops.CapabilitySet, len(ok))
	reachable := make(map[string]packageprops.CapabilitySet, len(ok))
	located := make(map[string]*packages.Package, len(ok))
	for _, p := range ok {
		direct[p.PkgPath] = 0
		reachable[p.PkgPath] = 0
		located[p.PkgPath] = p
	}

	unknown := map[string]struct{}{}
	for _, ci := range cil.GetCapabilityInfo() {
		// capslock's package_dir field carries the import path.
		ip := ci.GetPackageDir()
		if _, want := direct[ip]; !want {
			continue // a dependency capslock mentions but we did not query
		}
		c, known := mapCapability(ci.GetCapability())
		if !known {
			unknown[ci.GetCapability().String()] = struct{}{}
			continue
		}
		// Every record contributes to the closure; only a DIRECT one also
		// contributes to Direct. capslock emits a single record per
		// (package, capability) carrying its strongest type, so a capability
		// reached both directly and through a dependency arrives only as DIRECT —
		// which is exactly why Reachable must be built as a superset here rather
		// than from the TRANSITIVE records alone.
		reachable[ip] = reachable[ip].With(c)
		if ci.GetCapabilityType() == cpb.CapabilityType_CAPABILITY_TYPE_DIRECT {
			direct[ip] = direct[ip].With(c)
		}
	}
	for u := range unknown {
		s.Unknown = append(s.Unknown, u)
	}
	sort.Strings(s.Unknown)

	s.Packages = make([]PackageReport, 0, len(direct))
	for ip, d := range direct {
		p := located[ip]
		s.Packages = append(s.Packages, PackageReport{
			ImportPath: ip,
			Name:       p.Name,
			Dir:        packageDir(p),
			Direct:     orSafe(d),
			Reachable:  orSafe(reachable[ip]),
		})
	}
	sort.Slice(s.Packages, func(i, j int) bool { return s.Packages[i].ImportPath < s.Packages[j].ImportPath })
	return
}

// packageDir derives a package's directory from its files. go/packages exposes
// no Dir field, and a loaded package always has at least one file.
func packageDir(p *packages.Package) (dir string) {
	for _, fs := range [][]string{p.GoFiles, p.CompiledGoFiles} {
		if len(fs) > 0 {
			return filepath.Dir(fs[0])
		}
	}
	return ""
}

// orSafe turns an empty verdict from a completed survey into an explicit
// CapabilitySafe, keeping "surveyed and clean" distinct from "never surveyed".
func orSafe(s packageprops.CapabilitySet) (out packageprops.CapabilitySet) {
	if s == 0 {
		return packageprops.Caps(packageprops.CapabilitySafe)
	}
	return s
}

// mapCapability converts a capslock proto capability to the packageprops
// vocabulary. The numbering is shared by construction (ADR-0120 SD3), so this is
// a checked cast: known is false for a capability newer than the vocabulary,
// which the caller surfaces rather than dropping silently.
func mapCapability(c cpb.Capability) (out packageprops.Capability, known bool) {
	if c <= cpb.Capability_CAPABILITY_UNSPECIFIED || c > cpb.Capability_CAPABILITY_EXEC {
		return packageprops.CapabilityUnspecified, false
	}
	return packageprops.Capability(c), true
}
