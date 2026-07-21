// Package capslock cross-references google/capslock capability findings against
// runtime/app manifests per ADR-0026 §SD10.
//
// The gate asks one question per registered app: does the app's own code
// exercise a privileged capability that its Manifest.Caps subject filters do
// not justify? Findings format:
//
//	[MISSING_CAP] github.com/.../someapp :: CAPABILITY_FILES — no manifest cap matches fs.
//	[HARD_FAIL]   github.com/.../someapp :: CAPABILITY_EXEC   — no subject filter justifies this
//
// The analysis runs in-process: [Analyse] loads the app packages itself and
// calls the capslock library. There is no external `capslock` binary, no JSON
// contract and no wrapper script in the path — see the 2026-07-15 update to
// ADR-0026 for why the shape changed.
//
// # What counts as the app's own capability
//
// A finding is raised only for capabilities the app's *own code* exercises.
// That takes two conditions, and capslock's own verdict supplies only the first.
//
// First, the record must be CAPABILITY_TYPE_DIRECT. Transitive reachability is
// no signal at all here: it saturates completely. Measured over the app
// packages, every one of them reaches every capability capslock reports — 190 of
// 190 (package, capability) pairs — because the standard library and the runtime
// reach everything. A gate that fires on transitive reach fires on everything.
//
// Second — and this is not what DIRECT means — the capability-classified
// function must be called *by the app's own code*: the second-to-last function
// on the path must be in the originating package. capslock demotes a path to
// TRANSITIVE only when it passes through a non-stdlib package other than the
// originator (analyzer.go, `n != pName && !isStdLib(pName)`), so a stdlib hop
// never demotes and DIRECT really means "no third-party package on the path".
// The capability may still be incurred far inside the standard library, over an
// edge VTA guessed rather than proved:
//
//	imztop.formatPercent -> strconv.FormatFloat -> … -> internal/strconv.float32bits
//	  => DIRECT UNSAFE_POINTER, i.e. formatting a float hard-fails the gate.
//
//	godepview.Mount -> context.WithCancel$1 -> (*context.cancelCtx).cancel
//	  -> (*context.afterFuncCtx).cancel$1 -> (*net.netFD).connect$1
//	  => DIRECT NETWORK, because VTA links every func() ever passed to
//	     context.AfterFunc anywhere in the program.
//
// Requiring the app to make the call itself drops 17 of 27 such pairs, and every
// pair that survives names a real call site (play.newExecOptions -> os.Getpid;
// the widgets TestDriver -> os.MkdirAll). Both rules are lower bounds; this is
// the tighter one. What it gives up is capabilities reached through a higher-order
// stdlib wrapper (io.Copy, io.ReadAll) — precisely the cases where VTA cannot
// tell an *os.File from a net.Conn, so the verdict was a coin flip either way.
//
// This also subsumes the trust-boundary carve-out ADR-0026 §SD10 and ADR-0028
// §SD7 describe: a capability an app reaches only by calling fsbroker,
// inprocbus or another broker is reached through a non-stdlib package, so it is
// already TRANSITIVE and never raises a finding. The carve-out needed an
// explicit package list only because the previous implementation could not tell
// direct from transitive at all.
//
// # Bounds
//
// Verdicts are a lower bound. capslock builds its call graph with VTA, which
// links a call only where a concrete type demonstrably flows to it; reflection,
// unsafe, cgo and linkname carry no static edges. An absent capability is the
// absence of a finding, not proof of absence. The threat model is hygiene, not
// security: this raises the bar on accident, not on adversarial intent.
package capslock

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/google/capslock/analyzer"
	cpb "github.com/google/capslock/proto"
	"golang.org/x/tools/go/packages"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep/godepcollect"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"

	// Side-effect imports: each app's init() registers into app.DefaultRegistry,
	// and the gate can only evaluate apps registered in *this* binary. Every
	// package that registers an app must therefore appear here — an app missing
	// from this list is silently unchecked, which is why TestAppSetIsComplete
	// asserts the list against the tree rather than trusting it.
	_ "github.com/stergiotis/boxer/apps/adhocdemo"
	_ "github.com/stergiotis/boxer/apps/capdemo"
	_ "github.com/stergiotis/boxer/apps/capinspector"
	_ "github.com/stergiotis/boxer/apps/fibscope"
	_ "github.com/stergiotis/boxer/apps/godepview"
	_ "github.com/stergiotis/boxer/apps/imzrt"
	_ "github.com/stergiotis/boxer/apps/imztop"
	_ "github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/apps/splashscreen"
	_ "github.com/stergiotis/boxer/apps/sqlappletcreator"
	_ "github.com/stergiotis/boxer/apps/taskdemo"
	_ "github.com/stergiotis/boxer/apps/terrainscope"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/idsshowcase"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/logdemo"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/sccmap"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"
)

