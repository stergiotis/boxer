package wasmsurvey

import (
	"context"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// This file is the empirical confirm stage: it asks the real TinyGo compiler
// whether a package builds for a wasm target, rather than trusting the static
// guess. A package is wrapped in a synthetic main that references its exported
// functions (so TinyGo actually compiles the exported API, not just init),
// then `tinygo build` runs for the target. Build success ⇒ Green; a clean
// build failure ⇒ Red with the failure bucket parsed from stderr.
//
// Caveat (also in the ADR): the probe forces exported *functions* to be
// retained, so dead-code elimination can still drop unexported code an
// exported function never reaches. The verdict is "the exported API compiles
// and links under TinyGo," a necessary not fully-sufficient condition.

// tinygoAvailable reports whether a `tinygo` binary is on PATH. The empirical
// stage skips (leaving static verdicts) when it is not.
func tinygoAvailable() (ok bool) {
	_, err := exec.LookPath("tinygo")
	return err == nil
}

// tinygoVersion returns the first line of `tinygo version`, best-effort.
func tinygoVersion(ctx context.Context) (ver string) {
	out, err := exec.CommandContext(ctx, "tinygo", "version").CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

// tinygoPreflightE builds a trivial empty main for target to check the
// toolchain is usable at all before probing every package. Its main job is to
// catch the case where TinyGo refuses the active Go toolchain version (TinyGo
// 0.39 supports Go ≤1.25; boxer is on Go 1.26), which would otherwise make
// every per-package probe fail identically. ok=false carries a short detail
// for the survey to surface as a single warning instead of a wall of failures.
func tinygoPreflightE(ctx context.Context, root string, target TargetID, tags []string) (ok bool, detail string) {
	tmp, err := os.MkdirTemp(root, ".wasmsurvey-preflight-")
	if err != nil {
		return false, "preflight tempdir: " + err.Error()
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err = os.WriteFile(filepath.Join(tmp, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		return false, "preflight write: " + err.Error()
	}
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	args := []string{"build", "-target=" + target.TinyGoTarget(), "-o", filepath.Join(tmp, "out.wasm")}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, " "))
	}
	args = append(args, ".")
	cmd := exec.CommandContext(cctx, "tinygo", args...)
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=jsonv2") //boxer:lint disable=CS011 reason="forwards the ambient process environment into the tinygo build subprocess; not a boxer config read — the env registry cannot model inheriting the whole environment for a child process"
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return true, ""
	}
	return false, firstMeaningfulLine(string(out))
}

// exportedFuncs returns the names of exported, non-generic, package-level
// functions of the package in dir, selected under the target's build
// constraints (GOOS/GOARCH=wasm + tags) so we never reference a symbol that
// does not exist for the target. Methods, generics, main and init are
// excluded. A nil/empty result means the probe falls back to a blank import.
func exportedFuncs(dir string, goos string, tags []string) (names []string, err error) {
	bctx := build.Default
	bctx.GOOS = goos
	bctx.GOARCH = "wasm"
	bctx.CgoEnabled = false
	// Enumerate exports under the same build tags TinyGo compiles with —
	// crucially `tinygo` — so a synthetic main never references a !tinygo-only
	// export (e.g. eh's zerolog-gated funcs) that the real build won't have,
	// which would fail as "undefined: probe.X" and falsely score the package Red.
	bctx.BuildTags = appendTag(append([]string(nil), tags...), "tinygo")

	var bp *build.Package
	bp, err = bctx.ImportDir(dir, 0)
	if err != nil {
		// e.g. no buildable Go files under these constraints — let the caller
		// fall back to a blank import (which will itself fail the build for a
		// package with nothing to compile, correctly surfacing Red).
		return nil, err
	}

	fset := token.NewFileSet()
	for _, f := range bp.GoFiles {
		af, e := parser.ParseFile(fset, filepath.Join(dir, f), nil, 0)
		if e != nil {
			continue
		}
		for _, d := range af.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil { // skip non-funcs and methods
				continue
			}
			if fd.Type.TypeParams != nil && len(fd.Type.TypeParams.List) > 0 {
				continue // generic: not usable as a plain value without instantiation
			}
			name := fd.Name.Name
			if name == "main" || name == "init" || !ast.IsExported(name) {
				continue
			}
			names = append(names, name)
		}
	}
	return names, nil
}

