package marshallreflect

import (
	"reflect"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
)

// UnmarshalArgs gathers the plain-column accessors and per-section
// reader providers Unmarshal needs. Mirrors the parameter set of
// marshallgen's emitted <Kind>FillFromArrow but as a struct so the
// caller can populate the optional fields without positional empty
// slots.
type UnmarshalArgs struct {
	// NumRows is the entity count to project. Typically the id column's Len().
	NumRows int

	// PlainCol returns the Arrow array backing a plain column, looked up
	// by role name ("id" / "naturalKey" / "ts" / "expiresAt"). The
	// concrete type must be the one goplan.PlainArrowArrayType maps
	// the DTO field's Go type to (e.g. *array.Uint64 for a uint64 id,
	// *array.Timestamp for a time.Time ts, *array.FixedSizeBinary for a
	// [16]byte). Required for every plain column the DTO declares; "id"
	// is always declared.
	PlainCol func(name string) any

	// SectionAttrs returns the per-section attribute reader (e.g.
	// *ra.ReadAccessFactsTagged<X>.Attributes). Lookup-by-section-name.
	SectionAttrs func(sectionName string) any

	// SectionMembs returns the per-section membership reader (e.g.
	// *ra.ReadAccessFactsTagged<X>.Memberships).
	SectionMembs func(sectionName string) any
}

// Unmarshal appends NumRows entities to *out by reading the plain
// columns and walking per-section attribute / membership readers via
// reflect. T's struct tags drive the field-by-field decode. Lookup
// resolves non-verbatim membership names to uint64 ids so the per-
// row dispatch can match against the wire's LowCardRef channel.
func Unmarshal[T any](args UnmarshalArgs, out *[]T, lookup LookupI) (err error) {
	if lookup == nil {
		lookup = NoLookup{}
	}
	rowType := reflect.TypeOf((*T)(nil)).Elem()
	r, err := resolveForType(rowType)
	if err != nil {
		return
	}
	plan := r.plan

	// Pre-resolve ref-channel membership ids — cached so the inner
	// dispatch loop doesn't pay one lookup per attribute per row. Only
	// channels whose wire form takes a uint64 id need this; verbatim
	// and parametrized-only channels are matched by literal bytes.
	membIDs := map[string]uint64{}
	for _, f := range plan.Fields {
		if !f.Flags.Channel.NeedsKindVar() || f.IsConst {
			continue
		}
		var id uint64
		id, err = lookup.LookupMembership(f.LWMembership)
		if err != nil {
			err = eb.Build().Str("membership", f.LWMembership).Errorf("%w", err)
			return
		}
		membIDs[f.LWMembership] = id
	}

	groups := r.groups

	for i := 0; i < args.NumRows; i++ {
		rowPtr := reflect.New(rowType)
		rowVal := rowPtr.Elem()
		err = unmarshalPlain(rowVal, plan, args, i)
		if err != nil {
			err = eb.Build().Int("row", i).Errorf("plain decode: %w", err)
			return
		}
		for _, g := range groups {
			err = unmarshalSection(rowVal, g, args, i, membIDs)
			if err != nil {
				err = eb.Build().Int("row", i).Str("section", g.Section).Errorf("%w", err)
				return
			}
		}
		*out = append(*out, rowPtr.Elem().Interface().(T))
	}
	return
}

// unmarshalPlain reads the declared plain (entity-header) columns into
// the row. Strict 1:1: each column's Arrow array is read straight into
// its DTO field, whose Go type the writer already matched to the
// column. The four roles are read in fixed order; only those the DTO
// declares are present.
func unmarshalPlain(row reflect.Value, plan *mappingplan.Plan, args UnmarshalArgs, i int) (err error) {
	if args.PlainCol == nil {
		err = eb.Build().Errorf("UnmarshalArgs.PlainCol is required")
		return
	}
	for _, role := range [...]string{"id", "naturalKey", "ts", "expiresAt"} {
		p := goplan.FindPlainCol(plan, role)
		if p == nil {
			continue
		}
		col := args.PlainCol(role)
		if col == nil {
			err = eb.Build().Str("column", role).Errorf("plain column reader is nil")
			return
		}
		err = readPlainArrow(row.FieldByName(p.GoField), p.GoType(), col, i)
		if err != nil {
			err = eb.Build().Str("column", role).Errorf("%w", err)
			return
		}
	}
	return
}

