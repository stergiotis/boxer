//go:build llm_generated_opus47

package marshallreflect

import (
	"reflect"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
)

// UnmarshalArgs gathers the plain-column accessors and per-section
// reader providers Unmarshal needs. Mirrors the parameter set of
// marshallgen's emitted <Kind>FillFromArrow but as a struct so the
// caller can populate the optional fields without positional empty
// slots.
type UnmarshalArgs struct {
	// NumRows is the entity count to project. Typically idCol.Len().
	NumRows int

	// Plain accessors. IdCol is required; the others are required iff
	// the DTO declares the corresponding plain column.
	IdCol *array.Uint64
	NkCol *array.Binary
	TsCol *array.Timestamp
	LcCol *array.Timestamp

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

func unmarshalPlain(row reflect.Value, plan *marshallgen.Plan, args UnmarshalArgs, i int) (err error) {
	idCol := marshallgen.FindPlainCol(plan, "id")
	row.FieldByName(idCol.GoField).SetUint(args.IdCol.Value(i))

	if nkCol := marshallgen.FindPlainCol(plan, "naturalKey"); nkCol != nil {
		raw := args.NkCol.Value(i)
		switch nkCol.GoType {
		case "[]byte":
			cp := make([]byte, len(raw))
			copy(cp, raw)
			row.FieldByName(nkCol.GoField).SetBytes(cp)
		case "string":
			row.FieldByName(nkCol.GoField).SetString(string(raw))
		default:
			err = eb.Build().Str("type", nkCol.GoType).Errorf("plain naturalKey unsupported")
			return
		}
	}
	if tsCol := marshallgen.FindPlainCol(plan, "ts"); tsCol != nil {
		ns := int64(args.TsCol.Value(i))
		setTimeColumn(row.FieldByName(tsCol.GoField), tsCol.GoType, ns)
	}
	if lcCol := marshallgen.FindPlainCol(plan, "expiresAt"); lcCol != nil {
		ns := int64(args.LcCol.Value(i))
		setTimeColumn(row.FieldByName(lcCol.GoField), lcCol.GoType, ns)
	}
	return
}

func setTimeColumn(fld reflect.Value, goType string, ns int64) {
	switch goType {
	case "time.Time":
		fld.Set(reflect.ValueOf(time.Unix(0, ns).UTC()))
	case "int64":
		fld.SetInt(ns)
	}
}

func unmarshalSection(row reflect.Value, g marshallgen.SectionGroup, args UnmarshalArgs, i int, membIDs map[string]uint64) (err error) {
	attrs := reflect.ValueOf(args.SectionAttrs(g.Section))
	membs := reflect.ValueOf(args.SectionMembs(g.Section))
	if !attrs.IsValid() || !membs.IsValid() {
		err = eb.Build().Str("section", g.Section).Errorf("section reader returned nil")
		return
	}

	if len(g.SubColumns) > 1 {
		return unmarshalMultiSubColumn(row, g, attrs, membs, i, membIDs)
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
		case f.IsRoaring:
			// Bitmap lazily allocated on first value.
		case f.IsSlice:
			a.Slice = reflect.MakeSlice(reflect.SliceOf(goTypeReflect(f.GoType)), 0, 0)
		default:
			a.Val = reflect.New(goTypeReflect(f.GoType)).Elem()
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
// marshallgen.BuildEntities and marshallreflect.Marshal emit
// exactly one membership per attribute. The asymmetry only surfaces
// when a third-party producer of leeway-shaped data attaches
// multiple memberships to the same attribute. Codec wire
// compatibility (encode-then-decode through either path) is
// preserved; cross-producer compatibility against multi-membership
// attributes is not.
func dispatchMembership(membs reflect.Value, i int, attrJ int64, fields []marshallgen.TaggedField, membIDs map[string]uint64, ch marshallgen.MembershipChannel) (matched marshallgen.TaggedField, found bool) {
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
	Field  *marshallgen.TaggedField
	Val    reflect.Value
	Slice  reflect.Value
	Bitmap reflect.Value
	Count  int
}

func consumeValue(attrs reflect.Value, i int, attrJ int64, f marshallgen.TaggedField, a *accumulator) (err error) {
	switch {
	case f.IsRoaring:
		seq := mustCall(attrs, "GetAttrValueValue", reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		for _, v := range collectIterSeq(seq) {
			if !a.Bitmap.IsValid() {
				bm := newRoaringBitmap()
				a.Bitmap = bm
			}
			mustCall(a.Bitmap, "Add", v)
		}
	case f.IsSlice:
		seq := mustCall(attrs, "GetAttrValueValue", reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		for _, v := range collectIterSeq(seq) {
			// Defensive copy for []byte elements (Arrow buffer aliasing).
			if f.GoType == "[]byte" {
				src := v.Bytes()
				cp := make([]byte, len(src))
				copy(cp, src)
				a.Slice = reflect.Append(a.Slice, reflect.ValueOf(cp))
			} else {
				a.Slice = reflect.Append(a.Slice, v)
			}
		}
	default:
		// Single-value read — scalar section uses GetAttrValueValue
		// returning T; non-scalar section uses GetAttrValueSingleOrDefault.
		method := "GetAttrValueSingleOrDefault"
		switch marshallgen.ClassifyBegin(f) {
		case marshallgen.ShapeScalarBegin, marshallgen.ShapeExplodeBegin:
			method = "GetAttrValueValue"
		}
		v := mustCall(attrs, method, reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		switch {
		case marshallgen.IsFixedByteArray(f.GoType):
			// Copy bytes into a fresh [N]byte array from the wire blob.
			arrType := goTypeReflect(f.GoType)
			arr := reflect.New(arrType).Elem()
			src := v.Bytes()
			for k := 0; k < arr.Len() && k < len(src); k++ {
				arr.Index(k).SetUint(uint64(src[k]))
			}
			a.Val = arr
		case f.GoType == "[]byte":
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
	case a.Field.IsSlice:
		fld.Set(a.Slice)
	case a.Field.IsRoaring:
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

func unmarshalMultiSubColumn(row reflect.Value, g marshallgen.SectionGroup, attrs, membs reflect.Value, i int, membIDs map[string]uint64) (err error) {
	if len(g.Memberships) != 1 {
		err = eb.Build().Str("section", g.Section).Errorf("multi-sub-column section with multiple memberships not supported")
		return
	}
	memb := g.Memberships[0]
	expectedID, hasID := membIDs[memb.LWMembership]

	type subAcc struct {
		Field   *marshallgen.TaggedField
		ColName string
		Val     reflect.Value
	}
	subs := make([]subAcc, 0, len(g.SubColumns))
	for j := range g.SubColumns {
		sc := &g.SubColumns[j]
		f := &sc.Fields[0]
		subs = append(subs, subAcc{Field: f, ColName: sc.Name, Val: reflect.New(goTypeReflect(f.GoType)).Elem()})
	}

	n := mustCall(attrs, "GetNumberOfAttributes", reflect.ValueOf(entityIdx(i)))[0].Int()
	count := 0
	for attrJ := int64(0); attrJ < n; attrJ++ {
		locals := make([]reflect.Value, len(subs))
		for k, s := range subs {
			locals[k] = mustCall(attrs, "GetAttrValue"+marshallgen.UpperFirst(s.ColName), reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		}
		seq := mustCall(membs, "GetMembValueLowCardRef", reflect.ValueOf(entityIdx(i)), reflect.ValueOf(attributeIdx(attrJ)))[0]
		for _, v := range collectIterSeq(seq) {
			if !hasID || v.Uint() != expectedID {
				continue
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

// goTypeReflect maps the source-form Go type name back to the
// corresponding reflect.Type. Inverse of reflectGoTypeName for the
// types Unmarshal needs to instantiate accumulators for.
func goTypeReflect(name string) reflect.Type {
	if n, ok := marshallgen.FixedByteArrayLen(name); ok {
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
