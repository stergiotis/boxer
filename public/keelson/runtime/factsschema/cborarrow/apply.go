//go:build llm_generated_opus47

// Package cborarrow converts runtime.facts rows from sparse-CBOR (per
// arrowrowcbor's wire shape) into an ArrowStream IPC payload using the
// typed Arrow builder generated under factsschema/dml.
//
// Symmetric inverse of arrowrowcbor.RecordBuilder. Mirrors
// rowbinaryarrow's apply machinery (rowState / sectionState / applyTo
// + per-section apply functions); the per-row reader walks a CBOR map
// keyed by short-key dispatch instead of a byte-positional column
// stream — see [Convert] in convert.go.
package cborarrow

import (
	"time"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	dmlruntime "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
)

// --- Schema-agnostic generic apply helpers. ---
//
// The three helpers below cover every leeway-DML section shape. Each
// is parameterised over the section's writer types so the same helper
// can drive factsschema's DML, boxerstaging anchor's DML, or any
// future leeway-emitted DML — type inference binds the concrete types
// at the call site.
//
// applyScalarSection drives sections whose value column is scalar:
// each attribute holds one value and `lrcard[i]` tells how many
// attributes the kind `lr[i]` carries.
func applyScalarSection[
	V any,
	Ent any,
	Sec interface {
		BeginAttribute(V) Attr
		EndSection() Ent
	},
	Attr interface {
		dmlruntime.InAttributeMembershipLowCardRefI[Attr]
		EndAttribute() Sec
	},
](getSec func() Sec, vals []V, s *sectionState, section string) (err error) {
	sec := getSec()
	offset := 0
	for i, kind := range s.lr {
		card := int(s.lrcard[i])
		if offset+card > len(vals) {
			return eb.Build().Str("section", section).Int("lr_idx", i).Int("card", card).Int("offset", offset).Int("len(val)", len(vals)).
				Errorf("cborarrow: lrcard sum exceeds val length")
		}
		for j := 0; j < card; j++ {
			sec.BeginAttribute(vals[offset+j]).AddMembershipLowCardRef(kind).EndAttribute()
		}
		offset += card
	}
	sec.EndSection()
	return
}

// applyArraySection drives sections whose value column is non-scalar
// (homogeneous-array / set). Each attribute holds `countsPerAttr[i]`
// values fed via AddToContainer; one attribute per kind in `lr`.
func applyArraySection[
	V any,
	Ent any,
	Sec interface {
		BeginAttribute() Attr
		EndSection() Ent
	},
	Attr interface {
		AddToContainer(V) Attr
		dmlruntime.InAttributeMembershipLowCardRefI[Attr]
		EndAttribute() Sec
	},
](getSec func() Sec, vals []V, s *sectionState, section string) (err error) {
	err = validateArrayShape(s, section, len(vals))
	if err != nil {
		return
	}
	sec := getSec()
	offset := 0
	for i, kind := range s.lr {
		n := int(s.countsPerAttr[i])
		ia := sec.BeginAttribute()
		for j := 0; j < n; j++ {
			ia.AddToContainer(vals[offset+j])
		}
		ia.AddMembershipLowCardRef(kind).EndAttribute()
		offset += n
	}
	sec.EndSection()
	return
}

// applyRangeSection drives multi-sub-column scalar sections (e.g.
// u32Range with beginIncl + endExcl) — each attribute holds a pair
// of scalars; `lrcard[i]` carries the attribute count per kind.
func applyRangeSection[
	V1 any, V2 any,
	Ent any,
	Sec interface {
		BeginAttribute(V1, V2) Attr
		EndSection() Ent
	},
	Attr interface {
		dmlruntime.InAttributeMembershipLowCardRefI[Attr]
		EndAttribute() Sec
	},
](getSec func() Sec, vals1 []V1, vals2 []V2, s *sectionState, section string) (err error) {
	if len(vals1) != len(vals2) {
		return eb.Build().Str("section", section).Int("len(beginIncl)", len(vals1)).Int("len(endExcl)", len(vals2)).
			Errorf("cborarrow: range value length mismatch")
	}
	sec := getSec()
	offset := 0
	for i, kind := range s.lr {
		card := int(s.lrcard[i])
		if offset+card > len(vals1) {
			return eb.Build().Str("section", section).Int("lr_idx", i).Int("card", card).Int("offset", offset).Int("len(val)", len(vals1)).
				Errorf("cborarrow: lrcard sum exceeds val length")
		}
		for j := 0; j < card; j++ {
			sec.BeginAttribute(vals1[offset+j], vals2[offset+j]).AddMembershipLowCardRef(kind).EndAttribute()
		}
		offset += card
	}
	sec.EndSection()
	return
}