// readPlainArrow sets fld (a DTO plain field of source-form type goType)
// from row i of its Arrow array col. col's concrete type must be the one
// goplan.PlainArrowArrayType maps goType to. []byte / FixedSizeBinary
// are defensively copied out of the Arrow buffer; time.Time is rebuilt
// from the column's int64 timestamp honoring its declared TimeUnit.
func readPlainArrow(fld reflect.Value, goType string, col any, i int) (err error) {
	switch goType {
	case "uint8":
		fld.SetUint(uint64(col.(*array.Uint8).Value(i)))
	case "uint16":
		fld.SetUint(uint64(col.(*array.Uint16).Value(i)))
	case "uint32":
		fld.SetUint(uint64(col.(*array.Uint32).Value(i)))
	case "uint64":
		fld.SetUint(col.(*array.Uint64).Value(i))
	case "int8":
		fld.SetInt(int64(col.(*array.Int8).Value(i)))
	case "int16":
		fld.SetInt(int64(col.(*array.Int16).Value(i)))
	case "int32":
		fld.SetInt(int64(col.(*array.Int32).Value(i)))
	case "int64":
		fld.SetInt(col.(*array.Int64).Value(i))
	case "float32":
		fld.SetFloat(float64(col.(*array.Float32).Value(i)))
	case "float64":
		fld.SetFloat(col.(*array.Float64).Value(i))
	case "bool":
		fld.SetBool(col.(*array.Boolean).Value(i))
	case "string":
		fld.SetString(col.(*array.String).Value(i))
	case "[]byte":
		src := col.(*array.Binary).Value(i)
		cp := make([]byte, len(src))
		copy(cp, src)
		fld.SetBytes(cp)
	case "time.Time":
		// Honor the column's self-describing TimeUnit rather than assuming
		// nanoseconds. Plain ts/expiresAt columns are millisecond-width
		// (z32) in the in-tree schemas while section temporal columns are
		// nanosecond-width (z64); reading the raw int64 as nanos would be a
		// 10^6x error on a millisecond column. ToTime is exactly what the
		// generated ra readers + gocodegen.ArrowTypeToGoType use, and it
		// already normalises to UTC.
		ts := col.(*array.Timestamp)
		unit := ts.DataType().(*arrow.TimestampType).Unit
		fld.Set(reflect.ValueOf(ts.Value(i).ToTime(unit)))
	default:
		if _, ok := goplan.FixedByteArrayLen(goType); ok {
			src := col.(*array.FixedSizeBinary).Value(i)
			for k := 0; k < fld.Len() && k < len(src); k++ {
				fld.Index(k).SetUint(uint64(src[k]))
			}
			return
		}
		err = eb.Build().Str("type", goType).Errorf("unsupported plain column type")
	}
	return
}

