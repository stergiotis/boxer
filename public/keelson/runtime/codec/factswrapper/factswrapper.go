//go:build llm_generated_opus47

// Package factswrapper implements marshallgen.WrapperEmitterI for the
// keelson runtime.facts schema. It provides the schema-specific blocks
// — kindXxx vdd-resolved variables, ActiveSections / ActiveFields
// hints, dml_cbor InEntityFacts pool, Marshal / Reader / Unmarshal /
// buscodec.CodecI bridge — that wrap the schema-agnostic core produced
// by boxer/public/semistructured/leeway/marshallgen.
//
// Public entry: FactsWrapper{}.Generate(input, output). The wrapper
// also applies a back-compat plan transformation: scalar T or
// Option[T] fields targeting `*Array` / `*Set` / `*Range` sections
// get FieldFlags.Unit set automatically, matching the pre-flag
// runtime.facts emit behaviour for DTOs that haven't migrated to
// explicit `,unit` tags.
package factswrapper

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"

	cbdml "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml_cbor"
)

// FactsWrapper wires the marshallgen core into the runtime.facts
// dml_cbor / ra / cborarrow / buscodec stack. Zero-value usable.
type FactsWrapper struct{}

var _ marshallgen.WrapperEmitterI = FactsWrapper{}

// Generate parses inputPath, applies the runtime.facts back-compat
// Unit-flag inference for unchanged DTOs, then emits via marshallgen
// with this wrapper supplying the schema-coupled blocks. Returns the
// rendered bytes; if outputPath is non-empty, also writes to disk.
func (FactsWrapper) Generate(inputPath, outputPath string) (out []byte, err error) {
	var plan *mappingplan.Plan
	plan, err = marshallgen.ParsePlan(inputPath)
	if err != nil {
		return
	}
	inferUnitFromSectionSuffix(plan)
	out, err = marshallgen.EmitPlan(plan, FactsWrapper{})
	if err != nil {
		return
	}
	if outputPath != "" {
		err = os.WriteFile(outputPath, out, 0644)
		if err != nil {
			err = eb.Build().Str("path", outputPath).Errorf("factswrapper: write file: %w", err)
			return
		}
	}
	return
}

// inferUnitFromSectionSuffix flips FieldFlags.Unit on scalar T or
// Option[T] fields whose lw: tag section name ends in `Array` / `Set` /
// `Range`. This preserves the pre-flag emit behaviour where the
// runtime.facts generator automatically picked BeginAttributeSingle for
// scalar values landing in homogeneous-array section types. DTOs that
// declare an explicit `,unit` or `,explode` are left untouched.
func inferUnitFromSectionSuffix(plan *mappingplan.Plan) {
	for i := range plan.Fields {
		f := &plan.Fields[i]
		if f.Flags.Unit || f.Flags.Explode {
			continue
		}
		if f.IsSlice() || f.IsRoaring() {
			continue
		}
		if isNonScalarSectionName(f.LWSection) {
			f.Flags.Unit = true
		}
	}
}

func isNonScalarSectionName(s string) bool {
	return strings.HasSuffix(s, "Array") ||
		strings.HasSuffix(s, "Set") ||
		strings.HasSuffix(s, "Range")
}

// --- marshallgen.WrapperEmitterI methods. ---

// Imports lists the runtime.facts-locked imports the wrapper-emitted
// blocks reference: bytes (Encode/Decode buffers), io (Marshal
// io.Writer), strings (ActiveFields prefix scan), sync (sync.Pool +
// sync.OnceValue), arrow/arrow + ipc + memory (Allocator + IPC
// reader), and the per-package codec / facts imports.
func (FactsWrapper) Imports(plan *mappingplan.Plan) []string {
	return []string{
		`"bytes"`,
		`"io"`,
		`"strings"`,
		`"sync"`,
		``,
		`"github.com/apache/arrow-go/v18/arrow"`,
		`"github.com/apache/arrow-go/v18/arrow/ipc"`,
		`"github.com/apache/arrow-go/v18/arrow/memory"`,
		``,
		`"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"`,
		`"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/arrowrowcbor"`,
		`cbdml "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml_cbor"`,
		`"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ra"`,
		`"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/cborarrow"`,
		`"github.com/stergiotis/boxer/public/keelson/vdd"`,
	}
}

