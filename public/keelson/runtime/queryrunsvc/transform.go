package queryrunsvc

import (
	"context"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/keelson/data/storeexec"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryrunfacts"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/factsschema/dml"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// The capture under an insert transform (ADR-0402 and ADR-0115, updates of
// 2026-11-22). The facts are kept in a deployment's own table, so /pull
// serves the transform's output and the view appends it to the transform's
// target. The reconcile asks the transform where a query-run batch goes and
// in which schema, and every pull is held to that answer. The extract reads
// stored query runs through the read relation and drops those already
// stored; the view does not deduplicate.

// captureRoute is what a reconcile under a transform settled.
type captureRoute struct {
	t recordstore.InsertTransformI
	// target is the table the view appends to, as the transform named it.
	target string
	// canon is target as recordstore.CanonicalTableRef spells it, which a
	// pull's target is compared under.
	canon string
	// schema is the Arrow schema the transform returned for the probe; the
	// url() structure lists its fields.
	schema *arrow.Schema
}

// errNoCapture marks the reasons the capture cannot be set up under a
// transform; the reconcile reports them wrapping
// recordstore.ErrTransformedTable.
var errNoCapture = errors.New("the query-run capture cannot be set up for this transform")

// probeRoute asks the transform registered for the destination where a
// query-run batch goes, by transforming one probe row encoded as a pull
// encodes it. It does no I/O: the transform is a pure function.
func (s *Service) probeRoute() (route *captureRoute, err error) {
	t, ok := recordstore.RegisteredInsertTransform(s.FactsTable())
	if !ok {
		err = eh.Errorf("queryrunsvc: no insert transform is registered for the destination")
		return
	}
	ent := dml.NewInEntityFacts(memory.NewGoAllocator(), 1)
	if err = queryrunfacts.BuildEntities(ent, []queryrunfacts.Row{queryrunfacts.ProbeRow()}); err != nil {
		return
	}
	records, err := ent.TransferRecords(nil)
	if err != nil {
		return
	}
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	target, out, release, err := recordstore.TransformInsert(t, s.FactsTable(), records)
	if err != nil {
		err = eh.Errorf("queryrunsvc: the transform refused a query-run batch: %w: %w", err, errNoCapture)
		return
	}
	defer release()
	canon, qualified, okRef := recordstore.CanonicalTableRef(target)
	if !okRef || !qualified {
		err = eb.Build().Str("target", target).Errorf("queryrunsvc: the transform's target does not name its database, and the view's APPEND TO would resolve it in the view's: %w", errNoCapture)
		return
	}
	if len(out) == 0 {
		err = eb.Build().Str("target", target).Errorf("queryrunsvc: the transform returned no batch for a query-run row, so the schema it writes is unknown: %w", errNoCapture)
		return
	}
	schema := out[0].Schema()
	for _, r := range out[1:] {
		if !r.Schema().Equal(schema) {
			err = eb.Build().Str("target", target).Errorf("queryrunsvc: the transform returned batches in more than one schema, and a pull is one Arrow stream: %w", errNoCapture)
			return
		}
	}
	route = &captureRoute{t: t, target: target, canon: canon, schema: schema}
	return
}

// transform applies the route's transform to one pull's records and holds
// the result to what the reconcile settled: the same target and the same
// schema. A batch for another table would be appended to the reconciled
// one; it fails the pull instead, so the refresh fails and says so, and the
// events wait in query_log. An empty pull is answered in the settled schema
// without asking the transform.
func (inst *captureRoute) transform(table string, records []arrow.RecordBatch) (out []arrow.RecordBatch, release func(), err error) {
	release = func() {}
	if len(records) == 0 {
		return
	}
	target, out, release, err := recordstore.TransformInsert(inst.t, table, records)
	if err != nil {
		return
	}
	canon, _, _ := recordstore.CanonicalTableRef(target)
	if canon != inst.canon {
		release()
		out, release = nil, func() {}
		err = eb.Build().Str("target", target).Str("reconciled", inst.target).
			Errorf("queryrunsvc: the transform sent this batch to another table than the capture view appends to; restart the service once the transform sends every query-run batch to one table")
		return
	}
	for _, r := range out {
		if !r.Schema().Equal(inst.schema) {
			release()
			out, release = nil, func() {}
			err = eb.Build().Str("target", target).
				Errorf("queryrunsvc: the transform returned another schema than the capture view was composed against; restart the service to reconcile it")
			return
		}
	}
	return
}

// reconcileTransformed sets the capture up under an insert transform: the
// route by probe, chstore's setup (no facts table under a transform), the
// read relation and the target judged, then the view, which appends the
// transform's output to the target. A reason the capture cannot be set up
// is refused through refuseTransformed; anything else, a server that does
// not answer among it, is returned as it is.
func (s *Service) reconcileTransformed(ctx context.Context) (err error) {
	route, err := s.probeRoute()
	if err != nil {
		return s.refuseTransformed(ctx, err)
	}
	factsCfg := chstore.Config{Database: s.cfg.Database, Table: s.cfg.Table, ReadTable: s.cfg.ReadTable}
	stmts, err := chstore.SetupStatements(factsCfg, "")
	if err != nil {
		err = eh.Errorf("queryrunsvc: reconcile: %w", err)
		return
	}
	for _, stmt := range stmts {
		if err = s.cli.Exec(ctx, stmt); err != nil {
			err = eh.Errorf("queryrunsvc: reconcile: facts setup: %w", err)
			return
		}
	}
	exec, err := storeexec.New(s.cli, memory.NewGoAllocator())
	if err != nil {
		return
	}
	structure, err := targetStructure(ctx, exec, route)
	if err != nil {
		if errors.Is(err, errNoCapture) {
			err = s.refuseTransformed(ctx, err)
		}
		return
	}
	if err = checkReadRelation(ctx, exec, factsCfg); err != nil {
		if errors.Is(err, errNoCapture) {
			err = s.refuseTransformed(ctx, err)
		}
		return
	}
	mv, err := queryrunfacts.ComposeAppendMvSql(s.MvName(), route.target, s.PullURL(), structure, s.cadenceSeconds())
	if err != nil {
		return
	}
	// Held before the view exists: its first refresh may come at once.
	s.route.Store(route)
	err = s.installView(ctx, mv)
	return
}

// targetStructure reads the route's target from the server and composes
// the url() structure: the transform's columns in its order, with the types
// the target declares for them. Columns of the target the transform does
// not return keep their defaults.
func targetStructure(ctx context.Context, exec recordstore.ExecutorI, route *captureRoute) (structure string, err error) {
	facts, err := recordstore.ReadTableFacts(ctx, exec, route.target)
	b := eb.Build().Str("target", route.target)
	switch {
	case err != nil:
		err = b.Errorf("queryrunsvc: reconcile: the transform's target cannot be read: %w", err)
		return
	case !facts.Exists:
		err = b.Errorf("queryrunsvc: the transform's target does not exist: %w", errNoCapture)
		return
	case recordstore.TakesNoInsert(facts.Engine):
		err = b.Str("engine", facts.Engine).Errorf("queryrunsvc: the transform's target takes no insert: %w", errNoCapture)
		return
	}
	live := make(map[string]recordstore.LiveColumn, len(facts.Columns))
	for _, c := range facts.Columns {
		live[c.Name] = c
	}
	cols := make([][2]string, 0, route.schema.NumFields())
	for _, f := range route.schema.Fields() {
		c, ok := live[f.Name]
		switch {
		case !ok:
			err = b.Str("column", f.Name).Errorf("queryrunsvc: the transform returns a column its target does not have: %w", errNoCapture)
			return
		case c.DefaultKind == "MATERIALIZED" || c.DefaultKind == "ALIAS":
			err = b.Str("column", f.Name).Str("defaultKind", c.DefaultKind).Errorf("queryrunsvc: the transform returns a column its target computes and takes no insert into: %w", errNoCapture)
			return
		}
		cols = append(cols, [2]string{c.Name, c.Type})
	}
	structure = queryrunfacts.UrlStructureOf(cols)
	return
}

// checkReadRelation judges the relation the capture reads stored query runs
// from, by its columns (chstore.SchemaContracts; ADR-0402 §SD4). A relation
// that is missing or lacks a facts column, or returns one under another
// type, fails it: the watermark and the filter of stored events would read
// nothing, and every event would be appended again on each refresh.
func checkReadRelation(ctx context.Context, exec recordstore.ExecutorI, cfg chstore.Config) (err error) {
	contracts, err := chstore.SchemaContracts(cfg)
	if err != nil {
		return
	}
	for _, c := range contracts {
		v := recordstore.JudgeTable(ctx, exec, c)
		if v.Verdict == recordstore.SchemaVerdictOk {
			continue
		}
		b := eb.Build().Str("readTable", c.Table).Str("verdict", string(v.Verdict)).Str("summary", v.Summary())
		if v.Verdict == recordstore.SchemaVerdictUnchecked {
			return b.Errorf("queryrunsvc: reconcile: the read relation cannot be judged")
		}
		return b.Errorf("queryrunsvc: the read relation does not return every facts column with its type: %w", errNoCapture)
	}
	return
}

// refuseTransformed is the reconcile's refusal under a transform, for a
// reason the capture cannot be set up. A view an earlier binary installed
// is dropped first: it appends facts-shaped rows by the facts table's name,
// where a table of that name was left behind or the deployment's view now
// is.
func (s *Service) refuseTransformed(ctx context.Context, reason error) (err error) {
	s.route.Store(nil)
	dropped, derr := s.dropLeftoverView(ctx)
	b := eb.Build().Str("table", s.FactsTable())
	switch {
	case derr != nil:
		err = b.Str("leftoverView", derr.Error()).Errorf("queryrunsvc: reconcile: the query-run view is not installed, and whether an earlier binary left one behind could not be settled: %w: %w", reason, recordstore.ErrTransformedTable)
	case dropped:
		err = b.Str("dropped", s.MvName()).Errorf("queryrunsvc: reconcile: the query-run view is not installed; one an earlier binary left behind was dropped: %w: %w", reason, recordstore.ErrTransformedTable)
	default:
		err = b.Errorf("queryrunsvc: reconcile: the query-run view is not installed: %w: %w", reason, recordstore.ErrTransformedTable)
	}
	return
}