// writeProbeMainE writes the synthetic main.go into dir. With exported funcs
// it binds each as a value in a package-level `var _ = []any{...}`, which
// keeps TinyGo from dead-code-eliminating them; with none it blank-imports the
// package (exercising init and package-level vars only).
func writeProbeMainE(dir string, importPath string, funcs []string) (err error) {
	var b strings.Builder
	b.WriteString("// Code generated by wasmsurvey probe; DO NOT EDIT.\n")
	b.WriteString("package main\n\n")
	if len(funcs) == 0 {
		b.WriteString("import _ \"")
		b.WriteString(importPath)
		b.WriteString("\"\n\nfunc main() {}\n")
	} else {
		b.WriteString("import probe \"")
		b.WriteString(importPath)
		b.WriteString("\"\n\nvar _ = []any{\n")
		for _, fn := range funcs {
			b.WriteString("\tprobe.")
			b.WriteString(fn)
			b.WriteString(",\n")
		}
		b.WriteString("}\n\nfunc main() {}\n")
	}
	err = os.WriteFile(filepath.Join(dir, "main.go"), []byte(b.String()), 0o644)
	return
}

// probePackageE builds a synthetic consumer of importPath with `tinygo build`
// for target and reports the empirical verdict. A returned err signals a
// harness failure (could not create the temp package, etc.) — distinct from a
// clean build failure, which is reported as (TierRed, reason, _, nil). The
// temp main package is created inside the target package's own directory (so
// module resolution and internal/ import rules are satisfied) and removed
// before return.
func probePackageE(ctx context.Context, root string, importPath string, dir string, target TargetID, tags []string, timeout time.Duration) (tier Tier, reason Reason, millis int64, err error) {
	funcs, _ := exportedFuncs(dir, target.GOOS(), tags) // best-effort; nil ⇒ blank import

	// Create the probe package under the module root so the synthetic main
	// resolves the local (main) module — placing it deeper made `go` try to
	// fetch boxer from VCS ("invalid version: unknown revision"). internal/
	// packages cannot be imported from the root and are excluded upstream; any
	// that slip through are caught as harness errors below, not scored Red.
	var tmp string
	tmp, err = os.MkdirTemp(root, ".wasmsurvey-probe-")
	if err != nil {
		err = eb.Build().Str("root", root).Errorf("create probe temp dir: %w", err)
		return
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	if err = writeProbeMainE(tmp, importPath, funcs); err != nil {
		err = eb.Build().Str("pkg", importPath).Errorf("write probe main: %w", err)
		return
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	out := filepath.Join(tmp, "out.wasm")
	args := []string{"build", "-target=" + target.TinyGoTarget(), "-o", out}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, " "))
	}
	args = append(args, ".")

	cmd := exec.CommandContext(cctx, "tinygo", args...)
	cmd.Dir = tmp
	// Carry the repo's json/v2 experiment into the TinyGo build. Whether
	// TinyGo honors it is the survey's open question (ADR-0078); a rejection
	// is captured as the goexperiment reason rather than a tool error.
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=jsonv2") //boxer:lint disable=CS011 reason="forwards the ambient process environment into the tinygo build subprocess; not a boxer config read — the env registry cannot model inheriting the whole environment for a child process"

	start := time.Now()
	combined, runErr := cmd.CombinedOutput()
	millis = time.Since(start).Milliseconds()

	if runErr == nil {
		return TierGreen, Reason{}, millis, nil // empirically builds
	}

	// Some failures are about the probe setup, not the package's wasm
	// amenability (a test-only/internal package that cannot be imported, or a
	// module-resolution hiccup). Treat those as inconclusive (err) so the
	// static verdict stands, rather than scoring a false Red.
	if isHarnessError(string(combined)) {
		err = eb.Build().Str("pkg", importPath).Errorf("probe inconclusive: %s", firstMeaningfulLine(string(combined)))
		return TierUnknown, Reason{}, millis, err
	}

	// A genuine non-zero exit is a verdict.
	kind := classifyProbeOutput(string(combined))
	if cctx.Err() == context.DeadlineExceeded {
		kind = ReasonProbeOther
	}
	reason = Reason{
		Kind:   kind,
		Leaf:   importPath,
		Detail: firstMeaningfulLine(string(combined)),
	}
	return TierRed, reason, millis, nil
}

