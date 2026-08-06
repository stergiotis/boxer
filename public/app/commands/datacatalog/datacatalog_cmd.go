// Package datacatalog is the `boxer datacatalog` command: it discovers what a
// live ClickHouse instance holds and writes the answers into `boxer.*` as four
// derived tables (ADR-0170).
//
// One verb, because a catalog run is one thing:
//
//   - `refresh` reads system.tables and system.columns, classifies every table
//     against the leeway naming grammar, relates the leeway ones pairwise,
//     matches the opaque ones against the panel-shape batteries, and replaces
//     the catalog tables whole. `--dry-run` does everything but the writing and
//     prints the DDL plus the row counts it would have produced.
//
// The engine lives in
// [github.com/stergiotis/boxer/public/gov/datacatalog] and the shape vocabulary
// in
// [github.com/stergiotis/boxer/public/gov/datacatalog/panelshapes]; this
// package is only the CLI surface over them.
//
// ADR-0170 §SD6 specified a standalone `apps/datacatalog` binary on the
// jsonbench pattern. It is a subcommand instead: CODINGSTANDARDS § Entry Points
// forbids new `main()`s for utilities, and `boxer capmap` — the same
// shape of tool, from the neighbouring ADR — is the precedent that rule
// produces. jsonbench is a trial artifact and says so; this is not one.
package datacatalog

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/rs/zerolog/log"
	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/gov/datacatalog/panelshapes"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
)

// NewCliCommand wires `boxer datacatalog` and its subcommands.
func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "datacatalog",
		Usage: "discover a ClickHouse instance's tables and write the leeway/opaque catalog into " + datacatalog.DatabaseName,
		Subcommands: []*cli.Command{
			{
				Name:  "refresh",
				Usage: "rebuild " + datacatalog.DatabaseName + ".tables_* from the server's current schema",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "url", Value: chclient.Defaults().URL, Usage: "ClickHouse HTTP endpoint"},
					&cli.StringFlag{Name: "user", Value: chclient.Defaults().User, Usage: "ClickHouse user"},
					&cli.StringFlag{Name: "password", Usage: "ClickHouse password"},
					&cli.StringFlag{Name: "database", Value: "", Usage: "target database for the four catalog tables; empty means " + datacatalog.DatabaseName},
					&cli.BoolFlag{Name: "dry-run", Usage: "print the DDL and the row counts a run would write, and write nothing"},
				},
				Action: actionRefresh,
			},
			{
				Name:   "shapes",
				Usage:  "print the panel-shape battery this build carries (the same rows keelson('panel_shapes') serves)",
				Action: actionShapes,
			},
			{
				Name:   "ddl",
				Usage:  "print the catalog DDL without contacting a server",
				Action: actionDdl,
			},
		},
	}
}

func actionRefresh(c *cli.Context) (err error) {
	client := chclient.New(chclient.Config{
		URL:      c.String("url"),
		User:     c.String("user"),
		Password: c.String("password"),
	}, nil)
	target := datacatalog.TargetDatabase(c.String("database"))
	dryRun := c.Bool("dry-run")
	if dryRun {
		fmt.Print(target.DDLText())
	}
	// The fetcher still runs on a dry run — the point of one is to see what
	// this server would produce, which needs the server.
	res, err := datacatalog.Run(c.Context, datacatalog.NewChFetcher(client), client, target, dryRun, log.Logger)
	if err != nil {
		return
	}
	reportCounts(res, target, dryRun)
	return
}

func reportCounts(res datacatalog.Result, target datacatalog.TargetDatabase, dryRun bool) {
	out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	verb := "wrote"
	if dryRun {
		verb = "would write"
	}
	fmt.Fprintf(out, "run\t%s\t%s\n", res.RunId, res.DiscoveredAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(out, "%s\t%s\t%d\n", verb, target.Qualified(datacatalog.TableCatalog), len(res.Catalog))
	fmt.Fprintf(out, "\t%s\t%d\n", target.Qualified(datacatalog.TableLeeway), len(res.Leeway))
	fmt.Fprintf(out, "\t%s\t%d\n", target.Qualified(datacatalog.TableCompatibility), len(res.Pairs))
	fmt.Fprintf(out, "\t%s\t%d\n", target.Qualified(datacatalog.TableOpaqueShapes), len(res.Shapes))
	_ = out.Flush()
}

func actionShapes(_ *cli.Context) (err error) {
	out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, s := range panelshapes.Shapes() {
		for i, p := range s.Patterns {
			if i == 0 {
				fmt.Fprintf(out, "%s\t%d\t%s\n", s.Name, i, p)
				continue
			}
			fmt.Fprintf(out, "\t%d\t%s\n", i, p)
		}
	}
	_ = out.Flush()
	return
}

func actionDdl(_ *cli.Context) (err error) {
	fmt.Print(datacatalog.DDLText())
	return
}
