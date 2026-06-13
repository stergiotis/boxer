package card

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	"strings"
	"testing"

	"lukechampine.com/blake3"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
)

// driveSchemaSyntheticIR walks a synthetic IR through the schema emitter
// using the same SinkI shape that Driver.DriveSchema produces. The synthetic
// IR mirrors a small but realistic table: one plain section (entityId), two
// standalone tagged sections (string, int64), and one co-group (geo:
// latLng + h3).
func driveSchemaSyntheticIR(sink streamreadaccess.SinkI) {
	sink.BeginBatch()

	// Plain section: entityId (single u64 column)
	idCol := naming.MustBeValidStylableName("blake3hash")
	idCt := canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 64}
	sink.BeginPlainSection(common.PlainItemTypeEntityId,
		[]naming.StylableName{idCol},
		[]canonicaltypes.PrimitiveAstNodeI{idCt}, 0)
	_ = sink.EndPlainSection()

	sink.BeginTaggedSections()

	// Co-group: geo {latLng, h3}
	geoKey := naming.Key("geo")
	sink.BeginCoSectionGroup(geoKey)

	latCol := naming.MustBeValidStylableName("lat")
	lngCol := naming.MustBeValidStylableName("lng")
	f32 := canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericFloat, Width: 32}
	sink.BeginSection(naming.MustBeValidStylableName("latLng"),
		[]naming.StylableName{latCol, lngCol},
		[]canonicaltypes.PrimitiveAstNodeI{f32, f32}, useaspects.EmptyAspectSet, 0)
	_ = sink.EndSection()

	h3Col := naming.MustBeValidStylableName("value")
	u64 := canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 64}
	sink.BeginSection(naming.MustBeValidStylableName("h3"),
		[]naming.StylableName{h3Col},
		[]canonicaltypes.PrimitiveAstNodeI{u64}, useaspects.EmptyAspectSet, 0)
	_ = sink.EndSection()

	_ = sink.EndCoSectionGroup()

	// Standalone tagged section: string (with use-aspects)
	strCol := naming.MustBeValidStylableName("value")
	strCt := canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}
	withAspect := useaspects.EncodeAspectsMustValidate(useaspects.AspectSectionMembershipsAllPrimary)
	sink.BeginSection(naming.MustBeValidStylableName("string"),
		[]naming.StylableName{strCol},
		[]canonicaltypes.PrimitiveAstNodeI{strCt}, withAspect, 0)
	_ = sink.EndSection()

	// Standalone tagged section: int64
	intCol := naming.MustBeValidStylableName("value")
	i64 := canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 64}
	sink.BeginSection(naming.MustBeValidStylableName("int64"),
		[]naming.StylableName{intCol},
		[]canonicaltypes.PrimitiveAstNodeI{i64}, useaspects.EmptyAspectSet, 0)
	_ = sink.EndSection()

	_ = sink.EndTaggedSections()
	_ = sink.EndBatch()
}

func newSchemaEmitter() (sink *JsonCardSchemaEmitter, buf *bytes.Buffer) {
	buf = bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	sink = NewJsonCardSchemaEmitter(enc)
	return
}

func TestJsonCardSchemaEmitter_DocumentShape(t *testing.T) {
	sink, buf := newSchemaEmitter()
	driveSchemaSyntheticIR(sink)

	out := buf.String()

	for _, want := range []string{
		`"leewayCardSchema":"1"`,
		`"fingerprint":"blake3:`,
		`"plainSections":[`,
		`"taggedSections":[`,
		`"coSectionGroups":[`,
		// PlainItemTypeE.String() emits kebab-case.
		`"itemType":"entity-id"`,
		`"name":"blake3hash"`,
		`"type":"u64"`,
		// StylableName.String() also emits kebab-case ("latLng" → "lat-lng").
		`"name":"h3"`,
		`"name":"int64"`,
		`"name":"lat-lng"`,
		`"name":"string"`,
		`"key":"geo"`,
		`"sections":["h3","lat-lng"]`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %s in output, got: %s", want, out)
		}
	}
}