func unmarshalSection(row reflect.Value, g goplan.SectionGroup, args UnmarshalArgs, i int, membIDs map[string]uint64) (err error) {
	attrs := reflect.ValueOf(args.SectionAttrs(g.Section))
	membs := reflect.ValueOf(args.SectionMembs(g.Section))
	if !attrs.IsValid() || !membs.IsValid() {
		err = eb.Build().Str("section", g.Section).Errorf("section reader returned nil")
		return
	}

	if len(g.SubColumns) > 1 {
		return unmarshalMultiSubColumn(row, g, attrs, membs, i, membIDs)
	}

	if g.Channel().UsesCarrier() {
		return unmarshalCarrierSection(row, g, attrs, membs, i)
	}

	fields := g.SubColumns[0].Fields

	// Per-field accumulators (cardinality / count tracking).
	accs := make(map[string]*accumulator, len(fields))
	for j := range fields {
		f := &fields[j]
		if f.IsConst {
			continue // consts aren't projected to a Go field
		}
		a := &accumulator{Field: f}
		switch {
		case f.IsRoaring():
			// Bitmap lazily allocated on first value.
		case f.IsSlice():
			a.Slice = reflect.MakeSlice(reflect.SliceOf(goTypeReflect(f.GoType())), 0, 0)
		default:
			a.Val = reflect.New(goTypeReflect(f.GoType())).Elem()
		}
		accs[f.GoFieldName] = a
	}

	// Section channel is uniform across its fields (enforced by the
	// plan's channel-uniformity check); resolve it once for all attributes.
	ch := g.Channel()
	n := mustCall(attrs, "GetNumberOfAttributes", reflect.ValueOf(entityIdx(i)))[0].Int()
	for attrJ := int64(0); attrJ < n; attrJ++ {
		matchedField, found := dispatchMembership(membs, i, attrJ, fields, membIDs, ch)
		if !found {
			continue
		}
		if matchedField.IsConst {
			continue
		}
		a := accs[matchedField.GoFieldName]
		err = consumeValue(attrs, i, attrJ, matchedField, a)
		if err != nil {
			return
		}
	}

	// Project accumulators into the row.
	for _, a := range accs {
		err = projectAccumulator(row, a)
		if err != nil {
			return
		}
	}
	return
}

// dispatchMembership iterates the per-attribute membership channel
// (uint64 or []byte) and returns the first DTO field whose membership
// matches. Const fields are skipped (their value is fixed on the
// write side; nothing to project here).
//
// Multi-membership read asymmetry vs marshallgen. The codegen-emitted
// <Kind>FillFromArrow uses an inline switch inside the membership
// loop:
//
//	for membID := range membsVar.GetMembValueLowCardRef(...) {
//	    switch membID {
//	    case kindFoo: <consume into Foo accumulator>
//	    case kindBar: <consume into Bar accumulator>
//	    }
//	}
//
// If a single attribute carries memberships for both `foo` and `bar`,
// the codegen reader fires BOTH cases — the value is consumed once
// per matching DTO field. This implementation returns on the first
// match, so only one field's accumulator increments.
//
// The divergence is unreachable through codec-written wire: both
// the marshallgen-emitted BuildEntities and marshallreflect.Marshal emit
// exactly one membership per attribute. The asymmetry only surfaces
// when a third-party producer of leeway-shaped data attaches
// multiple memberships to the same attribute. Codec wire
// compatibility (encode-then-decode through either path) is
// preserved; cross-producer compatibility against multi-membership
// attributes is not.
func dispatchMembership(membs reflect.Value, i int, attrJ int64, fields []mappingplan.TaggedField, membIDs map[string]uint64, ch mappingplan.MembershipChannel) (matched mappingplan.TaggedField, found bool) {
	// ch is the section's (uniform) membership channel, resolved once by
	// the caller — all fields in a section agree on it per the plan's
	// channel-uniformity check.
	method := "GetMembValue" + ch.AddMethodSuffix()
	seq := mustCall(membs, method, reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]

	if ch.EmbedsLiteralName() {
		for _, v := range collectIterSeq(seq) {
			name := string(v.Bytes())
			for _, f := range fields {
				if f.IsConst || !f.Flags.Channel.EmbedsLiteralName() {
					continue
				}
				if f.LWMembership == name {
					return f, true
				}
			}
		}
		return
	}

	for _, v := range collectIterSeq(seq) {
		id := v.Uint()
		for _, f := range fields {
			if f.IsConst || f.Flags.Channel.EmbedsLiteralName() {
				continue
			}
			if membIDs[f.LWMembership] == id {
				return f, true
			}
		}
	}
	return
}

type accumulator struct {
	Field  *mappingplan.TaggedField
	Val    reflect.Value
	Slice  reflect.Value
	Bitmap reflect.Value
	Count  int
}

