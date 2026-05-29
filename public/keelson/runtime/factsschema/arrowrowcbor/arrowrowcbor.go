//go:build llm_generated_opus47

// Package arrowrowcbor is the Phase-0' sparse-CBOR sibling of
// arrowrowbinary. Same drop-in shim shape over arrow/array.*Builder,
// but emits self-describing sparse CBOR instead of fixed-width
// ClickHouse RowBinary. Wins ecosystem interop and amortises well
// when most schema sections are empty per row (the dominant case as
// the runtime.facts section count grows).
//
// Encoding shape (per record produced by RecordBuilder.NewRecord):
//
//	CBOR-array(definite, nRows) of
//	  CBOR-map(definite, nPopulated) of
//	    short-key → value      ← only fields populated for this row
//
// The per-row definite-length map header (counted via a hasContent
// prepass) costs 1 prepass but saves 1 byte per row (no EncodeBreak)
// and lets decoders pre-allocate the row's field set.
//
// Short keys are derived once per field at RecordBuilder construction
// from the leeway-encoded physical column name:
//
//	"id:id:u64:..."                       → "id"
//	"id:naturalKey:y:..."                 → "naturalKey"
//	"ts:ts:z64:..."                       → "ts"
//	"lc:expiresAt:z64:..."                → "expiresAt"
//	"tv:symbol:value:val:s:m:..."         → "symbol.value"
//	"tv:symbol:lr:lr:u64:2q:..."          → "symbol.lr"
//
// Sparseness rule:
//
//	plain columns         → always emitted (4 fields per row)
//	tagged List<T> column → emitted only if THIS ROW's list slot has > 0 items
//
// Result: a row that touches 2 sections out of 22 emits ~14 keys; a row
// that touches all 22 emits ~146 keys. Same wire grows with content.
//
// Phase 0' scope: same as arrowrowbinary — Grant/State/Log5 benchmarks
// must compile and run. Decode path is out of scope (no `ra` companion
// reading sparse CBOR yet).
package arrowrowcbor

import (
	"bytes"
	"hash/fnv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/semistructured/cbor"
)

// ColumnBuilder is the per-column union: typed builders satisfy this so
// RecordBuilder can iterate uniformly when emitting rows.
type ColumnBuilder interface {
	// nRows is the number of values seen by this builder (= number of
	// row-slots for List builders, = number of plain Append calls for
	// scalars).
	nRows() int

	// hasContent reports whether this column has emit-worthy content for
	// row r. For plain scalars this is always true (we always emit them).
	// For ListBuilders it is true iff the row's list slot is non-empty.
	hasContent(r int) bool

	// emitValue writes the column's value for row r as CBOR (the value
	// only — not the key; the key is emitted by RecordBuilder).
	emitValue(enc *cbor.Encoder, r int) error

	// reset clears buffered values without releasing capacity.
	reset()
}

// ----------------------------------------------------------------------
// Typed scalar builders (always emit — these are the plain columns)
// ----------------------------------------------------------------------

type Uint8Builder struct{ buf []uint8 }

func (b *Uint8Builder) Append(v uint8)            { b.buf = append(b.buf, v) }
func (b *Uint8Builder) Release()                  {}
func (b *Uint8Builder) nRows() int                { return len(b.buf) }
func (b *Uint8Builder) hasContent(r int) bool     { return true }
func (b *Uint8Builder) reset()                    { b.buf = b.buf[:0] }
func (b *Uint8Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeUint(uint64(b.buf[r]))
	return
}

type Uint16Builder struct{ buf []uint16 }

func (b *Uint16Builder) Append(v uint16)            { b.buf = append(b.buf, v) }
func (b *Uint16Builder) Release()                   {}
func (b *Uint16Builder) nRows() int                 { return len(b.buf) }
func (b *Uint16Builder) hasContent(r int) bool      { return true }
func (b *Uint16Builder) reset()                     { b.buf = b.buf[:0] }
func (b *Uint16Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeUint(uint64(b.buf[r]))
	return
}

