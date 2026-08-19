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
//
// A dml missing a method the plan drives is returned as an error, not raised as
// a panic; call Validate[T](dml) first to get every mismatch at once instead of
// the first one reached.
func Marshal[T any](dml any, rows []T, lookup LookupI) (err error) {
	defer recoverContract(&err)
	if lookup == nil {
		lookup = NoLookup{}
	}
	r, err := resolveForType(reflect.TypeFor[T]())
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

func marshalSection(dml, row reflect.Value, g goplan.SectionGroup, lookup LookupI) (err error) {
	sec := openSection(dml, g.Section)
	if err = emitSectionAttributes(sec, row, g, lookup); err != nil {
		return
	}
	closeSection(sec)
	return
}

// openSection / closeSection are the section FRAME, split from the emit so a
// caller can open a section once and write several DTOs' attributes into it
// (RowComposer). A section frame is opened once per entity and closed once —
// the generated DML does not reopen one — so anything wanting to contribute to
// a section another DTO also touches has to share the frame rather than take
// its own (ADR-0146 D6).
func openSection(dml reflect.Value, section string) reflect.Value {
	return mustCall(dml, "GetSection"+mappingplan.UpperFirst(section))[0]
}

func closeSection(sec reflect.Value) {
	mustCall(sec, "EndSection")
}

// emitSectionAttributes writes one row's attributes for a section into an
// ALREADY-OPEN frame. It never opens or closes the frame, so it composes.
func emitSectionAttributes(sec, row reflect.Value, g goplan.SectionGroup, lookup LookupI) (err error) {
	if ts, ok := g.TupleSpec(); ok {
		// Dynamic-membership tuple: one attribute per element of the outer
		// slice, each with its own membership (ADR-0103). Dispatched before
		// the sub-column-count split — a tuple may have any S + C ≥ 1.
		return marshalTupleSection(sec, row, g, ts, lookup)
	}

	if len(g.SubColumns) > 1 {
		// One tuple attribute per row, unless the S = 0 splice applies
		// (ADR-0101 D2).
		if multiSubColumnEmits(row, g) {
			return marshalMultiSubColumn(sec, row, g, lookup)
		}
		return
	}

	for _, f := range g.SubColumns[0].Fields {
		if err = marshalField(sec, row, f, lookup); err != nil {
			return
		}
	}
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
	err = addMembership(attr, row, g.Memberships[0], lookup)
	if err != nil {
		return
	}
	mustCall(attr, "EndAttributeP")
	return
}

// tupleRowElements returns the element reflect.Values a row contributes to a
// tuple-family section, by cardinality: Many → each element of the outer slice;
// One → the struct value once; Optional → zero-or-one (Slice-A Step 3, currently
// none).
func tupleRowElements(row reflect.Value, ts goplan.TupleSpec) []reflect.Value {
	fld := row.FieldByName(ts.GoField)
	switch ts.Cardinality {
	case mappingplan.AttrCardinalityOne:
		return []reflect.Value{fld}
	case mappingplan.AttrCardinalityOptional:
		// *S (nil ⇒ absent) or option.Option[S] (Has=false ⇒ absent).
		if fld.Kind() == reflect.Pointer {
			if fld.IsNil() {
				return nil
			}
			return []reflect.Value{fld.Elem()}
		}
		if !fld.FieldByName("Has").Bool() {
			return nil
		}
		return []reflect.Value{fld.FieldByName("Val")}
	default: // AttrCardinalityMany — the dynamic-membership tuple.
		out := make([]reflect.Value, fld.Len())
		for e := range out {
			out[e] = fld.Index(e)
		}
		return out
	}
}

// marshalTupleSection emits a tuple-family section — either a
// dynamic-membership tuple (ADR-0103/0109) or a nested static-membership section
// (Slice A). One attribute is emitted per element the row contributes:
// BeginAttribute(<scalar sub-columns…>), the zipped co-containers, the
// membership(s), then EndAttributeP. The call sequence mirrors marshallgen's
// writeTupleSectionDriver / the flat multi-sub-column driver exactly — the
// byte-identity invariant rests on it.
//
// Two axes generalise the original dynamic tuple:
//
//   - Cardinality (ts.Cardinality) fixes how many elements the row contributes:
//     Many → each element of the outer slice; One → the struct value once. (An
//     element always emits — its presence is the signal; zero elements emit
//     zero attributes. Optional cardinality and the all-container S=0 splice are
//     added in later Slice-A steps.)
//   - Membership source: a DYNAMIC tuple (ts.Memberships non-empty) emits one
//     AddMembership<Channel>P per `@membership` field, the value carried
//     directly. A STATIC nested section (ts.Memberships empty) resolves its one
//     membership through addMembership — the ref lookup / verbatim literal —
//     exactly like a flat section, NOT the raw per-element path.
func marshalTupleSection(sec, row reflect.Value, g goplan.SectionGroup, ts goplan.TupleSpec, lookup LookupI) (err error) {
	scalars := g.ScalarSubColumns()
	containers := g.ContainerSubColumns()
	addMethod := ""
	if len(containers) > 0 {
		addMethod = goplan.ContainerAddMethod(len(containers)) + "P"
	}

	elems := tupleRowElements(row, ts)

	containerVals := make([]reflect.Value, len(containers))
	args := make([]reflect.Value, 0, len(scalars))
	elemArgs := make([]reflect.Value, len(containers))
	for e, elem := range elems {
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
		// S=0 splice (H2): a One / Optional all-container element whose
		// containers are all empty emits zero attributes — matching the flat
		// multi-sub-column / single-container S=0 rule. A Many (dynamic-tuple)
		// element always emits: its slice presence is the signal.
		if ts.Cardinality != mappingplan.AttrCardinalityMany && len(scalars) == 0 && n == 0 {
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
		if len(ts.Memberships) == 0 {
			// STATIC nested section: one membership from the section tag,
			// resolved (ref lookup / verbatim literal) exactly like a flat
			// multi-sub-column section.
			if err = addMembership(attr, row, g.Memberships[0], lookup); err != nil {
				return
			}
		} else {
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
		}
		mustCall(attr, "EndAttributeP")
	}
	return
}

var uint64Type = reflect.TypeFor[uint64]()

// tupleMembArg converts a tuple element's membership value to the argument the
// AddMembership<Channel>P method takes: a []byte name for a verbatim channel
// (a string field / newtype re-cast to []byte; a []byte field passed as-is), or
// the uint64 id for a ref channel. v is the field value, or one element of a
// repeated (slice) membership field. It bridges the nested-model marker newtypes
// (lw.Ref / lw.Verbatim) whose Go type differs from the DML method's plain type.
func tupleMembArg(v reflect.Value, m mappingplan.TupleMembership) reflect.Value {
	if m.Channel.EmbedsLiteralName() {
		if m.GoType == "string" {
			return reflect.ValueOf([]byte(v.String()))
		}
		return v // []byte
	}
	// ref: a plain uint64 id. Convert an lw.Ref marker newtype (a no-op for a
	// plain uint64 field).
	return v.Convert(uint64Type)
}

// multiSubColumnEmits reports whether the section's tuple attribute emits for
// the row: an all-container tuple whose containers are all empty is spliced
// entirely (the S = 0 rule, ADR-0101 D2). Anything else emits exactly one
// attribute.
func multiSubColumnEmits(row reflect.Value, g goplan.SectionGroup) bool {
	if len(g.ScalarSubColumns()) > 0 {
		return true
	}
	for _, sc := range g.ContainerSubColumns() {
		if row.FieldByName(sc.Fields[0].GoFieldName).Len() > 0 {
			return true
		}
	}
	// No scalar sub-column and every container empty — nothing to emit.
	return len(g.ContainerSubColumns()) == 0
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
		// unwrapLwSingle unwraps an lw.Single[T] to its scalar; a no-op otherwise.
		val = reslicedIfFixedByte(unwrapLwSingle(row.FieldByName(f.GoFieldName)), f)
	}
	attr := mustCall(sec, beginMethod, val)[0]
	if err = addMembership(attr, row, f, lookup); err != nil {
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
		err = addMembership(attr, row, f, lookup)
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

// addMembership pushes the per-attribute membership, dispatching on the
// field's MembershipChannel. Carrier channels (UsesCarrier) read the
// membership-side data from the row's scalar carrier sibling — one carrier
// per attribute — handled first below. Otherwise the Verbatim pair embeds
// the lw: tag name as []byte and the Ref pair pushes the Lookup-resolved
// uint64.
func addMembership(attr, row reflect.Value, f mappingplan.TaggedField, lookup LookupI) (err error) {
	ch := f.Flags.Channel
	method := "AddMembership" + ch.AddMethodSuffix() + "P"
	// Carrier channels (Cut-2): the membership-side data is per-row, read
	// from the sibling carrier rather than from a lookup or a literal lw: name.
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

// contractPanic carries a write-contract violation raised deep inside the
// reflected call chain up to the nearest exported entry point, which converts
// it to an error (recoverContract). It exists so a caller mistake — a DML that
// does not satisfy the method set this codec drives — reaches the caller the
// way every other failure in this package does, as a returned error, without
// also swallowing genuine panics: any other panic value is re-raised untouched.
type contractPanic struct{ err error }

// recoverContract converts a contractPanic raised below into *err. Deferred by
// every exported entry point that drives a DML by reflection. A panic that is
// not a contract violation is re-raised, so a bug in this package or in the
// DML still fails loudly.
func recoverContract(err *error) {
	r := recover()
	if r == nil {
		return
	}
	cp, ok := r.(contractPanic)
	if !ok {
		panic(r)
	}
	*err = cp.err
}

// mustCall is the reflect.Value.MethodByName(name).Call(args...) shortcut.
//
// A missing method is a write-contract violation: it raises a contractPanic,
// which the exported entry point returns as an error. Validate[T](dml)
// preflights the whole method set and reports every mismatch at once, which is
// the better diagnostic — this is the backstop for callers that skip it.
//
// A method that exists with the wrong ARGUMENT types is not covered: reflect
// panics inside Call, and that panic is re-raised rather than converted, since
// it is indistinguishable from a panic raised by the DML method itself.
// Validate does not check argument types either (see its doc); the strict-1:1
// setters make them a compile-time concern for generated DMLs.
func mustCall(recv reflect.Value, name string, args ...reflect.Value) (rets []reflect.Value) {
	m := recv.MethodByName(name)
	if !m.IsValid() {
		panic(contractPanic{eb.Build().Str("method", name).Str("recv", recv.Type().String()).Errorf("target DML does not have method %s — call Validate[T](dml) to preflight the whole write contract", name)})
	}
	rets = m.Call(args)
	return
}
