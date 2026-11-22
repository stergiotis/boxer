// Package schema is `boxer schema`: the stored tables of this build judged
// against the columns it was generated for, and the mechanical part of
// bringing them in line (ADR-0387). The tables are boxer's own and the ones
// the linked packages declare (recordstore.DeclareStoredTables); --owner
// and --table select among them. `plan` prints the verdicts and the
// statements and changes nothing; `check` does the same and exits non-zero
// unless every table is in line; `apply` runs the kinds of statement it is
// told to, --create for a table that does not exist and --alter for one
// that does, and then answers as `check` does. A shape change that is not
// mechanical is refused with a pointer to doc/migration.
// `roundtrip` checks a deployment's facts insert transform against its read
// relation on clickhouse-local (ADR-0402 §SD6); it reaches no server.
// `load` sends facts rows of another table, or of an Arrow IPC capture,
// through the insert seam, so that transform applies to them.
package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

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
const flagOwner = "owner"
const flagJSON = "json"
const flagCreate = "create"
const flagAlter = "alter"

// Exit codes of `schema check` and `schema apply` when a table is not in
// line; 1 stays the code of a command that could not run.
const (
	// ExitPending: statements `schema apply` runs would bring every table
	// in line.
	ExitPending = 3
	// ExitBlocked: a table needs a change no statement is offered for.
	ExitBlocked = 4
	// ExitUnchecked: a table could not be read.
	ExitUnchecked = 5
)

// ReportSchema names the version of the JSON the commands print with --json.
const ReportSchema = "boxer.schema-report/1"

// NewCliCommand returns the `schema` command group.
func NewCliCommand() *cli.Command {
	selectFlags := []cli.Flag{
		&cli.StringSliceFlag{
			Name:  flagTable,
			Usage: "only the named tables (database-qualified, as the listing prints them); repeatable",
		},
		&cli.StringSliceFlag{
			Name:  flagOwner,
			Usage: "only the tables of the named owners, as the listing prints them; repeatable",
		},
		&cli.BoolFlag{Name: flagJSON, Usage: "print the report as JSON (" + ReportSchema + ") instead of SQL"},
	}
	return &cli.Command{
		Name:  "schema",
		Usage: "judge the stored tables of this build against it, and create or migrate them when told to (ADR-0387)",
		Commands: []*cli.Command{
			{
				Name:   "plan",
				Usage:  "print each table's verdict and the statements that bring it in line; changes nothing",
				Flags:  selectFlags,
				Action: runPlan,
			},
			{
				Name: "check",
				Usage: "print what `plan` prints and exit 0 only when every table is in line, or would be after an apply of the kinds named: " +
					fmt.Sprintf("%d when statements are pending, %d when a change is refused, %d when a table could not be read", ExitPending, ExitBlocked, ExitUnchecked),
				Flags: append(slices.Clone(selectFlags),
					&cli.BoolFlag{Name: flagCreate, Usage: "also accept a table that does not exist where `apply --create` would create it"},
					&cli.BoolFlag{Name: flagAlter, Usage: "also accept a table the statements of `apply --alter` would bring in line"},
				),
				Action: runCheck,
			},
			{
				Name:  "apply",
				Usage: "run the planned statements of the kinds named, then exit as `check` does; a refused table is left as it is",
				Flags: append(slices.Clone(selectFlags),
					&cli.BoolFlag{Name: flagCreate, Usage: "create the tables that do not exist, and their databases; changes no table that exists"},
					&cli.BoolFlag{Name: flagAlter, Usage: "run the ALTER statements that bring an existing table in line"},
				),
				Action: runApply,
			},
			roundTripCommand(),
			loadCommand(),
		},
	}
}

// Contracts are the stored tables of boxer itself, in their default
// layouts: the facts table (which the facts-bound stores share), the state
// table, the journal's three, the watchbill pair and the snapshot store's
// three. Where the facts are read from another relation, or stored in a
// deployment's own table, that relation is judged by its columns and gets
// no statement (ADR-0402 §SD4, §SD5).
func Contracts() (cs []recordstore.TableContract, err error) {
	ts, err := builtin()
	if err != nil {
		return
	}
	for _, t := range ts {
		cs = append(cs, t.Contract)
	}
	return
}