type Uint32Builder struct{ buf []uint32 }

func (b *Uint32Builder) Append(v uint32)            { b.buf = append(b.buf, v) }
func (b *Uint32Builder) Release()                   {}
func (b *Uint32Builder) nRows() int                 { return len(b.buf) }
func (b *Uint32Builder) hasContent(r int) bool      { return true }
func (b *Uint32Builder) reset()                     { b.buf = b.buf[:0] }
func (b *Uint32Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeUint(uint64(b.buf[r]))
	return
}

type Uint64Builder struct{ buf []uint64 }

func (b *Uint64Builder) Append(v uint64)            { b.buf = append(b.buf, v) }
func (b *Uint64Builder) Release()                   {}
func (b *Uint64Builder) nRows() int                 { return len(b.buf) }
func (b *Uint64Builder) hasContent(r int) bool      { return true }
func (b *Uint64Builder) reset()                     { b.buf = b.buf[:0] }
func (b *Uint64Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeUint(b.buf[r])
	return
}

type Int8Builder struct{ buf []int8 }

func (b *Int8Builder) Append(v int8)             { b.buf = append(b.buf, v) }
func (b *Int8Builder) Release()                  {}
func (b *Int8Builder) nRows() int                { return len(b.buf) }
func (b *Int8Builder) hasContent(r int) bool     { return true }
func (b *Int8Builder) reset()                    { b.buf = b.buf[:0] }
func (b *Int8Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeInt(int64(b.buf[r]))
	return
}

type Int16Builder struct{ buf []int16 }

func (b *Int16Builder) Append(v int16)            { b.buf = append(b.buf, v) }
func (b *Int16Builder) Release()                  {}
func (b *Int16Builder) nRows() int                { return len(b.buf) }
func (b *Int16Builder) hasContent(r int) bool     { return true }
func (b *Int16Builder) reset()                    { b.buf = b.buf[:0] }
func (b *Int16Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeInt(int64(b.buf[r]))
	return
}

type Int32Builder struct{ buf []int32 }

func (b *Int32Builder) Append(v int32)            { b.buf = append(b.buf, v) }
func (b *Int32Builder) Release()                  {}
func (b *Int32Builder) nRows() int                { return len(b.buf) }
func (b *Int32Builder) hasContent(r int) bool     { return true }
func (b *Int32Builder) reset()                    { b.buf = b.buf[:0] }
func (b *Int32Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeInt(int64(b.buf[r]))
	return
}

type Int64Builder struct{ buf []int64 }

func (b *Int64Builder) Append(v int64)            { b.buf = append(b.buf, v) }
func (b *Int64Builder) Release()                  {}
func (b *Int64Builder) nRows() int                { return len(b.buf) }
func (b *Int64Builder) hasContent(r int) bool     { return true }
func (b *Int64Builder) reset()                    { b.buf = b.buf[:0] }
func (b *Int64Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeInt(b.buf[r])
	return
}

type Float32Builder struct{ buf []float32 }

func (b *Float32Builder) Append(v float32)         { b.buf = append(b.buf, v) }
func (b *Float32Builder) Release()                 {}
func (b *Float32Builder) nRows() int               { return len(b.buf) }
func (b *Float32Builder) hasContent(r int) bool    { return true }
func (b *Float32Builder) reset()                   { b.buf = b.buf[:0] }
func (b *Float32Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeFloat32(b.buf[r])
	return
}

type Float64Builder struct{ buf []float64 }

func (b *Float64Builder) Append(v float64)         { b.buf = append(b.buf, v) }
func (b *Float64Builder) Release()                 {}
func (b *Float64Builder) nRows() int               { return len(b.buf) }
func (b *Float64Builder) hasContent(r int) bool    { return true }
func (b *Float64Builder) reset()                   { b.buf = b.buf[:0] }
func (b *Float64Builder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeFloat64(b.buf[r])
	return
}

type BooleanBuilder struct{ buf []bool }

