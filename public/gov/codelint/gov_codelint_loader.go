package codelint

import (
	"context"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/tools/go/packages"
)

// LoadConfig controls how the driver loads packages for analysis.
type LoadConfig struct {
	Ctx       context.Context
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

// IsGeneratedFile reports whether a file path is one of the generation
// suffixes we always skip. Matches the grep filters in scripts/ci/lint.sh.
func IsGeneratedFile(path string) (skip bool) {
	if strings.HasSuffix(path, ".out.go") || strings.HasSuffix(path, ".gen.go") {
		skip = true
	}
	return
}
