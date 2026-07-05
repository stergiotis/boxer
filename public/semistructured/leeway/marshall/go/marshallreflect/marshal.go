package marshallreflect

import (
	"reflect"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
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

func marshalRow(dml, row reflect.Value, plan *mappingplan.Plan, groups []goplan.SectionGroup, lookup LookupI) (err error) {
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
	idCol := goplan.FindPlainCol(plan, "id")
	idArgs := []reflect.Value{row.FieldByName(idCol.GoField)}
	if nkCol := goplan.FindPlainCol(plan, "naturalKey"); nkCol != nil {
		idArgs = append(idArgs, row.FieldByName(nkCol.GoField))
	}
	mustCall(dml, "SetId", idArgs...)

	if tsCol := goplan.FindPlainCol(plan, "ts"); tsCol != nil {
		mustCall(dml, "SetTimestamp", row.FieldByName(tsCol.GoField))
	}
	if lcCol := goplan.FindPlainCol(plan, "expiresAt"); lcCol != nil {
		mustCall(dml, "SetLifecycle", row.FieldByName(lcCol.GoField))
	}
	return
}

func marshalSection(dml, row reflect.Value, g goplan.SectionGroup, lookup LookupI, filter cardFilter) (err error) {
	if !sectionHasMatchingField(row, g, filter) {
		return
	}
	method := mappingplan.UpperFirst(g.Section)
	sec := mustCall(dml, "GetSection"+method)[0]

	if ts, ok := g.TupleSpec(); ok {
		// Dynamic-membership tuple: one attribute per element of the outer
		// slice, each with its own membership (ADR-0103). Dispatched before
		// the sub-column-count split — a tuple may have any S + C ≥ 1.
		err = marshalTupleSection(sec, row, g, ts, filter)
		if err != nil {
			return
		}
		mustCall(sec, "EndSection")
		return
	}

	if len(g.SubColumns) > 1 {
		// One tuple attribute per row; the shared container length drives
		// the cardinality pass (N ≤ 1 single-value, N > 1 multi-value) and
		// the S = 0 splice — ADR-0101 D7/D2. All-scalar tuples remain
		// single-value.
		if multiSubColumnEmitsForFilter(row, g, filter) {
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

// marshalMultiSubColumn emits a multi-sub-column section's one tuple
// attribute: BeginAttribute(<scalar sub-columns…>) plus the zipped
// co-containers via AddToContainerP / AddToCoContainersP, one call per
// element (ADR-0101 D1/D4). The call sequence mirrors marshallgen's
// writeMultiSubColumnDriver exactly — the byte-identity invariant
// between the two front-ends rests on it.
func marshalMultiSubColumn(sec, row reflect.Value, g goplan.SectionGroup, lookup LookupI) (err error) {
	if len(g.Memberships) != 1 {
		err = eb.Build().Str("section", g.Section).Errorf("multi-sub-column section with multiple memberships not supported")
		return
	}
	scalars := g.ScalarSubColumns()
	containers := g.ContainerSubColumns()

	args := make([]reflect.Value, 0, len(scalars))
	for _, sc := range scalars {
		args = append(args, reslicedIfFixedByte(row.FieldByName(sc.Fields[0].GoFieldName), sc.Fields[0]))
	}

	// Zip-length agreement across the container class: every container
	// advances in lockstep, so unequal lengths are a caller bug surfaced
	// as an error, never silent truncation (ADR-0101 D2).
	containerVals := make([]reflect.Value, len(containers))
	n := 0
	for j, sc := range containers {
		containerVals[j] = row.FieldByName(sc.Fields[0].GoFieldName)
		if j == 0 {
			n = containerVals[0].Len()
			continue
		}
		if containerVals[j].Len() != n {
			err = eb.Build().Str("section", g.Section).Str("field", sc.Fields[0].GoFieldName).Int("len", containerVals[j].Len()).Int("firstLen", n).Errorf("co-container slices have different lengths")
			return
		}
	}

	attr := mustCall(sec, "BeginAttribute", args...)[0]
	if len(containers) > 0 {
		addMethod := goplan.ContainerAddMethod(len(containers)) + "P"
		elemArgs := make([]reflect.Value, len(containers))
		for k := 0; k < n; k++ {
			for j := range containerVals {
				elemArgs[j] = reslicedIfFixedByte(containerVals[j].Index(k), containers[j].Fields[0])
			}
			mustCall(attr, addMethod, elemArgs...)
		}
	}
	err = addMembership(attr, row, g.Memberships[0], lookup, reflect.Value{})
	if err != nil {
		return
	}
	mustCall(attr, "EndAttributeP")
	return
}

// marshalTupleSection emits a dynamic-membership tuple section (ADR-0103,
// extended by ADR-0109): one attribute per element of the outer slice, in
// element order — BeginAttribute(<scalar sub-columns…>), the zipped
// co-containers, then the element's memberships, EndAttributeP. Each element
// emits one AddMembership<Channel>P call per `@membership` field (declaration
// order) — one per slice element for a repeated field — so an attribute may
// carry several memberships (`membership-card > 1`) on possibly heterogeneous
// channels; a ref channel passes the uint64 id directly, a verbatim channel the
// []byte name. The call sequence mirrors marshallgen's writeTupleSectionDriver
// exactly — the byte-identity invariant between the two front-ends rests on it.
// An element always emits (its presence in the slice is the signal — there is
// no per-element splice); zero elements emit zero attributes.
func marshalTupleSection(sec, row reflect.Value, g goplan.SectionGroup, ts goplan.TupleSpec, filter cardFilter) (err error) {
	scalars := g.ScalarSubColumns()
	containers := g.ContainerSubColumns()
	addMethod := ""
	if len(containers) > 0 {
		addMethod = goplan.ContainerAddMethod(len(containers)) + "P"
	}

	elems := row.FieldByName(ts.GoField)
	containerVals := make([]reflect.Value, len(containers))
	args := make([]reflect.Value, 0, len(scalars))
	elemArgs := make([]reflect.Value, len(containers))
	for e := 0; e < elems.Len(); e++ {
		elem := elems.Index(e)
		// Zip-length agreement across the container class, per element —
		// checked before the cardinality filter so a mis-zipped element is
		// an error on every RowComposer pass, not only the one it emits in.
		n := 0
		for j, sc := range containers {
			containerVals[j] = elem.FieldByName(sc.Fields[0].GoFieldName)
			if j == 0 {
				n = containerVals[0].Len()
				continue
			}
			if containerVals[j].Len() != n {
				err = eb.Build().Str("section", g.Section).Str("field", sc.Fields[0].GoFieldName).Int("element", e).Int("len", containerVals[j].Len()).Int("firstLen", n).Errorf("co-container slices have different lengths")
				return
			}
		}
		if !tupleElemCardMatches(n, filter) {
			continue
		}
		args = args[:0]
		for _, sc := range scalars {
			args = append(args, reslicedIfFixedByte(elem.FieldByName(sc.Fields[0].GoFieldName), sc.Fields[0]))
		}
		attr := mustCall(sec, "BeginAttribute", args...)[0]
		// n > 0 implies at least one container sub-column exists.
		for k := 0; k < n; k++ {
			for j := range containerVals {
				elemArgs[j] = reslicedIfFixedByte(containerVals[j].Index(k), containers[j].Fields[0])
			}
			mustCall(attr, addMethod, elemArgs...)
		}
		for _, m := range ts.Memberships {
			method := "AddMembership" + m.Channel.AddMethodSuffix() + "P"
			mf := elem.FieldByName(m.GoField)
			if m.IsSlice {
				for k := 0; k < mf.Len(); k++ {
					mustCall(attr, method, tupleMembArg(mf.Index(k), m))
				}
			} else {
				mustCall(attr, method, tupleMembArg(mf, m))
			}
		}
		mustCall(attr, "EndAttributeP")
	}
	return
}

// tupleMembArg converts a tuple element's membership value to the argument the
// AddMembership<Channel>P method takes: a []byte name for a verbatim channel
// (a string field re-cast to []byte; a []byte field passed as-is), or the
// uint64 id directly for a ref channel. v is the field value, or one element of
// a repeated (slice) membership field.
func tupleMembArg(v reflect.Value, m mappingplan.TupleMembership) reflect.Value {
	if m.Channel.EmbedsLiteralName() && m.GoType == "string" {
		return reflect.ValueOf([]byte(v.String()))
	}
	return v
}

// tupleElemCardMatches classifies one tuple element by its shared
// container length n for the RowComposer cardinality passes: n ≤ 1
// single-value, n > 1 multi-value — the runtime-cardinality rule the
// static mixed-shape tuple already follows (ADR-0101 D7), applied at
// element grain. All-scalar tuple elements have n = 0, always
// single-value.
func tupleElemCardMatches(n int, filter cardFilter) bool {
	switch filter {
	case cardFilterSingleValue:
		return n <= 1
	case cardFilterMultiValue:
		return n > 1
	default:
		return true
	}
}

// multiSubColumnEmitsForFilter reports whether the section's tuple
// attribute emits for the row under the cardinality filter. The shared
// container length N classifies the attribute (N ≤ 1 single-value,
// N > 1 multi-value; all-scalar tuples are N = 0); an all-container
// tuple with every container empty is spliced entirely (ADR-0101 D2/D7).
// sectionHasMatchingField and marshalSection both consult this one
// predicate so the frame decision and the emit cannot drift.
func multiSubColumnEmitsForFilter(row reflect.Value, g goplan.SectionGroup, filter cardFilter) bool {
	containers := g.ContainerSubColumns()
	n := 0
	anyElems := false
	for j, sc := range containers {
		l := row.FieldByName(sc.Fields[0].GoFieldName).Len()
		if j == 0 {
			n = l
		}
		if l > 0 {
			anyElems = true
		}
	}
	if len(containers) > 0 && len(g.ScalarSubColumns()) == 0 && !anyElems {
		return false // S = 0 splice — no attribute at all
	}
	switch filter {
	case cardFilterSingleValue:
		return n <= 1
	case cardFilterMultiValue:
		return n > 1
	default:
		return true
	}
}

func marshalField(sec, row reflect.Value, f mappingplan.TaggedField, lookup LookupI) (err error) {
	shape := goplan.ClassifyBegin(f)
	switch shape {
	case goplan.ShapeScalarBegin:
		err = marshalScalarOne(sec, row, f, lookup, "BeginAttribute")
	case goplan.ShapeScalarBeginSingle:
		err = marshalScalarOne(sec, row, f, lookup, "BeginAttributeSingle")
	case goplan.ShapeContainer:
		err = marshalContainer(sec, row, f, lookup)
	case goplan.ShapeExplodeBegin:
		err = marshalExplode(sec, row, f, lookup, "BeginAttribute")
	case goplan.ShapeExplodeBeginSingle:
		err = marshalExplode(sec, row, f, lookup, "BeginAttributeSingle")
	}
	return
}

func marshalScalarOne(sec, row reflect.Value, f mappingplan.TaggedField, lookup LookupI, beginMethod string) (err error) {
	// Resolve the BeginAttribute value per shape, then run the one shared
	// begin/addMembership/end tail. Option with Has=false emits nothing
	// (splice semantics).
	var val reflect.Value
	switch {
	case f.IsConst:
		val = reflect.ValueOf(f.ConstValue) // literal value, no Go-field read
	case f.IsOption:
		fld := row.FieldByName(f.GoFieldName)
		if !fld.FieldByName("Has").Bool() {
			return
		}
		val = reslicedIfFixedByte(fld.FieldByName("Val"), f)
	default:
		val = reslicedIfFixedByte(row.FieldByName(f.GoFieldName), f)
	}
	attr := mustCall(sec, beginMethod, val)[0]
	if err = addMembership(attr, row, f, lookup, reflect.Value{}); err != nil {
		return
	}
	mustCall(attr, "EndAttributeP")
	return
}

func marshalContainer(sec, row reflect.Value, f mappingplan.TaggedField, lookup LookupI) (err error) {
	switch {
	case f.IsRoaring():
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
		// One carrier (scalar) for the whole container attribute, if any.
		err = addMembership(attr, row, f, lookup, reflect.Value{})
		if err != nil {
			return
		}
		mustCall(attr, "EndAttributeP")
	case f.IsSlice():
		fld := row.FieldByName(f.GoFieldName)
		if fld.Len() == 0 {
			return
		}
		attr := mustCall(sec, "BeginAttribute")[0]
		for i := 0; i < fld.Len(); i++ {
			v := reslicedIfFixedByte(fld.Index(i), f)
			mustCall(attr, "AddToContainerP", v)
		}
		err = addMembership(attr, row, f, lookup, reflect.Value{})
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
	case f.IsRoaring():
		// Roaring carriers are rejected by PlanBuilder, so there is never a
		// carrier to index in this arm.
		bm := row.FieldByName(f.GoFieldName)
		if bm.IsNil() {
			return
		}
		it := mustCall(bm, "Iterator")[0]
		for mustCall(it, "HasNext")[0].Bool() {
			v := mustCall(it, "Next")[0]
			attr := mustCall(sec, beginMethod, v)[0]
			err = addMembership(attr, row, f, lookup, reflect.Value{})
			if err != nil {
				return
			}
			mustCall(attr, "EndAttributeP")
		}
	case f.IsSlice():
		fld := row.FieldByName(f.GoFieldName)
		// A slice carrier pairs element-wise with the value slice; the two are
		// independent Go fields, so their lengths must agree at runtime.
		var carrierFld reflect.Value
		if f.CarrierField != "" {
			carrierFld = row.FieldByName(f.CarrierField)
			if carrierFld.Len() != fld.Len() {
				err = eb.Build().Str("field", f.GoFieldName).Int("valueLen", fld.Len()).Int("carrierLen", carrierFld.Len()).Errorf("explode value and carrier slices have different lengths")
				return
			}
		}
		for i := 0; i < fld.Len(); i++ {
			v := reslicedIfFixedByte(fld.Index(i), f)
			attr := mustCall(sec, beginMethod, v)[0]
			carrierElem := reflect.Value{}
			if carrierFld.IsValid() {
				carrierElem = carrierFld.Index(i)
			}
			err = addMembership(attr, row, f, lookup, carrierElem)
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
// field's MembershipChannel. Carrier channels (UsesCarrier) read the
// membership-side data from the sibling carrier — handled first below.
// Otherwise the Verbatim pair embeds the lw: tag name as []byte and the Ref
// pair pushes the Lookup-resolved uint64.
//
// carrierElem selects the carrier struct: an invalid (zero) Value reads the
// scalar carrier field from the row (scalar / Option / container values); a
// valid Value is one element of a slice carrier, supplied per-element by the
// explode path.
func addMembership(attr, row reflect.Value, f mappingplan.TaggedField, lookup LookupI, carrierElem reflect.Value) (err error) {
	ch := f.Flags.Channel
	method := "AddMembership" + ch.AddMethodSuffix() + "P"
	// Carrier channels (Cut-2): the membership-side data is per-row, read
	// from the sibling carrier rather than from a lookup or a literal lw: name.
	if ch.UsesCarrier() {
		// Per-row membership data from the sibling carrier. Mixed channels
		// pass (value field Id/Name, Params); parametrized channels — whose
		// membership is the opaque blob alone — pass (Params) only. The
		// method suffix already selects the right AddMembership…P.
		carrier := carrierElem
		if !carrier.IsValid() {
			carrier = row.FieldByName(f.CarrierField)
		}
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
	if goplan.IsFixedByteArray(f.GoType()) {
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
