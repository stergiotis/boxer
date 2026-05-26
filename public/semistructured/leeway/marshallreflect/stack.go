//go:build llm_generated_opus47

package marshallreflect

import (
	"reflect"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
)

// MarshalStack drives `dml`'s reflected method chain to emit one
// entity per row index, with sections contributed by each DTO in
// `batches` interleaved between the entity's BeginEntity / CommitEntity
// frame. Implements ADR-0008 D1.
//
// `batches` is a heterogeneous list — each element must be a `[]TX`
// where TX is some DTO struct type. MarshalStack uses reflect to
// introspect each batch's element type at runtime, resolves the
// plan, cross-checks that:
//
//   - every batch has the same row count (lengths agree);
//   - every batch's DTO declares the same plain-column set (same
//     column names mapped to the same Go types).
//
// Either disagreement is an error. Sections may repeat across DTOs
// without conflict: the DML state machine permits re-entering a
// section within one entity, so two DTOs both declaring section `Foo`
// cleanly produce two `BeginSectionFoo`…`EndSection` cycles per row.
//
// Plain columns are written once per entity from the first batch's
// plan; the cross-DTO agreement check guarantees the remaining plans
// would have produced the same values.
//
// `dml`'s method set must satisfy the same reflective contract
// Marshal expects (BeginEntity / SetId / SetTimestamp / SetLifecycle
// / GetSection<X> / CommitEntity). Pass NoLookup{} if every membership
// in every DTO carries a verbatim channel.
func MarshalStack(dml any, batches []any, lookup LookupI) (err error) {
	if lookup == nil {
		lookup = NoLookup{}
	}
	if len(batches) == 0 {
		return
	}

	plans := make([]*marshallgen.Plan, len(batches))
	rowsVals := make([]reflect.Value, len(batches))
	for i, batch := range batches {
		rv := reflect.ValueOf(batch)
		if rv.Kind() != reflect.Slice {
			err = eb.Build().Int("batch", i).Str("type", rv.Type().String()).Errorf("marshallreflect: MarshalStack batch must be a slice ([]TX)")
			return
		}
		elemT := rv.Type().Elem()
		plans[i], err = planForType(elemT)
		if err != nil {
			err = eb.Build().Int("batch", i).Str("type", elemT.String()).Errorf("marshallreflect: plan for batch %d: %w", i, err)
			return
		}
		rowsVals[i] = rv
	}

	if err = checkStackedPlainAgreement(plans); err != nil {
		return
	}
	numRows := rowsVals[0].Len()
	for i := 1; i < len(rowsVals); i++ {
		if rowsVals[i].Len() != numRows {
			err = eb.Build().Int("batch", i).Int("expected", numRows).Int("got", rowsVals[i].Len()).Errorf("marshallreflect: MarshalStack row count disagreement between batch 0 and batch %d", i)
			return
		}
	}

	dmlVal := reflect.ValueOf(dml)
	for r := 0; r < numRows; r++ {
		err = marshalStackRow(dmlVal, plans, rowsVals, r, lookup)
		if err != nil {
			err = eb.Build().Int("row", r).Errorf("marshallreflect: stacked row %d: %w", r, err)
			return
		}
	}
	return
}

// checkStackedPlainAgreement verifies that every plan declares the
// same plain-column set (same column→Go-type mapping). Order is not
// load-bearing — only the (column, type) pairs.
func checkStackedPlainAgreement(plans []*marshallgen.Plan) (err error) {
	if len(plans) <= 1 {
		return
	}
	ref := plainShapeOf(plans[0])
	for i := 1; i < len(plans); i++ {
		got := plainShapeOf(plans[i])
		if len(got) != len(ref) {
			err = eb.Build().Int("batch", i).Int("expectedCount", len(ref)).Int("gotCount", len(got)).Errorf("marshallreflect: plain-column count mismatch between batch 0 and batch %d", i)
			return
		}
		for col, refType := range ref {
			gotType, ok := got[col]
			if !ok {
				err = eb.Build().Int("batch", i).Str("column", col).Errorf("marshallreflect: batch %d missing plain column %q present in batch 0", i, col)
				return
			}
			if gotType != refType {
				err = eb.Build().Int("batch", i).Str("column", col).Str("expectedType", refType).Str("gotType", gotType).Errorf("marshallreflect: plain column type mismatch between batches")
				return
			}
		}
	}
	return
}

func plainShapeOf(plan *marshallgen.Plan) map[string]string {
	out := make(map[string]string, len(plan.PlainCols))
	for _, p := range plan.PlainCols {
		out[p.Column] = p.GoType
	}
	return out
}

func marshalStackRow(dml reflect.Value, plans []*marshallgen.Plan, rowsVals []reflect.Value, r int, lookup LookupI) (err error) {
	mustCall(dml, "BeginEntity")

	// Plain columns from the first plan + first batch's row r. The
	// agreement check guarantees the other batches would produce the
	// same values.
	firstRow := rowsVals[0].Index(r)
	err = marshalPlain(dml, firstRow, plans[0])
	if err != nil {
		return
	}

	// Per-DTO section emit, batches in caller-supplied order.
	for bi, plan := range plans {
		rowVal := rowsVals[bi].Index(r)
		groups := computeGroups(plan)
		for _, g := range groups {
			err = marshalSection(dml, rowVal, g, lookup)
			if err != nil {
				err = eb.Build().Int("batch", bi).Str("section", g.Section).Errorf("marshallreflect: batch %d section %s: %w", bi, g.Section, err)
				return
			}
		}
	}

	rets := mustCall(dml, "CommitEntity")
	if len(rets) == 1 && !rets[0].IsNil() {
		err = rets[0].Interface().(error)
	}
	return
}
