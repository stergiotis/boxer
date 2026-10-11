package introspecthost

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/headstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence"
	"github.com/stergiotis/boxer/public/storage/recordstore/evidence/anchor"
)

// TableEvidenceChecks is keelson('evidence_checks'): the evidence laws
// served in-process (ADR-0327 §SD7) — one row per journal run for L31 and
// for L32, one per trail run for L34 and for its anchors — each saying
// whether the law holds over the host's stores as they are when the
// relation is read, and where it breaks.
const TableEvidenceChecks = "evidence_checks"

// SealVerifierI is the trail's recorder as the relation reads it.
type SealVerifierI interface {
	VerifySeals(ctx context.Context, run string) (broken evidence.SealBreak, unnamed []uint64, unanchored []uint64, err error)
}

// evidenceCheck is one row: the store, its scope, the law, and the
// verdict.
type evidenceCheck struct {
	Store  string
	Scope  string
	Law    string
	Holds  bool
	Broken uint64
	Why    string
}

// checkTimeout bounds one read of the relation.
const checkTimeout = 30 * time.Second

type evidenceChecksProvider struct {
	runs    agentrun.StoreI
	anchors anchor.StoreI
	trail   SealVerifierI
	run     string
}

func registerEvidenceChecks(reg *introspect.Registry, deps Deps) (err error) {
	return reg.Register(evidenceChecksProvider{runs: deps.AgentRuns, anchors: deps.Anchors, trail: deps.Trail, run: deps.Run})
}

func (evidenceChecksProvider) Name() string                         { return TableEvidenceChecks }
func (evidenceChecksProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (evidenceChecksProvider) Schema() *arrow.Schema                { return evidenceChecksTable(nil).Schema() }

func (inst evidenceChecksProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	rows := inst.journalChecks(ctx)
	rows = append(rows, inst.trailChecks(ctx)...)
	rec = evidenceChecksTable(rows).Build(proj, len(rows))
	return
}

// journalChecks is L31 and L32 over every run the journal store holds. A
// run whose entries cannot be read is one row saying so.
func (inst evidenceChecksProvider) journalChecks(ctx context.Context) (rows []evidenceCheck) {
	if inst.runs == nil {
		return
	}
	runs, err := inst.runs.ListRuns(ctx, "", 0)
	if err != nil {
		return []evidenceCheck{{Store: "journal", Law: "L31", Why: "list runs: " + err.Error()}}
	}
	for _, r := range runs {
		scope := strconv.FormatUint(r.Id, 10)
		entries, eerr := inst.runs.Entries(ctx, r.Id, 0)
		if eerr != nil {
			rows = append(rows, evidenceCheck{Store: "journal", Scope: scope, Law: "L31", Why: "read entries: " + eerr.Error()})
			continue
		}
		broken, verr := agentrun.VerifyRun(r, entries)
		row := evidenceCheck{Store: "journal", Scope: scope, Law: "L31", Holds: verr == nil && broken == 0, Broken: uint64(broken)}
		if verr != nil {
			row.Why = verr.Error()
		} else if broken != 0 {
			row.Why = "the chain breaks at the ordinal"
		}
		rows = append(rows, row)
		if inst.anchors == nil {
			continue
		}
		anchored, aerr := headstore.Over(inst.anchors).Read(ctx, r.Cell, r.Id)
		if aerr != nil {
			rows = append(rows, evidenceCheck{Store: "journal", Scope: scope, Law: "L32", Why: "read anchors: " + aerr.Error()})
			continue
		}
		bad := headstore.VerifyHeads(r.Digest, entries, anchored)
		row = evidenceCheck{Store: "journal", Scope: scope, Law: "L32", Holds: len(bad) == 0}
		if len(bad) > 0 {
			row.Broken = uint64(bad[0])
			row.Why = "heads break at " + ordinals32(bad)
		}
		rows = append(rows, row)
	}
	return
}

// trailChecks is L34 over this run's trail, and L32 over its anchored
// seals.
func (inst evidenceChecksProvider) trailChecks(ctx context.Context) (rows []evidenceCheck) {
	if inst.trail == nil || inst.run == "" {
		return
	}
	broken, unnamed, unanchored, err := inst.trail.VerifySeals(ctx, inst.run)
	if err != nil {
		return []evidenceCheck{{Store: "trail", Scope: inst.run, Law: "L34", Why: err.Error()}}
	}
	row := evidenceCheck{Store: "trail", Scope: inst.run, Law: "L34", Holds: broken.Why == evidence.SealBreakNone && len(unnamed) == 0, Broken: broken.Seal}
	switch {
	case broken.Why != evidence.SealBreakNone:
		row.Why = "the stream breaks: " + broken.Why.String()
		if broken.Row != 0 {
			row.Why += " at row " + strconv.FormatUint(broken.Row, 10)
		}
	case len(unnamed) > 0:
		row.Broken = unnamed[0]
		row.Why = strconv.Itoa(len(unnamed)) + " rows no seal names"
	}
	rows = append(rows, row)
	anchors := evidenceCheck{Store: "trail", Scope: inst.run, Law: "L32", Holds: broken.Why == evidence.SealBreakNone && len(unanchored) == 0}
	switch {
	case broken.Why != evidence.SealBreakNone:
		anchors.Why = "the stream does not order"
	case len(unanchored) > 0:
		anchors.Broken = unanchored[0]
		anchors.Why = "anchored seals break at " + ordinals64(unanchored)
	}
	return append(rows, anchors)
}

func ordinals32(xs []uint32) string {
	parts := make([]string, 0, len(xs))
	for _, x := range xs {
		parts = append(parts, strconv.FormatUint(uint64(x), 10))
	}
	return strings.Join(parts, ",")
}

func ordinals64(xs []uint64) string {
	parts := make([]string, 0, len(xs))
	for _, x := range xs {
		parts = append(parts, strconv.FormatUint(x, 10))
	}
	return strings.Join(parts, ",")
}

func evidenceChecksTable(rows []evidenceCheck) *introspect.Table {
	return introspect.NewTable().
		String("store", func(i int) string { return rows[i].Store }).
		String("scope", func(i int) string { return rows[i].Scope }).
		String("law", func(i int) string { return rows[i].Law }).
		Bool("holds", func(i int) bool { return rows[i].Holds }).
		Uint64("broken", func(i int) uint64 { return rows[i].Broken }).
		String("why", func(i int) string { return rows[i].Why })
}
