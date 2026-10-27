package agentrun

import (
	"context"
	"encoding/json/v2"
	"regexp"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableVerdicts is keelson('agent_verdicts') (ADR-0340 (proposed) §SD4):
// one row per verdict entry of the newest runs, its record flattened
// beside the assertion it judged, and the ordinal of the reply of its
// turn that refers to it.
const TableVerdicts = "agent_verdicts"

type verdictsProvider struct{ store StoreI }

func (verdictsProvider) Name() string                         { return TableVerdicts }
func (verdictsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (verdictsProvider) Schema() *arrow.Schema                { return verdictsTable(nil).Schema() }

func (inst verdictsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var entries []Entry
	if inst.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
		defer cancel()
		if entries, err = allEntries(ctx, inst.store); err != nil {
			return
		}
	}
	rows := VerdictRows(entries)
	rec = verdictsTable(rows).Build(proj, len(rows))
	return
}

// VerdictRow is one verdict as the table serves it.
type VerdictRow struct {
	Entry Entry
	// Says, Statement and Expect are the assertion's, read from the
	// entry's arguments; Observed is its body.
	Says, Statement, Expect string
	// Reply is the ordinal of the reply of the verdict's turn that refers
	// to it, 0 when none does.
	Reply uint32
}

// VerdictRows flattens the verdict entries among entries, in order, each
// with the reply of its turn that refers to it.
func VerdictRows(entries []Entry) (rows []VerdictRow) {
	for _, e := range entries {
		if e.Kind != KindVerdict || e.Verdict == nil {
			continue
		}
		row := VerdictRow{Entry: e}
		var a struct {
			Says      string `json:"says"`
			Statement string `json:"statement"`
			Expect    string `json:"expect"`
		}
		if json.Unmarshal([]byte(e.Arguments), &a) == nil {
			row.Says, row.Statement, row.Expect = a.Says, a.Statement, a.Expect
		}
		ref := regexp.MustCompile(`\[` + regexp.QuoteMeta(e.Name) + `\]`)
		for _, r := range entries {
			if r.Kind == KindReply && r.Run == e.Run && r.Turn == e.Turn && r.Ordinal > e.Ordinal && ref.MatchString(r.Body) {
				row.Reply = r.Ordinal
				break
			}
		}
		rows = append(rows, row)
	}
	return
}

func verdictsTable(rows []VerdictRow) *introspect.Table {
	return introspect.NewTable().
		Uint64("run", func(i int) uint64 { return rows[i].Entry.Run }).
		Uint64("ordinal", func(i int) uint64 { return uint64(rows[i].Entry.Ordinal) }).
		Int64("at_ms", func(i int) int64 { return ms(rows[i].Entry.At) }).
		String("turn", func(i int) string { return rows[i].Entry.Turn }).
		String("key", func(i int) string { return rows[i].Entry.Key }).
		String("assertion", func(i int) string { return rows[i].Entry.Name }).
		String("check", func(i int) string { return rows[i].Entry.Form }).
		String("form", func(i int) string { return rows[i].Entry.Verdict.Form }).
		String("outcome", func(i int) string { return rows[i].Entry.Verdict.Outcome }).
		StringList("rests_on", func(i int) []string { return rows[i].Entry.Verdict.RestsOn }).
		Uint64("result", func(i int) uint64 { return rows[i].Entry.Verdict.Result }).
		String("statement_digest", func(i int) string { return rows[i].Entry.Verdict.Digest }).
		String("source_digest", func(i int) string { return rows[i].Entry.Verdict.Source }).
		String("endpoint", func(i int) string { return rows[i].Entry.Verdict.Endpoint }).
		String("reason", func(i int) string { return rows[i].Entry.Verdict.Reason }).
		String("says", func(i int) string { return rows[i].Says }).
		String("statement", func(i int) string { return rows[i].Statement }).
		String("expect", func(i int) string { return rows[i].Expect }).
		String("observed", func(i int) string { return rows[i].Entry.Body }).
		Uint64("reply_ordinal", func(i int) uint64 { return uint64(rows[i].Reply) }).
		String("label_level", func(i int) string { return rows[i].Entry.Label.Level.String() }).
		String("label_tier", func(i int) string { return rows[i].Entry.Label.Tier.String() })
}
