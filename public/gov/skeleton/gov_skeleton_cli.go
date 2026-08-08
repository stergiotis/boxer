package skeleton

import (
	"fmt"
	"os"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "skeleton",
		Usage:     "emit and reconcile the repository mechanics a boxer consumer would otherwise copy (ADR-0179)",
		ArgsUsage: "   (no arguments; see the flags)",
		Description: "Default mode is --check: report generated files that have drifted from what\n" +
			"this boxer would emit. --write materialises them; files the consumer owns\n" +
			"(AGENTS.md, tags, the entry point) are seeded only when absent and are never\n" +
			"overwritten. Repository-local lint steps belong in scripts/ci/lint-local.sh,\n" +
			"which is never generated or checked.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "root",
				Value: ".",
				Usage: "repository root",
			},
			&cli.BoolFlag{
				Name:  "write",
				Value: false,
				Usage: "write the skeleton instead of checking it",
			},
			&cli.StringFlag{
				Name:  "module",
				Value: "",
				Usage: "module path; empty reads it from go.mod",
			},
			&cli.StringFlag{
				Name:  "name",
				Value: "",
				Usage: "repository short name used for the launcher; empty derives it from the module path",
			},
			&cli.StringFlag{
				Name:  "app-package",
				Value: "",
				Usage: "import-relative path of the entry point; empty means ./public/app",
			},
			&cli.BoolFlag{
				Name:  "list",
				Value: false,
				Usage: "print the published skeleton and who owns each member",
			},
		},
		Action: skeletonAction,
	}
}

func skeletonAction(ctx *cli.Context) (err error) {
	files := DefaultFiles()
	root := ctx.String("root")

	var p Params
	p, err = DeriveParamsE(root)
	if err != nil {
		return
	}
	if v := ctx.String("module"); v != "" {
		p.Module = v
	}
	if v := ctx.String("name"); v != "" {
		p.Name = v
	}
	if v := ctx.String("app-package"); v != "" {
		p.AppPackage = v
	}

	if ctx.Bool("list") {
		for _, f := range files {
			var rel string
			rel, _, err = RenderE(f, p)
			if err != nil {
				return
			}
			fmt.Printf("  %-10s  %s\n", f.Ownership.String(), rel)
		}
		return
	}

	if ctx.Bool("write") {
		var written []string
		written, err = WriteE(root, files, p)
		if err != nil {
			return
		}
		for _, w := range written {
			fmt.Printf("wrote %s\n", w)
		}
		if len(written) == 0 {
			fmt.Println("nothing to write; skeleton is already present")
		}
		return
	}

	var results []Result
	results, err = CheckE(root, files, p)
	if err != nil {
		return
	}

	var blocking uint32
	for _, r := range results {
		switch {
		case r.Status == StatusDrift:
			fmt.Printf("%s: drift at line %d\n", r.Path, r.FirstDiffLine)
			fmt.Printf("  want: %s\n", r.Want)
			fmt.Printf("  got:  %s\n", r.Got)
		case r.Status == StatusAbsent && r.Ownership == OwnershipGenerated:
			fmt.Printf("%s: absent\n", r.Path)
		case r.Status == StatusAbsent:
			fmt.Printf("%s: absent (yours to write; --write seeds a starter)\n", r.Path)
		}
		if r.Blocking() {
			blocking++
		}
	}

	if blocking > 0 {
		fmt.Println("run `gov skeleton --write` to reconcile; put local lint steps in scripts/ci/lint-local.sh")
		err = eb.Build().Uint32("files", blocking).Errorf("skeleton has drifted")
		return
	}
	_, _ = os.Stdout.WriteString("skeleton: ok\n")
	return
}
