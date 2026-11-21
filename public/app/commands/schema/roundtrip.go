package schema

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore/chexec"
)

const (
	flagSetup     = "setup"
	flagReadTable = "read-table"
	flagDir       = "dir"
)

// roundTripCommand is `schema roundtrip` (ADR-0402 §SD6): the insert
// transform this binary registered for boxer.facts and the deployment's read
// relation, checked against each other on clickhouse-local in a scratch
// directory. It reaches no server.
func roundTripCommand() *cli.Command {
	return &cli.Command{
		Name:  "roundtrip",
		Usage: "check that the registered facts insert transform and the read relation agree, on clickhouse-local in a scratch directory (no server)",
		Description: "Runs the --setup script, which creates the transform's target table and the read relation as the deployment defines them, " +
			"writes one row of every kind of the runtime vocabulary through the transform this binary registered, reads the rows back through the read relation " +
			"and compares every facts column by row id. Needs the clickhouse binary; it never contacts a server.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagSetup, Required: true, Usage: "SQL script creating the transform's target table and the read relation (statements separated by ';')"},
			&cli.StringFlag{Name: flagReadTable, Usage: "the database-qualified read relation; empty takes BOXER_FACTS_READ_TABLE, else boxer.facts"},
			&cli.StringFlag{Name: flagDir, Usage: "the scratch directory clickhouse-local keeps its state in, kept afterwards; empty uses a temporary one, removed afterwards"},
		},
		Action: runRoundTrip,
	}
}

func runRoundTrip(ctx context.Context, cmd *cli.Command) (err error) {
	script, err := os.ReadFile(cmd.String(flagSetup))
	if err != nil {
		return eb.Build().Str("setup", cmd.String(flagSetup)).Errorf("schema roundtrip: read the setup script: %w", err)
	}
	dir := cmd.String(flagDir)
	if dir == "" {
		if dir, err = os.MkdirTemp("", "boxer-roundtrip-"); err != nil {
			return eh.Errorf("schema roundtrip: scratch directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
	}
	exec, err := chexec.NewLocalExecutor(dir, nil)
	if err != nil {
		return eh.Errorf("schema roundtrip: %w", err)
	}
	if err = exec.Exec(ctx, string(script)); err != nil {
		return eh.Errorf("schema roundtrip: run the setup script: %w", err)
	}
	report, err := chstore.RoundTrip(ctx, exec, chstore.RoundTripOptions{ReadTable: cmd.String(flagReadTable)})
	if err != nil {
		return
	}
	PrintRoundTrip(cmd.Root().Writer, report)
	if !report.Ok() {
		err = eb.Build().Int("mismatches", len(report.Mismatches)).Str("columns", report.Columns.Summary()).
			Errorf("schema roundtrip: the transform and the read relation do not agree")
	}
	return
}

// PrintRoundTrip writes a round trip's report for an operator.
func PrintRoundTrip(w io.Writer, r chstore.RoundTripReport) {
	_, _ = fmt.Fprintf(w, "kinds: %d; rows written: %d, read back: %d\n", len(r.Kinds), r.Written, r.Read)
	_, _ = fmt.Fprintf(w, "written to: %s; read through: %s\n", r.Target, r.ReadTable)
	_, _ = fmt.Fprintf(w, "columns: %s\n", r.Columns.Summary())
	for _, m := range r.Columns.Missing {
		_, _ = fmt.Fprintf(w, "  missing: %s\n", m)
	}
	for _, reason := range r.Columns.Reasons {
		_, _ = fmt.Fprintf(w, "  %s\n", reason)
	}
	for _, m := range r.Mismatches {
		_, _ = fmt.Fprintf(w, "  differs: %s (%d rows, counted from both sides)\n", m.Column, m.Rows)
	}
	if r.Ok() {
		_, _ = fmt.Fprintln(w, "ok: every row and every column survived the round trip")
	}
}