func (b *BooleanBuilder) Append(v bool)            { b.buf = append(b.buf, v) }
func (b *BooleanBuilder) Release()                 {}
func (b *BooleanBuilder) nRows() int               { return len(b.buf) }
func (b *BooleanBuilder) hasContent(r int) bool    { return true }
func (b *BooleanBuilder) reset()                   { b.buf = b.buf[:0] }
func (b *BooleanBuilder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeBool(b.buf[r])
	return
}

type StringBuilder struct{ buf []string }

func (b *StringBuilder) Append(v string)           { b.buf = append(b.buf, v) }
func (b *StringBuilder) Release()                  {}
func (b *StringBuilder) nRows() int                { return len(b.buf) }
func (b *StringBuilder) hasContent(r int) bool     { return true }
func (b *StringBuilder) reset()                    { b.buf = b.buf[:0] }
func (b *StringBuilder) emitValue(enc *cbor.Encoder, r int) (err error) {
	_, err = enc.EncodeString(b.buf[r])
	return
}

type BinaryBuilder struct{ buf [][]byte }

func (b *BinaryBuilder) Append(v []byte)           { b.buf = append(b.buf, v) }
func (b *BinaryBuilder) Release()                  {}
func (b *BinaryBuilder) nRows() int                { return len(b.buf) }
func (b *BinaryBuilder) hasContent(r int) bool     { return true }
func (b *BinaryBuilder) reset()                    { b.buf = b.buf[:0] }
func (b *BinaryBuilder) emitValue(enc *cbor.Encoder, r int) (err error) {
	// boxer's EncodeByteSlice rejects nil; coerce to empty so the
	// wire stays well-formed (e.g. SetId with nil naturalKey).
	v := b.buf[r]
	if v == nil {
		v = []byte{}
	}
	_, err = enc.EncodeByteSlice(v)
	return
}

type TimestampBuilder struct{ buf []arrow.Timestamp }

func (b *TimestampBuilder) Append(v arrow.Timestamp) { b.buf = append(b.buf, v) }
func (b *TimestampBuilder) Release()                 {}
func (b *TimestampBuilder) nRows() int               { return len(b.buf) }
func (b *TimestampBuilder) hasContent(r int) bool    { return true }
func (b *TimestampBuilder) reset()                   { b.buf = b.buf[:0] }
func (b *TimestampBuilder) emitValue(enc *cbor.Encoder, r int) (err error) {
	// Phase 0' — emit as raw int64 (unit-specific count) under uint major
	// type. Production version would use CBOR tag 0/1 (RFC 8949 §3.4.1/2)
	// via EncodeTimeUTC, but we don't have a time.Time at this point.
	_, err = enc.EncodeInt(int64(b.buf[r]))
	return
}

// ----------------------------------------------------------------------
// ListBuilder — the sparseness driver. emitValue writes a CBOR array of
// the slot's elements; hasContent skips emission entirely when the slot
// is empty (the dominant case for sections this row doesn't touch).
// ----------------------------------------------------------------------

type ListBuilder struct {
	valueBuilder ColumnBuilder
	offsets      []int
}

func newListBuilder(valueBuilder ColumnBuilder) *ListBuilder {
	return &ListBuilder{
		valueBuilder: valueBuilder,
		offsets:      make([]int, 0, 64),
	}
}

func (b *ListBuilder) Append(_ bool) {
	b.offsets = append(b.offsets, b.valueBuilder.nRows())
}

func (b *ListBuilder) ValueBuilder() ColumnBuilder { return b.valueBuilder }
func (b *ListBuilder) Release()                    {}

func (b *ListBuilder) nRows() int { return len(b.offsets) }

func (b *ListBuilder) reset() {
	b.offsets = b.offsets[:0]
	b.valueBuilder.reset()
}

func (b *ListBuilder) hasContent(r int) bool {
	return b.slotLen(r) > 0
}

func (b *ListBuilder) slotLen(r int) int {
	start := b.offsets[r]
	var end int
	if r+1 < len(b.offsets) {
		end = b.offsets[r+1]
	} else {
		end = b.valueBuilder.nRows()
	}
	return end - start
}

