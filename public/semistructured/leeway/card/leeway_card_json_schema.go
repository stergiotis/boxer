//go:build llm_generated_opus47

package card

import (
	"bytes"
	"encoding/hex"
	"encoding/json/jsontext"
	"sort"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// tokenSink wraps a jsontext.Encoder so the per-call error checks can
// collapse into a single check at the end of encodeSchemaDocument.
// Once err is non-nil all subsequent write calls become no-ops; the
// first error wins, matching how a fold-and-return-on-error loop would
// behave but without dispersing 45 `if err != nil { return }` branches
// across the emission body.
type tokenSink struct {
	enc *jsontext.Encoder
	err error
}

func (inst *tokenSink) write(t jsontext.Token) {
	if inst.err != nil {
		return
	}
	inst.err = inst.enc.WriteToken(t)
}

var _ streamreadaccess.SinkI = (*JsonCardSchemaEmitter)(nil)

// FingerprintPrefix names the multihash-style scheme tag prepended to the
// hex-encoded blake3 digest. Stable across re-emissions of the same schema.
const FingerprintPrefix = "blake3:"

// JsonCardSchemaEmitter is a streamreadaccess.SinkI that ignores entity
// content and emits the schema-document shape from ADR-0018.
//
// Drive it with Driver.DriveSchema, which never invokes value or membership
// methods. Output (batch-object form):
//
//	{
//	  "leewayCardSchema": "1",
//	  "fingerprint": "blake3:<hex>",
//	  "plainSections": [{"itemType": "...", "columns": [{"name": "...", "type": "..."}]}],
//	  "taggedSections": [{"name": "...", "columns": [...], "useAspects": "..."}],
//	  "coSectionGroups": [{"key": "...", "sections": [...]}]
//	}
//
// The fingerprint is blake3 of the canonicalized JSON with `fingerprint`
// set to the empty string; consumers re-hash the bytes with the field
// blanked to verify.
type JsonCardSchemaEmitter struct {
	enc *jsontext.Encoder

	plainSections   []schemaPlainSection
	taggedSections  []schemaTaggedSection
	coSectionGroups []schemaCoSectionGroup

	// inCoGroup tracks the currently-open co-group so BeginSection inside
	// it routes the section into the group's sections list.
	inCoGroup    bool
	curCoGroup   naming.Key
	curCoGroupOf []naming.StylableName

	// fingerprint, computed at EndBatch and exposed via Fingerprint().
	fingerprint string

	err error
}

type schemaColumn struct {
	name naming.StylableName
	ct   canonicaltypes.PrimitiveAstNodeI
}

type schemaPlainSection struct {
	itemType common.PlainItemTypeE
	columns  []schemaColumn
}

type schemaTaggedSection struct {
	name       naming.StylableName
	columns    []schemaColumn
	useAspects useaspects.AspectSet
	coGroup    naming.Key // empty for standalone sections
}

type schemaCoSectionGroup struct {
	key      naming.Key
	sections []naming.StylableName
}

// NewJsonCardSchemaEmitter creates a schema emitter. Caller is responsible
// for flushing the encoder; the emitter buffers all output until EndBatch.
func NewJsonCardSchemaEmitter(enc *jsontext.Encoder) (inst *JsonCardSchemaEmitter) {
	inst = &JsonCardSchemaEmitter{
		enc: enc,
	}
	return
}

// Fingerprint returns the blake3 fingerprint computed during EndBatch.
// Empty until EndBatch has run.
func (inst *JsonCardSchemaEmitter) Fingerprint() (fp string) { return inst.fingerprint }

// --- SinkI: structural ---

func (inst *JsonCardSchemaEmitter) BeginBatch() {}

func (inst *JsonCardSchemaEmitter) EndBatch() (err error) {
	body, fp, err := buildSchemaDocument(inst.plainSections, inst.taggedSections, inst.coSectionGroups)
	if err != nil {
		inst.err = err
		return inst.err
	}
	inst.fingerprint = fp
	// WriteRawBytes bypasses jsontext.Encoder reformatting so the bytes
	// the user reads from their writer are byte-for-byte the same bytes
	// the fingerprint was computed over.
	rawWriter := inst.enc
	werr := rawWriter.WriteValue(jsontext.Value(body))
	if werr != nil {
		inst.err = werr
	}
	return inst.err
}

func (inst *JsonCardSchemaEmitter) BeginEntity()           {}
func (inst *JsonCardSchemaEmitter) EndEntity() (err error) { return inst.err }

func (inst *JsonCardSchemaEmitter) BeginPlainSection(itemType common.PlainItemTypeE, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, _ int) {
	cols := make([]schemaColumn, 0, len(valueNames))
	for i, name := range valueNames {
		var ct canonicaltypes.PrimitiveAstNodeI
		if i < len(valueCanonicalTypes) {
			ct = valueCanonicalTypes[i]
		}
		cols = append(cols, schemaColumn{name: name, ct: ct})
	}
	inst.plainSections = append(inst.plainSections, schemaPlainSection{
		itemType: itemType,
		columns:  cols,
	})
}

func (inst *JsonCardSchemaEmitter) EndPlainSection() (err error) { return inst.err }

func (inst *JsonCardSchemaEmitter) BeginPlainValue()           {}
func (inst *JsonCardSchemaEmitter) EndPlainValue() (err error) { return inst.err }

func (inst *JsonCardSchemaEmitter) BeginTaggedSections()           {}
func (inst *JsonCardSchemaEmitter) EndTaggedSections() (err error) { return inst.err }

func (inst *JsonCardSchemaEmitter) BeginCoSectionGroup(name naming.Key) {
	inst.inCoGroup = true
	inst.curCoGroup = name
	inst.curCoGroupOf = inst.curCoGroupOf[:0]
}

func (inst *JsonCardSchemaEmitter) EndCoSectionGroup() (err error) {
	if inst.inCoGroup {
		inst.coSectionGroups = append(inst.coSectionGroups, schemaCoSectionGroup{
			key:      inst.curCoGroup,
			sections: append([]naming.StylableName(nil), inst.curCoGroupOf...),
		})
		inst.inCoGroup = false
		inst.curCoGroup = ""
	}
	return inst.err
}

func (inst *JsonCardSchemaEmitter) BeginSection(name naming.StylableName, valueNames []naming.StylableName, valueCanonicalTypes []canonicaltypes.PrimitiveAstNodeI, useAspectsSet useaspects.AspectSet, _ int) {
	cols := make([]schemaColumn, 0, len(valueNames))
	for i, n := range valueNames {
		var ct canonicaltypes.PrimitiveAstNodeI
		if i < len(valueCanonicalTypes) {
			ct = valueCanonicalTypes[i]
		}
		cols = append(cols, schemaColumn{name: n, ct: ct})
	}
	cg := naming.Key("")
	if inst.inCoGroup {
		cg = inst.curCoGroup
		inst.curCoGroupOf = append(inst.curCoGroupOf, name)
	}
	inst.taggedSections = append(inst.taggedSections, schemaTaggedSection{
		name:       name,
		columns:    cols,
		useAspects: useAspectsSet,
		coGroup:    cg,
	})
}

func (inst *JsonCardSchemaEmitter) EndSection() (err error) { return inst.err }

func (inst *JsonCardSchemaEmitter) BeginTaggedValue()           {}
func (inst *JsonCardSchemaEmitter) EndTaggedValue() (err error) { return inst.err }

func (inst *JsonCardSchemaEmitter) BeginColumn(_ streamreadaccess.PhysicalColumnAddr, _ naming.StylableName, _ canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
}
func (inst *JsonCardSchemaEmitter) EndColumn() {}

func (inst *JsonCardSchemaEmitter) BeginScalarValue()           {}
func (inst *JsonCardSchemaEmitter) EndScalarValue() (err error) { return inst.err }

func (inst *JsonCardSchemaEmitter) BeginHomogenousArrayValue(_ int) {}
func (inst *JsonCardSchemaEmitter) EndHomogenousArrayValue()        {}

func (inst *JsonCardSchemaEmitter) BeginSetValue(_ int) {}
func (inst *JsonCardSchemaEmitter) EndSetValue()        {}

func (inst *JsonCardSchemaEmitter) BeginValueItem(_ int) {}
func (inst *JsonCardSchemaEmitter) EndValueItem()        {}

func (inst *JsonCardSchemaEmitter) Write(p []byte) (n int, err error)       { return len(p), nil }
func (inst *JsonCardSchemaEmitter) WriteString(s string) (n int, err error) { return len(s), nil }

func (inst *JsonCardSchemaEmitter) BeginTags(_ int) {}
func (inst *JsonCardSchemaEmitter) EndTags()        {}

func (inst *JsonCardSchemaEmitter) AddMembershipRef(_ bool, _ uint64) {}
func (inst *JsonCardSchemaEmitter) AddMembershipVerbatim(_ bool, _ string) {
}
func (inst *JsonCardSchemaEmitter) AddMembershipRefParametrized(_ bool, _ uint64, _ string) {
}
func (inst *JsonCardSchemaEmitter) AddMembershipMixedLowCardRefHighCardParam(_ uint64, _ string) {
}
func (inst *JsonCardSchemaEmitter) AddMembershipMixedLowCardVerbatimHighCardParam(_ string, _ string) {
}

// --- Document materialisation + fingerprint ---

// buildSchemaDocument materialises the canonical JSON bytes for the schema
// document and computes the blake3 fingerprint. Determinism: callers in
// `inst` may add sections in driver order; we re-sort here so that two
// equivalent schemas produce identical bytes regardless of driver visit
// order.
//
// Sort rules (per ADR-0018):
//   - plainSections by PlainItemTypeE underlying value (the IR order is
//     stable per enum)
//   - taggedSections by name (lex StylableName)
//   - coSectionGroups by key (naming.Key)
//
// The fingerprint is computed over bytes with `fingerprint` blanked, then
// the bytes are re-emitted with the populated field.
func buildSchemaDocument(plain []schemaPlainSection, tagged []schemaTaggedSection, coGroups []schemaCoSectionGroup) (out []byte, fingerprint string, err error) {
	plainSorted := append([]schemaPlainSection(nil), plain...)
	sortPlainSections(plainSorted)
	taggedSorted := append([]schemaTaggedSection(nil), tagged...)
	sortTaggedSections(taggedSorted)
	coSorted := append([]schemaCoSectionGroup(nil), coGroups...)
	sortCoGroups(coSorted)

	preimage, err := encodeSchemaDocument(plainSorted, taggedSorted, coSorted, "")
	if err != nil {
		return
	}
	digest := blake3.Sum256(preimage)
	fingerprint = FingerprintPrefix + hex.EncodeToString(digest[:])

	out, err = encodeSchemaDocument(plainSorted, taggedSorted, coSorted, fingerprint)
	return
}

func sortPlainSections(arr []schemaPlainSection) {
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].itemType < arr[j].itemType })
}