const widgetsPkgPath = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"

// defaultPatterns are the package patterns the gate analyses: everything that
// can register an app. Keeping the analysis app-scoped is deliberate — a
// whole-tree run costs minutes and gigabytes to answer a question about
// packages that are not apps.
var defaultPatterns = []string{
	"./apps/...",
	"./public/thestack/imzero2/egui2/demo/apps/...",
}

// Options configures [Analyse].
type Options struct {
	// Root is the module root to analyse. It is passed to packages.Config.Dir
	// rather than chdir'd into: capslock's own analyzer.LoadPackages sets no
	// Dir and loads from the process working directory, which a library may not
	// mutate.
	Root string
	// Tags are the build tags to load with. Nil resolves them from Root, which
	// is what callers should normally do — the tags are load-bearing here, and
	// a load that omits them analyses a tree that does not exist.
	Tags []string
	// Patterns are the package patterns to analyse. Nil uses defaultPatterns.
	Patterns []string
}

// Finding is one (app, capability) pair from the cross-reference.
type Finding struct {
	AppId  string
	Cap    string
	Status Status
	Reason string // empty when Status == StatusOK
}

// Status classifies a Finding against the §SD10 mapping table.
type Status uint8

const (
	StatusOK         Status = 0
	StatusMissingCap Status = 1 // capability present, no manifest cap covers it
	StatusHardFail   Status = 2 // capability cannot be justified by any subject
)

// Analyse runs capslock over the app packages under opts.Root and cross-checks
// each registered app's direct capabilities against its manifest. The returned
// findings are ordered by app then capability.
func Analyse(ctx context.Context, opts Options) (findings []Finding, err error) {
	capsByPkg, err := directCapabilities(ctx, opts)
	if err != nil {
		return
	}
	findings = evaluateAll(app.All(), capsByPkg)
	return
}

// directCapabilities loads the packages and reduces capslock's per-function
// records to the set of capabilities each package's own code exercises.
func directCapabilities(ctx context.Context, opts Options) (capsByPkg map[string]map[string]struct{}, err error) {
	root := opts.Root
	if root == "" {
		root = "."
	}
	tags := opts.Tags
	if tags == nil {
		tags = godepcollect.ResolveTags("", root)
	}
	patterns := opts.Patterns
	if len(patterns) == 0 {
		patterns = defaultPatterns
	}
	cfg := &packages.Config{
		Mode:    analyzer.PackagesLoadModeNeeded,
		Dir:     root,
		Context: ctx,
	}
	if len(tags) > 0 {
		cfg.BuildFlags = []string{"-tags=" + strings.Join(tags, ",")}
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		err = eh.Errorf("unable to load app packages: %w", err)
		return
	}
	// A pattern that matches nothing, or a package that fails to type-check,
	// yields an empty capability set — which reads as "no findings" and passes
	// the gate. Refuse to report a clean bill from a load that did not happen.
	if len(pkgs) == 0 {
		err = eh.Errorf("no packages matched %v under %q", patterns, root)
		return
	}
	var loadErrs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			loadErrs = append(loadErrs, p.PkgPath+": "+e.Error())
		}
	})
	if len(loadErrs) > 0 {
		sort.Strings(loadErrs)
		err = eh.Errorf("package load reported %d error(s), the analysis would be incomplete:\n\t%s",
			len(loadErrs), strings.Join(loadErrs, "\n\t"))
		return
	}
	queried := analyzer.GetQueriedPackages(pkgs)
	cil := analyzer.GetCapabilityInfo(pkgs, queried, &analyzer.Config{
		// excludeUnanalyzed=true is capslock's own CLI default, and it is the
		// right choice here rather than an inherited one. The classifier's
		// "unanalyzed" set is 39 higher-order standard-library functions —
		// sort.Slice, io.Copy, sync.Once.Do, errors.Is, the bufio Reader/Writer
		// methods — whose behaviour depends on a value the caller supplies.
		// Including them would make each a *sink*: the walk stops there and
		// reports CAPABILITY_UNANALYZED instead of continuing into the concrete
		// callee. That trades real findings for noise, since every non-trivial
		// Go program calls sort.Slice or io.Copy. Measured over the app
		// packages: excluding them yields 27 direct (package, capability)
		// pairs, including them yields 26 plus UNANALYZED on 3 packages.
		Classifier: analyzer.GetClassifier(true),
		// GranularityFunction, aggregated below — NOT GranularityPackage. At
		// package granularity capslock keeps only the sort-first function's
		// record per (capability, package), so the surviving record's
		// capabilityType is one arbitrary representative's rather than the
		// package's strongest. Filtering DIRECT there finds 4 pairs where this
		// aggregation finds 27.
		Granularity: analyzer.GranularityFunction,
	})
	capsByPkg = ownCapabilities(cil)
	return
}