// KindVars emits `var kindXxx uint64` for each unique non-verbatim
// membership. Resolved in Init from vdd.MembXxx.GetId().Value().
// Verbatim memberships are skipped — they embed the literal []byte
// name at the call site, no uint64 lookup needed.
func (FactsWrapper) KindVars(sb *strings.Builder, plan *mappingplan.Plan) {
	memberships := uniqueMemberships(plan)
	if len(memberships) == 0 {
		return
	}
	sb.WriteString("// --- Resolved membership ids from vdd. ---\n\n")
	sb.WriteString("var (\n")
	for _, f := range memberships {
		fmt.Fprintf(sb, "\t%s uint64\n", f.KindVar())
	}
	sb.WriteString(")\n\n")
}

// Init emits the package init() body: vdd lookups + auto-register the
// kind's CodecI with buscodec so any buscodec.Encode[<Kind>] /
// Decode[<Kind>] call routes through this codec instead of the CBOR
// fallback.
func (FactsWrapper) Init(sb *strings.Builder, plan *mappingplan.Plan) {
	sb.WriteString("func init() {\n")
	for _, f := range uniqueMemberships(plan) {
		fmt.Fprintf(sb, "\t%s = vdd.Memb%s.GetId().Value()\n", f.KindVar(), upperFirst(f.LWMembership))
	}
	fmt.Fprintf(sb, "\tbuscodec.Register[%s](%s)\n", plan.KindType, lowerFirst(plan.KindType)+"BusCodec")
	sb.WriteString("}\n\n")
}

// BeforeCore emits ActiveSections / ActiveFields hints + the
// per-kind sync.Pool of dml_cbor.InEntityFacts builders. These wrap
// the schema-agnostic Columns struct that marshallgen emits next.
func (FactsWrapper) BeforeCore(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	err = writeActiveHints(sb, plan)
	if err != nil {
		return
	}
	writePool(sb, plan)
	return
}

// AfterCore emits the facts-locked Marshal method (driver around
// BuildEntities + TransferRecords + JoinRecords), the per-kind reader
// composing the ra accessors, the Unmarshal method (driver around
// FillFromArrow), and the buscodec.CodecI bridge with its singleton.
func (FactsWrapper) AfterCore(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	err = writeMarshal(sb, plan)
	if err != nil {
		return
	}
	err = writeUnmarshal(sb, plan)
	if err != nil {
		return
	}
	writeCodec(sb, plan)
	return
}

// --- ActiveSections / ActiveFields. ---

