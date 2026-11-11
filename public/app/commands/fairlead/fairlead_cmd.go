// Package fairlead is the CLI over the statement journal of ADR-0367 §SD4:
// it reads what [fairlead.Journal] recorded and says, per exit point, how the
// statements classify.
package fairlead

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"text/tabwriter"

	"github.com/urfave/cli/v3"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/fairlead"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ReportRow counts the distinct statements of one exit point that share a
// classification.
type ReportRow struct {
	Site     string
	Exit     string
	Parsed   bool
	Kind     analysis.KindE
	Security analysis.QuerySecurityClassE
	Stratum  analysis.QueryStratumE
	// Statements counts distinct texts; Shapes counts them after
	// [analysis.StatementShape] has folded the ones that differ only in a
	// literal.
	Statements int64
	Shapes     int64
	// Script marks texts that hold several statements. Their classification
	// is that of the last one: the statements before it set a stage — a
	// temporary table — and the last is what the caller asked.
	Script bool
	// Introspects marks statements that report on a statement or an object
	// instead of returning rows ([analysis.Classification.Introspects]).
	Introspects bool
}

type reportKey struct {
	site, exit string
	parsed     bool
	kind       analysis.KindE
	security   analysis.QuerySecurityClassE
	stratum    analysis.QueryStratumE
	script     bool
	introspect bool
}

// Report folds journal files into rows. A statement text counts once per
// exit point however many processes recorded it. A torn last line ends that
// file's contribution and is reported in torn; any other read failure is an
// error.
type Report struct {
	seen   map[fairlead.JournalEntry]struct{}
	rows   map[reportKey]int64
	shapes map[reportKey]map[string]struct{}
	torn   int32
}

func NewReport() (inst *Report) {
	inst = &Report{
		seen:   make(map[fairlead.JournalEntry]struct{}, 1024),
		rows:   make(map[reportKey]int64, 64),
		shapes: make(map[reportKey]map[string]struct{}, 64),
	}
	return
}

// Add reads one journal.
func (inst *Report) Add(r io.Reader) {
	for e, err := range fairlead.ReadJournal(r) {
		if err != nil {
			inst.torn++
			return
		}
		if _, dup := inst.seen[e]; dup {
			continue
		}
		inst.seen[e] = struct{}{}
		c := analysis.Classify(e.SQL, "")
		script := false
		if !c.Parsed {
			// The grammar reads one statement. A script is classified by its
			// last statement and marked, so the row does not pass for a
			// statement that stood alone.
			if stmts := analysis.SplitStatements(e.SQL); len(stmts) > 1 {
				c = analysis.Classify(stmts[len(stmts)-1], "")
				script = true
			}
		}
		key := reportKey{e.Site, e.Exit, c.Parsed, c.Kind, c.Security, c.Stratum, script, c.Introspects}
		inst.rows[key]++
		set := inst.shapes[key]
		if set == nil {
			set = make(map[string]struct{}, 8)
			inst.shapes[key] = set
		}
		set[analysis.StatementShape(e.SQL)] = struct{}{}
	}
}

// Rows returns the folded counts, ordered by site, then from the most
// demanding classification to the least.
func (inst *Report) Rows() (rows []ReportRow) {
	rows = make([]ReportRow, 0, len(inst.rows))
	for k, n := range inst.rows {
		rows = append(rows, ReportRow{k.site, k.exit, k.parsed, k.kind, k.security, k.stratum, n, int64(len(inst.shapes[k])), k.script, k.introspect})
	}
	slices.SortFunc(rows, func(a, b ReportRow) int {
		return cmp.Or(
			cmp.Compare(a.Site, b.Site),
			cmp.Compare(a.Exit, b.Exit),
			cmp.Compare(a.Stratum, b.Stratum),
			cmp.Compare(a.Security, b.Security),
			cmp.Compare(a.Kind, b.Kind),
			cmp.Compare(btoi(a.Script), btoi(b.Script)),
			cmp.Compare(btoi(a.Introspects), btoi(b.Introspects)),
		)
	})
	return
}

func btoi(b bool) (i int) {
	if b {
		i = 1
	}
	return
}

// Torn is the number of journals that ended in an undecodable line.
func (inst *Report) Torn() (n int32) {
	return inst.torn
}

func writeRows(w io.Writer, rows []ReportRow) (err error) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "site\texit\tparsed\tkind\tsecurity\tstratum\tscript\tintrospects\tstatements\tshapes")
	for _, r := range rows {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%t\t%s\t%s\t%s\t%t\t%t\t%d\t%d\n", r.Site, r.Exit, r.Parsed, r.Kind, r.Security, r.Stratum, r.Script, r.Introspects, r.Statements, r.Shapes)
	}
	err = tw.Flush()
	return
}

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "fairlead",
		Usage: "the SQL statements this tree sends, as recorded at its exit points (ADR-0367)",
		Commands: []*cli.Command{
			{
				Name:  "report",
				Usage: "classify the statements in a journal directory, per exit point",
				Flags: []cli.Flag{
					fairlead.JournalDir.AsCliFlag(),
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					dir := fairlead.JournalDir.Get()
					if dir == "" {
						return eb.Build().Str("variable", fairlead.JournalDir.Spec().Name).Errorf("no journal directory: set the variable or pass its flag")
					}
					files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
					if err != nil {
						return eb.Build().Str("dir", dir).Errorf("list journals: %w", err)
					}
					rep := NewReport()
					for _, name := range files {
						f, openErr := os.Open(name)
						if openErr != nil {
							return eb.Build().Str("file", name).Errorf("open journal: %w", openErr)
						}
						rep.Add(f)
						_ = f.Close()
					}
					err = writeRows(os.Stdout, rep.Rows())
					if err != nil {
						return eh.Errorf("write report: %w", err)
					}
					if rep.Torn() > 0 {
						_, _ = fmt.Fprintf(os.Stderr, "%d of %d journals ended in a torn line\n", rep.Torn(), len(files))
					}
					return nil
				},
			},
		},
	}
}
