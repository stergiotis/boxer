package contracts

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	cli "github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// NewCliCommand returns `gov contracts`: `snapshot` writes the tree's
// contracts to a directory, `diff` compares the tree against one (ADR-0388).
func NewCliCommand() (cmd *cli.Command) {
	rootFlag := &cli.StringFlag{
		Name:  "root",
		Usage: "repository root the goldens and constants are read under",
		Value: ".",
	}
	cmd = &cli.Command{
		Name:  "contracts",
		Usage: "snapshot the tree's stored, wire and command-line contracts, and compare against a committed snapshot (ADR-0388)",
		Commands: []*cli.Command{
			{
				Name:  "snapshot",
				Usage: "write the tree's contracts to a new directory",
				Flags: []cli.Flag{
					rootFlag,
					&cli.StringFlag{Name: "out", Usage: "directory to write; must not exist", Required: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) (err error) {
					out := cmd.String("out")
					if _, serr := os.Stat(out); serr == nil {
						return eb.Build().Str("out", out).Errorf("the snapshot directory exists; remove it or name another")
					}
					snap, err := Collect(cmd.String("root"), cmd.Root())
					if err != nil {
						return
					}
					return snap.Write(out)
				},
			},
			{
				Name:      "diff",
				Usage:     "compare the tree against a snapshot; fails on a non-additive change no migration note acknowledges",
				ArgsUsage: "<snapshot dir>",
				Flags: []cli.Flag{
					rootFlag,
					&cli.StringFlag{
						Name:  "migrations",
						Usage: "directory of migration notes whose `contract: <kind>/<item>` lines acknowledge a non-additive change",
						Value: filepath.Join("doc", "migration"),
					},
				},
				Action: runDiff,
			},
		},
	}
	return
}

func runDiff(ctx context.Context, cmd *cli.Command) (err error) {
	if cmd.Args().Len() != 1 {
		return eb.Build().Errorf("diff takes one argument: the snapshot directory")
	}
	baseline, err := Load(cmd.Args().First())
	if err != nil {
		return
	}
	tree, err := Collect(cmd.String("root"), cmd.Root())
	if err != nil {
		return
	}
	changes := Diff(baseline, tree)
	acks, err := Acks(cmd.String("migrations"))
	if err != nil {
		return
	}
	Acknowledge(changes, acks)
	var b strings.Builder
	for _, c := range changes {
		verdict := "additive    "
		switch {
		case !c.Additive && c.AckedBy != "":
			verdict = "acknowledged"
		case !c.Additive:
			verdict = "NOT ADDITIVE"
		}
		b.WriteString(verdict + "  " + c.Key() + "  " + c.What + ": " + c.Why)
		if c.AckedBy != "" {
			b.WriteString(" (" + c.AckedBy + ")")
		}
		b.WriteString("\n")
	}
	if len(changes) == 0 {
		b.WriteString("no contract changed\n")
	}
	_, _ = os.Stdout.WriteString(b.String())
	if n := Unacknowledged(changes); n > 0 {
		err = eb.Build().Int("unacknowledged", n).Errorf("non-additive contract changes without a migration note naming them (`contract: <kind>/<item>`)")
	}
	return
}
