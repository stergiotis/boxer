package codelint

import (
	"context"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/tools/go/packages"
)

// LoadConfig controls how the driver loads packages for analysis.
//
// Dir is the directory the patterns resolve against; empty means the process
// working directory. An embedder linting a tree it did not chdir into — the
// composite gate run against another repository root — must set it, or the
// relative patterns silently resolve somewhere else.
type LoadConfig struct {
	Ctx       context.Context
	Dir       string
	BuildTags []string
	Tests     bool
}

// LoadPackagesE loads the package graph rooted at the supplied patterns
// (e.g. "./..."), populating the syntax + type info each analyzer needs.
//
// Generated files (*.out.go, *.gen.go) are filtered post-load — they
// remain in the package graph for type resolution but are not visited.
func LoadPackagesE(cfg LoadConfig, roots ...string) (pkgs []*packages.Package, err error) {
	if cfg.Ctx == nil {
		cfg.Ctx = context.Background()
	}
	pcfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedImports |
			packages.NeedCompiledGoFiles,
		Context: cfg.Ctx,
		Dir:     cfg.Dir,
		Tests:   cfg.Tests,
	}
	if len(cfg.BuildTags) > 0 {
		pcfg.BuildFlags = []string{"-tags=" + strings.Join(cfg.BuildTags, ",")}
	}
	pkgs, err = packages.Load(pcfg, roots...)
	if err != nil {
		err = eb.Build().Strs("roots", roots).Errorf("codelint load: %w", err)
		return
	}
	for _, p := range pkgs {
		if len(p.Errors) == 0 {
			continue
		}
		err = eb.Build().
			Str("pkg", p.PkgPath).
			Int("errCount", len(p.Errors)).
			Str("firstErr", p.Errors[0].Msg).
			Errorf("codelint load: package has errors")
		return
	}
	return
}

// TestSupportPackages names the packages that exist to serve tests — a shared
// conformance suite, a fixture builder, an assertion helper — and are therefore
// skipped by every rule.
//
// The `_test.go` suffix, which CS012 keys off, does not reach these: a
// conformance suite several packages import lives in ordinary files, so every
// rule sees its fixture-building and assertion code as shipped runtime. The
// findings that produces are not defects — an assertion message formats the
// values it is comparing, on purpose — and they recur with every rule added.
//
// This is a list rather than a match on the repo's trailing-"test" naming
// convention, because the two fail in opposite directions. A suffix rule
// exempts anything that happens to end in those four letters — "latest" does —
// and an exemption nobody declared is invisible. An unlisted test-support
// package instead produces findings, which someone sees. An embedder with its
// own such packages appends to this slice before running the linter.
var TestSupportPackages = []string{
	"codectest",
	"ebtest",
	"exchangetest",
	"genbuildertest",
	"pcmtest",
	"stashtest",
	"storagetest",
	"test",
	"unittest",
}

// IsTestSupportPackage reports whether name is one of TestSupportPackages. The
// external-test form (`package foo_test`) is also matched, since it is test
// code by construction; it is only in the package graph when LoadConfig.Tests
// is set, which neither the CLI nor the gate does.
func IsTestSupportPackage(name string) (skip bool) {
	if name == "" {
		return
	}
	if strings.HasSuffix(name, "_test") {
		skip = true
		return
	}
	for _, n := range TestSupportPackages {
		if name == n {
			skip = true
			return
		}
	}
	return
}

// IsGeneratedFile reports whether a file path is one of the generation
// suffixes we always skip. Matches the grep filters in scripts/ci/lint.sh.
func IsGeneratedFile(path string) (skip bool) {
	if strings.HasSuffix(path, ".out.go") || strings.HasSuffix(path, ".gen.go") {
		skip = true
	}
	return
}
