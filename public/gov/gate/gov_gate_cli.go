package gate

import (
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "gate",
		Usage:     "run the composite lint gate boxer publishes to consuming repositories (ADR-0179)",
		ArgsUsage: "   (no arguments; see the flags)",
		Description: "Runs " + strings.Join(stepNames(DefaultSteps()), ", ") + ".\n" +
			"gofmt and go vet are deliberately excluded — they must still run on a tree\n" +
			"too broken to build this binary, so they belong in the calling wrapper.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "root",
				Value: ".",
				Usage: "repository root",
			},
			&cli.StringFlag{
				Name:  "tags",
				Value: "",
				Usage: "comma-separated build tags; empty reads them from the tags file",
			},
			&cli.StringFlag{
				Name:  "tags-file",
				Value: "tags",
				Usage: "tag manifest, relative to --root",
			},
			&cli.StringSliceFlag{
				Name:  "doc-root",
				Usage: "tree for doclint to walk (repeatable); empty means --root",
			},
			&cli.StringSliceFlag{
				Name:  "code-pattern",
				Usage: "package pattern for codelint (repeatable); empty means ./public/...",
			},
			&cli.StringFlag{
				Name:  "entry-points-baseline",
				Value: "",
				Usage: "file of grandfathered non-conformant main packages, relative to --root",
			},
			&cli.StringFlag{
				Name:  "naming-baseline",
				Value: "",
				Usage: "file of grandfathered naming violations, relative to --root",
			},
			&cli.StringSliceFlag{
				Name:  "naming-root",
				Usage: "tree the naming rules audit (repeatable); empty means public, apps",
			},
			&cli.StringSliceFlag{
				Name:  "step",
				Usage: "run only this step (repeatable); empty runs all",
			},
			&cli.BoolFlag{
				Name:  "list-steps",
				Value: false,
				Usage: "print the published step list and exit",
			},
		},
		Action: gateAction,
	}
}

func gateAction(ctx *cli.Context) (err error) {
	steps := DefaultSteps()

	if ctx.Bool("list-steps") {
		for _, n := range stepNames(steps) {
			_, _ = os.Stdout.WriteString(n + "\n")
		}
		return
	}

	tags := make([]string, 0, 8)
	if t := ctx.String("tags"); t != "" {
		for x := range strings.SplitSeq(t, ",") {
			x = strings.TrimSpace(x)
			if x != "" {
				tags = append(tags, x)
			}
		}
	}

	want := ctx.StringSlice("step")
	err = ValidateStepNamesE(steps, want)
	if err != nil {
		return
	}

	cfg := Config{
		Root:                ctx.String("root"),
		Tags:                tags,
		TagsFile:            ctx.String("tags-file"),
		DocRoots:            ctx.StringSlice("doc-root"),
		CodePatterns:        ctx.StringSlice("code-pattern"),
		EntryPointsBaseline: ctx.String("entry-points-baseline"),
		NamingBaseline:      ctx.String("naming-baseline"),
		NamingRoots:         ctx.StringSlice("naming-root"),
		Steps:               want,
	}

	rep := Run(ctx.Context, cfg, steps, os.Stdout)
	rep.WriteTrailer(os.Stdout)

	if rep.Failed() {
		err = eb.Build().
			Int("steps", len(rep.Steps)).
			Errorf("gate failed")
	}
	return
}