// ownCapabilities keeps the capabilities a package's own code exercises. A
// (package, capability) pair qualifies when *any* function record for it does:
// capslock emits one record per originating function, so a pair is the app's as
// soon as one of its functions demonstrably incurs it, and a transitive record
// for the same pair must not mask a direct one. Filtering at
// GranularityPackage instead would ask capslock for one arbitrary
// representative's verdict — see the Granularity note in directCapabilities.
//
// The two conditions are documented at the package level: the record must be
// DIRECT, and the classified function must be called by the originating
// package's own code.
func ownCapabilities(cil *cpb.CapabilityInfoList) (out map[string]map[string]struct{}) {
	out = make(map[string]map[string]struct{}, 16)
	for _, ci := range cil.GetCapabilityInfo() {
		if ci.GetCapabilityType() != cpb.CapabilityType_CAPABILITY_TYPE_DIRECT {
			continue
		}
		path := ci.GetPath()
		if len(path) == 0 {
			continue
		}
		// Path[0] is the originating function and Path[len-1] the one that
		// incurs the capability. (CapabilityInfo.PackageDir carries the same
		// import path as Path[0]'s package despite its name, but the path is
		// what the attribution is about.)
		pkg := path[0].GetPackage()
		if pkg == "" {
			continue
		}
		if callerOfSink(path) != pkg {
			// The classified function is reached from somewhere other than this
			// package's code — a deeper stdlib frame. Not the app's operation.
			continue
		}
		capName := normaliseCapability(ci.GetCapabilityName())
		if capName == "" {
			continue
		}
		capName = refineCapability(capName, path[len(path)-1].GetName())
		if out[pkg] == nil {
			out[pkg] = make(map[string]struct{})
		}
		out[pkg][capName] = struct{}{}
	}
	return
}

// The §SD10 vocabulary for capabilities capslock reports too coarsely to
// decide on. The "/" qualifier follows capslock's own category convention;
// these names are produced after [normaliseCapability] has cut any qualifier
// capslock itself attached, and nothing downstream cuts again.
const (
	capReadSystemState = "CAPABILITY_READ_SYSTEM_STATE"
	capReadProcessSelf = capReadSystemState + "/process-self"
	capReadEnv         = capReadSystemState + "/env"
)

// processSelfFuncs report an ambient fact about the calling process itself.
// They confer no effect and disclose nothing beyond the process, and every use
// of them that matters is classified on its own — a path built from os.Getwd is
// FILES when it is opened, an address resolved from the environment is NETWORK
// when it is dialled. Requiring a subject to justify the *discovery* as well
// would count the same act twice, and there is no subject that would honestly
// justify it: see the READ_SYSTEM_STATE discussion in ADR-0026's 2026-07-15
// update.
var processSelfFuncs = map[string]struct{}{
	"os.Getwd":       {},
	"os.Getpid":      {},
	"os.Getppid":     {},
	"os.Executable":  {},
	"os.Getpagesize": {},
}

// envFuncs read the process environment. This gate defers to codelint CS011,
// which bans them outright outside public/config/env (ADR-0009) — a stricter
// rule than any subject filter, and one that applies to the whole tree rather
// than to apps. Deferring keeps §SD10 from issuing the wrong remedy: "declare a
// sysmetrics.* subject" is not the fix for reading an env var, "declare it in
// public/config/env" is.
//
// These cannot reach the gate today: apps read configuration through
// public/config/env, which is a non-stdlib hop and therefore transitive. The
// entry is here so that a direct call gets CS011's diagnosis alone rather than
// CS011's plus a misleading one.
var envFuncs = map[string]struct{}{
	"os.Getenv":      {},
	"os.LookupEnv":   {},
	"os.Environ":     {},
	"os.ExpandEnv":   {},
	"syscall.Getenv": {},
}

