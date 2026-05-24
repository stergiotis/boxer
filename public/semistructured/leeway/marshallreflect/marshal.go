//go:build llm_generated_opus47

package marshallreflect

import (
	"reflect"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
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
	plan, err := PlanFor[T]()
	if err != nil {
		return
	}
	dmlVal := reflect.ValueOf(dml)
	for i := range rows {
		rowVal := reflect.ValueOf(rows[i])
		err = marshalRow(dmlVal, rowVal, plan, lookup)
		if err != nil {
			err = eb.Build().Int("row", i).Errorf("marshallreflect: row %d: %w", i, err)
			return
		}
	}
	return
}

func marshalRow(dml, row reflect.Value, plan *marshallgen.Plan, lookup LookupI) (err error) {
	mustCall(dml, "BeginEntity")
	err = marshalPlain(dml, row, plan)
	if err != nil {
		return
	}
	groups := computeGroups(plan)
	for _, g := range groups {
		err = marshalSection(dml, row, g, lookup)
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

func marshalPlain(dml, row reflect.Value, plan *marshallgen.Plan) (err error) {
	idCol := findPlainCol(plan, "id")
	nkCol := findPlainCol(plan, "naturalKey")
	tsCol := findPlainCol(plan, "ts")
	lcCol := findPlainCol(plan, "expiresAt")

	idVal := row.FieldByName(idCol.GoField)
	var nkVal reflect.Value
	if nkCol == nil {
		nkVal = reflect.ValueOf([]byte(nil))
	} else {
		v := row.FieldByName(nkCol.GoField)
		switch nkCol.GoType {
		case "[]byte":
			nkVal = v
		case "string":
			nkVal = reflect.ValueOf([]byte(v.String()))
		default:
			err = eb.Build().Str("type", nkCol.GoType).Errorf("naturalKey unsupported")
			return
		}
	}
	mustCall(dml, "SetId", idVal, nkVal)

	if tsCol != nil {
		var tsVal reflect.Value
		tsVal, err = plainTimeReflect(row, tsCol)
		if err != nil {
			return
		}
		mustCall(dml, "SetTimestamp", tsVal)
	}
	if lcCol != nil {
		var lcVal reflect.Value
		lcVal, err = plainTimeReflect(row, lcCol)
		if err != nil {
			return
		}
		mustCall(dml, "SetLifecycle", lcVal)
	}
	return
}

func plainTimeReflect(row reflect.Value, p *marshallgen.PlainCol) (out reflect.Value, err error) {
	v := row.FieldByName(p.GoField)
	switch p.GoType {
	case "time.Time":
		out = v
	case "int64":
		out = reflect.ValueOf(time.Unix(0, v.Int()).UTC())
	default:
		err = eb.Build().Str("type", p.GoType).Errorf("plain time column unsupported")
	}
	return
}

func marshalSection(dml, row reflect.Value, g sectionGroup, lookup LookupI) (err error) {
	method := upperFirst(g.Section)
	sec := mustCall(dml, "GetSection"+method)[0]

	if len(g.SubColumns) > 1 {
		err = marshalMultiSubColumn(sec, row, g, lookup)
		if err != nil {
			return
		}
		mustCall(sec, "EndSection")
		return
	}
	for _, f := range g.SubColumns[0].Fields {
		err = marshalField(sec, row, f, lookup)
		if err != nil {
			return
		}
	}
	mustCall(sec, "EndSection")
	return
}

func marshalMultiSubColumn(sec, row reflect.Value, g sectionGroup, lookup LookupI) (err error) {
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
	err = addMembership(attr, g.Memberships[0], lookup)
	if err != nil {
		return
	}
	mustCall(attr, "EndAttributeP")
	return
}

func marshalField(sec, row reflect.Value, f marshallgen.TaggedField, lookup LookupI) (err error) {
	shape := classifyBegin(f)
	switch shape {
	case shapeScalarBegin:
		err = marshalScalarOne(sec, row, f, lookup, "BeginAttribute")
	case shapeScalarBeginSingle:
		err = marshalScalarOne(sec, row, f, lookup, "BeginAttributeSingle")
	case shapeContainer:
		err = marshalContainer(sec, row, f, lookup)
	case shapeExplodeBegin:
		err = marshalExplode(sec, row, f, lookup, "BeginAttribute")
	case shapeExplodeBeginSingle:
		err = marshalExplode(sec, row, f, lookup, "BeginAttributeSingle")
	}
	return
}

func marshalScalarOne(sec, row reflect.Value, f marshallgen.TaggedField, lookup LookupI, beginMethod string) (err error) {
	// Const: literal value, no Go-field read.
	if f.IsConst {
		attr := mustCall(sec, beginMethod, reflect.ValueOf(f.ConstValue))[0]
		err = addMembership(attr, f, lookup)
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
		err = addMembership(attr, f, lookup)
		if err != nil {
			return
		}
		mustCall(attr, "EndAttributeP")
		return
	}
	// Scalar T.
	val := reslicedIfFixedByte(row.FieldByName(f.GoFieldName), f)
	attr := mustCall(sec, beginMethod, val)[0]
	err = addMembership(attr, f, lookup)
	if err != nil {
		return
	}
	mustCall(attr, "EndAttributeP")
	return
}

func marshalContainer(sec, row reflect.Value, f marshallgen.TaggedField, lookup LookupI) (err error) {
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
		err = addMembership(attr, f, lookup)
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
		err = addMembership(attr, f, lookup)
		if err != nil {
			return
		}
		mustCall(attr, "EndAttributeP")
	default:
		err = eb.Build().Str("field", f.GoFieldName).Errorf("container shape on non-slice / non-roaring field")
	}
	return
}

func marshalExplode(sec, row reflect.Value, f marshallgen.TaggedField, lookup LookupI, beginMethod string) (err error) {
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
			err = addMembership(attr, f, lookup)
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
			err = addMembership(attr, f, lookup)
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

// addMembership pushes the per-attribute membership, choosing the
// AddMembershipLowCardVerbatimP([]byte) form when the field's
// Verbatim flag is set, otherwise the AddMembershipLowCardRefP(id)
// form with id resolved via the lookup.
func addMembership(attr reflect.Value, f marshallgen.TaggedField, lookup LookupI) (err error) {
	if f.Flags.Verbatim {
		mustCall(attr, "AddMembershipLowCardVerbatimP", reflect.ValueOf([]byte(f.LWMembership)))
		return
	}
	id, lookupErr := lookup.LookupMembership(f.LWMembership)
	if lookupErr != nil {
		err = eb.Build().Str("membership", f.LWMembership).Errorf("marshallreflect: %w", lookupErr)
		return
	}
	mustCall(attr, "AddMembershipLowCardRefP", reflect.ValueOf(id))
	return
}

// reslicedIfFixedByte converts a [N]byte field value to a []byte
// slice reference, mirroring marshallgen's blobSliceMaybe. Returns
// the value unchanged for any other shape.
func reslicedIfFixedByte(v reflect.Value, f marshallgen.TaggedField) reflect.Value {
	if f.GoType == "[4]byte" || f.GoType == "[16]byte" {
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
		panic(eb.Build().Str("method", name).Str("recv", recv.Type().String()).Errorf("marshallreflect: target DML does not have method %s", name))
	}
	rets = m.Call(args)
	return
}