func TestJsonCardSchemaEmitter_PlainSectionColumns(t *testing.T) {
	sink, buf := newSchemaEmitter()
	driveSchemaSyntheticIR(sink)

	out := buf.String()
	if !strings.Contains(out, `{"itemType":"entity-id","columns":[{"name":"blake3hash","type":"u64"}]}`) {
		t.Fatalf("plain section shape mismatch: %s", out)
	}
}

func TestJsonCardSchemaEmitter_TaggedSectionsLexSorted(t *testing.T) {
	sink, buf := newSchemaEmitter()
	driveSchemaSyntheticIR(sink)

	out := buf.String()
	idxH3 := strings.Index(out, `"name":"h3"`)
	idxInt := strings.Index(out, `"name":"int64"`)
	idxLat := strings.Index(out, `"name":"lat-lng"`)
	idxStr := strings.Index(out, `"name":"string"`)
	if idxH3 < 0 || idxInt < 0 || idxLat < 0 || idxStr < 0 {
		t.Fatalf("missing tagged section names: %s", out)
	}
	if !(idxH3 < idxInt && idxInt < idxLat && idxLat < idxStr) {
		t.Fatalf("tagged sections not lex-sorted (h3 < int64 < lat-lng < string): %s", out)
	}
}

func TestJsonCardSchemaEmitter_UseAspectsEmittedWhenNonEmpty(t *testing.T) {
	sink, buf := newSchemaEmitter()
	driveSchemaSyntheticIR(sink)

	// "string" section was given AspectHumanReadable.
	out := buf.String()
	if !strings.Contains(out, `"useAspects":`) {
		t.Fatalf("useAspects field missing for section that has aspects: %s", out)
	}
	// h3, latLng, int64 had EmptyAspectSet — useAspects should be absent
	// from those object slots, but `Contains` would still match the string
	// once. Verify by counting.
	count := strings.Count(out, `"useAspects":`)
	if count != 1 {
		t.Fatalf("expected exactly one useAspects field (only `string` has non-empty aspects), got %d: %s", count, out)
	}
}

// TestJsonCardSchemaEmitter_FingerprintRoundTrip verifies that the
// fingerprint embedded in the output is the blake3 of the canonical bytes
// with the fingerprint field blanked.
func TestJsonCardSchemaEmitter_FingerprintRoundTrip(t *testing.T) {
	sink, buf := newSchemaEmitter()
	driveSchemaSyntheticIR(sink)

	// Strip the encoder's trailing newline so the bytes match the
	// canonical preimage shape used to compute the fingerprint.
	out := bytes.TrimRight(buf.Bytes(), "\n")

	// Parse + extract fingerprint.
	var parsed struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	if !strings.HasPrefix(parsed.Fingerprint, FingerprintPrefix) {
		t.Fatalf("fingerprint missing prefix: %q", parsed.Fingerprint)
	}

	// Recompute: replace the fingerprint value with empty string in the
	// emitted bytes, hash with blake3, compare hex.
	fpField := []byte(`"fingerprint":"` + parsed.Fingerprint + `"`)
	blanked := bytes.Replace(out, fpField, []byte(`"fingerprint":""`), 1)
	if bytes.Equal(blanked, out) {
		t.Fatalf("fingerprint field not found in output: %s", out)
	}
	digest := blake3.Sum256(blanked)
	want := FingerprintPrefix + hex.EncodeToString(digest[:])
	if want != parsed.Fingerprint {
		t.Fatalf("fingerprint mismatch:\n  emitted = %s\n  recomputed = %s", parsed.Fingerprint, want)
	}
}

