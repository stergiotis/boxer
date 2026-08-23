// Package keelsonddl exposes the keelson facts-store setup DDL — the exact
// CREATE DATABASE / CREATE TABLE script chstore.SetupTable applies on first
// run — as a boxer subcommand. It prints the SQL to stdout and never opens a
// ClickHouse connection, so the output can be reviewed or piped into a client:
//
//	app keelsonddl | clickhouse-client -mn
//
// The SQL is composed by chstore.ComposeSetupSQL, the same function
// SetupTable uses, so this command and first-run initialisation cannot drift.
package keelsonddl

import (
	"fmt"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// NewCliCommand returns the top-level `keelsonddl` command.
func NewCliCommand() *cli.Command {
	def := chstore.Defaults()
	return &cli.Command{
		Name:  "keelsonddl",
		Usage: "print the boxer.facts setup DDL keelson applies on first run (stdout; no DB connection)",
		Description: "Emits the exact CREATE DATABASE + CREATE TABLE script chstore.SetupTable executes on first run. " +
			"With no flags the output matches the default first-run initialisation (" + def.Database + "." + def.Table + "). " +
			"Pipe it into a client to apply it, e.g. `app keelsonddl | clickhouse client -mn`.",
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
				Usage: "override the MergeTree engine clause (empty selects the first-run default: time-ordered MergeTree, no TTL/partitioning)",
			},
		},
		Action: func(c *cli.Context) (err error) {
			cfg := chstore.Config{
				Database: c.String("database"),
				Table:    c.String("table"),
			}
			var sql string
			sql, err = chstore.ComposeSetupSQL(cfg, c.String("engine"))
			if err != nil {
				return eh.Errorf("keelsonddl: %w", err)
			}
			if _, err = fmt.Fprint(os.Stdout, sql); err != nil {
				return eh.Errorf("keelsonddl: write stdout: %w", err)
			}
			return nil
		},
	}
}