// refineCapability sharpens a capability using the function that incurs it,
// where capslock's category is too coarse for the §SD10 mapping table to decide
// on. Only READ_SYSTEM_STATE needs this: capslock uses it for 36 functions
// spanning network topology (net.Interfaces), the environment (os.Getenv), user
// and host identity (os/user.Current, os.Hostname) and ambient process facts
// (os.Getwd) — one row cannot answer for all four. Everything it does not
// recognise keeps the unrefined name and the table's existing verdict.
func refineCapability(capName string, sink string) (out string) {
	out = capName
	if capName != capReadSystemState {
		return
	}
	if _, ok := processSelfFuncs[sink]; ok {
		out = capReadProcessSelf
		return
	}
	if _, ok := envFuncs[sink]; ok {
		out = capReadEnv
		return
	}
	return
}

// callerOfSink returns the package of the function that calls the
// capability-incurring function at the end of path. A single-element path is
// its own caller: the originating function is itself classified.
func callerOfSink(path []*cpb.Function) (pkg string) {
	if len(path) == 1 {
		pkg = path[0].GetPackage()
		return
	}
	pkg = path[len(path)-2].GetPackage()
	return
}

// normaliseCapability renders a capslock capability name in the CAPABILITY_*
// vocabulary ADR-0026 §SD10's mapping table is written in.
//
// The library reports the classifier's category string, which is unprefixed
// ("FILES", "NETWORK", "UNANALYZED") — unlike the JSON output's `capability`
// field, which serialises the proto enum and so reads "CAPABILITY_FILES".
// Feeding the raw name to [capRequirements] would send every capability to its
// defensive default and hard-fail the whole tree. The enum
// (CapabilityInfo.Capability) is prefixed already, but capslock's own proto
// marks it superseded by capability_name and it cannot represent a capability
// added after this pin — an unknown name must reach capRequirements intact so
// the default fires and a reviewer updates the table.
func normaliseCapability(name string) (out string) {
	// A category may carry a "/"-suffixed qualifier; capslock's own enum
	// mapping cuts at the first "/", so do the same.
	name, _, _ = strings.Cut(name, "/")
	if name == "" {
		return
	}
	if strings.HasPrefix(name, "CAPABILITY_") {
		out = name
		return
	}
	out = "CAPABILITY_" + name
	return
}

// evaluateAll cross-references every registered app's direct capabilities
// against its Manifest.Caps subject filters.
func evaluateAll(apps []app.AppI, capsByPkg map[string]map[string]struct{}) (findings []Finding) {
	type appEntry struct {
		id       string
		pkgPath  string
		declared []app.SubjectFilter
	}
	entries := make([]appEntry, 0, len(apps))
	for _, a := range apps {
		m := a.Manifest()
		entries = append(entries, appEntry{
			id:       string(m.Id),
			pkgPath:  packageForManifest(m.Id),
			declared: m.Caps,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].id < entries[j].id })
	for _, e := range entries {
		capabilities := capsByPkg[e.pkgPath]
		if capabilities == nil {
			continue
		}
		for _, capName := range sortedKeys(capabilities) {
			findings = append(findings, evaluate(e.id, capName, e.declared))
		}
	}
	return
}

// evaluate applies the §SD10 mapping table to one (app, capability) pair.
func evaluate(appId string, capName string, declared []app.SubjectFilter) (f Finding) {
	f = Finding{AppId: appId, Cap: capName, Status: StatusOK}
	prefixes, hardFail, alwaysOK := capRequirements(capName)
	if alwaysOK {
		return
	}
	if hardFail {
		f.Status = StatusHardFail
		f.Reason = "no subject filter justifies this capability; reviewer sign-off required"
		return
	}
	for _, p := range prefixes {
		for _, d := range declared {
			if strings.HasPrefix(d.Pattern, p) {
				return
			}
		}
	}
	f.Status = StatusMissingCap
	f.Reason = "no manifest cap matches " + strings.Join(prefixes, " | ")
	return
}