func writeActiveHints(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	indices, err := activeSectionIndices(plan)
	if err != nil {
		return
	}
	names := activeSectionNamesByIdx(plan)

	sb.WriteString("// --- ADR-0042 active-section / active-field hints. ---\n\n")

	fmt.Fprintf(sb, "// %sActiveSections is the dml_cbor section-index subset this kind\n", plan.KindType)
	fmt.Fprintf(sb, "// populates. Passed to InEntityFacts.SetActiveSections so the\n")
	fmt.Fprintf(sb, "// builder skips beginSection list-slot work for inactive sections.\n")
	fmt.Fprintf(sb, "var %sActiveSections = []int{", plan.KindType)
	parts := make([]string, len(indices))
	for i, idx := range indices {
		parts[i] = fmt.Sprintf("%d", idx)
	}
	sb.WriteString(strings.Join(parts, ", "))
	sb.WriteString("}\n\n")

	fmt.Fprintf(sb, "// %sActiveFields is the column-index subset this kind populates\n", plan.KindType)
	fmt.Fprintf(sb, "// in the runtime.facts Arrow schema. Lazily computed once via\n")
	fmt.Fprintf(sb, "// sync.OnceValue: scans cbdml.CreateSchemaFacts()'s tv:<section>:...\n")
	fmt.Fprintf(sb, "// field names against this kind's active sections plus the three\n")
	fmt.Fprintf(sb, "// plain prefixes (id:, ts:, lc:). Driven through RecordBuilder.\n")
	fmt.Fprintf(sb, "// SetActiveFields to skip per-row emit walks for unused columns.\n")
	fmt.Fprintf(sb, "var %sActiveFields = sync.OnceValue(func() []int {\n", plan.KindType)
	sb.WriteString("\tactive := map[string]bool{")
	nameParts := make([]string, len(names))
	for i, name := range names {
		nameParts[i] = fmt.Sprintf("%q: true", name)
	}
	sb.WriteString(strings.Join(nameParts, ", "))
	sb.WriteString("}\n")
	sb.WriteString("\tschema := cbdml.CreateSchemaFacts()\n")
	sb.WriteString("\tout := make([]int, 0, 4+len(active)*8)\n")
	sb.WriteString("\tfor i, f := range schema.Fields() {\n")
	sb.WriteString("\t\tname := f.Name\n")
	sb.WriteString("\t\tswitch {\n")
	sb.WriteString("\t\tcase strings.HasPrefix(name, \"id:\"),\n")
	sb.WriteString("\t\t\tstrings.HasPrefix(name, \"ts:\"),\n")
	sb.WriteString("\t\t\tstrings.HasPrefix(name, \"lc:\"):\n")
	sb.WriteString("\t\t\tout = append(out, i)\n")
	sb.WriteString("\t\tcase strings.HasPrefix(name, \"tv:\"):\n")
	sb.WriteString("\t\t\trest := name[3:]\n")
	sb.WriteString("\t\t\tcolon := strings.IndexByte(rest, ':')\n")
	sb.WriteString("\t\t\tif colon < 0 {\n\t\t\t\tcontinue\n\t\t\t}\n")
	sb.WriteString("\t\t\tif active[rest[:colon]] {\n")
	sb.WriteString("\t\t\t\tout = append(out, i)\n")
	sb.WriteString("\t\t\t}\n")
	sb.WriteString("\t\t}\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\treturn out\n")
	sb.WriteString("})\n\n")
	return
}

func writePool(sb *strings.Builder, plan *mappingplan.Plan) {
	pool := lowerFirst(plan.KindType) + "Pool"
	sb.WriteString("// --- Per-kind InEntityFacts pool. ---\n\n")
	fmt.Fprintf(sb, "// %s reuses dml_cbor.InEntityFacts instances across Marshal\n", pool)
	fmt.Fprintf(sb, "// calls. Each carries all ~300 column builders and ~21 section\n")
	fmt.Fprintf(sb, "// orchestrators; un-pooled construction dominates the single-row\n")
	fmt.Fprintf(sb, "// cost. After TransferRecords the state is back to Initial with\n")
	fmt.Fprintf(sb, "// buffers reset to len=0, so reuse within this kind is safe.\n")
	fmt.Fprintf(sb, "var %s = sync.Pool{\n", pool)
	sb.WriteString("\tNew: func() any {\n")
	sb.WriteString("\t\treturn cbdml.NewInEntityFacts(memory.NewGoAllocator(), 64)\n")
	sb.WriteString("\t},\n")
	sb.WriteString("}\n\n")
}

// --- Marshal driver wrapping BuildEntities. ---

func writeMarshal(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	pool := lowerFirst(plan.KindType) + "Pool"
	sb.WriteString("// --- Sparse-CBOR write (ADR-0042 driver path). ---\n\n")
	fmt.Fprintf(sb, "// Marshal writes the SoA buffer to w as the sparse-CBOR wire\n")
	fmt.Fprintf(sb, "// format produced through factsschema/dml_cbor. Thin wrapper\n")
	fmt.Fprintf(sb, "// around %sBuildEntities — the per-row chain lives there and\n", plan.KindType)
	fmt.Fprintf(sb, "// works against any leeway-DML class that structurally satisfies\n")
	fmt.Fprintf(sb, "// %sEntityI.\n", plan.KindType)
	fmt.Fprintf(sb, "func (c *%sColumns) Marshal(w io.Writer) (err error) {\n", plan.KindType)
	fmt.Fprintf(sb, "\tent := %s.Get().(*cbdml.InEntityFacts)\n", pool)
	fmt.Fprintf(sb, "\tdefer %s.Put(ent)\n", pool)
	fmt.Fprintf(sb, "\tent.SetActiveSections(%sActiveSections)\n", plan.KindType)
	fmt.Fprintf(sb, "\tent.Builder().SetActiveFields(%sActiveFields())\n", plan.KindType)
	fmt.Fprintf(sb, "\terr = %sBuildEntities(ent, c)\n", plan.KindType)
	sb.WriteString("\tif err != nil {\n\t\treturn\n\t}\n")
	sb.WriteString("\tvar recs []*arrowrowcbor.Record\n")
	sb.WriteString("\trecs, err = ent.TransferRecords(nil)\n")
	sb.WriteString("\tif err != nil {\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: transfer records: %%w\", err)\n", plan.PackageName)
	sb.WriteString("\t\treturn\n\t}\n")
	sb.WriteString("\trb := arrowrowcbor.JoinRecords(recs)\n")
	sb.WriteString("\t_, err = w.Write(rb)\n")
	sb.WriteString("\tif err != nil {\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: marshal write: %%w\", err)\n", plan.PackageName)
	sb.WriteString("\t}\n")
	sb.WriteString("\treturn\n")
	sb.WriteString("}\n\n")
	return
}

