package cborarrow

import (
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	cbor "github.com/fxamacker/cbor/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"
)

// Convert reads sparse-CBOR-encoded runtime.facts rows from `in` (as
// produced by arrowrowcbor.RecordBuilder.NewRecord — an outer CBOR
// array of per-row CBOR maps with short-key dispatch), drives
// dml.InEntityFacts to build Arrow records, and writes an ArrowStream
// IPC payload to `out`.
//
// CBOR is self-delimiting; no row count or schema is required from
// the caller. Sections the writer did not touch land as empty Array
// entries in the Arrow output — the receiving side sees the canonical
// runtime.facts schema regardless of which fact kind the row carries.
func Convert(in io.Reader, out io.Writer) (err error) {
	var rows []map[string]cbor.RawMessage
	dec := cbor.NewDecoder(in)
	err = dec.Decode(&rows)
	if err != nil {
		err = eh.Errorf("cborarrow: outer decode: %w", err)
		return
	}

	allocator := memory.NewGoAllocator()
	builder := dml.NewInEntityFacts(allocator, len(rows))

	for r, rowMap := range rows {
		var row rowState
		row.init()
		for key, raw := range rowMap {
			err = dispatchKey(&row, key, raw)
			if err != nil {
				err = eb.Build().Int("row", r).Str("key", key).Errorf("cborarrow: dispatch: %w", err)
				return
			}
		}
		err = row.applyTo(builder)
		if err != nil {
			err = eb.Build().Int("row", r).Errorf("cborarrow: apply row: %w", err)
			return
		}
	}

	var records []arrow.RecordBatch
	records, err = builder.TransferRecords(nil)
	if err != nil {
		err = eh.Errorf("cborarrow: transfer records: %w", err)
		return
	}

	w := ipc.NewWriter(out, ipc.WithSchema(builder.GetSchema()), ipc.WithAllocator(allocator))
	defer func() {
		if cerr := w.Close(); cerr != nil && err == nil {
			err = eh.Errorf("cborarrow: ipc close: %w", cerr)
		}
	}()

	for _, rec := range records {
		err = w.Write(rec)
		rec.Release()
		if err != nil {
			err = eh.Errorf("cborarrow: ipc write: %w", err)
			return
		}
	}
	return
}

// dispatchKey routes one (key, value) CBOR pair from a row map into
// rowState. Key shape matches arrowrowcbor.shortKeyForFieldName:
//
//	plain leeway column  → unqualified  ("id", "naturalKey", "ts", "expiresAt")
//	tagged leeway column → "<section>.<column>" ("symbol.value", "u32Array.lrcard")
func dispatchKey(rs *rowState, key string, raw cbor.RawMessage) (err error) {
	for i := 0; i < len(key); i++ {
		if key[i] == '.' {
			err = dispatchTaggedKey(rs, key[:i], key[i+1:], raw)
			return
		}
	}
	err = dispatchPlainKey(rs, key, raw)
	return
}

func dispatchPlainKey(rs *rowState, key string, raw cbor.RawMessage) (err error) {
	switch key {
	case "id":
		var v uint64
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		rs.id = v
		rs.hasId = true
	case "naturalKey":
		var v []byte
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		rs.naturalKey = v
		rs.hasNaturalKey = true
	case "ts":
		var v int64
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		rs.tsNanos = v
		rs.hasTs = true
	case "expiresAt":
		var v int64
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		rs.expiresAtNanos = v
		rs.hasExpiresAt = true
	default:
		err = eb.Build().Str("key", key).Errorf("cborarrow: unknown plain key")
	}
	return
}

