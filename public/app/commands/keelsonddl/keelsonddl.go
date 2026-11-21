// Package keelsonddl exposes the keelson facts-store setup DDL — the exact
// CREATE DATABASE / CREATE TABLE script chstore.SetupTable applies on first
// run — as a boxer subcommand. It prints the SQL to stdout and never opens a
// ClickHouse connection, so the output can be reviewed or piped into a client:
//
//	app keelsonddl | clickhouse-client -mn
//
// The SQL is composed by chstore.ComposeSetupSQL, the same function
// SetupTable uses, so this command and first-run initialisation cannot drift.
//
// # The trail views, opt-in
//
// --trail-views appends the read-only views over the audit trail
// (trailviews): the functions they and their readers call, then one flat
// view per trail kind, a timeline, the agentic digests, the audit entities
// and the review marks. They are not part of first-run setup — SetupTable never creates
// them — so they are printed only when asked for, after the facts DDL, which
// stays byte-identical as a prefix.
//
// The views inline leeway's SQL read surface when they are created, so the
// surface must be on the server first. --with-surface prints it between the
// facts DDL and the views (the same statements `leeway sqlsurface print`
// emits, marker included), which makes one pipe sufficient on a fresh
// server:
//
//	app keelsonddl --trail-views --with-surface | clickhouse-client -mn
//
// The views read the facts table, or the read table --read-table or
// BOXER_FACTS_READ_TABLE names (ADR-0402 §SD3), and are created in
// --database either way.
//
// Re-running the views half after a surface upgrade is what refreshes them:
// every view is CREATE OR REPLACE and stamped in its COMMENT with the
// revisions it was built from (trailviews.Stamp).
package keelsonddl

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// options is what one invocation prints.
type options struct {
	cfg          chstore.Config
	engineClause string
	// readTable is the relation the trail views read (ADR-0402 §SD3);
	// empty reads cfg's table.
	readTable   string
	trailViews  bool
	withSurface bool
}

// compose renders the script: the facts DDL, then — when asked — the read
// surface and the trail views, in the order a server needs them.
func compose(opts options) (sql string, err error) {
	if opts.withSurface && !opts.trailViews {
		return "", eh.Errorf("keelsonddl: --with-surface only accompanies --trail-views; `leeway sqlsurface print` prints the surface alone")
	}
	sql, err = chstore.ComposeSetupSQL(opts.cfg, opts.engineClause)
	if err != nil {
		return "", eh.Errorf("keelsonddl: %w", err)
	}
	if !opts.trailViews {
		return sql, nil
	}
	b := strings.Builder{}
	b.WriteString(sql)
	if !strings.HasSuffix(sql, "\n") {
		b.WriteString("\n")
	}
	if opts.withSurface {
		b.WriteString("\n-- leeway SQL read surface v" + strconv.Itoa(lwsqlsurface.Version) + ": the functions the trail views inline\n")
		for _, stmt := range lwsqlsurface.AllStatements("") {
			b.WriteString(stmt)
			b.WriteString(";\n")
		}
	}
	var views []string
	source := opts.readTable
	if source == "" {
		source = opts.cfg.Database + "." + opts.cfg.Table
	}
	views, err = trailviews.StatementsFrom(opts.cfg.Database, source)
	if err != nil {
		return "", eh.Errorf("keelsonddl: trail views: %w", err)
	}
	b.WriteString("\n-- " + string(trailviews.Stamp()) + ": not part of first-run setup; needs the read surface installed\n")
	for _, stmt := range views {
		b.WriteString(stmt)
		b.WriteString(";\n\n")
	}
	return b.String(), nil
}

// NewCliCommand returns the top-level `keelsonddl` command.
func NewCliCommand() *cli.Command {
	def := chstore.Defaults()
	return &cli.Command{
		Name:  "keelsonddl",
		Usage: "print the boxer.facts setup DDL keelson applies on first run, optionally with the trail views (stdout; no DB connection)",
		Description: "Emits the exact CREATE DATABASE + CREATE TABLE script chstore.SetupTable executes on first run. " +
			"With no flags the output matches the default first-run initialisation (" + def.Database + "." + def.Table + "). " +
			"Pipe it into a client to apply it, e.g. `app keelsonddl | clickhouse client -mn`. " +
			"--trail-views appends the audit-trail views (not part of first-run setup); they need leeway's SQL read surface " +
			"on the server first, which --with-surface prints ahead of them.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "database",
				Value: def.Database,
				Usage: "target database name",
			},
			&cli.StringFlag{
				Name:  "table",
				Value: def.Table,
				Usage: "target table name",
			},
			&cli.StringFlag{
				Name:  "engine",
				Value: "",
				Usage: "override the engine clause, without SETTINGS (empty selects BOXER_FACTS_ENGINE, by default time-sorted and partitioned by month, no TTL)",
			},
			&cli.StringFlag{
				Name:  "read-table",
				Usage: "with --trail-views: the database-qualified relation the views read (ADR-0402); empty takes BOXER_FACTS_READ_TABLE for the facts table, else database.table",
			},
			&cli.BoolFlag{
				Name:  "trail-views",
				Usage: "append the audit-trail functions and the read-only views over database.table (CREATE OR REPLACE, stamped); not part of first-run setup",
			},
			&cli.BoolFlag{
				Name:  "with-surface",
				Usage: "with --trail-views: print leeway's SQL read surface before the views, which inline it at CREATE time",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) (err error) {
			database, table := cmd.String("database"), cmd.String("table")
			readTable := cmd.String("read-table")
			if readTable == "" {
				if readTable, err = factsschema.ReadTableFor(database + "." + table); err != nil {
					return eh.Errorf("keelsonddl: %w", err)
				}
			}
			var sql string
			sql, err = compose(options{
				cfg: chstore.Config{
					Database: database,
					Table:    table,
				},
				readTable:    readTable,
				engineClause: cmd.String("engine"),
				trailViews:   cmd.Bool("trail-views"),
				withSurface:  cmd.Bool("with-surface"),
			})
			if err != nil {
				return err
			}
			if _, err = fmt.Fprint(os.Stdout, sql); err != nil {
				return eh.Errorf("keelsonddl: write stdout: %w", err)
			}
			return nil
		},
	}
}