// --- Reader + Unmarshal driver wrapping FillFromArrow. ---

func writeUnmarshal(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	err = writeReaderType(sb, plan)
	if err != nil {
		return
	}
	writeReaderCtor(sb, plan)
	writeReaderLoad(sb, plan)
	writeReaderRelease(sb, plan)
	err = writeUnmarshalMethod(sb, plan)
	return
}

func readerTypeName(plan *mappingplan.Plan) string {
	return lowerFirst(plan.KindType) + "Reader"
}

func writeReaderType(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	names := activeSectionNamesByDecl(plan)
	rt := readerTypeName(plan)
	sb.WriteString("// --- Read path (ra-based). ---\n\n")
	fmt.Fprintf(sb, "// %s composes the readaccess helpers this kind needs.\n", rt)
	fmt.Fprintf(sb, "// Constructed per Unmarshal call; released via defer.\n")
	fmt.Fprintf(sb, "type %s struct {\n", rt)
	sb.WriteString("\tEntityId        *ra.ReadAccessFactsPlainEntityIdAttributes\n")
	sb.WriteString("\tEntityTimestamp *ra.ReadAccessFactsPlainEntityTimestampAttributes\n")
	if planHasLifecycle(plan) {
		sb.WriteString("\tEntityLifecycle *ra.ReadAccessFactsPlainEntityLifecycleAttributes\n")
	}
	for _, name := range names {
		method := upperFirst(name)
		fmt.Fprintf(sb, "\t%s *ra.ReadAccessFactsTagged%s\n", method, method)
	}
	sb.WriteString("}\n\n")
	return
}

func writeReaderCtor(sb *strings.Builder, plan *mappingplan.Plan) {
	names := activeSectionNamesByDecl(plan)
	rt := readerTypeName(plan)
	fmt.Fprintf(sb, "func new%s() *%s {\n", upperFirst(rt), rt)
	fmt.Fprintf(sb, "\treturn &%s{\n", rt)
	sb.WriteString("\t\tEntityId:        ra.NewReadAccessFactsPlainEntityIdAttributes(),\n")
	sb.WriteString("\t\tEntityTimestamp: ra.NewReadAccessFactsPlainEntityTimestampAttributes(),\n")
	if planHasLifecycle(plan) {
		sb.WriteString("\t\tEntityLifecycle: ra.NewReadAccessFactsPlainEntityLifecycleAttributes(),\n")
	}
	for _, name := range names {
		method := upperFirst(name)
		fmt.Fprintf(sb, "\t\t%s: ra.NewReadAccessFactsTagged%s(),\n", method, method)
	}
	sb.WriteString("\t}\n}\n\n")
}

func writeReaderLoad(sb *strings.Builder, plan *mappingplan.Plan) {
	names := activeSectionNamesByDecl(plan)
	rt := readerTypeName(plan)
	fmt.Fprintf(sb, "func (r *%s) loadFromRecord(rec arrow.Record) (err error) {\n", rt)
	pkg := plan.PackageName
	loadOne := func(field, label string) {
		fmt.Fprintf(sb, "\terr = r.%s.LoadFromRecord(rec)\n", field)
		sb.WriteString("\tif err != nil {\n")
		fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: load %s: %%w\", err)\n", pkg, label)
		sb.WriteString("\t\treturn\n\t}\n")
	}
	loadOne("EntityId", "EntityId")
	loadOne("EntityTimestamp", "EntityTimestamp")
	if planHasLifecycle(plan) {
		loadOne("EntityLifecycle", "EntityLifecycle")
	}
	for _, name := range names {
		method := upperFirst(name)
		loadOne(method, method)
	}
	sb.WriteString("\treturn\n}\n\n")
}

