package releasenotes

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// NewCliCommand is `boxer gov releasenotes`.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "releasenotes",
		Usage: "Compile release notes for a commit range: breaking commits, removed packages, toolchain and dependency floors, contract snapshots, migration notes",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "from", Required: true, Usage: "start of the range (exclusive), e.g. the previous tag"},
			&cli.StringFlag{Name: "to", Value: "HEAD", Usage: "end of the range (inclusive)"},
			&cli.StringFlag{Name: "dir", Value: ".", Usage: "repository root"},
			&cli.StringFlag{Name: "out", Usage: "output file, relative to --dir; empty writes to stdout"},
			&cli.StringFlag{Name: "apidiff", Usage: "a file holding scripts/ci/apidiff.sh output for the same range, included verbatim"},
		},
		Action: releaseNotesAction,
	}
}

func releaseNotesAction(ctx context.Context, cmd *cli.Command) (err error) {
	dir := cmd.String("dir")
	var n Notes
	if n, err = Collect(ctx, dir, cmd.String("from"), cmd.String("to")); err != nil {
		return
	}
	var apidiff []byte
	if p := cmd.String("apidiff"); p != "" {
		if apidiff, err = os.ReadFile(p); err != nil {
			return eb.Build().Str("apidiff", p).Errorf("releasenotes: read apidiff report: %w", err)
		}
	}
	out := cmd.String("out")
	src := Render(n, filepath.ToSlash(filepath.Dir(out)), string(apidiff))
	if out == "" {
		_, err = os.Stdout.Write(src)
		return
	}
	full := filepath.Join(dir, out)
	if err = os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return eb.Build().Str("out", full).Errorf("releasenotes: mkdir: %w", err)
	}
	if err = os.WriteFile(full, src, 0o644); err != nil {
		return eb.Build().Str("out", full).Errorf("releasenotes: write: %w", err)
	}
	log.Info().Str("out", full).Int("commits", n.NumCommits).Int("breaking", len(n.Breaking)).Msg("release notes written")
	return
}