// builtin are boxer's own stored tables under their owners. None carries a
// Provision: the store that owns each creates it when it starts.
func builtin() (ts []recordstore.StoredTable, err error) {
	facts, err := chstore.SchemaContracts(chstore.ConfigFromEnv())
	if err != nil {
		return
	}
	add := func(owner string, cs ...recordstore.TableContract) {
		for _, c := range cs {
			ts = append(ts, recordstore.StoredTable{Owner: owner, Contract: c})
		}
	}
	lading := ladingschema.Layout{}
	add("facts", facts...)
	add("persist", persiststore.PersistSchemaContract(""))
	add("agentrun", agentrunstore.Layout{}.SchemaContracts()...)
	add("watchbill", watchbillstore.Layout{}.SchemaContracts()...)
	add("lading",
		ladingmeta.MetaSchemaContract(lading.MetaTable()),
		ladingdata.DataSchemaContract(lading.DataTable()),
		// The snapshot index decodes through the entry store's columns.
		ladingmeta.MetaSchemaContract(lading.SnapTable()),
	)
	return
}

// Tables are the stored tables this build knows: boxer's own and the ones
// the linked packages declare.
func Tables() (ts []recordstore.StoredTable, err error) {
	ts, err = builtin()
	if err != nil {
		return
	}
	declared, err := recordstore.DeclaredStoredTables()
	if err != nil {
		return
	}
	ts = append(ts, declared...)
	err = recordstore.CheckStoredTables(ts)
	return
}

func selected(cmd *cli.Command) (ts []recordstore.StoredTable, err error) {
	all, err := Tables()
	if err != nil {
		return
	}
	ts, err = selectTables(all, cmd.StringSlice(flagTable), cmd.StringSlice(flagOwner))
	return
}

// selectTables keeps the tables named by tables and owned by owners, an
// empty list selecting all; a name that selects nothing is an error.
func selectTables(all []recordstore.StoredTable, tables []string, owners []string) (ts []recordstore.StoredTable, err error) {
	for _, t := range tables {
		if !slices.ContainsFunc(all, func(s recordstore.StoredTable) bool { return s.Contract.Table == t }) {
			err = eb.Build().Str("table", t).Errorf("schema: no stored table of this build has that name")
			return
		}
	}
	for _, o := range owners {
		if !slices.ContainsFunc(all, func(s recordstore.StoredTable) bool { return s.Owner == o }) {
			err = eb.Build().Str("owner", o).Strs("owners", ownersOf(all)).Errorf("schema: no stored table of this build has that owner")
			return
		}
	}
	for _, s := range all {
		if len(tables) > 0 && !slices.Contains(tables, s.Contract.Table) {
			continue
		}
		if len(owners) > 0 && !slices.Contains(owners, s.Owner) {
			continue
		}
		ts = append(ts, s)
	}
	if len(ts) == 0 {
		err = eh.Errorf("schema: the selection names no stored table")
	}
	return
}

func ownersOf(ts []recordstore.StoredTable) (owners []string) {
	for _, t := range ts {
		if !slices.Contains(owners, t.Owner) {
			owners = append(owners, t.Owner)
		}
	}
	slices.Sort(owners)
	return
}

func plans(ctx context.Context, cmd *cli.Command) (exec recordstore.ExecutorI, ts []recordstore.StoredTable, ps []schemaplan.Plan, err error) {
	ts, err = selected(cmd)
	if err != nil {
		return
	}
	exec, err = storeexec.ConnectFromEnv(ctx)
	if err != nil {
		return
	}
	ps = planAll(ctx, exec, ts)
	return
}

func planAll(ctx context.Context, exec recordstore.ExecutorI, ts []recordstore.StoredTable) (ps []schemaplan.Plan) {
	for _, t := range ts {
		ps = append(ps, schemaplan.PlanStored(ctx, exec, t))
	}
	return
}

func runPlan(ctx context.Context, cmd *cli.Command) (err error) {
	_, _, ps, err := plans(ctx, cmd)
	if err != nil {
		return
	}
	err = write(cmd, ps, nil)
	return
}

func runCheck(ctx context.Context, cmd *cli.Command) (err error) {
	_, _, ps, err := plans(ctx, cmd)
	if err != nil {
		return
	}
	if err = write(cmd, ps, nil); err != nil {
		return
	}
	err = exitFor(Unaccepted(ps, cmd.Bool(flagCreate), cmd.Bool(flagAlter)))
	return
}

// Unaccepted returns the plans that are not in line and that an apply of
// the kinds named would not bring in line either: every plan that is
// blocked or unchecked, and a pending one holding statements of a kind not
// named. `check --create --alter` exits by these, so it answers whether an
// apply of those kinds would end in line, without running it.
func Unaccepted(ps []schemaplan.Plan, create bool, alter bool) (rest []schemaplan.Plan) {
	for _, p := range ps {
		switch p.Outcome() {
		case schemaplan.OutcomeInSync:
			continue
		case schemaplan.OutcomePending:
			if (len(p.Creates) == 0 || create) && (len(p.Statements) == 0 || alter) {
				continue
			}
		}
		rest = append(rest, p)
	}
	return
}