func writeReaderRelease(sb *strings.Builder, plan *mappingplan.Plan) {
	names := activeSectionNamesByDecl(plan)
	rt := readerTypeName(plan)
	fmt.Fprintf(sb, "func (r *%s) release() {\n", rt)
	sb.WriteString("\tif r == nil {\n\t\treturn\n\t}\n")
	releaseOne := func(field string) {
		fmt.Fprintf(sb, "\tif r.%s != nil {\n\t\tr.%s.Release()\n\t}\n", field, field)
	}
	releaseOne("EntityId")
	releaseOne("EntityTimestamp")
	if planHasLifecycle(plan) {
		releaseOne("EntityLifecycle")
	}
	for _, name := range names {
		releaseOne(upperFirst(name))
	}
	sb.WriteString("}\n\n")
}

func writeUnmarshalMethod(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	t := plan.KindType
	rt := readerTypeName(plan)
	names := activeSectionNamesByDecl(plan)

	fmt.Fprintf(sb, "// Unmarshal appends one row to c per entity in rec, projecting\n")
	fmt.Fprintf(sb, "// the runtime.facts columns through factsschema/ra. Thin wrapper\n")
	fmt.Fprintf(sb, "// around %sFillFromArrow — the per-row decode lives there.\n", t)
	fmt.Fprintf(sb, "func (c *%sColumns) Unmarshal(rec arrow.Record) (err error) {\n", t)
	fmt.Fprintf(sb, "\tr := new%s()\n", upperFirst(rt))
	sb.WriteString("\tdefer r.release()\n")
	sb.WriteString("\terr = r.loadFromRecord(rec)\n")
	sb.WriteString("\tif err != nil {\n\t\treturn\n\t}\n")
	sb.WriteString("\tn := r.EntityId.Len()\n")
	fmt.Fprintf(sb, "\terr = %sFillFromArrow(\n", t)
	sb.WriteString("\t\tc,\n")
	sb.WriteString("\t\tn,\n")
	sb.WriteString("\t\tr.EntityId.ValueId,\n")
	if planNaturalKeyCol(plan) != nil {
		sb.WriteString("\t\tr.EntityId.ValueNaturalKey,\n")
	}
	if planTsCol(plan) != nil {
		sb.WriteString("\t\tr.EntityTimestamp.ValueTs,\n")
	}
	if planExpiresAtCol(plan) != nil {
		sb.WriteString("\t\tr.EntityLifecycle.ValueExpiresAt,\n")
	}
	for _, name := range names {
		method := upperFirst(name)
		fmt.Fprintf(sb, "\t\tr.%s.Attributes, r.%s.Memberships,\n", method, method)
	}
	sb.WriteString("\t)\n")
	sb.WriteString("\treturn\n}\n\n")
	return
}

// --- buscodec.CodecI bridge. ---

