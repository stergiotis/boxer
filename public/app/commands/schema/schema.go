// Package schema is `boxer schema`: the stored boxer tables judged against
// the columns this build was generated for, and the mechanical part of
// bringing them in line (ADR-0387). `plan` prints the verdicts and the
// statements; `apply` runs the statements and needs --yes. A shape change
// that is not mechanical is refused with a pointer to doc/migration.
// `roundtrip` checks a deployment's facts insert transform against its read
// relation on clickhouse-local (ADR-0402 §SD6); it reaches no server.
// `load` sends facts rows of another table, or of an Arrow IPC capture,
// through the insert seam, so that transform applies to them.
package schema

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/fs/lading/ladingdata"
	"github.com/stergiotis/boxer/public/fs/lading/ladingmeta"
	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/agentrunstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist/persiststore"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/storage/recordstore"
	"github.com/stergiotis/boxer/public/storage/recordstore/schemaplan"
)

const flagTable = "table"
const flagYes = "yes"

// NewCliCommand returns the `schema` command group.
func NewCliCommand() *cli.Command {
	tableFlag := &cli.StringSliceFlag{
		Name:  flagTable,
		Usage: "only the named tables (database-qualified, as the listing prints them); repeatable",
	}
	return &cli.Command{
		Name:  "schema",
		Usage: "judge the stored boxer tables against this build and plan the mechanical migrations (ADR-0387)",
		Commands: []*cli.Command{
			{
				Name:   "plan",
				Usage:  "print each table's verdict and the statements that bring it in line; changes nothing",
				Flags:  []cli.Flag{tableFlag},
				Action: runPlan,
			},
			{
				Name:  "apply",
				Usage: "run the planned statements; a refused table is left as it is",
				Flags: []cli.Flag{
					tableFlag,
					&cli.BoolFlag{Name: flagYes, Usage: "required: run the statements rather than print them"},
				},
				Action: runApply,
			},
			roundTripCommand(),
			loadCommand(),
		},
	}
}

// Contracts are the stored tables this build knows, in their default
// layouts: the facts table (which the facts-bound stores share), the state
// table, the journal's three, the watchbill pair and the snapshot store's
// three. Where the facts are read from another relation, or stored in a
// deployment's own table, that relation is judged by its columns and gets
// no statement (ADR-0402 §SD4, §SD5).
func Contracts() (cs []recordstore.TableContract, err error) {
	facts, err := chstore.SchemaContracts(chstore.ConfigFromEnv())
	if err != nil {
		return
	}
	lading := ladingschema.Layout{}
	cs = append(cs, facts...)
	cs = append(cs, persiststore.PersistSchemaContract(""))
	cs = append(cs, agentrunstore.Layout{}.SchemaContracts()...)
	cs = append(cs, watchbillstore.Layout{}.SchemaContracts()...)
	cs = append(cs,
		ladingmeta.MetaSchemaContract(lading.MetaTable()),
		ladingdata.DataSchemaContract(lading.DataTable()),
		// The snapshot index decodes through the entry store's columns.
		ladingmeta.MetaSchemaContract(lading.SnapTable()),
	)
	return
}

func selected(cmd *cli.Command) (cs []recordstore.TableContract, err error) {
	all, err := Contracts()
	if err != nil {
		return
	}
	only := cmd.StringSlice(flagTable)
	if len(only) == 0 {
		return all, nil
	}
	for _, c := range all {
		if slices.Contains(only, c.Table) {
			cs = append(cs, c)
		}
	}
	for _, t := range only {
		if !slices.ContainsFunc(cs, func(c recordstore.TableContract) bool { return c.Table == t }) {
			err = eb.Build().Str("table", t).Errorf("schema: no stored table of this build has that name")
			return
		}
	}
	return
}

func plans(ctx context.Context, cmd *cli.Command) (exec recordstore.ExecutorI, ps []schemaplan.Plan, err error) {
	cs, err := selected(cmd)
	if err != nil {
		return
	}
	exec, err = storeexec.ConnectFromEnv(ctx)
	if err != nil {
		return
	}
	for _, c := range cs {
		ps = append(ps, schemaplan.PlanTable(ctx, exec, c))
	}
	return
}

func runPlan(ctx context.Context, cmd *cli.Command) (err error) {
	_, ps, err := plans(ctx, cmd)
	if err != nil {
		return
	}
	Print(cmd.Root().Writer, ps)
	return
}

func runApply(ctx context.Context, cmd *cli.Command) (err error) {
	if !cmd.Bool(flagYes) {
		return eh.Errorf("schema apply changes the tables; pass --yes to run it, or run `schema plan` to see what it would do")
	}
	exec, ps, err := plans(ctx, cmd)
	if err != nil {
		return
	}
	w := cmd.Root().Writer
	Print(w, ps)
	for _, p := range ps {
		if p.IsRefused() || len(p.Statements) == 0 {
			continue
		}
		var n int
		n, err = schemaplan.Apply(ctx, exec, p)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "-- applied %d statement(s) to %s\n", n, p.Verdict.Table)
	}
	return
}

// Print writes the plans as SQL a reader can run by hand: each table's
// verdict and notes as comments, its statements terminated.
func Print(w io.Writer, ps []schemaplan.Plan) {
	for _, p := range ps {
		_, _ = fmt.Fprintf(w, "-- %s\n", p.Verdict.Summary())
		for _, n := range p.Verdict.Notes {
			_, _ = fmt.Fprintf(w, "--   note: %s\n", n)
		}
		for _, r := range p.Refused {
			_, _ = fmt.Fprintf(w, "--   refused: %s\n", r)
		}
		for _, s := range p.Statements {
			_, _ = fmt.Fprintf(w, "%s;\n", s)
		}
	}
}
