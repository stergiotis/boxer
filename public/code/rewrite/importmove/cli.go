package importmove

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// NewCliCommand is `boxer code importmove`.
func NewCliCommand() (cmd *cli.Command) {
	return &cli.Command{
		Name:  "importmove",
		Usage: "rewrite Go import paths after packages moved; report string literals, go:generate lines and templates naming a moved path",
		Description: "Each --map old=new moves old and every path below it at a path-segment boundary; the longest matching old wins. " +
			"--keep names a path under a moved prefix that did not move. An unaliased import whose last segment changes gains the old name as an alias. " +
			"No import is added or removed; run goimports afterwards if qualifiers changed by hand.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "root", Value: ".", Usage: "directory to walk"},
			&cli.StringSliceFlag{Name: "map", Usage: "old=new import path move (repeatable)"},
			&cli.StringSliceFlag{Name: "keep", Usage: "import path under a moved prefix that stays (repeatable)"},
			&cli.BoolFlag{Name: "dryRun", Usage: "report what would change without writing"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			var plan Plan
			for _, kv := range cmd.StringSlice("map") {
				from, to, ok := strings.Cut(kv, "=")
				if !ok {
					return eb.Build().Str("map", kv).Errorf("a mapping is old=new")
				}
				plan.Mappings = append(plan.Mappings, Mapping{From: strings.TrimSpace(from), To: strings.TrimSpace(to)})
			}
			plan.Keep = cmd.StringSlice("keep")
			results, err := plan.Run(cmd.String("root"), !cmd.Bool("dryRun"))
			if err != nil {
				return
			}
			w := cmd.Root().Writer
			for _, r := range results {
				if r.Unparsed != nil {
					_, _ = fmt.Fprintf(w, "%s: not valid Go, left as it is: %v\n", r.Path, r.Unparsed)
				}
				if r.Rewrites > 0 {
					_, _ = fmt.Fprintf(w, "%s: %d import(s) rewritten\n", r.Path, r.Rewrites)
				}
				for _, a := range r.Aliased {
					_, _ = fmt.Fprintf(w, "%s: aliased %s to its old name\n", r.Path, a)
				}
				for _, f := range r.Findings {
					_, _ = fmt.Fprintf(w, "%s:%d: %s names %s (moves to %s); not rewritten\n", f.Path, f.Line, f.Kind, f.Moved, f.Target)
				}
			}
			return
		},
	}
}
