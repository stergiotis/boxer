package schema

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	flagFromTable  = "from-table"
	flagFromStream = "from-stream"
	flagSince      = "since"
	flagUntil      = "until"
	flagBlockRows  = "block-rows"
	flagAppend     = "append"
	flagYes        = "yes"
)

// loadCommand is `schema load`: facts-shaped rows of another relation, or
// of an Arrow IPC capture, sent through the insert seam into the facts
// table, so that the insert transform this binary registered applies to
// them (ADR-0402).
func loadCommand() *cli.Command {
	return &cli.Command{
		Name:  "load",
		Usage: "send facts rows of another table, or of an Arrow IPC capture, through the insert seam into the facts table",
		Description: "Reads rows in the facts columns from --from-table (a facts table moved aside) or --from-stream (an Arrow IPC stream of facts batches) " +
			"and inserts them into the facts table through the record-store executor, so the insert transform this binary registered applies to them as to any write. " +
			"The rows keep their ids. A load is not idempotent: it refuses when the facts already hold rows in the window unless --append is given, " +
			"and afterwards checks that the read relation shows the rows it inserted. Load a large table in parts with --since and --until.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagFromTable, Usage: "the database-qualified relation to read facts rows from"},
			&cli.StringFlag{Name: flagFromStream, Usage: "an Arrow IPC stream file of facts batches to read"},
			&cli.TimestampFlag{Name: flagSince, Usage: "with --from-table: only rows at or after this time (RFC 3339)", Config: cli.TimestampConfig{Layouts: []string{time.RFC3339}}},
			&cli.TimestampFlag{Name: flagUntil, Usage: "with --from-table: only rows before this time (RFC 3339)", Config: cli.TimestampConfig{Layouts: []string{time.RFC3339}}},
			&cli.IntFlag{Name: flagBlockRows, Usage: "with --from-table: rows per insert; 0 takes the default"},
			&cli.BoolFlag{Name: flagAppend, Usage: "load although the facts already hold rows in the window"},
			&cli.BoolFlag{Name: flagYes, Usage: "required: write the rows"},
		},
		Action: runLoad,
	}
}

func runLoad(ctx context.Context, cmd *cli.Command) (err error) {
	if !cmd.Bool(flagYes) {
		return eh.Errorf("schema load writes facts; pass --yes to run it")
	}
	opts := chstore.LoadOptions{
		FromTable: cmd.String(flagFromTable),
		Since:     cmd.Timestamp(flagSince),
		Until:     cmd.Timestamp(flagUntil),
		BlockRows: int(cmd.Int(flagBlockRows)),
		Append:    cmd.Bool(flagAppend),
	}
	if path := cmd.String(flagFromStream); path != "" {
		var f *os.File
		if f, err = os.Open(path); err != nil {
			return eb.Build().Str("stream", path).Errorf("schema load: open the stream: %w", err)
		}
		defer func() { _ = f.Close() }()
		opts.FromStream = f
	}
	exec, err := storeexec.ConnectFromEnv(ctx)
	if err != nil {
		return
	}
	cfg := chstore.ConfigFromEnv()
	report, err := chstore.LoadFacts(ctx, exec, cfg, opts)
	PrintLoad(cmd.Root().Writer, report, chstore.WriteTransformed(cfg))
	return
}

// PrintLoad writes a load's report for an operator.
func PrintLoad(w io.Writer, r chstore.LoadReport, transformed bool) {
	through := ""
	if transformed {
		through = " (through the registered insert transform)"
	}
	_, _ = fmt.Fprintf(w, "written to: %s%s; read through: %s\n", r.WrittenTo, through, r.ReadThrough)
	_, _ = fmt.Fprintf(w, "rows inserted: %d in %d inserts\n", r.Rows, r.Batches)
	_, _ = fmt.Fprintf(w, "rows in the window: %d before, %d after\n", r.Before, r.After)
}
