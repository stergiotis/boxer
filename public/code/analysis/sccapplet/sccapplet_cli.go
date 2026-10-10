package sccapplet

import (
	"context"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// NewCliCommand is `boxer code analysis sccapplet`.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "sccapplet",
		Usage: "write a repository's code volume and complexity, as scc counts them, into a SQL applet a browser tab answers without a database (ADR-0299)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Value: ".", Usage: "a directory inside the git worktree to scan"},
			&cli.StringFlag{Name: "out", Required: true, Usage: "the applet document to write; its base name is the applet's slug"},
			&cli.IntFlag{Name: "depth", Usage: "fold directories deeper than this many levels into their ancestor; 0 folds nothing"},
			&cli.StringFlag{Name: "revision", Usage: "the commit the worktree is at, stated in the document; empty states none"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			groups, err := Scan(ctx, cmd.String("dir"))
			if err != nil {
				return
			}
			doc, st, err := Compose(groups, Options{Depth: int(cmd.Int("depth")), Revision: cmd.String("revision")})
			if err != nil {
				return
			}
			if err = os.WriteFile(cmd.String("out"), doc, 0o644); err != nil {
				return eh.Errorf("sccapplet: write: %w", err)
			}
			log.Info().Str("out", cmd.String("out")).Int("dirs", st.Dirs).Int("files", st.Files).
				Int64("code", st.Code).Int("bytes", len(doc)).Msg("sccapplet: written")
			return
		},
	}
}