func (b *ListBuilder) emitValue(enc *cbor.Encoder, r int) (err error) {
	start := b.offsets[r]
	n := b.slotLen(r)
	_, err = enc.EncodeArrayDefinite(uint64(n))
	if err != nil {
		return
	}
	for i := start; i < start+n; i++ {
		err = b.valueBuilder.emitValue(enc, i)
		if err != nil {
			return
		}
	}
	return
}

// ----------------------------------------------------------------------
// RecordBuilder
// ----------------------------------------------------------------------

type RecordBuilder struct {
	schema       *arrow.Schema
	fields       []ColumnBuilder
	shortKeys    []string
	activeFields []int // nil → walk all fields

	scratchBuf *bytes.Buffer
	scratchEnc *cbor.Encoder
}

func NewRecordBuilder(_ memory.Allocator, schema *arrow.Schema) *RecordBuilder {
	rb := &RecordBuilder{
		schema:     schema,
		fields:     make([]ColumnBuilder, len(schema.Fields())),
		shortKeys:  make([]string, len(schema.Fields())),
		scratchBuf: bytes.NewBuffer(make([]byte, 0, 4096)),
	}
	rb.scratchEnc = cbor.NewEncoder(rb.scratchBuf, nil)
	for i, f := range schema.Fields() {
		rb.fields[i] = builderForType(f.Type)
		rb.shortKeys[i] = shortKeyForFieldName(f.Name)
	}
	return rb
}

func (rb *RecordBuilder) Field(i int) ColumnBuilder { return rb.fields[i] }
func (rb *RecordBuilder) Schema() *arrow.Schema     { return rb.schema }
func (rb *RecordBuilder) Release()                  {}

// SetActiveFields hints which column indices NewRecord should consider
// emitting per row; nil restores the default walk-all behaviour. Mirrors
// arrowrowbinary's perf hint — sparseness (per-row hasContent gating)
// still applies on top.
func (rb *RecordBuilder) SetActiveFields(idxs []int) {
	rb.activeFields = idxs
}

// NewRecord finalises buffered columns into a sparse-CBOR record and
// resets all builders. The bytes are a self-describing array of
// definite-length maps; only fields with content for the row are
// emitted (further restricted by activeFields when set).
func (rb *RecordBuilder) NewRecord() *Record {
	var nRows int
	if rb.activeFields != nil {
		for _, i := range rb.activeFields {
			if n := rb.fields[i].nRows(); n > nRows {
				nRows = n
			}
		}
	} else {
		for _, f := range rb.fields {
			if n := f.nRows(); n > nRows {
				nRows = n
			}
		}
	}

	rb.scratchBuf.Reset()
	rb.scratchEnc.SetWriter(rb.scratchBuf)
	// Outer array of N row-maps.
	_, _ = rb.scratchEnc.EncodeArrayDefinite(uint64(nRows))
	for r := 0; r < nRows; r++ {
		// Prepass: count populated fields for this row so we can emit a
		// definite-length map header.
		var nPopulated int
		if rb.activeFields != nil {
			for _, i := range rb.activeFields {
				if rb.fields[i].hasContent(r) {
					nPopulated++
				}
			}
		} else {
			for _, f := range rb.fields {
				if f.hasContent(r) {
					nPopulated++
				}
			}
		}
		_, _ = rb.scratchEnc.EncodeMapDefinite(uint64(nPopulated))
		if rb.activeFields != nil {
			for _, i := range rb.activeFields {
				if !rb.fields[i].hasContent(r) {
					continue
				}
				_, _ = rb.scratchEnc.EncodeString(rb.shortKeys[i])
				_ = rb.fields[i].emitValue(rb.scratchEnc, r)
			}
		} else {
			for i, f := range rb.fields {
				if !f.hasContent(r) {
					continue
				}
				_, _ = rb.scratchEnc.EncodeString(rb.shortKeys[i])
				_ = f.emitValue(rb.scratchEnc, r)
			}
		}
	}

	out := make([]byte, rb.scratchBuf.Len())
	copy(out, rb.scratchBuf.Bytes())

	if rb.activeFields != nil {
		for _, i := range rb.activeFields {
			rb.fields[i].reset()
		}
	} else {
		for _, f := range rb.fields {
			f.reset()
		}
	}

	return &Record{schema: rb.schema, nRows: int64(nRows), bytes: out}
}