// capRequirements maps a capslock capability to the subject prefixes that
// justify it. alwaysOK is true when the capability is universally permitted
// (CAPABILITY_RUNTIME, SAFE, UNSPECIFIED). hardFail is true when no manifest
// cap can justify it.
//
// CAPABILITY_UNANALYZED has no row: the classifier is configured never to
// report it (see directCapabilities), so it can only arrive here as an unknown,
// where the default already does the right thing.
func capRequirements(capName string) (prefixes []string, hardFail bool, alwaysOK bool) {
	switch capName {
	case "CAPABILITY_FILES":
		prefixes = []string{"fs."}
	case "CAPABILITY_NETWORK":
		prefixes = []string{"nats.", "ch.", "kafka.", "net."}
	case capReadProcessSelf, capReadEnv:
		// Refined out of READ_SYSTEM_STATE by [refineCapability]; the reasons
		// are on processSelfFuncs and envFuncs.
		alwaysOK = true
	case capReadSystemState:
		// What is left after the refinement: network topology, and user and
		// host identity. Note that no app triggers this today, and imztop —
		// the app the sysmetrics row was written for — cannot: it reads /proc
		// through the sysmetrics packages, a non-stdlib hop, so its access is
		// transitive and invisible here. The row stands for an app that reads
		// the machine directly.
		prefixes = []string{"sysmetrics."}
	case "CAPABILITY_RUNTIME", "CAPABILITY_SAFE", "CAPABILITY_UNSPECIFIED":
		alwaysOK = true
	case "CAPABILITY_OPERATING_SYSTEM", "CAPABILITY_EXEC",
		"CAPABILITY_ARBITRARY_EXECUTION", "CAPABILITY_SYSTEM_CALLS",
		"CAPABILITY_CGO", "CAPABILITY_UNSAFE_POINTER",
		"CAPABILITY_REFLECT", "CAPABILITY_MODIFY_SYSTEM_STATE":
		hardFail = true
	default:
		// Unknown capability — defensive default is hard fail so reviewers
		// notice and the mapping table is updated.
		hardFail = true
	}
	return
}

// packageForManifest returns the Go package path capslock reports for an app.
// For folded demos (Id = "<widgets>/<demo>") the underlying package IS widgets —
// strip the demo segment. Every other manifest Id equals its package path
// verbatim, an invariant the l12manifestid designlint rule enforces.
func packageForManifest(id app.AppIdT) (pkg string) {
	s := string(id)
	if strings.HasPrefix(s, widgetsPkgPath+"/") {
		pkg = widgetsPkgPath
		return
	}
	pkg = s
	return
}

func sortedKeys(m map[string]struct{}) (out []string) {
	out = make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return
}

// Run executes the cross-check and reports findings against the accepted
// baseline. args is the os.Args-shaped slice from the caller (binary name at
// index 0). Returns the exit code the binary should propagate: 0 when every
// finding is already in the baseline, 1 on drift or analysis failure, 2 on
// flag-parse failure.
func Run(args []string) (exitCode int) {
	fs := flag.NewFlagSet("capslock-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", ".", "module root to analyse")
	tags := fs.String("tags", "", "build tags (default: the root's tags file)")
	err := fs.Parse(args[1:])
	if err != nil {
		exitCode = 2
		return
	}
	opts := Options{Root: *root}
	if *tags != "" {
		opts.Tags = godepcollect.SplitTags(*tags)
	}
	findings, err := Analyse(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capslock-check: %v\n", err)
		exitCode = 1
		return
	}
	drift, stale := CompareToBaseline(findings)
	printFindings(findings, drift, stale)
	if len(drift) > 0 || len(stale) > 0 {
		exitCode = 1
	}
	return
}

func printFindings(findings []Finding, drift []Finding, stale []string) {
	var ok, missing, hard int
	for _, f := range findings {
		switch f.Status {
		case StatusOK:
			ok++
		case StatusMissingCap:
			missing++
			fmt.Printf("[MISSING_CAP] %s :: %s — %s\n", f.AppId, f.Cap, f.Reason)
		case StatusHardFail:
			hard++
			fmt.Printf("[HARD_FAIL]   %s :: %s — %s\n", f.AppId, f.Cap, f.Reason)
		}
	}
	for _, f := range drift {
		fmt.Fprintf(os.Stderr, "[DRIFT]       %s :: %s — not in the accepted baseline\n", f.AppId, f.Cap)
	}
	for _, s := range stale {
		fmt.Fprintf(os.Stderr, "[STALE]       %s — in the baseline but no longer reported; remove it\n", s)
	}
	fmt.Fprintf(os.Stderr,
		"capslock-check: %d ok, %d missing-cap, %d hard-fail; %d drift, %d stale\n",
		ok, missing, hard, len(drift), len(stale))
}