// rowState buffers one row's worth of decoded CBOR values. Plain
// columns are stored directly; tagged-value columns are stored
// per-section keyed by the logical column name (e.g. "value" for
// single-value sections, "beginIncl"/"endExcl" for u32Range).
type rowState struct {
	hasId            bool
	id               uint64
	hasNaturalKey    bool
	naturalKey       []byte
	hasTs            bool
	tsNanos          int64 // unix nanos (CH-canonical RowBinary DateTime64(9))
	hasExpiresAt     bool
	expiresAtNanos   int64 // unix nanos

	sec map[string]*sectionState
}

// sectionState buffers all decoded columns of one tagged-value section
// for the current row. Per-canonical-type slice fields are kept typed;
// only the slice for the section's actual canonical type is populated.
// All 11 canonical-type maps are nil until the section's reader stores
// into them, so the per-row allocation cost is paid only for the
// sections that the producing kind actually touches.
type sectionState struct {
	valString  map[string][]string
	valBytes   map[string][][]byte
	valBool    map[string][]bool
	valUint8   map[string][]uint8
	valUint16  map[string][]uint16
	valUint32  map[string][]uint32
	valUint64  map[string][]uint64
	valInt8    map[string][]int8
	valInt16   map[string][]int16
	valInt32   map[string][]int32
	valInt64   map[string][]int64
	valFloat32 map[string][]float32
	valFloat64 map[string][]float64

	lr     []uint64
	lrcard []uint64
	// countsPerAttr is the length per attribute for HomogenousArray
	// sections (`tv:<section>:len`) or the cardinality per attribute
	// for Set sections (`tv:<section>:card`). Scalar sections leave
	// this nil. Apply functions for *Array / *Set sections walk
	// countsPerAttr in lockstep with lr to slice the flat value
	// array by per-attribute count.
	countsPerAttr []uint64
}

func (inst *rowState) init() {
	inst.hasId = false
	inst.hasNaturalKey = false
	inst.hasTs = false
	inst.hasExpiresAt = false
	inst.sec = nil
}

func (inst *rowState) section(name string) *sectionState {
	if inst.sec == nil {
		inst.sec = make(map[string]*sectionState, 8)
	}
	s, ok := inst.sec[name]
	if !ok {
		s = &sectionState{}
		inst.sec[name] = s
	}
	return s
}

// applyTo drives a dml.InEntityFacts to produce one Arrow row from the
// buffered state.
func (inst *rowState) applyTo(b *dml.InEntityFacts) (err error) {
	b.BeginEntity()
	if inst.hasId || inst.hasNaturalKey {
		b.SetId(inst.id, inst.naturalKey)
	}
	if inst.hasTs {
		b.SetTimestamp(time.Unix(0, inst.tsNanos).UTC())
	}
	if inst.hasExpiresAt {
		b.SetLifecycle(time.Unix(0, inst.expiresAtNanos).UTC())
	}

	for section, s := range inst.sec {
		err = inst.applySection(b, section, s)
		if err != nil {
			return
		}
	}
	err = b.CommitEntity()
	return
}