// isHarnessError reports whether a failed build is about the probe setup
// rather than the package itself: a package that cannot be imported by a
// synthetic main (test-only ⇒ empty package name; internal/ ⇒ scope rule) or a
// transient module-resolution failure. These must not be scored as a Red
// wasm verdict.
func isHarnessError(out string) (b bool) {
	l := strings.ToLower(out)
	switch {
	case strings.Contains(l, "invalid package name"):
		return true
	case strings.Contains(l, "use of internal package") && strings.Contains(l, "not allowed"):
		return true
	case strings.Contains(l, "invalid version") || strings.Contains(l, "unknown revision"):
		return true
	case strings.Contains(l, "no buildable go source files") || strings.Contains(l, "build constraints exclude all go files"):
		return true
	case strings.Contains(l, "cannot find main module") || strings.Contains(l, "go.mod file not found"):
		return true
	default:
		return false
	}
}

// classifyProbeOutput maps a failed `tinygo build` output to a reason bucket.
// Checks run most-specific first. The buckets mirror ReasonKind so empirical
// and static reasons aggregate together in the report.
func classifyProbeOutput(out string) (kind ReasonKind) {
	l := strings.ToLower(out)
	switch {
	case strings.Contains(l, "requires go version") || (strings.Contains(l, "requires go") && strings.Contains(l, "got go")) || strings.Contains(l, "unsupported go version"):
		return ReasonToolchain
	case strings.Contains(l, "goexperiment") || strings.Contains(l, "json/v2") || strings.Contains(l, "jsonv2") || strings.Contains(l, "encoding/json/v2"):
		return ReasonGoexperimentJSONv2
	case strings.Contains(l, "//go:linkname") || strings.Contains(l, "go:linkname"):
		return ReasonLinker
	case strings.Contains(l, "wasm-ld") || strings.Contains(l, "undefined symbol") || strings.Contains(l, "ld.lld") || strings.Contains(l, "link error"):
		return ReasonLinker
	case strings.Contains(l, "cgo") || strings.Contains(l, "import \"c\""):
		return ReasonCgo
	case strings.Contains(l, "syscall"):
		return ReasonSyscall
	case strings.Contains(l, "reflect"):
		return ReasonReflect
	case strings.Contains(l, "unsafe"):
		return ReasonUnsafe
	case strings.Contains(l, "is not in std") || strings.Contains(l, "could not import") ||
		strings.Contains(l, "cannot find package") || strings.Contains(l, "no required module") ||
		strings.Contains(l, "package ") && strings.Contains(l, " is not "):
		return ReasonUnsupportedStdlib
	default:
		return ReasonProbeOther
	}
}

// firstMeaningfulLine returns the first non-boilerplate line of tinygo output,
// trimmed and length-capped, for a compact report Detail.
func firstMeaningfulLine(out string) (line string) {
	const maxLen = 200
	for ln := range strings.SplitSeq(out, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "tinygo:") && strings.Contains(ln, "build") {
			continue
		}
		if len(ln) > maxLen {
			ln = ln[:maxLen] + "…"
		}
		return ln
	}
	return ""
}
