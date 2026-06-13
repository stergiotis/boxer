package card

import (
	"encoding/json/jsontext"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

var _ streamreadaccess.SinkI = (*JsonCardEmitter)(nil)
var _ streamreadaccess.MembershipSinkI = (*JsonCardEmitter)(nil)

// JsonCardEmitterOption configures a JsonCardEmitter.
type JsonCardEmitterOption func(*JsonCardEmitter)

// WithClassifier overrides the default membership-role classifier.
func WithClassifier(c membershiprole.ClassifierI) JsonCardEmitterOption {
	return func(inst *JsonCardEmitter) { inst.classifier = c }
}

// WithRenderer overrides the default membership renderer (value→string).
func WithRenderer(r *membership.Renderer) JsonCardEmitterOption {
	return func(inst *JsonCardEmitter) { inst.renderer = r }
}

// WithNDJSON emits a header object on the first line followed by one entity
// object per line, instead of a wrapping batch object.
func WithNDJSON() JsonCardEmitterOption {
	return func(inst *JsonCardEmitter) { inst.ndjson = true }
}

// WithSchemaFingerprint sets the schemaFingerprint field written into the
// data document header. Compute it via JsonCardSchemaEmitter before calling
// the data emitter. Empty string suppresses the field.
func WithSchemaFingerprint(fp string) JsonCardEmitterOption {
	return func(inst *JsonCardEmitter) { inst.schemaFingerprint = fp }
}

// WithSchemaDocument supplies the canonical schema-document bytes (as
// produced by JsonCardSchemaEmitter). In NDJSON mode the bytes are
// embedded as the first line with a `leewayCardData:"1"` discriminator
// prepended, so a single-stream consumer can read both schema and data.
// In batch-object mode the schema document is a sidecar; this option is
// ignored.
func WithSchemaDocument(doc []byte) JsonCardEmitterOption {
	return func(inst *JsonCardEmitter) { inst.schemaDocument = doc }
}

// JsonCardEmitter renders Leeway entities as canonical card-JSON per ADR-0018.
//
// Output (batch-object mode):
//
//	{
//	  "leewayCardData": "1",
//	  "entities": [
//	    {
//	      "byStructure": {
//	        "plain": [{"itemType": "...", "values": {...}}],
//	        "sections": [{"name": "...", "nAttrs": N}, ...],
//	        "coGroups": [{"key": "...", "sections": [...], "nAttrs": N}]
//	      },
//	      "byAttribute": {
//	        "<primaryKey>": {"section": "...", "scalar"|"value"|"values"|"indexed": ...,
//	                         "labels": [...]?, "aliases": [...]?, "coGroup": "..."?}
//	      }
//	    }
//	  ]
//	}
//
// NDJSON mode emits the header object as the first line and one entity per
// subsequent line (no outer "entities" array).
//
// Per-section UseAspects arrive through SinkI.BeginSection; callers driving
// the sink directly (without the boxer Driver) can pass useaspects.EmptyAspectSet
// when a section has no relevant aspects.
type JsonCardEmitter struct {
	enc               *jsontext.Encoder
	classifier        membershiprole.ClassifierI
	renderer          *membership.Renderer
	schemaFingerprint string
	schemaDocument    []byte // pre-encoded schema bytes; embedded as first NDJSON line when set
	ndjson            bool

	// Per-entity state, reset at BeginEntity.
	plainBuf      []plainSectionRec
	sectionBuf    []sectionShapeRec
	coGroupBuf    []coGroupRec
	attributes    []attributeRec
	headerWritten bool

	// Per-plain-section state.
	curPlainItemType  common.PlainItemTypeE
	curPlainColNames  []naming.StylableName
	curPlainColTypes  []canonicaltypes.PrimitiveAstNodeI
	curPlainColValues map[string]jsontext.Value

	// Per-tagged-section state.
	curSectionName       naming.StylableName
	curSectionUseAspects useaspects.AspectSet
	curSectionColNames   []naming.StylableName
	curSectionColTypes   []canonicaltypes.PrimitiveAstNodeI
	curSectionAttrIdx    int

	// Co-group state.
	inCoGroup          bool
	curCoGroupKey      naming.Key
	curCoGroupSections []naming.StylableName

	// Per-attribute state (current attribute being built).
	curAttr *attributeRec

	// Per-column-emit state.
	curColName       naming.StylableName
	curColType       canonicaltypes.PrimitiveAstNodeI
	curColValueShape valueShapeE
	curCollection    []jsontext.Value
	curScalar        jsontext.Value

	err error
}

type valueShapeE uint8

const (
	valueShapeUnset valueShapeE = iota
	valueShapeScalar
	valueShapeArray
	valueShapeSet
)

type plainSectionRec struct {
	itemType common.PlainItemTypeE
	values   map[string]jsontext.Value
	colOrder []naming.StylableName
}

type sectionShapeRec struct {
	name    naming.StylableName
	nAttrs  int32
	coGroup naming.Key
}

type coGroupRec struct {
	key      naming.Key
	sections []naming.StylableName
	nAttrs   int32
}

// attributeRec accumulates one tagged attribute as it streams through SinkI.
type attributeRec struct {
	sectionName naming.StylableName
	coGroup     naming.Key
	attrIdx     int32
	colValues   map[string]jsontext.Value
	colShape    valueShapeE
	tags        []membershipRec
}

type membershipRec struct {
	value          membership.MembershipValue
	role           membershiprole.MembershipRoleE
	paramTreatment membershiprole.ParamTreatmentE
}

// NewJsonCardEmitter creates a data-document emitter.
//
// The legacy second positional argument (the IR) is preserved for
// backwards-compatibility; new callers should pass nil. UseAspects now
// arrive through SinkI.BeginSection rather than IR lookup.
func NewJsonCardEmitter(enc *jsontext.Encoder, _ *common.IntermediateTableRepresentation, opts ...JsonCardEmitterOption) (inst *JsonCardEmitter) {
	inst = &JsonCardEmitter{
		enc:        enc,
		classifier: membershiprole.DefaultClassifier{},
		renderer:   membership.DefaultRenderer(),
	}
	for _, o := range opts {
		o(inst)
	}
	return
}

// --- Token helpers ---

func (inst *JsonCardEmitter) writeToken(t jsontext.Token) {
	if inst.err != nil {
		return
	}
	err := inst.enc.WriteToken(t)
	if err != nil {
		inst.err = err
	}
}

func (inst *JsonCardEmitter) writeValue(v jsontext.Value) {
	if inst.err != nil {
		return
	}
	err := inst.enc.WriteValue(v)
	if err != nil {
		inst.err = err
	}
}

// writeRawNdjsonHeader emits the schema-document bytes as the first
// NDJSON line, with `leewayCardData:"1"` injected as the first field so
// downstream tooling can discriminate header from entity lines without
// peeking at deeper keys.
//
// The schema document is structurally `{"leewayCardSchema":"1",...}`;
// we pass the bytes through as a JSON value, leaning on the
// jsontext.Encoder's normalisation rather than parsing+re-emitting.
func (inst *JsonCardEmitter) writeRawNdjsonHeader() {
	doc := inst.schemaDocument
	if len(doc) < 2 || doc[0] != '{' {
		inst.writeToken(jsontext.BeginObject)
		inst.writeToken(jsontext.String("leewayCardData"))
		inst.writeToken(jsontext.String("1"))
		inst.writeToken(jsontext.EndObject)
		return
	}
	// Build merged header bytes: `{"leewayCardData":"1",<rest of schema doc>`.
	// jsontext re-encodes the value to ensure framing is preserved.
	merged := make([]byte, 0, len(doc)+24)
	merged = append(merged, '{')
	merged = append(merged, []byte(`"leewayCardData":"1",`)...)
	merged = append(merged, doc[1:]...)
	inst.writeValue(jsontext.Value(merged))
}

// --- Section-context lookup ---

func (inst *JsonCardEmitter) sectionContext() (sec membershiprole.SectionContext) {
	sec.Name = inst.curSectionName
	sec.UseAspects = inst.curSectionUseAspects
	return
}

// --- SinkI: Batch / Entity ---

func (inst *JsonCardEmitter) BeginBatch() {
	if inst.ndjson {
		// NDJSON mode: emit a header object as the first line. When the
		// caller supplied a full schema document via WithSchemaDocument,
		// embed it (prefixed with the `leewayCardData:"1"` discriminator)
		// so a single-stream consumer reads schema + data together.
		// Otherwise emit a minimal header that references the schema by
		// fingerprint only.
		if len(inst.schemaDocument) > 0 {
			inst.writeRawNdjsonHeader()
		} else {
			inst.writeToken(jsontext.BeginObject)
			inst.writeToken(jsontext.String("leewayCardData"))
			inst.writeToken(jsontext.String("1"))
			if inst.schemaFingerprint != "" {
				inst.writeToken(jsontext.String("schemaFingerprint"))
				inst.writeToken(jsontext.String(inst.schemaFingerprint))
			}
			inst.writeToken(jsontext.EndObject)
		}
		inst.headerWritten = true
		return
	}
	inst.writeToken(jsontext.BeginObject)
	inst.writeToken(jsontext.String("leewayCardData"))
	inst.writeToken(jsontext.String("1"))
	if inst.schemaFingerprint != "" {
		inst.writeToken(jsontext.String("schemaFingerprint"))
		inst.writeToken(jsontext.String(inst.schemaFingerprint))
	}
	inst.writeToken(jsontext.String("entities"))
	inst.writeToken(jsontext.BeginArray)
}

func (inst *JsonCardEmitter) EndBatch() (err error) {
	if !inst.ndjson {
		inst.writeToken(jsontext.EndArray)
		inst.writeToken(jsontext.EndObject)
	}
	return inst.err
}

func (inst *JsonCardEmitter) BeginEntity() {
	inst.plainBuf = inst.plainBuf[:0]
	inst.sectionBuf = inst.sectionBuf[:0]
	inst.coGroupBuf = inst.coGroupBuf[:0]
	inst.attributes = inst.attributes[:0]
}

func (inst *JsonCardEmitter) EndEntity() (err error) {
	inst.flushEntity()
	return inst.err
}

// --- SinkI: Plain section ---

func (inst *JsonCardEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, nAttrs int) {
	inst.curPlainItemType = itemType
	inst.curPlainColNames = valueNames
	inst.curPlainColTypes = valueCanonicalTypes
	inst.curPlainColValues = make(map[string]jsontext.Value, len(valueNames))
}

func (inst *JsonCardEmitter) EndPlainSection() (err error) {
	rec := plainSectionRec{
		itemType: inst.curPlainItemType,
		values:   inst.curPlainColValues,
	}
	rec.colOrder = append(rec.colOrder, inst.curPlainColNames...)
	inst.plainBuf = append(inst.plainBuf, rec)
	inst.curPlainColValues = nil
	return inst.err
}

func (inst *JsonCardEmitter) BeginPlainValue() {
	// Plain values have no buffering distinct from the section; columns stream into curPlainColValues.
}

func (inst *JsonCardEmitter) EndPlainValue() (err error) {
	return inst.err
}

// --- SinkI: Tagged sections / co-groups / sections / values ---

func (inst *JsonCardEmitter) BeginTaggedSections() {}

func (inst *JsonCardEmitter) EndTaggedSections() (err error) { return inst.err }

func (inst *JsonCardEmitter) BeginCoSectionGroup(name naming.Key) {
	inst.inCoGroup = true
	inst.curCoGroupKey = name
	inst.curCoGroupSections = inst.curCoGroupSections[:0]
}

func (inst *JsonCardEmitter) EndCoSectionGroup() (err error) {
	if inst.inCoGroup {
		var nAttrs int32
		for _, s := range inst.sectionBuf {
			if s.coGroup == inst.curCoGroupKey {
				if s.nAttrs > nAttrs {
					nAttrs = s.nAttrs
				}
			}
		}
		inst.coGroupBuf = append(inst.coGroupBuf, coGroupRec{
			key:      inst.curCoGroupKey,
			sections: append([]naming.StylableName(nil), inst.curCoGroupSections...),
			nAttrs:   nAttrs,
		})
		inst.inCoGroup = false
		inst.curCoGroupKey = ""
	}
	return inst.err
}

func (inst *JsonCardEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, useAspectsSet useaspects.AspectSet, nAttrs int) {
	inst.curSectionName = name
	inst.curSectionUseAspects = useAspectsSet
	inst.curSectionColNames = valueNames
	inst.curSectionColTypes = valueCanonicalTypes
	inst.curSectionAttrIdx = 0

	cg := naming.Key("")
	if inst.inCoGroup {
		cg = inst.curCoGroupKey
		inst.curCoGroupSections = append(inst.curCoGroupSections, name)
	}
	inst.sectionBuf = append(inst.sectionBuf, sectionShapeRec{
		name:    name,
		nAttrs:  int32(nAttrs),
		coGroup: cg,
	})
}

func (inst *JsonCardEmitter) EndSection() (err error) {
	inst.curSectionName = ""
	inst.curSectionUseAspects = ""
	return inst.err
}

func (inst *JsonCardEmitter) BeginTaggedValue() {
	inst.curAttr = &attributeRec{
		sectionName: inst.curSectionName,
		coGroup:     inst.coGroupForCurrentSection(),
		attrIdx:     int32(inst.curSectionAttrIdx),
		colValues:   make(map[string]jsontext.Value, len(inst.curSectionColNames)),
	}
}

func (inst *JsonCardEmitter) coGroupForCurrentSection() naming.Key {
	if inst.inCoGroup {
		return inst.curCoGroupKey
	}
	return ""
}

func (inst *JsonCardEmitter) EndTaggedValue() (err error) {
	if inst.curAttr != nil {
		inst.attributes = append(inst.attributes, *inst.curAttr)
		inst.curAttr = nil
	}
	inst.curSectionAttrIdx++
	return inst.err
}

// --- SinkI: Column / Value shape ---

func (inst *JsonCardEmitter) BeginColumn(colAddr streamreadaccess.PhysicalColumnAddr, name naming.StylableName, canonicalType canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
	inst.curColName = name
	inst.curColType = canonicalType
	inst.curColValueShape = valueShapeUnset
	inst.curCollection = inst.curCollection[:0]
	inst.curScalar = nil
}

func (inst *JsonCardEmitter) EndColumn() {
	var v jsontext.Value
	switch inst.curColValueShape {
	case valueShapeScalar:
		if inst.curScalar == nil {
			v = jsontext.Value("null")
		} else {
			v = inst.curScalar
		}
	case valueShapeArray:
		v = wrapArray(inst.curCollection)
	case valueShapeSet:
		v = wrapSet(inst.curCollection)
	default:
		v = jsontext.Value("null")
	}
	if inst.curAttr != nil {
		inst.curAttr.colValues[inst.curColName.String()] = v
		if inst.curAttr.colShape == valueShapeUnset || inst.curColValueShape != valueShapeUnset {
			inst.curAttr.colShape = inst.curColValueShape
		}
	} else if inst.curPlainColValues != nil {
		inst.curPlainColValues[inst.curColName.String()] = v
	}
}

func (inst *JsonCardEmitter) BeginScalarValue() {
	inst.curColValueShape = valueShapeScalar
}

func (inst *JsonCardEmitter) EndScalarValue() (err error) { return inst.err }

func (inst *JsonCardEmitter) BeginHomogenousArrayValue(card int) {
	inst.curColValueShape = valueShapeArray
	inst.curCollection = inst.curCollection[:0]
}

func (inst *JsonCardEmitter) EndHomogenousArrayValue() {}

func (inst *JsonCardEmitter) BeginSetValue(card int) {
	inst.curColValueShape = valueShapeSet
	inst.curCollection = inst.curCollection[:0]
}

func (inst *JsonCardEmitter) EndSetValue() {}

func (inst *JsonCardEmitter) BeginValueItem(index int) {}

func (inst *JsonCardEmitter) EndValueItem() {
	// Item value was written via Write/WriteString into a staging buffer (curScalar
	// for the current item); now move it into curCollection.
	if inst.curScalar != nil {
		inst.curCollection = append(inst.curCollection, inst.curScalar)
		inst.curScalar = nil
	} else {
		inst.curCollection = append(inst.curCollection, jsontext.Value("null"))
	}
}

// Write / WriteString receive the formatted scalar text from the driver.
// The shape selected by Begin*Value determines whether the value lands in
// curScalar (scalar / per-item) or accumulates into curCollection.

func (inst *JsonCardEmitter) Write(p []byte) (n int, err error) {
	return inst.WriteString(string(p))
}

func (inst *JsonCardEmitter) WriteString(s string) (n int, err error) {
	ct := inst.curColType
	// When inside an array/set the column type carries the `h`/`m` modifier;
	// per-item encoding wants the underlying scalar type so JSON-native
	// numbers / bools land instead of falling back to a quoted string.
	if inst.curColValueShape == valueShapeArray || inst.curColValueShape == valueShapeSet {
		ct = stripScalarModifier(ct)
	}
	v := encodeScalarTyped(ct, s)
	inst.curScalar = v
	return len(s), nil
}

// --- SinkI: Tags ---

func (inst *JsonCardEmitter) BeginTags(nTags int) {
	if inst.curAttr != nil && cap(inst.curAttr.tags) < nTags {
		inst.curAttr.tags = make([]membershipRec, 0, nTags)
	}
}

func (inst *JsonCardEmitter) EndTags() {}

func (inst *JsonCardEmitter) AddMembershipRef(lowCard bool, ref uint64) {
	inst.classifyAndAdd(membership.MembershipValue{
		Kind:    membership.IdentityRef,
		LowCard: lowCard,
		Ref:     ref,
	})
}

func (inst *JsonCardEmitter) AddMembershipVerbatim(lowCard bool, verbatim string) {
	inst.classifyAndAdd(membership.MembershipValue{
		Kind:     membership.IdentityVerbatim,
		LowCard:  lowCard,
		Verbatim: verbatim,
	})
}

func (inst *JsonCardEmitter) AddMembershipRefParametrized(lowCard bool, ref uint64, params string) {
	inst.classifyAndAdd(membership.MembershipValue{
		Kind:    membership.IdentityPerRowBlob,
		LowCard: lowCard,
		Ref:     ref,
		Params:  params,
	})
}

func (inst *JsonCardEmitter) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	inst.classifyAndAdd(membership.MembershipValue{
		Kind:   membership.IdentityPerRowId,
		Ref:    ref,
		Params: params,
	})
}

func (inst *JsonCardEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	inst.classifyAndAdd(membership.MembershipValue{
		Kind:     membership.IdentityPerRowName,
		Verbatim: verbatim,
		Params:   params,
	})
}

func (inst *JsonCardEmitter) classifyAndAdd(mv membership.MembershipValue) {
	if inst.curAttr == nil {
		return
	}
	if membership.IsPlaceholder(mv) {
		return
	}
	role, pt := inst.classifier.Classify(inst.sectionContext(), mv)
	inst.curAttr.tags = append(inst.curAttr.tags, membershipRec{
		value:          mv,
		role:           role,
		paramTreatment: pt,
	})
}