// applySection routes one section's decoded sectionState into the
// appropriate generic apply helper. The dispatch is facts-schema-
// specific (knows the section vocabulary), but each case is a single
// line — the per-section apply logic lives in the schema-agnostic
// applyScalarSection / applyArraySection / applyRangeSection helpers
// above.
func (inst *rowState) applySection(b *dml.InEntityFacts, section string, s *sectionState) (err error) {
	if len(s.lr) != len(s.lrcard) {
		err = eb.Build().Str("section", section).Int("lr", len(s.lr)).Int("lrcard", len(s.lrcard)).
			Errorf("cborarrow: lr/lrcard length mismatch")
		return
	}
	// Sections with empty lr are wire-aligned no-ops: the producer
	// didn't touch this section so beginSection ran but no attribute
	// did. Skip the dml dispatch entirely.
	if len(s.lr) == 0 {
		return
	}
	switch section {
	// --- scalar value column (BeginAttribute(value)) ---
	case "symbol":
		err = applyScalarSection(b.GetSectionSymbol, s.valString["value"], s, section)
	case "bool":
		err = applyScalarSection(b.GetSectionBool, s.valBool["value"], s, section)
	case "foreignKey":
		err = applyScalarSection(b.GetSectionForeignKey, s.valUint64["value"], s, section)

	// --- range (multi-sub-column scalar, BeginAttribute(beginIncl, endExcl)) ---
	case "u32Range":
		err = applyRangeSection(b.GetSectionU32Range, s.valUint32["beginIncl"], s.valUint32["endExcl"], s, section)

	// --- non-scalar value column (BeginAttribute() + AddToContainer) ---
	case "stringArray":
		err = applyArraySection(b.GetSectionStringArray, s.valString["value"], s, section)
	case "symbolArray":
		err = applyArraySection(b.GetSectionSymbolArray, s.valString["value"], s, section)
	case "textArray":
		err = applyArraySection(b.GetSectionTextArray, s.valString["value"], s, section)
	case "blobArray":
		err = applyArraySection(b.GetSectionBlobArray, s.valBytes["value"], s, section)
	case "u8Array":
		err = applyArraySection(b.GetSectionU8Array, s.valUint8["value"], s, section)
	case "u16Array":
		err = applyArraySection(b.GetSectionU16Array, s.valUint16["value"], s, section)
	case "u32Array":
		err = applyArraySection(b.GetSectionU32Array, s.valUint32["value"], s, section)
	case "u64Array":
		err = applyArraySection(b.GetSectionU64Array, s.valUint64["value"], s, section)
	case "i8Array":
		err = applyArraySection(b.GetSectionI8Array, s.valInt8["value"], s, section)
	case "i16Array":
		err = applyArraySection(b.GetSectionI16Array, s.valInt16["value"], s, section)
	case "i32Array":
		err = applyArraySection(b.GetSectionI32Array, s.valInt32["value"], s, section)
	case "i64Array":
		err = applyArraySection(b.GetSectionI64Array, s.valInt64["value"], s, section)
	case "f32Array":
		err = applyArraySection(b.GetSectionF32Array, s.valFloat32["value"], s, section)
	case "f64Array":
		err = applyArraySection(b.GetSectionF64Array, s.valFloat64["value"], s, section)
	case "u32Set":
		err = applyArraySection(b.GetSectionU32Set, s.valUint32["value"], s, section)
	case "u64Set":
		err = applyArraySection(b.GetSectionU64Set, s.valUint64["value"], s, section)

	// --- timeArray: wire is int64 unix-nanos, dml takes time.Time ---
	case "timeArray":
		raw := s.valInt64["value"]
		vals := make([]time.Time, len(raw))
		for k, ns := range raw {
			vals[k] = time.Unix(0, ns).UTC()
		}
		err = applyArraySection(b.GetSectionTimeArray, vals, s, section)

	default:
		err = eb.Build().Str("section", section).Errorf("cborarrow: unknown section (extend applySection)")
	}
	return
}

// validateArrayShape factors out the per-section bounds checks for
// non-scalar sections. The wire must declare a per-attribute count
// (lr / countsPerAttr same length) and the flat value array must
// hold sum(counts) elements.
func validateArrayShape(s *sectionState, section string, valLen int) (err error) {
	if len(s.lr) != len(s.countsPerAttr) {
		err = eb.Build().Str("section", section).Int("lr", len(s.lr)).Int("counts", len(s.countsPerAttr)).
			Errorf("cborarrow: lr / countsPerAttr length mismatch")
		return
	}
	var total int
	for _, c := range s.countsPerAttr {
		total += int(c)
	}
	if total > valLen {
		err = eb.Build().Str("section", section).Int("total", total).Int("len(val)", valLen).
			Errorf("cborarrow: countsPerAttr sum exceeds value length")
		return
	}
	return
}

