//go:build llm_generated_opus46

package card

import (
	"encoding/json/jsontext"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

var _ streamreadaccess.SinkI = (*JsonCardEmitter)(nil)

// JsonCardEmitter renders Leeway entities as streaming JSON using jsontext.Encoder.
// Zero buffering — tokens are written directly to the underlying io.Writer.
//
// Output structure:
//
//	[                                            // batch
//	  {                                          // entity
//	    "plainSections": [ ... ],
//	    "taggedSections": [
//	      {
//	        "name": "symbol",
//	        "columns": [{"name":"value","type":"s"}],
//	        "nAttrs": 1,
//	        "attributes": [
//	          {
//	            "values": { "value": "HEARTBEAT" },
//	            "tags": [ ... ]
//	          }
//	        ]
//	      }
//	    ]
//	  }
//	]
//
// Names are emitted using StylableName.String() — the IR is assumed to already
// carry the desired naming style.
type JsonCardEmitter struct {
	enc *jsontext.Encoder

	currentColName naming.StylableName
	valuesOpen     bool
	inCollection   bool

	err error
}

func NewJsonCardEmitter(enc *jsontext.Encoder) (inst *JsonCardEmitter) {
	inst = &JsonCardEmitter{
		enc: enc,
	}
	return
}

// --- token helpers ---

func (inst *JsonCardEmitter) writeToken(t jsontext.Token) {
	if inst.err != nil {
		return
	}
	err := inst.enc.WriteToken(t)
	if err != nil {
		inst.err = err
	}
}

func (inst *JsonCardEmitter) writeKey(key string) {
	inst.writeToken(jsontext.String(key))
}

func (inst *JsonCardEmitter) writeName(name naming.StylableName) {
	inst.writeToken(jsontext.String(name.String()))
}

func (inst *JsonCardEmitter) writeStringValue(s string) {
	inst.writeToken(jsontext.String(s))
}

func (inst *JsonCardEmitter) writeUint64Value(v uint64) {
	inst.writeToken(jsontext.Uint(v))
}

func (inst *JsonCardEmitter) writeBoolValue(v bool) {
	inst.writeToken(jsontext.Bool(v))
}

func (inst *JsonCardEmitter) writeIntValue(v int) {
	inst.writeToken(jsontext.Int(int64(v)))
}

func (inst *JsonCardEmitter) beginObject() { inst.writeToken(jsontext.BeginObject) }
func (inst *JsonCardEmitter) endObject()   { inst.writeToken(jsontext.EndObject) }
func (inst *JsonCardEmitter) beginArray()  { inst.writeToken(jsontext.BeginArray) }
func (inst *JsonCardEmitter) endArray()    { inst.writeToken(jsontext.EndArray) }

func (inst *JsonCardEmitter) writeCanonicalType(ct canonicaltypes.PrimitiveAstNodeI) {
	if ct != nil {
		inst.writeStringValue(ct.String())
	} else {
		inst.writeToken(jsontext.Null)
	}
}

func (inst *JsonCardEmitter) writeColumnSchema(valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI) {
	inst.writeKey("columns")
	inst.beginArray()
	for i, n := range valueNames {
		inst.beginObject()
		inst.writeKey("name")
		inst.writeName(n)
		inst.writeKey("type")
		if i < len(valueCanonicalTypes) {
			inst.writeCanonicalType(valueCanonicalTypes[i])
		} else {
			inst.writeToken(jsontext.Null)
		}
		inst.endObject()
	}
	inst.endArray()
}

// --- Batch ---

func (inst *JsonCardEmitter) BeginBatch() {
	inst.beginArray()
}

func (inst *JsonCardEmitter) EndBatch() (err error) {
	inst.endArray()
	return inst.err
}

// --- Entity ---

func (inst *JsonCardEmitter) BeginEntity() {
	inst.beginObject()
	inst.writeKey("plainSections")
	inst.beginArray()
}

func (inst *JsonCardEmitter) EndEntity() (err error) {
	inst.endObject() // entity
	return inst.err
}

// --- Plain section ---

func (inst *JsonCardEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.beginObject()

	inst.writeKey("itemType")
	inst.writeStringValue(itemType.String())

	inst.writeColumnSchema(valueNames, valueCanonicalTypes)
}

func (inst *JsonCardEmitter) EndPlainSection() (err error) {
	inst.endObject() // plain section
	return inst.err
}

// --- Plain value ---

func (inst *JsonCardEmitter) BeginPlainValue() {
	inst.writeKey("values")
	inst.beginObject()
	inst.valuesOpen = true
}

func (inst *JsonCardEmitter) EndPlainValue() (err error) {
	if inst.valuesOpen {
		inst.endObject() // values
		inst.valuesOpen = false
	}
	return inst.err
}

// --- Tagged sections scope ---

func (inst *JsonCardEmitter) BeginTaggedSections() {
	inst.endArray() // close plainSections
	inst.writeKey("taggedSections")
	inst.beginArray()
}

func (inst *JsonCardEmitter) EndTaggedSections() (err error) {
	inst.endArray() // close taggedSections
	return inst.err
}

// --- Co-section group ---

func (inst *JsonCardEmitter) BeginCoSectionGroup(name naming.Key) {
	inst.beginObject()
	inst.writeKey("coGroup")
	inst.writeStringValue(string(name))
	inst.writeKey("sections")
	inst.beginArray()
}

func (inst *JsonCardEmitter) EndCoSectionGroup() (err error) {
	inst.endArray()  // sections
	inst.endObject() // co-group
	return inst.err
}

// --- Section ---

func (inst *JsonCardEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.beginObject()

	inst.writeKey("name")
	inst.writeName(name)

	inst.writeColumnSchema(valueNames, valueCanonicalTypes)

	inst.writeKey("nAttrs")
	inst.writeIntValue(nAttrs)

	inst.writeKey("attributes")
	inst.beginArray()
}

func (inst *JsonCardEmitter) EndSection() (err error) {
	inst.endArray()  // attributes
	inst.endObject() // section
	return inst.err
}

// --- Tagged value ---

func (inst *JsonCardEmitter) BeginTaggedValue() {
	inst.beginObject()
	inst.writeKey("values")
	inst.beginObject()
	inst.valuesOpen = true
}

func (inst *JsonCardEmitter) EndTaggedValue() (err error) {
	if inst.valuesOpen {
		inst.endObject()
		inst.valuesOpen = false
	}
	inst.endObject() // attribute
	return inst.err
}

// --- Column ---

func (inst *JsonCardEmitter) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI) {
	inst.currentColName = name
	inst.inCollection = false
	// Write column name as key in "values" object
	inst.writeName(name)
}