// dispatchTaggedKey handles "<section>.<column>" pairs. The `column`
// suffix routes role-keyed columns (lr / lrcard / len / card and the
// consume-and-discard hr / lmr / mrhp / hrcard / lmrcard) uniformly;
// value / beginIncl / endExcl dispatch to the section's typed slot.
func dispatchTaggedKey(rs *rowState, section, column string, raw cbor.RawMessage) (err error) {
	s := rs.section(section)

	switch column {
	case "lr":
		err = cbor.Unmarshal(raw, &s.lr)
	case "lrcard":
		err = cbor.Unmarshal(raw, &s.lrcard)
	case "len", "card":
		err = cbor.Unmarshal(raw, &s.countsPerAttr)
	case "hr", "lmr", "hrcard", "lmrcard", "mrhp":
		// Consume + discard. The HighCardRef (`hr` / `hrcard`),
		// MixedLowCardRef (`lmr` / `lmrcard`), and
		// MixedRefHighCardParameters (`mrhp`) roles are part of the
		// leeway schema but no in-tree fact kind populates them — the
		// codec's Marshal emits these columns as empty per-attribute
		// slots (`81 00` = array(1) of [0] for the cards; map header
		// counts them as populated only when the producer-side
		// hasContent passes, which for these never fires today).
		//
		// We still match the key so the dispatcher doesn't error on
		// rows that DO include them (forward-compat with a future
		// HighCard-using kind), but apply machinery has no read path
		// — the first such kind needs to wire one in applySection
		// alongside lr/lrcard.
	case "value", "beginIncl", "endExcl":
		err = dispatchValueColumn(s, section, column, raw)
	default:
		err = eb.Build().Str("section", section).Str("column", column).Errorf("cborarrow: unsupported tagged column")
	}
	return
}

// dispatchValueColumn decodes the section's typed value slice. Section
// → canonical-type mapping mirrors rowbinaryarrow.taggedValueReader.
func dispatchValueColumn(s *sectionState, section, column string, raw cbor.RawMessage) (err error) {
	switch section {
	case "string", "stringArray", "text", "textArray", "symbol", "symbolArray":
		var v []string
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valString == nil {
			s.valString = make(map[string][]string, 1)
		}
		s.valString[column] = v
	case "blob", "blobArray":
		var v [][]byte
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valBytes == nil {
			s.valBytes = make(map[string][][]byte, 1)
		}
		s.valBytes[column] = v
	case "bool":
		var v []bool
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valBool == nil {
			s.valBool = make(map[string][]bool, 1)
		}
		s.valBool[column] = v
	case "u8", "u8Array":
		var v []uint8
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valUint8 == nil {
			s.valUint8 = make(map[string][]uint8, 1)
		}
		s.valUint8[column] = v
	case "u16", "u16Array":
		var v []uint16
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valUint16 == nil {
			s.valUint16 = make(map[string][]uint16, 1)
		}
		s.valUint16[column] = v
	case "u32", "u32Array", "u32Set", "u32Range":
		var v []uint32
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valUint32 == nil {
			s.valUint32 = make(map[string][]uint32, 1)
		}
		s.valUint32[column] = v
	case "u64", "u64Array", "u64Set", "foreignKey":
		var v []uint64
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valUint64 == nil {
			s.valUint64 = make(map[string][]uint64, 1)
		}
		s.valUint64[column] = v
	case "i8", "i8Array":
		var v []int8
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valInt8 == nil {
			s.valInt8 = make(map[string][]int8, 1)
		}
		s.valInt8[column] = v
	case "i16", "i16Array":
		var v []int16
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valInt16 == nil {
			s.valInt16 = make(map[string][]int16, 1)
		}
		s.valInt16[column] = v
	case "i32", "i32Array":
		var v []int32
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valInt32 == nil {
			s.valInt32 = make(map[string][]int32, 1)
		}
		s.valInt32[column] = v
	case "i64", "i64Array", "time", "timeArray":
		// time / timeArray ride the int64-nanos wire (z64 = DateTime64(9)).
		var v []int64
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valInt64 == nil {
			s.valInt64 = make(map[string][]int64, 1)
		}
		s.valInt64[column] = v
	case "f32", "f32Array":
		var v []float32
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valFloat32 == nil {
			s.valFloat32 = make(map[string][]float32, 1)
		}
		s.valFloat32[column] = v
	case "f64", "f64Array":
		var v []float64
		err = cbor.Unmarshal(raw, &v)
		if err != nil {
			return
		}
		if s.valFloat64 == nil {
			s.valFloat64 = make(map[string][]float64, 1)
		}
		s.valFloat64[column] = v
	default:
		err = eb.Build().Str("section", section).Str("column", column).Errorf("cborarrow: unsupported value section")
	}
	return
}
