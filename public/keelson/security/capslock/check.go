// Package capslock cross-references google/capslock JSON output against
// runtime/app manifests per ADR-0026 §SD10. M2.7 advisory mode: prints
// findings to stdout and always returns exit code 0 — CI does not gate
// on this until a later promotion phase. Findings format:
//
//	[FILES]   github.com/.../someapp      — no fs.* cap declared
//	[NETWORK] github.com/.../play         — ok (matches ch.query.>)
//
// The mapping table comes from ADR-0026 §SD10:
//
//	FILES                    → fs.*
//	NETWORK                  → nats.* | ch.* | kafka.* | net.*
//	READ_SYSTEM_STATE        → sysmetrics.*
//	RUNTIME, SAFE, UNSPECIFIED → always allowed
//	OS / EXEC / SYS_CALLS / ARBITRARY_EXECUTION / CGO / UNSAFE_POINTER /
//	REFLECT / MODIFY_SYSTEM_STATE → hard fail (no subject justifies)
//	UNANALYZED               → investigation required
//
// The cmd/capslock-check binary is a thin shim over [Run].
package capslock

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"

	// Side-effect imports: each app's init() registers into app.DefaultRegistry.
	_ "github.com/stergiotis/boxer/apps/adrboard"
	_ "github.com/stergiotis/boxer/apps/fibscope"
	_ "github.com/stergiotis/boxer/apps/imztop"
	_ "github.com/stergiotis/boxer/apps/play"
	_ "github.com/stergiotis/boxer/apps/terrainscope"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/leewaywidgets"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/regex_explorer"
	_ "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"
)

const widgetsPkgPath = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"

// capslockReport mirrors the relevant fields of the JSON capslock emits
// with -output=json. The rest of the structure is ignored.
type capslockReport struct {
	CapabilityInfo []capInfo `json:"capabilityInfo"`
}

type capInfo struct {
	Capability string     `json:"capability"`
	Path       []pathStep `json:"path"`
}

type pathStep struct {
	Package string `json:"package"`
}

// finding is one (app, capability) pair from the cross-reference.
type finding struct {
	AppId  string
	Cap    string
	Status findingStatus
	Reason string // empty when status == findingOK
}

type findingStatus uint8

const (
	findingOK            findingStatus = 0
	findingMissingCap    findingStatus = 1 // capability present, no manifest cap covers it
	findingHardFail      findingStatus = 2 // capability cannot be justified by any subject
	findingNeedsAnalysis findingStatus = 3 // CAPABILITY_UNANALYZED
)

// Run executes the cross-check against capslock JSON read from the file
// named by -in (or stdin when -in=-). args is the os.Args-shaped slice
// from the caller (binary name at index 0). Returns the exit code the
// binary should propagate: 0 always in M2.7 advisory mode, 1 on
// I/O/decode failure, 2 on flag-parse failure.
func Run(args []string) (exitCode int) {
	fs := flag.NewFlagSet("capslock-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	in := fs.String("in", "-", "capslock JSON file path; '-' reads stdin")
	err := fs.Parse(args[1:])
	if err != nil {
		exitCode = 2
		return
	}
	var r io.Reader
	switch *in {
	case "-":
		r = os.Stdin
	default:
		var f *os.File
		f, err = os.Open(*in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "capslock-check: open %q: %v\n", *in, err)
			exitCode = 1
			return
		}
		defer f.Close()
		r = f
	}
	var report capslockReport
	dec := json.NewDecoder(r)
	err = dec.Decode(&report)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capslock-check: decode: %v\n", err)
		exitCode = 1
		return
	}
	capsByPkg := aggregateByPackage(report)
	findings := evaluateAll(app.All(), capsByPkg)
	printFindings(findings)
	// Advisory mode: never return non-zero on findings. Promotion happens
	// in a later phase per ADR-0026 §SD10.
	return
}

// trustBoundaryPackages are runtime-internal packages that interpose
// a capability-mediated subject interface between apps and privileged
// syscalls. A capability reached ONLY through one of these packages
// is absorbed by the broker — the importing app sees the bus subject,
// not the syscall — and is therefore not propagated for the app
// manifest cross-check. ADR-0026 §SD10 names this carve-out; ADR-0028
// §SD7 extends it to chlocalbroker / chlocalpool.
//
// An app that imports os/exec directly (without going through one of
// these packages) is still flagged.
var trustBoundaryPackages = []string{
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker",
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool",
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker",
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker",
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus",
	"github.com/stergiotis/boxer/public/keelson/runtime/persist",
}

func pathTraversesTrustBoundary(path []pathStep) (yes bool) {
	// Skip the originator (Path[0]); inspect intermediate steps for a
	// hop through a runtime-internal trust boundary.
	for i := 1; i < len(path); i++ {
		for _, b := range trustBoundaryPackages {
			if path[i].Package == b || strings.HasPrefix(path[i].Package, b+"/") {
				yes = true
				return
			}
		}
	}
	return
}