func consumeValue(attrs reflect.Value, i int, attrJ int64, f mappingplan.TaggedField, a *accumulator) (err error) {
	switch {
	case f.IsRoaring():
		seq := mustCall(attrs, "GetAttrValueValue", reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		for _, v := range collectIterSeq(seq) {
			if !a.Bitmap.IsValid() {
				bm := newRoaringBitmap()
				a.Bitmap = bm
			}
			mustCall(a.Bitmap, "Add", v)
		}
	case f.IsSlice():
		seq := mustCall(attrs, "GetAttrValueValue", reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		for _, v := range collectIterSeq(seq) {
			// Defensive copy for []byte elements (Arrow buffer aliasing).
			if goplan.CopyStrategy(f.GoType()) == goplan.CopyBytes {
				src := v.Bytes()
				cp := make([]byte, len(src))
				copy(cp, src)
				a.Slice = reflect.Append(a.Slice, reflect.ValueOf(cp))
			} else {
				a.Slice = reflect.Append(a.Slice, v)
			}
		}
	default:
		// Single-value read — accessor chosen by field shape, shared with
		// the codegen emitter via goplan.SingleValueReadAccessor.
		v := mustCall(attrs, goplan.SingleValueReadAccessor(f), reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		switch goplan.CopyStrategy(f.GoType()) {
		case goplan.CopyFixedByte:
			// Copy bytes into a fresh [N]byte array from the wire blob.
			arrType := goTypeReflect(f.GoType())
			arr := reflect.New(arrType).Elem()
			src := v.Bytes()
			for k := 0; k < arr.Len() && k < len(src); k++ {
				arr.Index(k).SetUint(uint64(src[k]))
			}
			a.Val = arr
		case goplan.CopyBytes:
			src := v.Bytes()
			cp := make([]byte, len(src))
			copy(cp, src)
			a.Val = reflect.ValueOf(cp)
		default:
			a.Val = v
		}
		a.Count++
	}
	return
}

func projectAccumulator(row reflect.Value, a *accumulator) (err error) {
	fld := row.FieldByName(a.Field.GoFieldName)
	switch {
	case a.Field.IsOption:
		if a.Count == 1 {
			fld.FieldByName("Val").Set(a.Val)
			fld.FieldByName("Has").SetBool(true)
		}
		// Else: leave as zero-value (Has=false).
	case a.Field.IsSlice():
		fld.Set(a.Slice)
	case a.Field.IsRoaring():
		if a.Bitmap.IsValid() {
			fld.Set(a.Bitmap)
		}
	default:
		if a.Count != 1 {
			err = eb.Build().Str("field", a.Field.GoFieldName).Errorf("expected exactly one occurrence per row")
			return
		}
		fld.Set(a.Val)
	}
	return
}

func unmarshalMultiSubColumn(row reflect.Value, g goplan.SectionGroup, attrs, membs reflect.Value, i int, membIDs map[string]uint64) (err error) {
	if len(g.Memberships) != 1 {
		err = eb.Build().Str("section", g.Section).Errorf("multi-sub-column section with multiple memberships not supported")
		return
	}
	memb := g.Memberships[0]
	expectedID, hasID := membIDs[memb.LWMembership]

	type subAcc struct {
		Field   *mappingplan.TaggedField
		ColName string
		Val     reflect.Value
	}
	subs := make([]subAcc, 0, len(g.SubColumns))
	for j := range g.SubColumns {
		sc := &g.SubColumns[j]
		f := &sc.Fields[0]
		subs = append(subs, subAcc{Field: f, ColName: sc.Name, Val: reflect.New(goTypeReflect(f.GoType())).Elem()})
	}

	// Dispatch on the section's (uniform) channel like the single-membership
	// path, instead of hard-coding GetMembValueLowCardRef + uint64-id matching.
	// The previous code never matched a verbatim membership (hasID==false),
	// skipping every attribute and failing every such DTO (review E-1).
	ch := g.Channel()
	method := "GetMembValue" + ch.AddMethodSuffix()
	embedsName := ch.EmbedsLiteralName()
	n := mustCall(attrs, "GetNumberOfAttributes", reflect.ValueOf(entityIdx(i)))[0].Int()
	count := 0
	for attrJ := int64(0); attrJ < n; attrJ++ {
		locals := make([]reflect.Value, len(subs))
		for k, s := range subs {
			locals[k] = mustCall(attrs, "GetAttrValue"+mappingplan.UpperFirst(s.ColName), reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		}
		seq := mustCall(membs, method, reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		for _, v := range collectIterSeq(seq) {
			if embedsName {
				if string(v.Bytes()) != memb.LWMembership {
					continue
				}
			} else {
				if !hasID || v.Uint() != expectedID {
					continue
				}
			}
			for k := range subs {
				subs[k].Val = locals[k]
			}
			count++
		}
	}
	if count != 1 {
		err = eb.Build().Str("membership", memb.LWMembership).Errorf("expected exactly one occurrence per row")
		return
	}
	for _, s := range subs {
		row.FieldByName(s.Field.GoFieldName).Set(s.Val)
	}
	return
}

// unmarshalCarrierSection decodes a mixed / parametrized section (ADR-0008
// Cut-2, value shapes lifted per OQ#4). PlanBuilder guarantees one membership
// — one value+carrier field — per such section, so every attribute belongs to
// that field and no id matching is needed. The value field's shape selects the
// decode (mirroring the codegen emitter): scalar / Option pair one value with
// a scalar carrier; a container []T pairs N values (one attribute) with a
// scalar carrier; an exploded []T pairs N attributes (one value each) with a
// slice carrier. The carrier's per-row membership data (id/name + params)
// comes from the combined Seq2 (mixed) or Seq (parametrized) accessor.
func unmarshalCarrierSection(row reflect.Value, g goplan.SectionGroup, attrs, membs reflect.Value, i int) (err error) {
	var f *mappingplan.TaggedField
	for j := range g.SubColumns[0].Fields {
		if g.SubColumns[0].Fields[j].Flags.Channel.UsesCarrier() {
			f = &g.SubColumns[0].Fields[j]
			break
		}
	}
	if f == nil {
		err = eb.Build().Str("section", g.Section).Errorf("carrier section has no value field")
		return
	}

	readMethod := "GetMembValue" + f.Flags.Channel.CarrierReadMethodSuffix()
	carrierType := carrierStructType(row, f)
	// Mirror the codegen emitter's accessor choice so the two front-ends read
	// the same accessor (shared via goplan.SingleValueReadAccessor).
	valMethod := goplan.SingleValueReadAccessor(*f)
	n := mustCall(attrs, "GetNumberOfAttributes", reflect.ValueOf(entityIdx(i)))[0].Int()

	switch {
	case f.IsSlice() && f.Flags.Explode:
		// N attributes → a value slice paired with a carrier slice.
		valSlice := reflect.MakeSlice(reflect.SliceOf(goTypeReflect(f.GoType())), 0, 0)
		carrierSlice := reflect.MakeSlice(reflect.SliceOf(carrierType), 0, 0)
		for attrJ := int64(0); attrJ < n; attrJ++ {
			carrierVal, ok := readCarrierStruct(membs, f, carrierType, readMethod, i, attrJ)
			if !ok {
				continue
			}
			valSlice = reflect.Append(valSlice, readCarrierValue(attrs, f, valMethod, i, attrJ))
			carrierSlice = reflect.Append(carrierSlice, carrierVal)
		}
		row.FieldByName(f.GoFieldName).Set(valSlice)
		row.FieldByName(f.CarrierField).Set(carrierSlice)

	case f.IsSlice():
		// Container: one attribute carrying N values (a Seq) + one carrier.
		valSlice := reflect.MakeSlice(reflect.SliceOf(goTypeReflect(f.GoType())), 0, 0)
		carrierVal := reflect.New(carrierType).Elem()
		for attrJ := int64(0); attrJ < n; attrJ++ {
			if cv, ok := readCarrierStruct(membs, f, carrierType, readMethod, i, attrJ); ok {
				carrierVal = cv
			}
			seq := mustCall(attrs, "GetAttrValueValue", reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
			for _, v := range collectIterSeq(seq) {
				if goplan.CopyStrategy(f.GoType()) == goplan.CopyBytes {
					valSlice = reflect.Append(valSlice, reflect.ValueOf(append([]byte(nil), v.Bytes()...)))
				} else {
					valSlice = reflect.Append(valSlice, v)
				}
			}
		}
		row.FieldByName(f.GoFieldName).Set(valSlice)
		row.FieldByName(f.CarrierField).Set(carrierVal)

	default:
		// Scalar value (exactly one attribute) or Option (zero or one). The
		// carrier column gets one entry per row regardless (zero when absent).
		valAcc := reflect.New(goTypeReflect(f.GoType())).Elem()
		carrierVal := reflect.New(carrierType).Elem()
		count := 0
		for attrJ := int64(0); attrJ < n; attrJ++ {
			cv, ok := readCarrierStruct(membs, f, carrierType, readMethod, i, attrJ)
			if !ok {
				continue
			}
			carrierVal = cv
			valAcc = readCarrierValue(attrs, f, valMethod, i, attrJ)
			count++
		}
		if f.IsOption {
			optFld := row.FieldByName(f.GoFieldName)
			if count == 1 {
				optFld.FieldByName("Val").Set(valAcc)
				optFld.FieldByName("Has").SetBool(true)
			}
			// else: leave the zero value (Has=false).
		} else {
			if count != 1 {
				err = eb.Build().Str("field", f.GoFieldName).Errorf("expected exactly one occurrence per row for a mixed/parametrized value")
				return
			}
			row.FieldByName(f.GoFieldName).Set(valAcc)
		}
		row.FieldByName(f.CarrierField).Set(carrierVal)
	}
	return
}

// carrierStructType returns the carrier *struct* reflect.Type: the field type
// for a scalar carrier, or its element type for a slice carrier ([]X → X).
func carrierStructType(row reflect.Value, f *mappingplan.TaggedField) reflect.Type {
	t := row.FieldByName(f.CarrierField).Type()
	if f.CarrierIsSlice {
		return t.Elem()
	}
	return t
}

// readCarrierStruct reconstructs one carrier struct (value of carrierType)
// from the per-attribute membership accessor. Returns ok=false when the
// attribute carries no membership (e.g. an absent Option). Parametrized
// channels read a single Seq[[]byte] (params only); mixed channels read the
// Seq2 (membership value, params).
func readCarrierStruct(membs reflect.Value, f *mappingplan.TaggedField, carrierType reflect.Type, readMethod string, i int, attrJ int64) (reflect.Value, bool) {
	carrierVal := reflect.New(carrierType).Elem()
	seq := mustCall(membs, readMethod, reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
	if f.Flags.Channel.CarrierValueField() == "" {
		blobs := collectIterSeq(seq)
		if len(blobs) == 0 {
			return reflect.Value{}, false
		}
		carrierVal.FieldByName("Params").SetBytes(append([]byte(nil), blobs[0].Bytes()...))
		return carrierVal, true
	}
	keys, params := collectIterSeq2(seq)
	if len(keys) == 0 {
		return reflect.Value{}, false
	}
	valField := carrierVal.FieldByName(f.Flags.Channel.CarrierValueField())
	if f.Flags.Channel.CarrierValueIsBytes() {
		valField.SetBytes(append([]byte(nil), keys[0].Bytes()...)) // verbatim name — copy out of the Arrow buffer
	} else {
		valField.SetUint(keys[0].Uint())
	}
	carrierVal.FieldByName("Params").SetBytes(append([]byte(nil), params[0].Bytes()...))
	return carrierVal, true
}

// readCarrierValue reads a single section value for attribute attrJ into a
// value of f's Go type, applying the per-type copy strategy.
func readCarrierValue(attrs reflect.Value, f *mappingplan.TaggedField, valMethod string, i int, attrJ int64) reflect.Value {
	v := mustCall(attrs, valMethod, reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
	switch goplan.CopyStrategy(f.GoType()) {
	case goplan.CopyFixedByte:
		arr := reflect.New(goTypeReflect(f.GoType())).Elem()
		src := v.Bytes()
		for k := 0; k < arr.Len() && k < len(src); k++ {
			arr.Index(k).SetUint(uint64(src[k]))
		}
		return arr
	case goplan.CopyBytes:
		return reflect.ValueOf(append([]byte(nil), v.Bytes()...))
	default:
		return v
	}
}

// goTypeReflect maps the source-form Go type name back to the
// corresponding reflect.Type. Inverse of reflectGoTypeName for the
// types Unmarshal needs to instantiate accumulators for.
func goTypeReflect(name string) reflect.Type {
	if n, ok := goplan.FixedByteArrayLen(name); ok {
		return reflect.ArrayOf(n, reflect.TypeOf(byte(0)))
	}
	switch name {
	case "uint8":
		return reflect.TypeOf(uint8(0))
	case "uint16":
		return reflect.TypeOf(uint16(0))
	case "uint32":
		return reflect.TypeOf(uint32(0))
	case "uint64":
		return reflect.TypeOf(uint64(0))
	case "int8":
		return reflect.TypeOf(int8(0))
	case "int16":
		return reflect.TypeOf(int16(0))
	case "int32":
		return reflect.TypeOf(int32(0))
	case "int64":
		return reflect.TypeOf(int64(0))
	case "float32":
		return reflect.TypeOf(float32(0))
	case "float64":
		return reflect.TypeOf(float64(0))
	case "bool":
		return reflect.TypeOf(false)
	case "string":
		return reflect.TypeOf("")
	case "time.Time":
		return reflect.TypeOf(time.Time{})
	case "[]byte":
		return reflect.TypeOf([]byte(nil))
	}
	return nil
}

// collectIterSeq drains an iter.Seq[T] returned via reflect into a
// []reflect.Value. The iter.Seq's element type T determines the
// reflect.Type of each entry (uint64 for ref membership, []byte for
// verbatim membership, T for GetAttrValueValue).
//
// Implementation builds a yield closure via reflect.MakeFunc that
// appends into the collector, then calls the seq with it.
func collectIterSeq(seq reflect.Value) (out []reflect.Value) {
	seqType := seq.Type()
	yieldType := seqType.In(0)
	yield := reflect.MakeFunc(yieldType, func(args []reflect.Value) []reflect.Value {
		out = append(out, args[0])
		return []reflect.Value{reflect.ValueOf(true)}
	})
	seq.Call([]reflect.Value{yield})
	return
}

// collectIterSeq2 drains an iter.Seq2[K, V] returned via reflect into
// parallel key/value slices — the carrier channels' combined accessor
// yields (membership-value, params) pairs this way.
func collectIterSeq2(seq reflect.Value) (keys, vals []reflect.Value) {
	yieldType := seq.Type().In(0) // func(K, V) bool
	yield := reflect.MakeFunc(yieldType, func(args []reflect.Value) []reflect.Value {
		keys = append(keys, args[0])
		vals = append(vals, args[1])
		return []reflect.Value{reflect.ValueOf(true)}
	})
	seq.Call([]reflect.Value{yield})
	return
}

// entityIdx / attributeIdx wrap int / int64 to the raruntime's
// typed-int constructors so reflect.Value.Call sees the exact
// parameter type the ra method signature declares.
func entityIdx(i int) raruntime.EntityIdx         { return raruntime.EntityIdx(i) }
func attributeIdx(i int64) raruntime.AttributeIdx { return raruntime.AttributeIdx(i) }

// newRoaringBitmap returns a reflect.Value wrapping a freshly
// allocated *roaring.Bitmap. Direct import — every in-tree DTO that
// declares a roaring field already pulls the dependency in.
func newRoaringBitmap() reflect.Value {
	return reflect.ValueOf(roaring.New())
}