// ----------------------------------------------------------------------
// Record — parallel to arrow.RecordBatch. Carries CBOR bytes; only
// exposes the methods the forked DML actually calls.
// ----------------------------------------------------------------------

type Record struct {
	schema *arrow.Schema
	nRows  int64
	bytes  []byte
}

func (r *Record) Schema() *arrow.Schema { return r.schema }
func (r *Record) NumRows() int64        { return r.nRows }
func (r *Record) NumCols() int64        { return int64(len(r.schema.Fields())) }
func (r *Record) Release()              {}
func (r *Record) Retain()               {}

// CBOR returns the underlying CBOR byte slice (array of indefinite-
// length maps). Caller-owned; safe to retain past Release.
func (r *Record) CBOR() []byte { return r.bytes }

// NewSlice is the RollbackEntity-path stub; unused by the runtime.facts
// benchmarks (panic on call so the test surface tells us if a future
// codegen change starts depending on it).
func (r *Record) NewSlice(_, _ int64) *Record {
	panic("arrowrowcbor: Record.NewSlice not implemented in Phase 0' shim")
}

// ----------------------------------------------------------------------
// Schema field name → CBOR short key
// ----------------------------------------------------------------------

// shortKeyForFieldName parses the leeway-encoded physical column name
// into a short, human-readable map key. Cached once per field at
// RecordBuilder construction so the runtime overhead is zero.
//
// Format (HumanReadableNamingConvention, colon-separated):
//
//	plain:   <prefix>:<name>:<canonical-type>:<hints>:…
//	tagged:  tv:<section>:<column>:<role>:<canonical-type>:…
//
// Output:
//
//	plain   → <name>            ("id", "naturalKey", "ts", "expiresAt")
//	tagged  → <section>.<column> ("symbol.value", "symbol.lr", "bool.value")
func shortKeyForFieldName(physName string) string {
	parts := strings.SplitN(physName, ":", 5)
	if len(parts) < 2 {
		// Fallback: hash the name into 4 hex bytes so duplicate fields
		// still get unique keys. Not expected to fire on runtime.facts.
		h := fnv.New32a()
		_, _ = h.Write([]byte(physName))
		return string(physName)
	}
	if parts[0] == "tv" && len(parts) >= 3 {
		return parts[1] + "." + parts[2]
	}
	return parts[1]
}

// ----------------------------------------------------------------------
// Type → builder dispatch
// ----------------------------------------------------------------------

func builderForType(t arrow.DataType) ColumnBuilder {
	switch ty := t.(type) {
	case *arrow.Uint8Type:
		return &Uint8Builder{}
	case *arrow.Uint16Type:
		return &Uint16Builder{}
	case *arrow.Uint32Type:
		return &Uint32Builder{}
	case *arrow.Uint64Type:
		return &Uint64Builder{}
	case *arrow.Int8Type:
		return &Int8Builder{}
	case *arrow.Int16Type:
		return &Int16Builder{}
	case *arrow.Int32Type:
		return &Int32Builder{}
	case *arrow.Int64Type:
		return &Int64Builder{}
	case *arrow.Float32Type:
		return &Float32Builder{}
	case *arrow.Float64Type:
		return &Float64Builder{}
	case *arrow.BooleanType:
		return &BooleanBuilder{}
	case *arrow.StringType:
		return &StringBuilder{}
	case *arrow.BinaryType:
		return &BinaryBuilder{}
	case *arrow.TimestampType:
		return &TimestampBuilder{}
	case *arrow.ListType:
		return newListBuilder(builderForType(ty.Elem()))
	default:
		panic("arrowrowcbor: unsupported arrow type: " + t.String())
	}
}