// TestJsonCardSchemaEmitter_Determinism — emit twice, expect byte equality.
func TestJsonCardSchemaEmitter_Determinism(t *testing.T) {
	emit := func() []byte {
		sink, buf := newSchemaEmitter()
		driveSchemaSyntheticIR(sink)
		return append([]byte(nil), buf.Bytes()...)
	}
	a := emit()
	b := emit()
	if !bytes.Equal(a, b) {
		t.Fatalf("non-deterministic schema emission:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
}

// TestJsonCardSchemaEmitter_AccessorMatchesEmitted — Fingerprint() returns
// the same string that the document carries.
func TestJsonCardSchemaEmitter_AccessorMatchesEmitted(t *testing.T) {
	sink, buf := newSchemaEmitter()
	driveSchemaSyntheticIR(sink)

	var parsed struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sink.Fingerprint() != parsed.Fingerprint {
		t.Fatalf("Fingerprint() = %q, embedded = %q", sink.Fingerprint(), parsed.Fingerprint)
	}
}

// TestJsonCardEmitter_DataDocumentSchemaFingerprint — the data emitter
// embeds the schemaFingerprint when WithSchemaFingerprint is supplied,
// and the value matches the schema document's fingerprint.
func TestJsonCardEmitter_DataDocumentSchemaFingerprint(t *testing.T) {
	// First, emit the schema and capture its fingerprint.
	schemaSink, schemaBuf := newSchemaEmitter()
	driveSchemaSyntheticIR(schemaSink)
	schemaFp := schemaSink.Fingerprint()
	if schemaFp == "" {
		t.Fatalf("schema fingerprint empty after EndBatch")
	}

	// Emit a data document with WithSchemaFingerprint.
	dataBuf := bytes.NewBuffer(nil)
	dataEnc := jsontext.NewEncoder(dataBuf)
	dataSink := NewJsonCardEmitter(dataEnc, nil, WithSchemaFingerprint(schemaFp))
	dataSink.BeginBatch()
	dataSink.BeginEntity()
	dataSink.BeginTaggedSections()
	driveOneAttribute(t, dataSink, "string", "value", "alpha", func() {
		dataSink.AddMembershipVerbatim(true, "/x")
	})
	_ = dataSink.EndTaggedSections()
	_ = dataSink.EndEntity()
	_ = dataSink.EndBatch()

	want := `"schemaFingerprint":"` + schemaFp + `"`
	if !strings.Contains(dataBuf.String(), want) {
		t.Fatalf("data document missing schemaFingerprint=%s:\n%s", schemaFp, dataBuf.String())
	}

	// Cross-check: the schema document's fingerprint field equals the
	// data document's schemaFingerprint field.
	var schemaParsed struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.Unmarshal(schemaBuf.Bytes(), &schemaParsed); err != nil {
		t.Fatalf("schema parse: %v", err)
	}
	if schemaParsed.Fingerprint != schemaFp {
		t.Fatalf("schema fingerprint mismatch: doc=%q accessor=%q", schemaParsed.Fingerprint, schemaFp)
	}
}

// TestJsonCardEmitter_NDJSONEmbedsSchemaDocument — when NDJSON mode is on
// and WithSchemaDocument is supplied, the first line is the schema doc
// with leewayCardData:"1" prepended; subsequent lines are entities.
func TestJsonCardEmitter_NDJSONEmbedsSchemaDocument(t *testing.T) {
	schemaSink, schemaBuf := newSchemaEmitter()
	driveSchemaSyntheticIR(schemaSink)
	schemaDoc := append([]byte(nil), schemaBuf.Bytes()...)

	dataBuf := bytes.NewBuffer(nil)
	dataEnc := jsontext.NewEncoder(dataBuf)
	dataSink := NewJsonCardEmitter(dataEnc, nil, WithNDJSON(), WithSchemaDocument(schemaDoc))

	dataSink.BeginBatch()
	dataSink.BeginEntity()
	dataSink.BeginTaggedSections()
	driveOneAttribute(t, dataSink, "string", "value", "alpha", func() {
		dataSink.AddMembershipVerbatim(true, "/x")
	})
	_ = dataSink.EndTaggedSections()
	_ = dataSink.EndEntity()
	_ = dataSink.EndBatch()

	out := strings.TrimRight(dataBuf.String(), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected schema header + entity, got %d lines: %q", len(lines), out)
	}
	header := lines[0]
	if !strings.HasPrefix(header, `{"leewayCardData":"1",`) {
		t.Fatalf("first line missing leewayCardData discriminator: %q", header)
	}
	if !strings.Contains(header, `"leewayCardSchema":"1"`) {
		t.Fatalf("first line missing embedded schema doc: %q", header)
	}
	if !strings.Contains(header, `"fingerprint":"blake3:`) {
		t.Fatalf("first line missing schema fingerprint: %q", header)
	}
	if !strings.Contains(lines[1], `"/x"`) {
		t.Fatalf("entity line missing tagged value: %q", lines[1])
	}
}