func sortTaggedSections(arr []schemaTaggedSection) {
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].name.Compare(arr[j].name) < 0 })
}

func sortCoGroups(arr []schemaCoSectionGroup) {
	sort.SliceStable(arr, func(i, j int) bool { return arr[i].key < arr[j].key })
}

// encodeSchemaDocument emits the schema document bytes given a fingerprint
// string. With fp == "" the field appears as the empty string, which is
// the form used for hashing.
func encodeSchemaDocument(plain []schemaPlainSection, tagged []schemaTaggedSection, coGroups []schemaCoSectionGroup, fp string) (out []byte, err error) {
	buf := bytes.NewBuffer(nil)
	s := &tokenSink{enc: jsontext.NewEncoder(buf)}

	s.write(jsontext.BeginObject)
	s.write(jsontext.String("leewayCardSchema"))
	s.write(jsontext.String("1"))

	s.write(jsontext.String("fingerprint"))
	s.write(jsontext.String(fp))

	s.write(jsontext.String("plainSections"))
	s.write(jsontext.BeginArray)
	for _, ps := range plain {
		s.write(jsontext.BeginObject)
		s.write(jsontext.String("itemType"))
		s.write(jsontext.String(ps.itemType.String()))
		s.write(jsontext.String("columns"))
		s.writeSchemaColumns(ps.columns)
		s.write(jsontext.EndObject)
	}
	s.write(jsontext.EndArray)

	s.write(jsontext.String("taggedSections"))
	s.write(jsontext.BeginArray)
	for _, ts := range tagged {
		s.write(jsontext.BeginObject)
		s.write(jsontext.String("name"))
		s.write(jsontext.String(ts.name.String()))
		s.write(jsontext.String("columns"))
		s.writeSchemaColumns(ts.columns)
		if ts.useAspects != "" && !ts.useAspects.IsEmptySet() {
			s.write(jsontext.String("useAspects"))
			s.write(jsontext.String(string(ts.useAspects)))
		}
		if ts.coGroup != "" {
			s.write(jsontext.String("coGroup"))
			s.write(jsontext.String(string(ts.coGroup)))
		}
		s.write(jsontext.EndObject)
	}
	s.write(jsontext.EndArray)

	s.write(jsontext.String("coSectionGroups"))
	s.write(jsontext.BeginArray)
	for _, cg := range coGroups {
		s.write(jsontext.BeginObject)
		s.write(jsontext.String("key"))
		s.write(jsontext.String(string(cg.key)))
		s.write(jsontext.String("sections"))
		s.write(jsontext.BeginArray)
		secs := append([]naming.StylableName(nil), cg.sections...)
		sort.Slice(secs, func(i, j int) bool { return secs[i].Compare(secs[j]) < 0 })
		for _, n := range secs {
			s.write(jsontext.String(n.String()))
		}
		s.write(jsontext.EndArray)
		s.write(jsontext.EndObject)
	}
	s.write(jsontext.EndArray)

	s.write(jsontext.EndObject)

	if s.err != nil {
		err = eb.Build().Errorf("encode schema document: %w", s.err)
		return
	}
	out = bytes.TrimRight(buf.Bytes(), "\n")
	return
}

func (inst *tokenSink) writeSchemaColumns(cols []schemaColumn) {
	inst.write(jsontext.BeginArray)
	for _, c := range cols {
		inst.write(jsontext.BeginObject)
		inst.write(jsontext.String("name"))
		inst.write(jsontext.String(c.name.String()))
		inst.write(jsontext.String("type"))
		typeStr := ""
		if c.ct != nil {
			typeStr = c.ct.String()
		}
		inst.write(jsontext.String(typeStr))
		inst.write(jsontext.EndObject)
	}
	inst.write(jsontext.EndArray)
}
