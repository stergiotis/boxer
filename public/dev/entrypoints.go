//go:build llm_generated_opus47

package dev

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
	"golang.org/x/tools/go/packages"
)

const (
	urfaveCliV2ImportPath = "github.com/urfave/cli/v2"
	// loggingApplyFQN is referenced by every main that wires
	// `Before: logging.Apply` on its cli.App. The logging package
	// installs eh.MarshalError via init(), so no explicit
	// setup-call is needed; what mains must do instead is hook
	// Apply into the cli lifecycle so the writer/level/startup
	// record actually engage.
	loggingApplyFQN     = "github.com/stergiotis/boxer/public/observability/logging.Apply"
	buildVersionInfoFQN = "github.com/stergiotis/boxer/public/observability/vcs.BuildVersionInfo"
)

func newEntryPointsSubcommand() *cli.Command {
	return &cli.Command{
		Name:  "entry-points",
		Usage: "audit package main entry points against CODINGSTANDARDS.md 'Entry Points'",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "root",
				Value: ".",
				Usage: "directory to load packages from (passed as packages.Config.Dir)",
			},
			&cli.StringFlag{
				Name:  "tags",
				Value: "",
				Usage: "comma-separated build tags forwarded to packages.Load",
			},
			&cli.StringFlag{
				Name:  "baseline",
				Value: "",
				Usage: "path to a baseline file (one grandfathered package import path per line; # comments and blank lines allowed); listed packages are reported but do not trigger --strict failure",
			},
			&cli.BoolFlag{
				Name:  "strict",
				Value: false,
				Usage: "exit non-zero if any entry point fails any of the three checks (after baseline subtraction)",
			},
		},
		Action: entryPointsAction,
	}
}

func entryPointsAction(ctx *cli.Context) (err error) {
	root := ctx.String("root")
	strict := ctx.Bool("strict")
	baselinePath := ctx.String("baseline")

	tags := make([]string, 0, 8)
	if t := ctx.String("tags"); t != "" {
		for x := range strings.SplitSeq(t, ",") {
			x = strings.TrimSpace(x)
			if x != "" {
				tags = append(tags, x)
			}
		}
	}

	baseline := make(map[string]struct{}, 0)
	if baselinePath != "" {
		baseline, err = loadBaseline(baselinePath)
		if err != nil {
			return
		}
	}

	pcfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedSyntax |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedImports,
		Context: ctx.Context,
		Dir:     root,
	}
	if len(tags) > 0 {
		pcfg.BuildFlags = []string{"-tags=" + strings.Join(tags, ",")}
	}

	var pkgs []*packages.Package
	pkgs, err = packages.Load(pcfg, "./...")
	if err != nil {
		err = eb.Build().Str("root", root).Strs("tags", tags).Errorf("unable to load packages: %w", err)
		return
	}

	mains := make([]*packages.Package, 0, len(pkgs))
	for _, p := range pkgs {
		if p.Name == "main" {
			mains = append(mains, p)
		}
	}
	sort.Slice(mains, func(i, j int) bool { return mains[i].PkgPath < mains[j].PkgPath })

	if len(mains) == 0 {
		log.Info().Str("root", root).Strs("tags", tags).Msg("no main packages discovered")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ENTRY POINT\tCLI/V2\tLOGGING.APPLY\tBUILDVERSIONINFO\tSTATUS")

	failCount := uint64(0)
	baselinedCount := uint64(0)
	for _, p := range mains {
		if len(p.Errors) > 0 {
			log.Warn().
				Str("pkg", p.PkgPath).
				Int("errCount", len(p.Errors)).
				Str("firstErr", p.Errors[0].Msg).
				Msg("package has load errors; audit may be incomplete")
		}
		_, cliOK := p.Imports[urfaveCliV2ImportPath]
		loggingOK := packageReferencesFunc(p, loggingApplyFQN)
		vcsOK := packageCallsFunc(p, buildVersionInfoFQN)
		conformant := cliOK && loggingOK && vcsOK
		_, isBaselined := baseline[p.PkgPath]
		var status string
		switch {
		case conformant:
			status = "ok"
		case isBaselined:
			status = "baselined"
			baselinedCount++
		default:
			status = "fail"
			failCount++
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.PkgPath, mark(cliOK), mark(loggingOK), mark(vcsOK), status)
	}
	_ = tw.Flush()

	log.Info().
		Int("mains", len(mains)).
		Uint64("fail", failCount).
		Uint64("baselined", baselinedCount).
		Bool("strict", strict).
		Msg("entry-points audit complete")

	if strict && failCount > 0 {
		err = eb.Build().Uint64("fail", failCount).Uint64("baselined", baselinedCount).Errorf("entry-points: one or more entry points fail conformance check (not in baseline)")
	}
	return
}

// loadBaseline reads a list of grandfathered package import paths from
// path. Each non-empty, non-comment line is a fully-qualified package
// import path. '#' starts a line comment. Whitespace is trimmed. Unknown
// paths in the file are not an error here; the entry-points action
// merely looks them up by string equality against discovered mains.
func loadBaseline(path string) (out map[string]struct{}, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("read baseline: %w", err)
		return
	}
	out = make(map[string]struct{}, 16)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = struct{}{}
	}
	return
}

func packageCallsFunc(p *packages.Package, fullName string) (found bool) {
	if p == nil || p.TypesInfo == nil {
		return
	}
	for _, file := range p.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var ident *ast.Ident
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr:
				ident = fn.Sel
			case *ast.Ident:
				ident = fn
			default:
				return true
			}
			tf, ok := p.TypesInfo.Uses[ident].(*types.Func)
			if !ok {
				return true
			}
			if tf.FullName() == fullName {
				found = true
				return false
			}
			return true
		})
		if found {
			return
		}
	}
	return
}

// packageReferencesFunc reports whether p contains any reference to the
// function identified by fullName. Unlike packageCallsFunc this matches
// value uses (e.g. `Before: logging.Apply` — assigning the function
// value to a struct field, not invoking it), which is the shape mains
// must use to hook logging.Apply into cli.App.Before.
func packageReferencesFunc(p *packages.Package, fullName string) (found bool) {
	if p == nil || p.TypesInfo == nil {
		return
	}
	for ident, obj := range p.TypesInfo.Uses {
		if found {
			break
		}
		tf, ok := obj.(*types.Func)
		if !ok {
			continue
		}
		if tf.FullName() == fullName {
			found = true
			_ = ident
		}
	}
	return
}

func mark(ok bool) string {
	if ok {
		return "ok"
	}
	return "miss"
}
