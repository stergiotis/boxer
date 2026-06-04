//go:build llm_generated_opus47

package marshallreflect

import (
	"reflect"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// Marshal drives `dml`'s reflected method chain to emit one entity
// per row of `rows`. `dml` is any pointer whose method set satisfies
// the same shape the marshallgen-emitted BuildEntities expects
// (BeginEntity / SetId / SetTimestamp / SetLifecycle / GetSection<X> /
// CommitEntity), with per-section method types matching the field
// shapes T uses.
//
// `lookup` resolves non-verbatim membership names to uint64 ids
// (typically a wrapper over a vdd-style registry). Pass NoLookup{} if
// every membership in T carries `,verbatim`.
//
// After Marshal returns, the caller drains via dml's own
// TransferRecords (or schema-specific equivalent) — wire bytes live
// outside this package.
func Marshal[T any](dml any, rows []T, lookup LookupI) (err error) {
	if lookup == nil {
		lookup = NoLookup{}
	}
	r, err := resolveForType(reflect.TypeOf((*T)(nil)).Elem())
	if err != nil {
		return
	}
	dmlVal := reflect.ValueOf(dml)
	for i := range rows {
		rowVal := reflect.ValueOf(rows[i])
		err = marshalRow(dmlVal, rowVal, r.plan, r.groups, lookup)
		if err != nil {
			err = eb.Build().Int("row", i).Errorf("row %d: %w", i, err)
			return
		}
	}
	return
}

func marshalRow(dml, row reflect.Value, plan *mappingplan.Plan, groups []mappingplan.SectionGroup, lookup LookupI) (err error) {
	mustCall(dml, "BeginEntity")
	err = marshalPlain(dml, row, plan)
	if err != nil {
		return
	}
	for _, g := range groups {
		err = marshalSection(dml, row, g, lookup, cardFilterAll)
		if err != nil {
			return
		}
	}
	rets := mustCall(dml, "CommitEntity")
	if len(rets) == 1 && !rets[0].IsNil() {
		err = rets[0].Interface().(error)
	}
	return
}

// marshalPlain drives the entity-header setters from the DTO's plain
// fields. Strict 1:1: each plain field's Go type already equals its
// setter's argument type, so the field value is passed verbatim — the
// codec inserts no conversion. SetId's arity follows the declared
// columns: SetId(id) when no naturalKey is declared, SetId(id,
// naturalKey) when it is.
func marshalPlain(dml, row reflect.Value, plan *mappingplan.Plan) (err error) {
	idCol := mappingplan.FindPlainCol(plan, "id")
	idArgs := []reflect.Value{row.FieldByName(idCol.GoField)}
	if nkCol := mappingplan.FindPlainCol(plan, "naturalKey"); nkCol != nil {
		idArgs = append(idArgs, row.FieldByName(nkCol.GoField))
	}
	mustCall(dml, "SetId", idArgs...)

	if tsCol := mappingplan.FindPlainCol(plan, "ts"); tsCol != nil {
		mustCall(dml, "SetTimestamp", row.FieldByName(tsCol.GoField))
	}
	if lcCol := mappingplan.FindPlainCol(plan, "expiresAt"); lcCol != nil {
		mustCall(dml, "SetLifecycle", row.FieldByName(lcCol.GoField))
	}
	return
}

func marshalSection(dml, row reflect.Value, g mappingplan.SectionGroup, lookup LookupI, filter cardFilter) (err error) {
	if !sectionHasMatchingField(row, g, filter) {
		return
	}
	method := mappingplan.UpperFirst(g.Section)
	sec := mustCall(dml, "GetSection"+method)[0]

	if len(g.SubColumns) > 1 {
		// Multi-sub-column attributes carry one tuple per row — treated
		// as single-value attributes for the cardFilter partition.
		if filter != cardFilterMultiValue {
			err = marshalMultiSubColumn(sec, row, g, lookup)
			if err != nil {
				return
			}
		}
		mustCall(sec, "EndSection")
		return
	}
	for _, f := range g.SubColumns[0].Fields {
		if !fieldEmitsForFilter(row, f, filter) {
			continue
		}
		err = marshalField(sec, row, f, lookup)
		if err != nil {
			return
		}
	}
	mustCall(sec, "EndSection")
	return
}

func marshalMultiSubColumn(sec, row reflect.Value, g mappingplan.SectionGroup, lookup LookupI) (err error) {
	if len(g.Memberships) != 1 {
		err = eb.Build().Str("section", g.Section).Errorf("multi-sub-column section with multiple memberships not supported")
		return
	}
	args := make([]reflect.Value, 0, len(g.SubColumns))
	for _, sc := range g.SubColumns {
		f := sc.Fields[0]
		args = append(args, row.FieldByName(f.GoFieldName))
	}
	attr := mustCall(sec, "BeginAttribute", args...)[0]
	err = addMembership(attr, row, g.Memberships[0], lookup)
	if err != nil {
		return
	}
	mustCall(attr, "EndAttributeP")
	return
}

func marshalField(sec, row reflect.Value, f mappingplan.TaggedField, lookup LookupI) (err error) {
	shape := mappingplan.ClassifyBegin(f)
	switch shape {
	case mappingplan.ShapeScalarBegin:
		err = marshalScalarOne(sec, row, f, lookup, "BeginAttribute")
	case mappingplan.ShapeScalarBeginSingle:
		err = marshalScalarOne(sec, row, f, lookup, "BeginAttributeSingle")
	case mappingplan.ShapeContainer:
		err = marshalContainer(sec, row, f, lookup)
	case mappingplan.ShapeExplodeBegin:
		err = marshalExplode(sec, row, f, lookup, "BeginAttribute")
	case mappingplan.ShapeExplodeBeginSingle:
		err = marshalExplode(sec, row, f, lookup, "BeginAttributeSingle")
	}
	return
}

func marshalScalarOne(sec, row reflect.Value, f mappingplan.TaggedField, lookup LookupI, beginMethod string) (err error) {
	// Const: literal value, no Go-field read.
	if f.IsConst {
		attr := mustCall(sec, beginMethod, reflect.ValueOf(f.ConstValue))[0]
		err = addMembership(attr, row, f, lookup)
		if err != nil {
			return
		}
		mustCall(attr, "EndAttributeP")
		return
	}
	// Option: emit only when Has is true.
	if f.IsOption {
		fld := row.FieldByName(f.GoFieldName)
		if !fld.FieldByName("Has").Bool() {
			return
		}
		val := reslicedIfFixedByte(fld.FieldByName("Val"), f)
		attr := mustCall(sec, beginMethod, val)[0]
		err = addMembership(attr, row, f, lookup)
		if err != nil {
			return
		}
		mustCall(attr, "EndAttributeP")
		return
	}
	// Scalar T.
	val := reslicedIfFixedByte(row.FieldByName(f.GoFieldName), f)
	attr := mustCall(sec, beginMethod, val)[0]
	err = addMembership(attr, row, f, lookup)
	if err != nil {
		return
	}
	mustCall(attr, "EndAttributeP")
	return
}

func marshalContainer(sec, row reflect.Value, f mappingplan.TaggedField, lookup LookupI) (err error) {
	switch {
	case f.IsRoaring:
		bm := row.FieldByName(f.GoFieldName)
		if bm.IsNil() {
			return
		}
		if isEmpty := mustCall(bm, "IsEmpty")[0].Bool(); isEmpty {
			return
		}
		attr := mustCall(sec, "BeginAttribute")[0]
		it := mustCall(bm, "Iterator")[0]
		for mustCall(it, "HasNext")[0].Bool() {
			v := mustCall(it, "Next")[0]
			mustCall(attr, "AddToContainerP", v)
		}
		err = addMembership(attr, row, f, lookup)
		if err != nil {
			return
		}
		mustCall(attr, "EndAttributeP")
	case f.IsSlice:
		fld := row.FieldByName(f.GoFieldName)
		if fld.Len() == 0 {
			return
		}
		attr := mustCall(sec, "BeginAttribute")[0]
		for i := 0; i < fld.Len(); i++ {
			v := reslicedIfFixedByte(fld.Index(i), f)
			mustCall(attr, "AddToContainerP", v)
		}
		err = addMembership(attr, row, f, lookup)
		if err != nil {
			return
		}
		mustCall(attr, "EndAttributeP")
	default:
		err = eb.Build().Str("field", f.GoFieldName).Errorf("container shape on non-slice / non-roaring field")
	}
	return
}

func marshalExplode(sec, row reflect.Value, f mappingplan.TaggedField, lookup LookupI, beginMethod string) (err error) {
	switch {
	case f.IsRoaring:
		bm := row.FieldByName(f.GoFieldName)
		if bm.IsNil() {
			return
		}
		it := mustCall(bm, "Iterator")[0]
		for mustCall(it, "HasNext")[0].Bool() {
			v := mustCall(it, "Next")[0]
			attr := mustCall(sec, beginMethod, v)[0]
			err = addMembership(attr, row, f, lookup)
			if err != nil {
				return
			}
			mustCall(attr, "EndAttributeP")
		}
	case f.IsSlice:
		fld := row.FieldByName(f.GoFieldName)
		for i := 0; i < fld.Len(); i++ {
			v := reslicedIfFixedByte(fld.Index(i), f)
			attr := mustCall(sec, beginMethod, v)[0]
			err = addMembership(attr, row, f, lookup)
			if err != nil {
				return
			}
			mustCall(attr, "EndAttributeP")
		}
	default:
		err = eb.Build().Str("field", f.GoFieldName).Errorf("explode shape on non-slice / non-roaring field")
	}
	return
}

// addMembership pushes the per-attribute membership, dispatching on the
// field's MembershipChannel. The Verbatim pair embeds the lw: tag name
// as []byte; the Ref pair pushes the Lookup-resolved uint64. The
// parametrized / mixed channels are rejected upstream by SplitLW, so
// this function never sees them.
func addMembership(attr, row reflect.Value, f mappingplan.TaggedField, lookup LookupI) (err error) {
	ch := f.Flags.Channel
	method := "AddMembership" + ch.AddMethodSuffix() + "P"
	// Carrier channels (Cut-2): the membership-side data is per-row, read
	// from the sibling carrier field rather than from a lookup or a literal
	// lw: name.
	if ch.UsesCarrier() {
		// Per-row membership data from the sibling carrier. Mixed channels
		// pass (value field Id/Name, Params); parametrized channels — whose
		// membership is the opaque blob alone — pass (Params) only. The
		// method suffix already selects the right AddMembership…P.
		carrier := row.FieldByName(f.CarrierField)
		if vf := ch.CarrierValueField(); vf != "" {
			mustCall(attr, method, carrier.FieldByName(vf), carrier.FieldByName("Params"))
		} else {
			mustCall(attr, method, carrier.FieldByName("Params"))
		}
		return
	}
	if ch.EmbedsLiteralName() {
		mustCall(attr, method, reflect.ValueOf([]byte(f.LWMembership)))
		return
	}
	id, lookupErr := lookup.LookupMembership(f.LWMembership)
	if lookupErr != nil {
		err = eb.Build().Str("membership", f.LWMembership).Errorf("%w", lookupErr)
		return
	}
	mustCall(attr, method, reflect.ValueOf(id))
	return
}

// reslicedIfFixedByte converts a [N]byte field value to a []byte
// slice reference, mirroring marshallgen's blobSliceMaybe. Returns
// the value unchanged for any other shape.
func reslicedIfFixedByte(v reflect.Value, f mappingplan.TaggedField) reflect.Value {
	if mappingplan.IsFixedByteArray(f.GoType) {
		// Take address-of element 0 + slice — reflect lacks a direct
		// "convert array to slice" but Slice(v, 0, len) works on
		// addressable arrays. Field values via FieldByName are not
		// addressable; copy into a new []byte instead.
		out := make([]byte, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = byte(v.Index(i).Uint())
		}
		return reflect.ValueOf(out)
	}
	return v
}

// mustCall is the reflect.Value.MethodByName(name).Call(args...)
// shortcut. Panics if the method doesn't exist — DTOs whose target
// DML doesn't satisfy the codec contract should fail fast and noisy.
func mustCall(recv reflect.Value, name string, args ...reflect.Value) (rets []reflect.Value) {
	m := recv.MethodByName(name)
	if !m.IsValid() {
		panic(eb.Build().Str("method", name).Str("recv", recv.Type().String()).Errorf("target DML does not have method %s", name))
	}
	rets = m.Call(args)
	return
}