func (inst *JsonCardEmitter) EndColumn() {
	inst.inCollection = false
}

// --- Scalar ---

func (inst *JsonCardEmitter) BeginScalarValue() {
	inst.inCollection = false
}

func (inst *JsonCardEmitter) EndScalarValue() (err error) {
	return inst.err
}

// --- Array ---

func (inst *JsonCardEmitter) BeginHomogenousArrayValue(card int) {
	inst.inCollection = true
	inst.beginArray()
}

func (inst *JsonCardEmitter) EndHomogenousArrayValue() {
	inst.endArray()
	inst.inCollection = false
}

// --- Set ---

func (inst *JsonCardEmitter) BeginSetValue(card int) {
	inst.inCollection = true
	// Wrap in {"set": [...]} to distinguish from ordered arrays
	inst.beginObject()
	inst.writeKey("set")
	inst.beginArray()
}

func (inst *JsonCardEmitter) EndSetValue() {
	inst.endArray()
	inst.endObject()
	inst.inCollection = false
}

// --- Value item ---

func (inst *JsonCardEmitter) BeginValueItem(index int) {}
func (inst *JsonCardEmitter) EndValueItem()            {}

// --- Write ---

func (inst *JsonCardEmitter) Write(p []byte) (n int, err error) {
	return inst.WriteString(string(p))
}

func (inst *JsonCardEmitter) WriteString(s string) (n int, err error) {
	n = len(s)
	inst.writeStringValue(s)
	return
}

// --- Tags ---

func (inst *JsonCardEmitter) BeginTags(nTags int) {
	if inst.valuesOpen {
		inst.endObject()
		inst.valuesOpen = false
	}
	inst.writeKey("tags")
	inst.beginArray()
}

func (inst *JsonCardEmitter) EndTags() {
	inst.endArray()
}

func (inst *JsonCardEmitter) AddMembershipRef(lowCard bool, ref uint64, humanReadableRef string) {
	inst.beginObject()
	inst.writeKey("type")
	inst.writeStringValue("ref")
	inst.writeKey("lowCard")
	inst.writeBoolValue(lowCard)
	inst.writeKey("ref")
	inst.writeUint64Value(ref)
	inst.writeKey("display")
	inst.writeStringValue(humanReadableRef)
	inst.endObject()
}

func (inst *JsonCardEmitter) AddMembershipVerbatim(lowCard bool, verbatim string, humanReadableVerbatim string) {
	inst.beginObject()
	inst.writeKey("type")
	inst.writeStringValue("verbatim")
	inst.writeKey("lowCard")
	inst.writeBoolValue(lowCard)
	inst.writeKey("value")
	inst.writeStringValue(verbatim)
	inst.writeKey("display")
	inst.writeStringValue(humanReadableVerbatim)
	inst.endObject()
}

func (inst *JsonCardEmitter) AddMembershipRefParametrized(lowCard bool, ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	inst.beginObject()
	inst.writeKey("type")
	inst.writeStringValue("refParam")
	inst.writeKey("lowCard")
	inst.writeBoolValue(lowCard)
	inst.writeKey("ref")
	inst.writeUint64Value(ref)
	inst.writeKey("display")
	inst.writeStringValue(humanReadableRef)
	if params != "" {
		inst.writeKey("params")
		inst.writeStringValue(params)
		inst.writeKey("paramsDisplay")
		inst.writeStringValue(humanReadableParams)
	}
	inst.endObject()
}

func (inst *JsonCardEmitter) AddMembershipMixedLowCardRefHighCardParam(ref uint64, humanReadableRef string, params string, humanReadableParams string) {
	inst.beginObject()
	inst.writeKey("type")
	inst.writeStringValue("mixedRef")
	inst.writeKey("ref")
	inst.writeUint64Value(ref)
	inst.writeKey("display")
	inst.writeStringValue(humanReadableRef)
	if params != "" {
		inst.writeKey("params")
		inst.writeStringValue(params)
		inst.writeKey("paramsDisplay")
		inst.writeStringValue(humanReadableParams)
	}
	inst.endObject()
}

func (inst *JsonCardEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, humanReadableVerbatim string, params string, humanReadableParams string) {
	inst.beginObject()
	inst.writeKey("type")
	inst.writeStringValue("mixedVerbatim")
	inst.writeKey("value")
	inst.writeStringValue(verbatim)
	inst.writeKey("display")
	inst.writeStringValue(humanReadableVerbatim)
	if params != "" {
		inst.writeKey("params")
		inst.writeStringValue(params)
		inst.writeKey("paramsDisplay")
		inst.writeStringValue(humanReadableParams)
	}
	inst.endObject()
}
