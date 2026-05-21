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
	setupZeroLogFQN       = "github.com/stergiotis/boxer/public/observability/logging.SetupZeroLog"
	buildVersionInfoFQN   = "github.com/stergiotis/boxer/public/observability/vcs.BuildVersionInfo"
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
			&cli.BoolFlag{
				Name:  "strict",
				Value: false,
				Usage: "exit non-zero if any entry point fails any of the three checks",
			},
		},
		Action: entryPointsAction,
	}
}

func entryPointsAction(ctx *cli.Context) (err error) {
	root := ctx.String("root")
	strict := ctx.Bool("strict")

	tags := make([]string, 0, 8)
	if t := ctx.String("tags"); t != "" {
		for x := range strings.SplitSeq(t, ",") {
			x = strings.TrimSpace(x)
			if x != "" {
				tags = append(tags, x)
			}
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
	_, _ = fmt.Fprintln(tw, "ENTRY POINT\tCLI/V2\tSETUPZEROLOG\tBUILDVERSIONINFO")

	missCount := uint64(0)
	for _, p := range mains {
		if len(p.Errors) > 0 {
			log.Warn().
				Str("pkg", p.PkgPath).
				Int("errCount", len(p.Errors)).
				Str("firstErr", p.Errors[0].Msg).
				Msg("package has load errors; audit may be incomplete")
		}
		_, cliOK := p.Imports[urfaveCliV2ImportPath]
		zerologOK := packageCallsFunc(p, setupZeroLogFQN)
		vcsOK := packageCallsFunc(p, buildVersionInfoFQN)
		if !cliOK || !zerologOK || !vcsOK {
			missCount++
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.PkgPath, mark(cliOK), mark(zerologOK), mark(vcsOK))
	}
	_ = tw.Flush()

	log.Info().
		Int("mains", len(mains)).
		Uint64("nonConformant", missCount).
		Bool("strict", strict).
		Msg("entry-points audit complete")

	if strict && missCount > 0 {
		err = eb.Build().Uint64("nonConformant", missCount).Errorf("entry-points: one or more entry points fail conformance check")
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

func mark(ok bool) string {
	if ok {
		return "ok"
	}
	return "miss"
}