// aggregateByPackage walks the capslock report and returns a map
// pkgPath → set-of-capability-strings. Capabilities reached through a
// runtime trust boundary (trustBoundaryPackages) are skipped — the
// broker absorbs them.
func aggregateByPackage(report capslockReport) (out map[string]map[string]struct{}) {
	out = make(map[string]map[string]struct{}, 16)
	for _, ci := range report.CapabilityInfo {
		if len(ci.Path) == 0 {
			continue
		}
		if pathTraversesTrustBoundary(ci.Path) {
			continue
		}
		pkg := ci.Path[0].Package
		if out[pkg] == nil {
			out[pkg] = make(map[string]struct{})
		}
		out[pkg][ci.Capability] = struct{}{}
	}
	return
}

// evaluateAll cross-references every registered app's capabilities (as
// reported by capslock) against its Manifest.Caps subject filters.
func evaluateAll(apps []app.AppI, capsByPkg map[string]map[string]struct{}) (findings []finding) {
	for _, a := range apps {
		m := a.Manifest()
		pkgPath := packageForManifest(m.Id)
		capabilities := capsByPkg[pkgPath]
		if capabilities == nil {
			continue
		}
		caps := sortedKeys(capabilities)
		for _, capName := range caps {
			f := evaluate(string(m.Id), capName, m.Caps)
			findings = append(findings, f)
		}
	}
	return
}

// evaluate applies the §SD10 mapping table to one (app, capability) pair.
func evaluate(appId, cap string, declared []app.SubjectFilter) (f finding) {
	f = finding{AppId: appId, Cap: cap, Status: findingOK}
	prefixes, hardFail, alwaysOK := capRequirements(cap)
	if alwaysOK {
		return
	}
	if cap == "CAPABILITY_UNANALYZED" {
		f.Status = findingNeedsAnalysis
		f.Reason = "capslock could not analyse the call site; treat as build-system bug"
		return
	}
	if hardFail {
		f.Status = findingHardFail
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
	f.Status = findingMissingCap
	f.Reason = "no manifest cap matches " + strings.Join(prefixes, " | ")
	return
}

// capRequirements maps a capslock capability string to the subject
// prefixes that justify it. alwaysOK is true when the capability is
// universally permitted (CAPABILITY_RUNTIME, SAFE, UNSPECIFIED).
// hardFail is true when no manifest cap can justify it.
func capRequirements(cap string) (prefixes []string, hardFail bool, alwaysOK bool) {
	switch cap {
	case "CAPABILITY_FILES":
		prefixes = []string{"fs."}
	case "CAPABILITY_NETWORK":
		prefixes = []string{"nats.", "ch.", "kafka.", "net."}
	case "CAPABILITY_READ_SYSTEM_STATE":
		prefixes = []string{"sysmetrics."}
	case "CAPABILITY_RUNTIME", "CAPABILITY_SAFE", "CAPABILITY_UNSPECIFIED":
		alwaysOK = true
	case "CAPABILITY_OPERATING_SYSTEM", "CAPABILITY_EXEC",
		"CAPABILITY_ARBITRARY_EXECUTION", "CAPABILITY_SYSTEM_CALLS",
		"CAPABILITY_CGO", "CAPABILITY_UNSAFE_POINTER",
		"CAPABILITY_REFLECT", "CAPABILITY_MODIFY_SYSTEM_STATE":
		hardFail = true
	case "CAPABILITY_UNANALYZED":
		// Handled by caller as findingNeedsAnalysis.
		hardFail = true
	default:
		// Unknown capability — defensive default is hard fail so reviewers
		// notice and the mapping table is updated.
		hardFail = true
	}
	return
}

// packageForManifest returns the Go package path that capslock reports
// for an app. For folded demos (Id = "<widgets>/<demo>") the underlying
// package IS widgets — strip the demo segment.
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

func printFindings(findings []finding) {
	var ok, missing, hard, unanalysed int
	for _, f := range findings {
		switch f.Status {
		case findingOK:
			ok++
		case findingMissingCap:
			missing++
			fmt.Printf("[MISSING_CAP] %s :: %s — %s\n", f.AppId, f.Cap, f.Reason)
		case findingHardFail:
			hard++
			fmt.Printf("[HARD_FAIL]   %s :: %s — %s\n", f.AppId, f.Cap, f.Reason)
		case findingNeedsAnalysis:
			unanalysed++
			fmt.Printf("[INVESTIGATE] %s :: %s — %s\n", f.AppId, f.Cap, f.Reason)
		}
	}
	fmt.Fprintf(os.Stderr,
		"capslock-check: %d ok, %d missing-cap, %d hard-fail, %d investigate (M2.7 advisory — exit 0)\n",
		ok, missing, hard, unanalysed)
}