func runApply(ctx context.Context, cmd *cli.Command) (err error) {
	create, alter := cmd.Bool(flagCreate), cmd.Bool(flagAlter)
	if !create && !alter {
		return eh.Errorf("schema apply changes the server: name what it may run, --%s for tables that do not exist, --%s for ALTER statements on those that do; `schema plan` shows what each would run", flagCreate, flagAlter)
	}
	exec, ts, ps, err := plans(ctx, cmd)
	if err != nil {
		return
	}
	var applied []Applied
	for _, p := range ps {
		a := Applied{Table: p.Verdict.Table}
		if create && len(p.Creates) > 0 {
			if a.Created, err = schemaplan.ApplyCreates(ctx, exec, p); err != nil {
				return
			}
		}
		if alter && !p.IsRefused() && len(p.Statements) > 0 {
			if a.Altered, err = schemaplan.Apply(ctx, exec, p); err != nil {
				return
			}
		}
		if a.Created+a.Altered > 0 {
			applied = append(applied, a)
		}
	}
	// What the server holds now decides the answer, not what was planned.
	after := planAll(ctx, exec, ts)
	if err = write(cmd, after, applied); err != nil {
		return
	}
	err = exitFor(after)
	return
}

// Applied counts the statements `schema apply` ran on one table.
type Applied struct {
	Table   string `json:"table"`
	Created int    `json:"created,omitempty"`
	Altered int    `json:"altered,omitempty"`
}

// Report is what the commands print with --json.
type Report struct {
	Schema string `json:"schema"`
	// Outcome is the worst outcome among the tables.
	Outcome schemaplan.OutcomeE `json:"outcome"`
	// Applied lists what `schema apply` ran; the tables are as found after.
	Applied []Applied     `json:"applied,omitempty"`
	Tables  []ReportTable `json:"tables"`
}

// ReportTable is one table of a report: its plan and where that leaves it.
type ReportTable struct {
	Outcome schemaplan.OutcomeE `json:"outcome"`
	schemaplan.Plan
}

// NewReport is the report of ps, after applied was run.
func NewReport(ps []schemaplan.Plan, applied []Applied) (r Report) {
	r = Report{Schema: ReportSchema, Outcome: Worst(ps), Applied: applied, Tables: make([]ReportTable, 0, len(ps))}
	for _, p := range ps {
		r.Tables = append(r.Tables, ReportTable{Outcome: p.Outcome(), Plan: p})
	}
	return
}

// Worst is the outcome that decides for all of ps: unchecked before
// blocked before pending before in sync.
func Worst(ps []schemaplan.Plan) (o schemaplan.OutcomeE) {
	rank := map[schemaplan.OutcomeE]int{schemaplan.OutcomeInSync: 0, schemaplan.OutcomePending: 1, schemaplan.OutcomeBlocked: 2, schemaplan.OutcomeUnchecked: 3}
	o = schemaplan.OutcomeInSync
	for _, p := range ps {
		if po := p.Outcome(); rank[po] > rank[o] {
			o = po
		}
	}
	return
}

// ExitCode is the exit code `check` and `apply` end with for ps: 0 when
// every table is in line.
func ExitCode(ps []schemaplan.Plan) (code int) {
	switch Worst(ps) {
	case schemaplan.OutcomePending:
		code = ExitPending
	case schemaplan.OutcomeBlocked:
		code = ExitBlocked
	case schemaplan.OutcomeUnchecked:
		code = ExitUnchecked
	}
	return
}

// exitFor ends the process with ps's exit code and one line naming the
// tables that are not in line; nil when all are.
func exitFor(ps []schemaplan.Plan) (err error) {
	code := ExitCode(ps)
	if code == 0 {
		return
	}
	var names []string
	for _, p := range ps {
		if o := p.Outcome(); o != schemaplan.OutcomeInSync {
			names = append(names, p.Verdict.Table+" ("+string(o)+")")
		}
	}
	err = cli.Exit("schema: not in line: "+strings.Join(names, ", "), code)
	return
}

func write(cmd *cli.Command, ps []schemaplan.Plan, applied []Applied) (err error) {
	w := cmd.Root().Writer
	if cmd.Bool(flagJSON) {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		err = enc.Encode(NewReport(ps, applied))
		return
	}
	for _, a := range applied {
		_, _ = fmt.Fprintf(w, "-- applied to %s: %d creating, %d altering statement(s)\n", a.Table, a.Created, a.Altered)
	}
	Print(w, ps)
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
		for _, s := range p.Creates {
			_, _ = fmt.Fprintf(w, "%s;\n", s)
		}
		for _, s := range p.Statements {
			_, _ = fmt.Fprintf(w, "%s;\n", s)
		}
	}
}
