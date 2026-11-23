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
// # The id tables, opt-in
//
// First-run setup also provisions two small tables beside the facts: the id
// lease table the store mints row ids through (ADR-0397 §SD2) and the id
// directory the keyed generators resolve natural keys in (ADR-0405 §SD3).
// --with-id-tables prints both after the facts DDL, for a server a host does
// not set up itself — one prepared by an operator, or a test server. Without
// the flag the output stays the facts DDL alone.
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
// --database either way. Under an insert transform for the table the facts
// are stored in a deployment's own table and there is no setup DDL: the
// command refuses without --trail-views, and with it prints the database
// and the views alone.
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

	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowalloc"
	"github.com/stergiotis/boxer/public/identity/identgen/leased/rowintern"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail/trailviews"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/storage/recordstore"
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
	// withIdTables also prints the id lease table and the id directory
	// first-run setup provisions beside the facts.
	withIdTables bool
}

// compose renders the script: the facts DDL, then — when asked — the read
// surface and the trail views, in the order a server needs them.
func compose(opts options) (sql string, err error) {
	if opts.withSurface && !opts.trailViews {
		return "", eh.Errorf("keelsonddl: --with-surface only accompanies --trail-views; `leeway sqlsurface print` prints the surface alone")
	}
	if chstore.WriteTransformed(opts.cfg) {
		// The facts are stored in the deployment's own table (ADR-0402
		// §SD5): there is no facts DDL to print, only the views over the
		// read table.
		if !opts.trailViews {
			return "", eb.Build().Str("table", opts.cfg.Database+"."+opts.cfg.Table).Errorf("keelsonddl: an insert transform is registered for the table, so its storage is the deployment's and there is no setup DDL; --trail-views prints the views over the read table: %w", recordstore.ErrTransformedTable)
		}
		sql = "-- " + opts.cfg.Database + "." + opts.cfg.Table + ": stored in the deployment's own table under an insert transform; no setup DDL (ADR-0402)\nCREATE DATABASE IF NOT EXISTS " + opts.cfg.Database + ";\n"
	} else {
		sql, err = chstore.ComposeSetupSQL(opts.cfg, opts.engineClause)
		if err != nil {
			return "", eh.Errorf("keelsonddl: %w", err)
		}
	}
	if opts.withIdTables {
		ids := strings.Builder{}
		ids.WriteString(sql)
		if !strings.HasSuffix(sql, "\n") {
			ids.WriteString("\n")
		}
		ids.WriteString("\n-- the id lease table and the id directory first-run setup provisions beside the facts (ADR-0397, ADR-0405)\n")
		for _, stmt := range rowalloc.ProvisionSQL(rowalloc.Layout{Database: opts.cfg.Database}) {
			ids.WriteString(stmt)
			ids.WriteString(";\n")
		}
		for _, stmt := range rowintern.ProvisionSQL(rowintern.Layout{Database: opts.cfg.Database}) {
			ids.WriteString(stmt)
			ids.WriteString(";\n")
		}
		sql = ids.String()
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
			"--with-id-tables appends the id lease table and the id directory first-run setup provisions beside it. " +
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
				Name:  "with-id-tables",
				Usage: "append the id lease table and the id directory that first-run setup provisions in the database, for a server no host sets up",
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
				withIdTables: cmd.Bool("with-id-tables"),
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