func writeCodec(sb *strings.Builder, plan *mappingplan.Plan) {
	t := plan.KindType
	pkg := plan.PackageName
	codecVar := lowerFirst(t) + "BusCodec"

	sb.WriteString("// --- buscodec.CodecI bridge. ---\n\n")

	fmt.Fprintf(sb, "// %sCodec is the buscodec.CodecI bridge for %s. Encodes one row\n", t, t)
	fmt.Fprintf(sb, "// through dml_cbor; decodes via cborarrow.Convert + ra-Unmarshal +\n")
	fmt.Fprintf(sb, "// Row(0). Auto-registered in init() so callers using buscodec.Encode\n")
	fmt.Fprintf(sb, "// / Decode route through here instead of the CBOR fallback.\n")
	fmt.Fprintf(sb, "type %sCodec struct{}\n\n", t)
	fmt.Fprintf(sb, "var _ buscodec.CodecI = (*%sCodec)(nil)\n\n", t)

	fmt.Fprintf(sb, "// %s is the package-local singleton wired into buscodec.\n", codecVar)
	fmt.Fprintf(sb, "var %s = &%sCodec{}\n\n", codecVar, t)

	fmt.Fprintf(sb, "func (inst *%sCodec) Name() (n string) {\n", t)
	fmt.Fprintf(sb, "\tn = %q\n\treturn\n}\n\n", plan.KindName+"-sparse-cbor")
	fmt.Fprintf(sb, "func (inst *%sCodec) ContentType() (ct string) {\n", t)
	fmt.Fprintf(sb, "\tct = %q\n\treturn\n}\n\n", "application/x-runtime-facts+rb;kind="+plan.KindName)

	fmt.Fprintf(sb, "func (inst *%sCodec) Encode(v any) (b []byte, err error) {\n", t)
	fmt.Fprintf(sb, "\tvar row %s\n", t)
	sb.WriteString("\tswitch x := v.(type) {\n")
	fmt.Fprintf(sb, "\tcase %s:\n", t)
	sb.WriteString("\t\trow = x\n")
	fmt.Fprintf(sb, "\tcase *%s:\n", t)
	sb.WriteString("\t\tif x == nil {\n")
	fmt.Fprintf(sb, "\t\t\terr = eh.Errorf(\"%s: Encode nil *%s\")\n", pkg, t)
	sb.WriteString("\t\t\treturn\n\t\t}\n")
	sb.WriteString("\t\trow = *x\n")
	sb.WriteString("\tdefault:\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: Encode requires %s or *%s, got %%T\", v)\n", pkg, t, t)
	sb.WriteString("\t\treturn\n\t}\n")
	fmt.Fprintf(sb, "\tcols := &%sColumns{}\n", t)
	sb.WriteString("\tcols.Append(row)\n")
	sb.WriteString("\tvar buf bytes.Buffer\n")
	sb.WriteString("\terr = cols.Marshal(&buf)\n")
	sb.WriteString("\tif err != nil {\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: Encode marshal: %%w\", err)\n", pkg)
	sb.WriteString("\t\treturn\n\t}\n")
	sb.WriteString("\tb = buf.Bytes()\n")
	sb.WriteString("\treturn\n")
	sb.WriteString("}\n\n")

	fmt.Fprintf(sb, "func (inst *%sCodec) Decode(b []byte, v any) (err error) {\n", t)
	fmt.Fprintf(sb, "\ttarget, ok := v.(*%s)\n", t)
	sb.WriteString("\tif !ok {\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: Decode requires *%s, got %%T\", v)\n", pkg, t)
	sb.WriteString("\t\treturn\n\t}\n")
	sb.WriteString("\tvar arrowBuf bytes.Buffer\n")
	sb.WriteString("\terr = cborarrow.Convert(bytes.NewReader(b), &arrowBuf)\n")
	sb.WriteString("\tif err != nil {\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: Decode cborarrow: %%w\", err)\n", pkg)
	sb.WriteString("\t\treturn\n\t}\n")
	sb.WriteString("\trd, err := ipc.NewReader(&arrowBuf)\n")
	sb.WriteString("\tif err != nil {\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: Decode ipc reader: %%w\", err)\n", pkg)
	sb.WriteString("\t\treturn\n\t}\n")
	sb.WriteString("\tdefer rd.Release()\n")
	sb.WriteString("\tif !rd.Next() {\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: Decode expected one Arrow record, got none\")\n", pkg)
	sb.WriteString("\t\treturn\n\t}\n")
	sb.WriteString("\trec := rd.RecordBatch()\n")
	sb.WriteString("\tdefer rec.Release()\n")
	fmt.Fprintf(sb, "\tcols := &%sColumns{}\n", t)
	sb.WriteString("\terr = cols.Unmarshal(rec)\n")
	sb.WriteString("\tif err != nil {\n")
	fmt.Fprintf(sb, "\t\terr = eh.Errorf(\"%s: Decode unmarshal: %%w\", err)\n", pkg)
	sb.WriteString("\t\treturn\n\t}\n")
	sb.WriteString("\tif cols.Len() != 1 {\n")
	fmt.Fprintf(sb, "\t\terr = eb.Build().Int(\"got\", cols.Len()).Errorf(\"%s: Decode expected 1 row\")\n", pkg)
	sb.WriteString("\t\treturn\n\t}\n")
	sb.WriteString("\t*target = cols.Row(0)\n")
	sb.WriteString("\treturn\n")
	sb.WriteString("}\n")
}

// --- helpers shared with marshallgen via convention. ---

// uniqueMemberships returns each distinct LWMembership once, skipping
// channels that need no kindXxx var (Verbatim / Parametrized) — those
// either embed the literal name at the call site or carry the payload
// directly. Generalised by boxer ADR-0057 D3 from the original
// "Verbatim-only" skip.
func uniqueMemberships(plan *mappingplan.Plan) (out []mappingplan.TaggedField) {
	seen := map[string]bool{}
	for _, f := range plan.Fields {
		if !f.Flags.Channel.NeedsKindVar() {
			continue
		}
		if seen[f.LWMembership] {
			continue
		}
		seen[f.LWMembership] = true
		out = append(out, f)
	}
	return
}

func planTsCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return findPlainCol(plan, "ts")
}

func planNaturalKeyCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return findPlainCol(plan, "naturalKey")
}

func planExpiresAtCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return findPlainCol(plan, "expiresAt")
}

func planHasLifecycle(plan *mappingplan.Plan) bool {
	return planExpiresAtCol(plan) != nil
}

func findPlainCol(plan *mappingplan.Plan, col string) *mappingplan.PlainCol {
	for i := range plan.PlainCols {
		if plan.PlainCols[i].Column == col {
			return &plan.PlainCols[i]
		}
	}
	return nil
}

// activeSectionIndices returns the sorted dml_cbor section indices the
// kind populates. Drives SetActiveSections at write time.
func activeSectionIndices(plan *mappingplan.Plan) (out []int, err error) {
	seen := map[int]bool{}
	for _, f := range plan.Fields {
		idx, ok := cbdml.InEntityFactsSectionIndices[f.LWSection]
		if !ok {
			err = eb.Build().Str("section", f.LWSection).Errorf("factswrapper: section not in dml_cbor table (must be one of the 21 runtime.facts schema sections)")
			return
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		out = append(out, idx)
	}
	sort.Ints(out)
	return
}

// activeSectionNamesByIdx returns each populated section name in
// dml-index order. Only the ActiveFields scan body uses this — its
// emitted lookup table is order-independent, but a deterministic
// order keeps generator output stable across runs.
func activeSectionNamesByIdx(plan *mappingplan.Plan) (out []string) {
	type entry struct {
		idx  int
		name string
	}
	seen := map[string]bool{}
	var entries []entry
	for _, f := range plan.Fields {
		if seen[f.LWSection] {
			continue
		}
		seen[f.LWSection] = true
		idx, ok := cbdml.InEntityFactsSectionIndices[f.LWSection]
		if !ok {
			continue
		}
		entries = append(entries, entry{idx, f.LWSection})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].idx < entries[j].idx })
	out = make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.name
	}
	return
}

// activeSectionNamesByDecl returns each populated section name in DTO
// declaration order. Reader struct fields + Unmarshal call argument
// order must match marshallgen's BuildEntities / FillFromArrow type-
// parameter order, which is computeGroups order (first-seen, DTO
// declaration order).
func activeSectionNamesByDecl(plan *mappingplan.Plan) (out []string) {
	seen := map[string]bool{}
	for _, f := range plan.Fields {
		if seen[f.LWSection] {
			continue
		}
		seen[f.LWSection] = true
		if _, ok := cbdml.InEntityFactsSectionIndices[f.LWSection]; !ok {
			continue
		}
		out = append(out, f.LWSection)
	}
	return
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'A' && s[0] <= 'Z' {
		return string(s[0]+32) + s[1:]
	}
	return s
}

// Section-name → dml index lookups go through
// cbdml.InEntityFactsSectionIndices — boxer's dml generator emits
// the map alongside the InEntityFacts struct, derived from the same
// TableDesc that drives the section00Inst…sectionNNInst slot
// assignment. Drift between the wrapper and the dml output is
// impossible by construction.
