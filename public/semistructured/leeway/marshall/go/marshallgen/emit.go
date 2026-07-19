package marshallgen

import (
	"fmt"
	"go/format"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// EmitPlan renders a parsed mappingplan.Plan to .out.go source text and runs
// go/format on the result. Returns the formatted bytes.
//
// Schema-agnostic core (always emitted):
//
//   - writeHeader / writeImports
//   - <Kind>Columns + Len + Append + Row
//   - per-section AttrI + SecI interfaces
//   - <Kind>EntityI
//   - <Kind>BuildEntities helper
//   - per-section AttrsReadI + MembsReadI interfaces
//   - <Kind>FillFromArrow helper
//
// Wrapper hooks (target-specific blocks, optional):
//
//   - w.Imports(plan)          → extra import lines
//   - w.KindVars(sb, plan)     → kindXxx symbol decls
//   - w.Init(sb, plan)         → package init() body
//   - w.BeforeCore(sb, plan)   → pool, active-hints, etc.
//   - w.AfterCore(sb, plan)    → Marshal/Unmarshal/Codec etc.
//
// NoOpWrapper provides anchor-style emit (consts + no init + no
// pre/post). The schema-agnostic core compiles against any leeway DML
// / RA implementation whose method shapes satisfy the derived
// interfaces; Go type inference at the BuildEntities / FillFromArrow
// call site binds the type parameters from the concrete DML pointer.
// EmitModeE selects which pieces EmitPlan renders.
type EmitModeE uint8

const (
	// EmitModeCodec is the full exported codec — SoA <Kind>Columns,
	// BuildEntities, FillFromArrow, AddSections, ReadRow and the
	// constraint interfaces. The zero value; the product for the
	// keelson codecs and the anchor demos.
	EmitModeCodec EmitModeE = 0
	// EmitModeStoreSupport emits only what a generated record store
	// consumes — AddSections, ReadRow and their constraint interfaces —
	// with the kind-derived identifier prefix unexported (the DTO type
	// itself is the consumer's hand-written name and stays as written).
	// No SoA Columns, no BuildEntities, no FillFromArrow (ADR-0100).
	EmitModeStoreSupport EmitModeE = 1
)

// EmitOpts parameterizes EmitPlan. The zero value is the full codec.
type EmitOpts struct {
	Mode EmitModeE
}

// kindIdent renders the kind-derived identifier prefix for emitted
// names: exported in codec mode, unexported in store-support mode.
// Never use it for the DTO type itself.
func kindIdent(kind string, mode EmitModeE) string {
	if mode == EmitModeStoreSupport {
		return lowerFirst(kind)
	}
	return kind
}

func EmitPlan(plan *mappingplan.Plan, wrapper WrapperEmitterI, opts EmitOpts) (out []byte, err error) {
	if wrapper == nil {
		wrapper = NoOpWrapper{}
	}
	// The body is emitted before the imports so the import set can be gated
	// on what the emitted code actually uses (the eb import varies by field
	// shapes; predicating it on plan properties drifted once already).
	var body strings.Builder
	wrapper.KindVars(&body, plan)
	wrapper.Init(&body, plan)
	err = wrapper.BeforeCore(&body, plan)
	if err != nil {
		err = eb.Build().Errorf("wrapper BeforeCore: %w", err)
		return
	}

	switch opts.Mode {
	case EmitModeStoreSupport:
		// Only what a generated record store consumes, kind prefix
		// unexported: the constraint interfaces, AddSections and ReadRow.
		// No SoA Columns, no BuildEntities, no FillFromArrow — driving
		// entity frames past the store's bookkeeping is a coherence
		// bypass there (ADR-0100).
		groups := goplan.ComputeGroups(plan)
		for _, g := range groups {
			err = writeSectionInterfaces(&body, plan, g, opts.Mode)
			if err != nil {
				return
			}
		}
		err = writeEntityInterface(&body, plan, groups, opts.Mode)
		if err != nil {
			return
		}
		err = writeAddSectionsFunc(&body, plan, groups, opts.Mode)
		if err != nil {
			err = eb.Build().Errorf("emit AddSections: %w", err)
			return
		}
		for _, g := range groups {
			err = writeSectionReadInterfaces(&body, plan, g, opts.Mode)
			if err != nil {
				return
			}
		}
		err = writeReadRowHelper(&body, plan, opts.Mode)
		if err != nil {
			err = eb.Build().Errorf("emit ReadRow: %w", err)
			return
		}
	default: // EmitModeCodec — the full exported codec.
		writeColumnsStruct(&body, plan)
		writeLenAndAppend(&body, plan)
		writeRowExtract(&body, plan)

		err = writeBuildHelper(&body, plan)
		if err != nil {
			err = eb.Build().Errorf("emit BuildEntities: %w", err)
			return
		}
		err = writeFillHelper(&body, plan)
		if err != nil {
			err = eb.Build().Errorf("emit FillFromArrow: %w", err)
			return
		}
		err = writeReadRowHelper(&body, plan, opts.Mode)
		if err != nil {
			err = eb.Build().Errorf("emit ReadRow: %w", err)
			return
		}
	}

	err = wrapper.AfterCore(&body, plan)
	if err != nil {
		err = eb.Build().Errorf("wrapper AfterCore: %w", err)
		return
	}

	var sb strings.Builder
	writeHeader(&sb, plan)
	writeImports(&sb, plan, wrapper, strings.Contains(body.String(), "eb.Build("), strings.Contains(body.String(), "iter."), opts.Mode)
	sb.WriteString(body.String())

	raw := []byte(sb.String())
	out, err = format.Source(raw)
	if err != nil {
		err = eb.Build().Str("emitted", string(raw)).Errorf("gofmt rejected output: %w", err)
		return
	}
	return
}

// Generate is the one-call convenience: ParsePlan then EmitPlan then
// writeFile. Returns the rendered bytes for callers that want to
// byte-compare against a golden file.
func Generate(inputPath, outputPath string, wrapper WrapperEmitterI, opts EmitOpts) (out []byte, err error) {
	var plan *mappingplan.Plan
	plan, err = ParsePlan(inputPath)
	if err != nil {
		return
	}
	out, err = EmitPlan(plan, wrapper, opts)
	if err != nil {
		return
	}
	if outputPath != "" {
		err = writeFile(outputPath, out)
		if err != nil {
			return
		}
	}
	return
}

func writeFile(path string, data []byte) (err error) {
	err = os.WriteFile(path, data, 0644)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("write file: %w", err)
		return
	}
	return
}

// methodFor returns the PascalCase section name used in the DML's
// `GetSection<X>()` getter and the ra reader's `Tagged<X>` type. This
// is convention-based: mappingplan.UpperFirst of the section name from the lw:
// tag. Same convention boxer's gocodegen uses.
func methodFor(section string) string {
	return mappingplan.UpperFirst(section)
}

// --- render helpers. ---

func writeHeader(sb *strings.Builder, plan *mappingplan.Plan) {
	line(sb, 0, "// Code generated by boxer/public/semistructured/leeway/marshall/go/marshallgen — DO NOT EDIT.\n")
	linef(sb, 0, "package %s\n", plan.PackageName)
}

func writeImports(sb *strings.Builder, plan *mappingplan.Plan, wrapper WrapperEmitterI, bodyUsesEB, bodyUsesIter bool, mode EmitModeE) {
	needsRoaring := false
	needsTime := false
	needsMarshalltypes := false
	needsLw := false
	for _, f := range plan.Fields {
		if f.IsRoaring() {
			needsRoaring = true
		}
		if f.GoType() == "time.Time" {
			needsTime = true
		}
		if f.CarrierField != "" {
			needsMarshalltypes = true
		}
		// An lw.* marker membership emits newtype conversions (lw.Ref(v) / …) in
		// the decode, which need the lw package.
		for _, m := range f.TupleMemberships {
			if m.MarkerGoType != "" {
				needsLw = true
			}
		}
	}
	for _, p := range plan.PlainCols {
		// A time.Time plain column needs the time import: FillFromArrow
		// reconstructs it from Arrow nanos via time.Unix — a codec-mode
		// piece. Non-time columns (e.g. int64 nanos) don't — strict 1:1
		// inserts no conversion.
		if p.GoType() == "time.Time" && mode == EmitModeCodec {
			needsTime = true
		}
	}
	// Each contributor declares the imports its OWN emitted code uses; the
	// import set dedups by path so the core and the wrapper need not coordinate
	// (e.g. both may declare eb — it collapses to one, no duplicate-import
	// error and no need for either to mirror the other's gating). Section-only
	// imports (iter Seq accessors, the dml/ra runtimes) are gated on the plan
	// actually having a tagged field; eb on the already-emitted body actually
	// using it (the occurrence / carrier-count checks appear only for
	// scalar-decoded shapes — an array-only kind emits none). array + eh
	// belong to codec-mode pieces only: plain reads (FillFromArrow) use
	// array, BuildEntities's commit wrap uses eh — the store-support mode
	// emits neither piece.
	hasTagged := len(plan.Fields) > 0

	imps := newImportSet()

	stdlib := []string{}
	// iter is used only where a container / dynamic-membership section emits an
	// iter.Seq accessor; a scalar-only static nested section reads no Seq. Gate
	// on the emitted body (like eb), not merely on having a tagged field.
	if bodyUsesIter {
		stdlib = append(stdlib, `"iter"`)
	}
	if needsTime {
		stdlib = append(stdlib, `"time"`)
	}
	imps.group(stdlib...)

	thirdParty := []string{}
	if needsRoaring {
		thirdParty = append(thirdParty, `"github.com/RoaringBitmap/roaring"`)
	}
	if mode == EmitModeCodec {
		thirdParty = append(thirdParty, `"github.com/apache/arrow-go/v18/arrow/array"`)
	}
	imps.group(thirdParty...)

	boxer := []string{}
	if mode == EmitModeCodec {
		boxer = append(boxer, `"github.com/stergiotis/boxer/public/observability/eh"`)
	}
	if bodyUsesEB {
		boxer = append(boxer, `"github.com/stergiotis/boxer/public/observability/eh/eb"`)
	}
	if hasTagged {
		boxer = append(boxer,
			`dmlruntime "github.com/stergiotis/boxer/public/semistructured/leeway/dml/runtime"`,
			`raruntime "github.com/stergiotis/boxer/public/semistructured/leeway/readaccess/runtime"`)
	}
	if needsMarshalltypes {
		boxer = append(boxer, `"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/marshalltypes"`)
	}
	if needsLw {
		boxer = append(boxer, `"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/lw"`)
	}
	imps.group(boxer...)

	// Wrapper-supplied groups (its "" entries delimit them). Deduped against the
	// core's imports above.
	for _, g := range splitImportGroups(wrapper.Imports(plan)) {
		imps.group(g...)
	}

	imps.render(sb)
}

// importSet accumulates Go import specs into blank-line-delimited groups and
// renders a deduplicated import block. A spec whose import path was already
// added — in any group — is dropped, so independent contributors (the
// schema-agnostic core and a WrapperEmitterI) can each declare the imports
// their own emitted code needs without coordinating: overlaps (e.g. both using
// eb) collapse to one import rather than a duplicate-import compile error.
// go/format sorts within each group and collapses any blank line a dropped spec
// would otherwise leave behind.
type importSet struct {
	seen   map[string]struct{}
	groups [][]string
}

func newImportSet() *importSet { return &importSet{seen: map[string]struct{}{}} }

// group adds one blank-line-delimited group, skipping blank entries and any
// spec whose import path is already present. A group left empty is not emitted.
func (s *importSet) group(specs ...string) {
	var g []string
	for _, spec := range specs {
		p := importSpecPath(spec)
		if p == "" {
			continue
		}
		if _, dup := s.seen[p]; dup {
			continue
		}
		s.seen[p] = struct{}{}
		g = append(g, spec)
	}
	if len(g) > 0 {
		s.groups = append(s.groups, g)
	}
}

func (s *importSet) render(sb *strings.Builder) {
	line(sb, 0, "import (")
	for i, g := range s.groups {
		if i > 0 {
			blank(sb)
		}
		for _, spec := range g {
			linef(sb, 1, "%s", spec)
		}
	}
	line(sb, 0, ")\n")
}

// importSpecPath extracts the quoted path from an import spec line, e.g.
// `cbdml "github.com/x/y"` → "github.com/x/y", `"iter"` → "iter". Returns ""
// for a spec carrying no quoted path (a "" group separator).
func importSpecPath(spec string) string {
	i := strings.IndexByte(spec, '"')
	j := strings.LastIndexByte(spec, '"')
	if i < 0 || j <= i {
		return ""
	}
	return spec[i+1 : j]
}

// splitImportGroups splits a WrapperEmitterI.Imports() result into groups on its
// "" blank-line separators — the convention wrappers use to group imports.
func splitImportGroups(specs []string) (out [][]string) {
	var cur []string
	for _, s := range specs {
		if s == "" {
			if len(cur) > 0 {
				out = append(out, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, s)
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return
}

// --- Columns SoA + Append/Row adapters. ---

func writeColumnsStruct(sb *strings.Builder, plan *mappingplan.Plan) {
	line(sb, 0, "// --- SoA columns + AoS Append adapter. ---")
	blank(sb)
	linef(sb, 0, "// %sColumns is the SoA storage for batches of %s rows.", plan.KindType, plan.KindType)
	line(sb, 0, "// All slices grow in lockstep — Len returns the row count.")
	linef(sb, 0, "type %sColumns struct {", plan.KindType)
	for _, p := range plan.PlainCols {
		linef(sb, 1, "%s []%s", p.GoField, p.GoType())
	}
	blank(sb)
	seenTuple := map[string]bool{}
	for _, f := range plan.Fields {
		if f.IsConst {
			continue // const fields have no Go-side storage
		}
		// A tuple / nested section's sub-column fields live inside the element
		// struct; the SoA column is the outer field, once per tuple. Its shape
		// follows the attributes-per-row cardinality: Many `[][]S`, One `[]S`,
		// Optional decomposed into `<F>Val []S` + `<F>Has []bool` (mirroring the
		// scalar-Option split above).
		if f.TupleField != "" {
			if !seenTuple[f.TupleField] {
				seenTuple[f.TupleField] = true
				switch f.TupleCardinality {
				case mappingplan.AttrCardinalityOne:
					linef(sb, 1, "%s []%s", f.TupleField, f.TupleStructType)
				case mappingplan.AttrCardinalityOptional:
					linef(sb, 1, "%sVal []%s", f.TupleField, f.TupleStructType)
					linef(sb, 1, "%sHas []bool", f.TupleField)
				default: // Many
					linef(sb, 1, "%s [][]%s", f.TupleField, f.TupleStructType)
				}
			}
			continue
		}
		switch {
		case f.IsOption:
			linef(sb, 1, "%sVal []%s", f.GoFieldName, f.GoType())
			linef(sb, 1, "%sHas []bool", f.GoFieldName)
		case f.IsSlice():
			linef(sb, 1, "%s [][]%s", f.GoFieldName, f.GoType())
		case f.IsRoaring():
			linef(sb, 1, "%s []*roaring.Bitmap", f.GoFieldName)
		default:
			linef(sb, 1, "%s []%s", f.GoFieldName, f.GoType())
		}
		// Cut-2 carrier sibling: its own SoA column, emits no attribute —
		// one scalar carrier per attribute, so []X in the SoA.
		if f.CarrierField != "" {
			linef(sb, 1, "%s []marshalltypes.%s", f.CarrierField, f.CarrierType)
		}
	}
	line(sb, 0, "}\n")
}

func writeLenAndAppend(sb *strings.Builder, plan *mappingplan.Plan) {
	linef(sb, 0, "// Len returns the number of rows currently in the batch.")
	linef(sb, 0, "func (c *%sColumns) Len() int { return len(c.%s) }", plan.KindType, plan.PlainCols[0].GoField)
	blank(sb)

	linef(sb, 0, "// Append pushes one AoS record into the SoA buffers.")
	linef(sb, 0, "//")
	linef(sb, 0, "// Aliasing: slice and pointer fields (`[]T`, `*roaring.Bitmap`) are")
	linef(sb, 0, "// stored by reference, not copied. Callers must not mutate")
	linef(sb, 0, "// row.<F> after Append unless they want Marshal to read the")
	linef(sb, 0, "// mutation. Scalar fields (T, Option[T]) are copied by value.")
	linef(sb, 0, "func (c *%sColumns) Append(row %s) {", plan.KindType, plan.KindType)
	for _, p := range plan.PlainCols {
		linef(sb, 1, "c.%s = append(c.%s, row.%s)", p.GoField, p.GoField, p.GoField)
	}
	seenTuple := map[string]bool{}
	for _, f := range plan.Fields {
		if f.IsConst {
			continue
		}
		if f.TupleField != "" {
			if !seenTuple[f.TupleField] {
				seenTuple[f.TupleField] = true
				if f.TupleCardinality == mappingplan.AttrCardinalityOptional {
					linef(sb, 1, "c.%sVal = append(c.%sVal, row.%s.Val)", f.TupleField, f.TupleField, f.TupleField)
					linef(sb, 1, "c.%sHas = append(c.%sHas, row.%s.Has)", f.TupleField, f.TupleField, f.TupleField)
				} else {
					linef(sb, 1, "c.%s = append(c.%s, row.%s)", f.TupleField, f.TupleField, f.TupleField)
				}
			}
			continue
		}
		if f.IsOption {
			linef(sb, 1, "c.%sVal = append(c.%sVal, row.%s.Val)", f.GoFieldName, f.GoFieldName, f.GoFieldName)
			linef(sb, 1, "c.%sHas = append(c.%sHas, row.%s.Has)", f.GoFieldName, f.GoFieldName, f.GoFieldName)
		} else {
			linef(sb, 1, "c.%s = append(c.%s, row.%s)", f.GoFieldName, f.GoFieldName, f.GoFieldName)
		}
		if f.CarrierField != "" {
			linef(sb, 1, "c.%s = append(c.%s, row.%s)", f.CarrierField, f.CarrierField, f.CarrierField)
		}
	}
	line(sb, 0, "}\n")
}

func writeRowExtract(sb *strings.Builder, plan *mappingplan.Plan) {
	linef(sb, 0, "// Row reconstructs entity i as an AoS %s record. Inverse of", plan.KindType)
	linef(sb, 0, "// Append: slice / pointer fields are shared by reference (no")
	linef(sb, 0, "// defensive copy); scalar fields and Option[T] are copied.")
	linef(sb, 0, "func (c *%sColumns) Row(i int) (row %s) {", plan.KindType, plan.KindType)
	for _, p := range plan.PlainCols {
		linef(sb, 1, "row.%s = c.%s[i]", p.GoField, p.GoField)
	}
	seenTuple := map[string]bool{}
	for _, f := range plan.Fields {
		if f.IsConst {
			continue
		}
		if f.TupleField != "" {
			if !seenTuple[f.TupleField] {
				seenTuple[f.TupleField] = true
				if f.TupleCardinality == mappingplan.AttrCardinalityOptional {
					linef(sb, 1, "row.%s.Val = c.%sVal[i]", f.TupleField, f.TupleField)
					linef(sb, 1, "row.%s.Has = c.%sHas[i]", f.TupleField, f.TupleField)
				} else {
					linef(sb, 1, "row.%s = c.%s[i]", f.TupleField, f.TupleField)
				}
			}
			continue
		}
		if f.IsOption {
			linef(sb, 1, "row.%s.Val = c.%sVal[i]", f.GoFieldName, f.GoFieldName)
			linef(sb, 1, "row.%s.Has = c.%sHas[i]", f.GoFieldName, f.GoFieldName)
		} else {
			linef(sb, 1, "row.%s = c.%s[i]", f.GoFieldName, f.GoFieldName)
		}
		if f.CarrierField != "" {
			linef(sb, 1, "row.%s = c.%s[i]", f.CarrierField, f.CarrierField)
		}
	}
	line(sb, 1, "return\n}\n")
}

// --- plain-column helpers. ---

func planIdCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return goplan.FindPlainCol(plan, "id")
}
func planTsCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return goplan.FindPlainCol(plan, "ts")
}
func planNaturalKeyCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return goplan.FindPlainCol(plan, "naturalKey")
}
func planExpiresAtCol(plan *mappingplan.Plan) *mappingplan.PlainCol {
	return goplan.FindPlainCol(plan, "expiresAt")
}

// plainArrowParam renders the Arrow accessor parameter type for a plain
// column in FillFromArrow, e.g. "*array.Uint64". The Go type was
// validated as a supported plain type at parse time.
func plainArrowParam(p *mappingplan.PlainCol) string {
	at, _ := goplan.PlainArrowArrayType(p.GoType())
	return "*" + at
}

// writePlainRead emits the append of colVar.Value(i) into c.<GoField> in
// FillFromArrow, with the per-type read handling: defensive copy for
// []byte, time.Time reconstruction from Arrow nanos, and a copy into a
// fresh array for fixed-width [N]byte. Scalars pass straight through
// (strict 1:1 — the column Go type is the value type).
func writePlainRead(sb *strings.Builder, depth int, p *mappingplan.PlainCol, colVar string) {
	f := p.GoField
	switch goplan.CopyStrategy(p.GoType()) {
	case goplan.CopyTime:
		linef(sb, depth, "c.%s = append(c.%s, time.Unix(0, int64(%s.Value(i))).UTC())", f, f, colVar)
	case goplan.CopyBytes:
		line(sb, depth, "{")
		linef(sb, depth+1, "src := %s.Value(i)", colVar)
		line(sb, depth+1, "cp := make([]byte, len(src))")
		line(sb, depth+1, "copy(cp, src)")
		linef(sb, depth+1, "c.%s = append(c.%s, cp)", f, f)
		line(sb, depth, "}")
	case goplan.CopyFixedByte:
		line(sb, depth, "{")
		linef(sb, depth+1, "var v %s", p.GoType())
		linef(sb, depth+1, "copy(v[:], %s.Value(i))", colVar)
		linef(sb, depth+1, "c.%s = append(c.%s, v)", f, f)
		line(sb, depth, "}")
	default:
		linef(sb, depth, "c.%s = append(c.%s, %s.Value(i))", f, f, colVar)
	}
}

// --- BuildEntities core + derived interfaces. ---

func writeBuildHelper(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	groups := goplan.ComputeGroups(plan)

	line(sb, 0, "// --- Composed-interface BuildEntities helper (schema-agnostic). ---\n//")
	linef(sb, 0, "// %sBuildEntities walks the SoA columns and emits one entity per", plan.KindType)
	line(sb, 0, "// row through dml.BeginEntity / per-section BeginAttribute* /")
	line(sb, 0, "// AddMembershipLowCardRefP / AddToContainerP / EndAttributeP /")
	line(sb, 0, "// EndSection / CommitEntity. dml is generic — any leeway-DML class")
	line(sb, 0, "// whose method shapes satisfy the derived interfaces qualifies;")
	line(sb, 0, "// Go's type inference binds the type parameters at the call site.\n//")
	line(sb, 0, "// Callers drain via dml.TransferRecords (or schema-specific")
	line(sb, 0, "// equivalents) — left outside the helper because the record type")
	line(sb, 0, "// varies by target.\n")

	for _, g := range groups {
		err = writeSectionInterfaces(sb, plan, g, EmitModeCodec)
		if err != nil {
			return
		}
	}
	err = writeEntityInterface(sb, plan, groups, EmitModeCodec)
	if err != nil {
		return
	}
	err = writeBuildEntitiesFunc(sb, plan, groups)
	if err != nil {
		return
	}
	err = writeAddSectionsFunc(sb, plan, groups, EmitModeCodec)
	return
}

// elemType reports the per-attribute / per-element argument type the
// section's value column accepts. For `*roaring.Bitmap` the wire
// element is uint32; for `[N]byte` re-sliced into a blob the element
// is `[]byte`. For scalar fixed-width arrays kept as-is, the type is
// the array literal (the DML expects `[N]byte` directly only on plain
// columns — tagged-value fixed-width blobs always re-slice into
// `[]byte`).
func elemType(f mappingplan.TaggedField) string {
	if f.IsRoaring() {
		return "uint32"
	}
	if goplan.IsFixedByteArray(f.GoType()) {
		return "[]byte"
	}
	return f.GoType()
}

// writeSectionInterfaces emits the per-section AttrI + SecI interface
// pair for one section group. AttrI lists only the methods this DTO's
// fields use (membership P op, container-append P op, EndAttribute P
// op); SecI lists the BeginAttribute method shapes any field needs
// plus EndSection.
//
// All AttrI methods are P-variants (void return) — no F-bounded
// `[Self]` parameter is needed. SecI keeps `[Attr, Ent]` parameters
// because BeginAttribute* still return Attr (caller needs the handle)
// and EndSection returns Ent for the chain back to the entity.
func writeSectionInterfaces(sb *strings.Builder, plan *mappingplan.Plan, g goplan.SectionGroup, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)
	method := methodFor(g.Section)

	// Survey which Begin* shapes and container-append the SecI / AttrI
	// must expose, based on the union of field shapes in this group.
	needContainerOpen := false // shape: sec.BeginAttribute() (no args, opens container)
	needBeginSingleVal := ""   // shape: sec.BeginAttributeSingle(value T) — element type
	needBeginScalarVal := ""   // shape: sec.BeginAttribute(value T) — element type
	needAddToContainer := ""   // attr.AddToContainerP(v T) — element type (single-sub-column container)
	multiSubColAttr := false   // multi-sub-column — BeginAttribute(<scalars…>) + zipped co-containers (ADR-0101)
	multiSubColScalars := []argPair{}
	multiSubColContainers := []argPair{}

	// A dynamic-membership tuple section (ADR-0103) drives the same
	// per-attribute call shape — BeginAttribute(<scalars…>) + zipped
	// co-containers — at ANY sub-column count, so it routes through the
	// multi-sub-column survey even with a single sub-column.
	_, isTuple := g.TupleSpec()
	if isTuple || len(g.SubColumns) > 1 {
		multiSubColAttr = true
		for _, sc := range g.SubColumns {
			// Backstops only — PlanBuilder.Finish rejects these shapes for
			// both front-ends (ADR-0101 D3); hand-built plans still hit them.
			if len(sc.Fields) != 1 {
				err = eb.Build().Str("section", g.Section).Str("column", sc.Name).Errorf("multi-field sub-column in multi-sub-column section not supported")
				return
			}
			f := sc.Fields[0]
			if f.IsOption || f.IsRoaring() {
				err = eb.Build().Str("section", g.Section).Str("field", f.GoFieldName).Errorf("Option / roaring field in multi-sub-column section not supported")
				return
			}
		}
		// Scalar class → BeginAttribute arguments; container class →
		// AddToContainerP / AddToCoContainersP arguments (declaration order
		// within each class, matching the DML generator's per-class rule).
		for _, sc := range g.ScalarSubColumns() {
			multiSubColScalars = append(multiSubColScalars, argPair{Name: sc.Name, Type: sc.Fields[0].GoType()})
		}
		for _, sc := range g.ContainerSubColumns() {
			multiSubColContainers = append(multiSubColContainers, argPair{Name: sc.Name, Type: elemType(sc.Fields[0])})
		}
	} else {
		for _, f := range g.SubColumns[0].Fields {
			switch goplan.ClassifyBegin(f) {
			case goplan.ShapeScalarBegin:
				needBeginScalarVal = elemType(f)
			case goplan.ShapeScalarBeginSingle:
				needBeginSingleVal = elemType(f)
			case goplan.ShapeContainer:
				needContainerOpen = true
				needAddToContainer = elemType(f)
			}
		}
	}

	// --- AttrI. ---
	linef(sb, 0, "// %s%sAttrI is the InAttr-side view of the %s section. P-variants only —", kind, method, g.Section)
	line(sb, 0, "// every method returns void so no F-bounded `[Self]` parameter is")
	line(sb, 0, "// needed.")
	linef(sb, 0, "type %s%sAttrI interface {", kind, method)
	if ts, ok := g.TupleSpec(); ok && len(ts.Memberships) > 0 {
		// A DYNAMIC tuple element may carry memberships on several channels
		// (ADR-0109 D4); the AttrI embeds one InAttributeMembership<Channel>PI
		// per channel.
		for _, ch := range ts.Channels() {
			linef(sb, 1, "dmlruntime.InAttributeMembership%sPI", ch.AddMethodSuffix())
		}
	} else {
		// A plain section OR a STATIC nested section carries one section
		// membership (g.Channel()); the static tuple resolves it exactly like a
		// flat section.
		linef(sb, 1, "dmlruntime.InAttributeMembership%sPI", g.Channel().AddMethodSuffix())
	}
	if needAddToContainer != "" {
		linef(sb, 1, "AddToContainerP(value %s)", needAddToContainer)
	}
	if multiSubColAttr && len(multiSubColContainers) > 0 {
		argDecls := make([]string, 0, len(multiSubColContainers))
		for _, p := range multiSubColContainers {
			argDecls = append(argDecls, fmt.Sprintf("%s %s", p.Name, p.Type))
		}
		linef(sb, 1, "%sP(%s)", goplan.ContainerAddMethod(len(multiSubColContainers)), strings.Join(argDecls, ", "))
	}
	line(sb, 1, "EndAttributeP()")
	line(sb, 0, "}\n")

	// --- SecI. ---
	linef(sb, 0, "// %s%sSecI is the Section-side view: opens an attribute and closes", kind, method)
	line(sb, 0, "// the section. Attr and Ent are bound at the call site by inference.")
	linef(sb, 0, "type %s%sSecI[Attr any, Ent any] interface {", kind, method)
	switch {
	case multiSubColAttr:
		argDecls := make([]string, 0, len(multiSubColScalars))
		for _, p := range multiSubColScalars {
			argDecls = append(argDecls, fmt.Sprintf("%s %s", p.Name, p.Type))
		}
		linef(sb, 1, "BeginAttribute(%s) Attr", strings.Join(argDecls, ", "))
	default:
		if needContainerOpen {
			line(sb, 1, "BeginAttribute() Attr")
		}
		if needBeginScalarVal != "" && !needContainerOpen {
			linef(sb, 1, "BeginAttribute(value %s) Attr", needBeginScalarVal)
		} else if needBeginScalarVal != "" {
			// container path + per-element scalar Begin both exist on the
			// same section: would require Go overloading; not supported.
			err = eb.Build().Str("section", g.Section).Errorf("section mixes container default and scalar BeginAttribute on different fields — disambiguate via flags (add `,explode` everywhere, or none)")
			return
		}
		if needBeginSingleVal != "" {
			linef(sb, 1, "BeginAttributeSingle(value %s) Attr", needBeginSingleVal)
		}
	}
	line(sb, 1, "EndSection() Ent")
	line(sb, 0, "}\n")
	return
}

// writeEntityInterface emits the entity-level interface — section
// getters + entity-lifecycle methods. Per-section: one Attr + one Sec
// type parameter (no F-bounded recursion on Attr). Ent stays on the
// EntityI (BeginEntity / SetId / SetTimestamp / SetLifecycle return
// it; CommitEntity returns error).
func writeEntityInterface(sb *strings.Builder, plan *mappingplan.Plan, groups []goplan.SectionGroup, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)

	linef(sb, 0, "// %sEntityI is the entity-builder surface %sAddSections drives.", kind, kind)
	line(sb, 0, "// It always lists the per-section getters; the entity-frame methods")
	line(sb, 0, "// (BeginEntity / plain setters / CommitEntity) are added only for the")
	line(sb, 0, "// full codec's BuildEntities. AddSections stacks sections onto a frame")
	line(sb, 0, "// the caller already owns, so it needs none of them — which lets a")
	line(sb, 0, "// store drive it with a builder whose frame control is unexported")
	line(sb, 0, "// (ADR-0100 SD6). Ent is the builder pointer.")
	linef(sb, 0, "type %sEntityI[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttr %s%sAttrI,", method, kind, method)
		linef(sb, 1, "%sSec %s%sSecI[%sAttr, Ent],", method, kind, method, method)
	}
	line(sb, 1, "Ent any,")
	line(sb, 0, "] interface {")

	idCol := planIdCol(plan)
	if idCol == nil {
		err = eb.Build().Errorf("plain spec missing required `id` column")
		return
	}
	// Frame-lifecycle methods are what BuildEntities drives; AddSections
	// never calls them, so the store-support product omits them and its
	// constraint stays satisfiable by a builder with unexported control.
	if mode != EmitModeStoreSupport {
		line(sb, 1, "BeginEntity() Ent")
		if nkCol := planNaturalKeyCol(plan); nkCol != nil {
			linef(sb, 1, "SetId(id %s, naturalKey %s) Ent", idCol.GoType(), nkCol.GoType())
		} else {
			linef(sb, 1, "SetId(id %s) Ent", idCol.GoType())
		}
		if tsCol := planTsCol(plan); tsCol != nil {
			linef(sb, 1, "SetTimestamp(ts %s) Ent", tsCol.GoType())
		}
		if lcCol := planExpiresAtCol(plan); lcCol != nil {
			linef(sb, 1, "SetLifecycle(expiresAt %s) Ent", lcCol.GoType())
		}
	}
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "GetSection%s() %sSec", method, method)
	}
	if mode != EmitModeStoreSupport {
		line(sb, 1, "CommitEntity() (err error)")
	}
	line(sb, 0, "}\n")
	return
}

// writeBuildEntitiesFunc emits the generic function that loops the
// SoA columns and drives the entity-builder calls. Schema-free — every
// call routes through the derived interfaces; flag-driven per-field
// dispatch picks BeginAttribute / BeginAttributeSingle / container /
// explode pattern.
func writeBuildEntitiesFunc(sb *strings.Builder, plan *mappingplan.Plan, groups []goplan.SectionGroup) (err error) {
	kind := plan.KindType

	linef(sb, 0, "// %sBuildEntities walks c row-by-row, drives dml's entity / section", kind)
	line(sb, 0, "// chain, and returns once every row has been committed. The dml")
	line(sb, 0, "// argument's concrete type binds every type parameter via Go's")
	line(sb, 0, "// type inference at the call site.")
	linef(sb, 0, "func %sBuildEntities[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttr %s%sAttrI,", method, kind, method)
		linef(sb, 1, "%sSec %s%sSecI[%sAttr, Ent],", method, kind, method, method)
	}
	line(sb, 1, "Ent any,")
	linef(sb, 1, "DML %sEntityI[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 2, "%sAttr, %sSec,", method, method)
	}
	line(sb, 2, "Ent,")
	line(sb, 1, "],")
	linef(sb, 0, "](dml DML, c *%sColumns) (err error) {", kind)
	line(sb, 1, "n := c.Len()")
	line(sb, 1, "for i := 0; i < n; i++ {")

	idCol := planIdCol(plan)
	nkCol := planNaturalKeyCol(plan)
	tsCol := planTsCol(plan)
	lcCol := planExpiresAtCol(plan)
	if idCol == nil {
		err = eb.Build().Errorf("plain spec missing required `id` column")
		return
	}
	line(sb, 2, "dml.BeginEntity()")
	if nkCol != nil {
		linef(sb, 2, "dml.SetId(c.%s[i], c.%s[i])", idCol.GoField, nkCol.GoField)
	} else {
		linef(sb, 2, "dml.SetId(c.%s[i])", idCol.GoField)
	}
	if tsCol != nil {
		linef(sb, 2, "dml.SetTimestamp(c.%s[i])", tsCol.GoField)
	}
	if lcCol != nil {
		linef(sb, 2, "dml.SetLifecycle(c.%s[i])", lcCol.GoField)
	}

	for _, g := range groups {
		err = writeSectionDriver(sb, g, soaValueSrc())
		if err != nil {
			return
		}
	}

	line(sb, 2, "err = dml.CommitEntity()")
	line(sb, 2, "if err != nil {")
	line(sb, 3, "err = eh.Errorf(\"commit row %d: %w\", i, err)")
	line(sb, 3, "return")
	line(sb, 2, "}")
	line(sb, 1, "}")
	line(sb, 1, "return")
	line(sb, 0, "}\n")
	return
}

// writeAddSectionsFunc emits the entity-frame-free variant of
// BuildEntities (ADR-0100 SD6): the same section drivers over one row
// value, without BeginEntity / plain setters / CommitEntity. A composer
// that owns the entity frame (e.g. a recordstore builder assembling one
// entity from several components) calls it between BeginEntity and
// CommitEntity; sections from several kinds stack on one row the way
// marshallreflect's RowComposer stacks DTOs (ADR-0070).
func writeAddSectionsFunc(sb *strings.Builder, plan *mappingplan.Plan, groups []goplan.SectionGroup, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)

	linef(sb, 0, "// %sAddSections contributes this kind's tagged sections to the OPEN", kind)
	line(sb, 0, "// entity on dml — the BuildEntities body without the entity frame.")
	line(sb, 0, "// The caller owns BeginEntity / plain setters / CommitEntity.")
	linef(sb, 0, "func %sAddSections[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttr %s%sAttrI,", method, kind, method)
		linef(sb, 1, "%sSec %s%sSecI[%sAttr, Ent],", method, kind, method, method)
	}
	line(sb, 1, "Ent any,")
	linef(sb, 1, "DML %sEntityI[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 2, "%sAttr, %sSec,", method, method)
	}
	line(sb, 2, "Ent,")
	line(sb, 1, "],")
	linef(sb, 0, "](dml DML, row %s) (err error) {", plan.KindType)

	for _, g := range groups {
		err = writeSectionDriver(sb, g, rowValueSrc())
		if err != nil {
			return
		}
	}

	line(sb, 1, "return")
	line(sb, 0, "}\n")
	return
}

// valueSrc renders access to a kind's field values in emitted driver code.
// BuildEntities reads the SoA columns at row i (`c.X[i]`, options split
// into `c.XVal[i]` / `c.XHas[i]`); AddSections reads a single row value
// (`row.X`, options nested as `row.X.Val` / `row.X.Has`).
type valueSrc struct {
	field     func(goField string) string
	optionVal func(goField string) string
	optionHas func(goField string) string
	// rowErrCtx is the eb context fragment naming the row in error
	// messages — `.Int("row", i)` for the SoA loop, empty for row shape.
	rowErrCtx string
}

func soaValueSrc() valueSrc {
	return valueSrc{
		field:     func(goField string) string { return "c." + goField + "[i]" },
		optionVal: func(goField string) string { return "c." + goField + "Val[i]" },
		optionHas: func(goField string) string { return "c." + goField + "Has[i]" },
		rowErrCtx: `.Int("row", i)`,
	}
}

func rowValueSrc() valueSrc {
	return valueSrc{
		field:     func(goField string) string { return "row." + goField },
		optionVal: func(goField string) string { return "row." + goField + ".Val" },
		optionHas: func(goField string) string { return "row." + goField + ".Has" },
	}
}

func writeSectionDriver(sb *strings.Builder, g goplan.SectionGroup, src valueSrc) (err error) {
	method := methodFor(g.Section)
	secVar := lowerFirst(method) + "Sec"
	linef(sb, 2, "// --- %s. ---", g.Section)
	linef(sb, 2, "%s := dml.GetSection%s()", secVar, method)

	if ts, ok := g.TupleSpec(); ok {
		writeTupleSectionDriver(sb, g, ts, secVar, src)
		linef(sb, 2, "%s.EndSection()", secVar)
		return
	}
	if len(g.SubColumns) > 1 {
		err = writeMultiSubColumnDriver(sb, g, secVar, src)
		if err != nil {
			return
		}
		linef(sb, 2, "%s.EndSection()", secVar)
		return
	}
	for _, f := range g.SubColumns[0].Fields {
		err = writeFieldDriver(sb, f, secVar, src)
		if err != nil {
			return
		}
	}
	linef(sb, 2, "%s.EndSection()", secVar)
	return
}

// writeTupleSectionDriver emits the write driver for a tuple-family section —
// a dynamic-membership tuple (ADR-0103/0109) or a nested static-membership
// section (Slice A/C). One attribute per element the row contributes:
// BeginAttribute(<scalar sub-columns…>), the zipped co-containers, the
// membership(s), then EndAttributeP. The per-element sequence mirrors
// marshallreflect.marshalTupleSection exactly (the byte-identity invariant).
// Two axes generalise the original Many/dynamic tuple:
//
//   - Cardinality (ts.Cardinality): Many → each slice element; One → the struct
//     value once; Optional → the present-gated option.Option[S] value. A One /
//     Optional all-container element whose containers are all empty splices to
//     zero attributes (the S=0 rule), matching the flat multi-sub-column driver.
//   - Membership source: a DYNAMIC tuple (ts.Memberships non-empty) emits one
//     AddMembership<Channel>P per `@membership` field (one per element for a
//     repeated field). A STATIC nested section (ts.Memberships empty) resolves
//     its one section membership through writeMembershipAdd — the ref lookup
//     symbol / verbatim literal — exactly like a flat section.
func writeTupleSectionDriver(sb *strings.Builder, g goplan.SectionGroup, ts goplan.TupleSpec, secVar string, src valueSrc) {
	scalars := g.ScalarSubColumns()
	containers := g.ContainerSubColumns()
	elemVar := secVar + "Elem"
	attrVar := secVar + "Attr"
	// Element fields are reached through the loop / block variable; reusing the
	// valueSrc contract lets the scalar / blob render helpers apply unchanged.
	// Options cannot occur inside a tuple element (PlanBuilder).
	elemSrc := valueSrc{
		field:     func(goField string) string { return elemVar + "." + goField },
		rowErrCtx: src.rowErrCtx,
	}

	// Enumerate the attribute element(s) by cardinality, binding elemVar; the
	// per-element body runs at depth 3.
	switch ts.Cardinality {
	case mappingplan.AttrCardinalityOne:
		line(sb, 2, "{")
		linef(sb, 3, "%s := %s", elemVar, src.field(ts.GoField))
	case mappingplan.AttrCardinalityOptional:
		linef(sb, 2, "if %s {", src.optionHas(ts.GoField))
		linef(sb, 3, "%s := %s", elemVar, src.optionVal(ts.GoField))
	default: // Many
		linef(sb, 2, "for _, %s := range %s {", elemVar, src.field(ts.GoField))
	}

	if len(containers) > 1 {
		for _, sc := range containers[1:] {
			linef(sb, 3, "if len(%s) != len(%s) {", elemSrc.field(sc.Fields[0].GoFieldName), elemSrc.field(containers[0].Fields[0].GoFieldName))
			linef(sb, 4, "err = eb.Build()%s.Str(\"section\", %q).Str(\"field\", %q).Errorf(\"co-container slices have different lengths\")", src.rowErrCtx, g.Section, sc.Fields[0].GoFieldName)
			line(sb, 4, "return")
			line(sb, 3, "}")
		}
	}

	// S=0 splice (H2): a One / Optional all-container element with every
	// container empty emits no attribute. A Many element always emits.
	depth := 3
	if ts.Cardinality != mappingplan.AttrCardinalityMany && len(scalars) == 0 && len(containers) > 0 {
		linef(sb, 3, "if len(%s) > 0 {", elemSrc.field(containers[0].Fields[0].GoFieldName))
		depth = 4
	}

	args := make([]string, 0, len(scalars))
	for _, sc := range scalars {
		args = append(args, scalarValueExpr(sc.Fields[0], elemSrc))
	}
	linef(sb, depth, "%s := %s.BeginAttribute(%s)", attrVar, secVar, strings.Join(args, ", "))
	if len(containers) > 0 {
		elems := make([]string, 0, len(containers))
		for _, sc := range containers {
			f := sc.Fields[0]
			elems = append(elems, sliceElemExpr(f, elemSrc.field(f.GoFieldName)+"[k]"))
		}
		linef(sb, depth, "for k := range %s {", elemSrc.field(containers[0].Fields[0].GoFieldName))
		linef(sb, depth+1, "%s.%sP(%s)", attrVar, goplan.ContainerAddMethod(len(containers)), strings.Join(elems, ", "))
		line(sb, depth, "}")
	}
	if len(ts.Memberships) == 0 {
		writeMembershipAdd(sb, strings.Repeat("\t", depth), attrVar, g.Memberships[0], elemSrc)
	} else {
		for _, m := range ts.Memberships {
			suffix := m.Channel.AddMethodSuffix()
			if m.IsSlice {
				linef(sb, depth, "for _, mv := range %s {", elemSrc.field(m.GoField))
				linef(sb, depth+1, "%s.AddMembership%sP(%s)", attrVar, suffix, tupleMembExpr("mv", m))
				line(sb, depth, "}")
			} else {
				linef(sb, depth, "%s.AddMembership%sP(%s)", attrVar, suffix, tupleMembExpr(elemSrc.field(m.GoField), m))
			}
		}
	}
	linef(sb, depth, "%s.EndAttributeP()", attrVar)
	if depth == 4 {
		line(sb, 3, "}")
	}
	line(sb, 2, "}")
}

// tupleMembExpr renders the AddMembership<Channel>P argument from a tuple
// element's membership value expression (a field access, or a slice-loop var):
// `[]byte(x)` for a verbatim string field, the expression as-is for a []byte or
// a ref uint64 field.
func tupleMembExpr(valExpr string, m mappingplan.TupleMembership) string {
	if m.Channel.EmbedsLiteralName() {
		// verbatim: []byte(name). A string field or an lw.Verbatim newtype both
		// convert directly; a []byte field passes as-is.
		if m.GoType == "string" {
			return "[]byte(" + valExpr + ")"
		}
		return valExpr
	}
	// ref: the uint64 id. An lw.Ref marker newtype needs an explicit conversion
	// to the DML method's plain uint64 parameter.
	if m.MarkerGoType != "" {
		return "uint64(" + valExpr + ")"
	}
	return valExpr
}

func writeMultiSubColumnDriver(sb *strings.Builder, g goplan.SectionGroup, secVar string, src valueSrc) (err error) {
	if len(g.Memberships) != 1 {
		err = eb.Build().Str("section", g.Section).Errorf("multi-sub-column section with multiple memberships not supported")
		return
	}
	scalars := g.ScalarSubColumns()
	containers := g.ContainerSubColumns()
	args := make([]string, 0, len(scalars))
	for _, sc := range scalars {
		args = append(args, src.field(sc.Fields[0].GoFieldName))
	}
	memb := g.Memberships[0]

	// Zip-length agreement across the container class (ADR-0101 D2): all
	// container sub-columns advance in lockstep through one
	// AddTo(Co)Container(s)P call per element, so unequal lengths are a
	// caller bug surfaced as an error, never silent truncation.
	if len(containers) > 1 {
		for _, sc := range containers[1:] {
			linef(sb, 2, "if len(%s) != len(%s) {", src.field(sc.Fields[0].GoFieldName), src.field(containers[0].Fields[0].GoFieldName))
			linef(sb, 3, "err = eb.Build()%s.Str(\"section\", %q).Str(\"field\", %q).Errorf(\"co-container slices have different lengths\")", src.rowErrCtx, g.Section, sc.Fields[0].GoFieldName)
			line(sb, 3, "return")
			line(sb, 2, "}")
		}
	}

	// S = 0 splice: an all-container tuple with every container empty emits
	// no attribute — the lone-container splice rule generalised. With S ≥ 1
	// the scalar tuple is the presence signal and the attribute always
	// emits, containers possibly empty (ADR-0101 D2).
	depth := 2
	if len(scalars) == 0 && len(containers) > 0 {
		linef(sb, 2, "if len(%s) > 0 {", src.field(containers[0].Fields[0].GoFieldName))
		depth = 3
	}
	indent := strings.Repeat("\t", depth)
	linef(sb, depth, "%sAttr := %s.BeginAttribute(%s)", secVar, secVar, strings.Join(args, ", "))
	if len(containers) > 0 {
		elems := make([]string, 0, len(containers))
		for _, sc := range containers {
			f := sc.Fields[0]
			elems = append(elems, sliceElemExpr(f, src.field(f.GoFieldName)+"[k]"))
		}
		linef(sb, depth, "for k := range %s {", src.field(containers[0].Fields[0].GoFieldName))
		linef(sb, depth+1, "%sAttr.%sP(%s)", secVar, goplan.ContainerAddMethod(len(containers)), strings.Join(elems, ", "))
		linef(sb, depth, "}")
	}
	writeMembershipAdd(sb, indent, secVar+"Attr", memb, src)
	linef(sb, depth, "%sAttr.EndAttributeP()", secVar)
	if depth == 3 {
		line(sb, 2, "}")
	}
	return
}

// writeMembershipAdd emits the per-attribute membership push, choosing the
// AddMembership<Channel>P method per ADR-0008 D3. Ref channels push the
// lookup-resolved kindXxx symbol; Verbatim channels push the literal lw: name
// as []byte; carrier (mixed / parametrized) channels read the per-row
// membership data from the sibling carrier column. carrierIdx is "" for a
// scalar carrier (`c.<C>[i]`) and the explode loop variable (e.g. "k") for a
// slice carrier paired element-wise with an exploded value (`c.<C>[i][k]`).
func writeMembershipAdd(sb *strings.Builder, indent, attrVar string, f mappingplan.TaggedField, src valueSrc) {
	ch := f.Flags.Channel
	method := "AddMembership" + ch.AddMethodSuffix() + "P"
	if ch.UsesCarrier() {
		// Cut-2: per-row membership data from the sibling carrier column —
		// one scalar carrier per attribute. Mixed channels pass (value field
		// Id/Name, Params); parametrized channels pass (Params) only. The
		// method suffix selects the channel.
		carrier := src.field(f.CarrierField)
		if vf := ch.CarrierValueField(); vf != "" {
			linef(sb, 0, "%s%s.%s(%s.%s, %s.Params)", indent, attrVar, method, carrier, vf, carrier)
		} else {
			linef(sb, 0, "%s%s.%s(%s.Params)", indent, attrVar, method, carrier)
		}
		return
	}
	if ch.EmbedsLiteralName() {
		linef(sb, 0, "%s%s.%s([]byte(%q))", indent, attrVar, method, f.LWMembership)
		return
	}
	linef(sb, 0, "%s%s.%s(%s)", indent, attrVar, method, f.KindVar())
}

// writeFieldDriver emits the per-field BuildEntities lines for one
// field of a single-sub-column section. Flag-driven; never inspects
// section name. Const fields (IsConst) emit a literal-valued attribute
// per row instead of reading from a Go-side slot.
func writeFieldDriver(sb *strings.Builder, f mappingplan.TaggedField, secVar string, src valueSrc) (err error) {
	tag := f.GoFieldName
	if tag == "" {
		tag = mappingplan.UpperFirst(f.LWMembership) // const fields have no Go name
	}
	attrVar := secVar + "Attr_" + tag
	shape := goplan.ClassifyBegin(f)

	switch shape {
	case goplan.ShapeScalarBegin:
		valExpr := scalarValueExpr(f, src)
		if f.IsOption {
			linef(sb, 2, "if %s {", src.optionHas(f.GoFieldName))
			linef(sb, 3, "%s := %s.BeginAttribute(%s)", attrVar, secVar, valExpr)
			writeMembershipAdd(sb, "\t\t\t", attrVar, f, src)
			linef(sb, 3, "%s.EndAttributeP()", attrVar)
			line(sb, 2, "}")
			return
		}
		linef(sb, 2, "%s := %s.BeginAttribute(%s)", attrVar, secVar, valExpr)
		writeMembershipAdd(sb, "\t\t", attrVar, f, src)
		linef(sb, 2, "%s.EndAttributeP()", attrVar)

	case goplan.ShapeScalarBeginSingle:
		valExpr := scalarValueExpr(f, src)
		if f.IsOption {
			linef(sb, 2, "if %s {", src.optionHas(f.GoFieldName))
			linef(sb, 3, "%s := %s.BeginAttributeSingle(%s)", attrVar, secVar, valExpr)
			writeMembershipAdd(sb, "\t\t\t", attrVar, f, src)
			linef(sb, 3, "%s.EndAttributeP()", attrVar)
			line(sb, 2, "}")
			return
		}
		linef(sb, 2, "%s := %s.BeginAttributeSingle(%s)", attrVar, secVar, valExpr)
		writeMembershipAdd(sb, "\t\t", attrVar, f, src)
		linef(sb, 2, "%s.EndAttributeP()", attrVar)

	case goplan.ShapeContainer:
		// 1 attribute, N values via AddToContainerP, 1 carrier (if any). Empty
		// / nil skips (leeway splice semantics: empty non-scalars vanish — the
		// carrier of an empty container is therefore not emitted).
		switch {
		case f.IsRoaring():
			linef(sb, 2, "if %s != nil && !%s.IsEmpty() {", src.field(f.GoFieldName), src.field(f.GoFieldName))
			linef(sb, 3, "%s := %s.BeginAttribute()", attrVar, secVar)
			linef(sb, 3, "it := %s.Iterator()", src.field(f.GoFieldName))
			line(sb, 3, "for it.HasNext() {")
			linef(sb, 4, "%s.AddToContainerP(it.Next())", attrVar)
			line(sb, 3, "}")
			writeMembershipAdd(sb, "\t\t\t", attrVar, f, src)
			linef(sb, 3, "%s.EndAttributeP()", attrVar)
			line(sb, 2, "}")
		case f.IsSlice():
			linef(sb, 2, "if len(%s) > 0 {", src.field(f.GoFieldName))
			linef(sb, 3, "%s := %s.BeginAttribute()", attrVar, secVar)
			linef(sb, 3, "for _, v := range %s {", src.field(f.GoFieldName))
			linef(sb, 4, "%s.AddToContainerP(%s)", attrVar, sliceElemExpr(f, "v"))
			line(sb, 3, "}")
			writeMembershipAdd(sb, "\t\t\t", attrVar, f, src)
			linef(sb, 3, "%s.EndAttributeP()", attrVar)
			line(sb, 2, "}")
		default:
			err = eb.Build().Str("field", f.GoFieldName).Errorf("container shape on non-slice / non-roaring field — should have been caught by parser")
		}
	}
	return
}

// scalarValueExpr renders the BeginAttribute(value) argument for a
// scalar / Option field. For Option fields, the Has guard is emitted
// separately; this returns the raw value access. For const fields,
// returns the constant's Go literal (always a quoted string).
func scalarValueExpr(f mappingplan.TaggedField, src valueSrc) string {
	if f.IsConst {
		return fmt.Sprintf("%q", f.ConstValue)
	}
	if f.IsOption {
		return blobSliceMaybe(f, src.optionVal(f.GoFieldName))
	}
	return blobSliceMaybe(f, src.field(f.GoFieldName))
}

// sliceElemExpr renders the per-element expression inside a container
// loop. Re-slices fixed-width byte arrays so the AttrI's
// AddToContainerP / SecI's BeginAttribute (which take []byte for blob
// sections) accepts them.
func sliceElemExpr(f mappingplan.TaggedField, elemVar string) string {
	if goplan.IsFixedByteArray(f.GoType()) {
		return elemVar + "[:]"
	}
	return elemVar
}

// blobSliceMaybe re-slices a fixed-width byte array Go value into the
// []byte the blob-section BeginAttribute expects. No-op for any other
// type.
func blobSliceMaybe(f mappingplan.TaggedField, base string) string {
	if goplan.IsFixedByteArray(f.GoType()) {
		return base + "[:]"
	}
	return base
}

// --- FillFromArrow core + derived read interfaces. ---

func writeFillHelper(sb *strings.Builder, plan *mappingplan.Plan) (err error) {
	groups := goplan.ComputeGroups(plan)

	line(sb, 0, "// --- Composed-interface FillFromArrow helper (schema-agnostic). ---\n//")
	linef(sb, 0, "// %sFillFromArrow walks the Arrow record row-by-row and appends", plan.KindType)
	line(sb, 0, "// each entity's plain + tagged-section values into c. Plain columns")
	line(sb, 0, "// enter as concrete Arrow accessors (uniform across schemas);")
	line(sb, 0, "// per-section Attrs + Membs bind through type-parameter interfaces.")
	blank(sb)

	for _, g := range groups {
		err = writeSectionReadInterfaces(sb, plan, g, EmitModeCodec)
		if err != nil {
			return
		}
	}
	err = writeFillFromArrowFunc(sb, plan, groups)
	return
}

// argPair is a parsed (name, type) pair, used for multi-sub-column
// section read interfaces (one accessor per sub-column).
type argPair struct{ Name, Type string }

func writeSectionReadInterfaces(sb *strings.Builder, plan *mappingplan.Plan, g goplan.SectionGroup, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)
	method := methodFor(g.Section)

	// Classify which read-side accessors this DTO's fields require for
	// this section. Section is scalar (only GetAttrValueValue T) iff
	// every field uses a scalar-section write shape (no Unit, no
	// container); non-scalar otherwise (GetAttrValueValue iter.Seq[T]
	// for container shapes, GetAttrValueSingleOrDefault T for Unit
	// single-value shapes).
	var hasScalarValue, hasSingleVal, hasIterVal bool
	for _, sc := range g.SubColumns {
		for _, f := range sc.Fields {
			switch goplan.ClassifyBegin(f) {
			case goplan.ShapeScalarBegin:
				hasScalarValue = true
			case goplan.ShapeScalarBeginSingle:
				hasSingleVal = true
			case goplan.ShapeContainer:
				hasIterVal = true
			}
		}
	}
	if len(g.SubColumns) == 1 && hasScalarValue && (hasSingleVal || hasIterVal) {
		// Single-sub-column only: two field shapes would contend for one
		// GetAttrValueValue signature. A multi-sub-column section reads each
		// sub-column through its own accessor, so mixed shapes are fine
		// there (ADR-0101 D5).
		err = eb.Build().Str("section", g.Section).Errorf("section mixes scalar-section field shape with non-scalar-section field shape — disambiguate via flags so the read API resolves to one method set")
		return
	}

	linef(sb, 0, "// %s%sAttrsReadI is the Attributes-side view of the %s section.", kind, method, g.Section)
	linef(sb, 0, "type %s%sAttrsReadI interface {", kind, method)
	// A tuple section reads every sub-column through its own named
	// accessor at any sub-column count (its decode addresses columns by
	// name, and a lone sub-column may not be named "value") — ADR-0103.
	_, isTuple := g.TupleSpec()
	if isTuple || len(g.SubColumns) > 1 {
		// Per-sub-column accessor, each shaped to its own subtype: scalar
		// sub-columns read the value directly, container sub-columns drain
		// an iter.Seq (the RA generator emits exactly this pair of shapes).
		for _, sc := range g.SubColumns {
			f := sc.Fields[0]
			if f.IsSlice() {
				linef(sb, 1, "GetAttrValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[%s]", mappingplan.UpperFirst(sc.Name), elemType(f))
			} else {
				linef(sb, 1, "GetAttrValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) %s", mappingplan.UpperFirst(sc.Name), f.GoType())
			}
		}
	} else {
		f := g.SubColumns[0].Fields[0]
		vt := elemType(f)
		switch {
		case hasScalarValue:
			// Scalar section — GetAttrValueValue returns the value directly.
			linef(sb, 1, "GetAttrValueValue(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) %s", vt)
		default:
			// Non-scalar section — expose what the fields actually use.
			if hasIterVal {
				linef(sb, 1, "GetAttrValueValue(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[%s]", vt)
			}
			if hasSingleVal {
				linef(sb, 1, "GetAttrValueSingleOrDefault(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) %s", vt)
			}
		}
	}
	line(sb, 1, "GetNumberOfAttributes(entityIdx raruntime.EntityIdx) int64")
	line(sb, 0, "}\n")

	linef(sb, 0, "// %s%sMembsReadI is the Memberships-side view of the %s section.", kind, method, g.Section)
	linef(sb, 0, "type %s%sMembsReadI interface {", kind, method)
	if ts, ok := g.TupleSpec(); ok {
		// A tuple element may read memberships on several channels (ADR-0109
		// D4); expose one GetMembValue<Channel> per channel (all simple
		// channels — a plain Seq of the id / name).
		for _, tch := range ts.Channels() {
			linef(sb, 1, "GetMembValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[%s]", tch.AddMethodSuffix(), tch.ReadIterElemType())
		}
		line(sb, 0, "}\n")
		return
	}
	ch := g.Channel()
	switch {
	case ch.UsesCarrier() && ch.CarrierValueField() != "":
		// Mixed channel: the combined Seq2 accessor yields the per-row
		// membership value (id/name) + params together.
		linef(sb, 1, "GetMembValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq2[%s]", ch.CarrierReadMethodSuffix(), ch.CarrierReadSeq2Types())
	case ch.UsesCarrier():
		// Parametrized channel: a single Seq of the opaque params blob.
		linef(sb, 1, "GetMembValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[[]byte]", ch.CarrierReadMethodSuffix())
	default:
		linef(sb, 1, "GetMembValue%s(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[%s]", ch.AddMethodSuffix(), ch.ReadIterElemType())
	}
	line(sb, 0, "}\n")
	return
}

func writeFillFromArrowFunc(sb *strings.Builder, plan *mappingplan.Plan, groups []goplan.SectionGroup) (err error) {
	kind := plan.KindType

	idCol := planIdCol(plan)
	nkCol := planNaturalKeyCol(plan)
	tsCol := planTsCol(plan)
	lcCol := planExpiresAtCol(plan)
	if idCol == nil {
		err = eb.Build().Errorf("plain spec missing required `id` column")
		return
	}

	linef(sb, 0, "// %sFillFromArrow walks rec row-by-row and appends each entity's", kind)
	line(sb, 0, "// plain + tagged-section values into c. Plain columns enter as")
	line(sb, 0, "// concrete Arrow accessors; per-section Attrs + Membs bind through")
	line(sb, 0, "// type-parameter interfaces.")
	if len(groups) == 0 {
		// Plain-only entity (no tagged sections): FillFromArrow is a plain,
		// non-generic func. An empty type-parameter list `[]` is invalid Go,
		// so the `[ … ]` block is emitted only when there is at least one
		// per-section reader type parameter to put in it.
		linef(sb, 0, "func %sFillFromArrow(", kind)
	} else {
		linef(sb, 0, "func %sFillFromArrow[", kind)
		for _, g := range groups {
			method := methodFor(g.Section)
			linef(sb, 1, "%sAttrs %s%sAttrsReadI,", method, kind, method)
			linef(sb, 1, "%sMembs %s%sMembsReadI,", method, kind, method)
		}
		line(sb, 0, "](")
	}
	linef(sb, 1, "c *%sColumns,", kind)
	line(sb, 1, "n int,")
	linef(sb, 1, "idCol %s,", plainArrowParam(idCol))
	if nkCol != nil {
		linef(sb, 1, "nkCol %s,", plainArrowParam(nkCol))
	}
	if tsCol != nil {
		linef(sb, 1, "tsCol %s,", plainArrowParam(tsCol))
	}
	if lcCol != nil {
		linef(sb, 1, "lcCol %s,", plainArrowParam(lcCol))
	}
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttrs %sAttrs,", lowerFirst(method), method)
		linef(sb, 1, "%sMembs %sMembs,", lowerFirst(method), method)
	}
	line(sb, 0, ") (err error) {")
	line(sb, 1, "for i := 0; i < n; i++ {")

	writePlainRead(sb, 2, idCol, "idCol")
	if nkCol != nil {
		writePlainRead(sb, 2, nkCol, "nkCol")
	}
	if tsCol != nil {
		writePlainRead(sb, 2, tsCol, "tsCol")
	}
	if lcCol != nil {
		writePlainRead(sb, 2, lcCol, "lcCol")
	}

	for _, g := range groups {
		err = writeSectionDecode(sb, g)
		if err != nil {
			return
		}
	}

	line(sb, 1, "}")
	line(sb, 1, "return\n}\n")
	return
}

func writeSectionDecode(sb *strings.Builder, g goplan.SectionGroup) (err error) {
	method := methodFor(g.Section)
	attrsVar := lowerFirst(method) + "Attrs"
	membsVar := lowerFirst(method) + "Membs"
	prefix := lowerFirst(method)

	linef(sb, 2, "// --- %s. ---", g.Section)

	if ts, ok := g.TupleSpec(); ok {
		writeTupleSectionDecode(sb, g, ts, attrsVar, membsVar, prefix)
		return
	}

	if len(g.SubColumns) > 1 {
		return writeMultiSubColumnDecode(sb, g, attrsVar, membsVar, prefix)
	}

	if g.Channel().UsesCarrier() {
		return writeCarrierSectionDecode(sb, g, attrsVar, membsVar, prefix)
	}

	fields := writeSectionMatchLoops(sb, g, attrsVar, membsVar, prefix)
	for _, f := range fields {
		writeFieldAppend(sb, f, prefix)
	}
	return
}

// writeTupleSectionDecode emits the FillFromArrow decode of a
// dynamic-membership tuple section (ADR-0103, extended by ADR-0109). Every
// attribute of the row belongs to the tuple field (PlanBuilder.Finish
// guarantees the section carries no other field), so there is no membership
// match: each attribute decodes to ONE element — its sub-column values read
// positionally and its memberships distributed to the element's `@membership`
// fields per channel (fixed fields positional, a repeated field taking the
// whole channel Seq). Zero attributes decode to a nil element slice. Mirrors
// marshallreflect.unmarshalTupleSection.
func writeTupleSectionDecode(sb *strings.Builder, g goplan.SectionGroup, ts goplan.TupleSpec, attrsVar, membsVar, prefix string) {
	elemsVar := prefix + ts.GoField + "Elems"
	linef(sb, 2, "var %s []%s", elemsVar, ts.StructType)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	for _, sc := range g.SubColumns {
		f := sc.Fields[0]
		localVar := prefix + f.GoFieldName + "Local"
		accessor := fmt.Sprintf("%s.GetAttrValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", attrsVar, mappingplan.UpperFirst(sc.Name))
		if !f.IsSlice() {
			// Scalar sub-column: read with the field's copy strategy — the
			// element is retained inside the tuple slice, so a []byte value
			// must not alias the reused Arrow buffer.
			linef(sb, 3, "var %s %s", localVar, f.GoType())
			writeCarrierValueRead(sb, 3, f, localVar, accessor)
			continue
		}
		// Container sub-column: drain the per-attribute Seq into a fresh
		// slice (nil for an empty container — an N = 0 attribute reads
		// back as a nil slice, ADR-0101 D5).
		linef(sb, 3, "var %s []%s", localVar, f.GoType())
		linef(sb, 3, "for v := range %s {", accessor)
		if goplan.CopyStrategy(f.GoType()) == goplan.CopyBytes {
			line(sb, 4, "cp := make([]byte, len(v))")
			line(sb, 4, "copy(cp, v)")
			linef(sb, 4, "%s = append(%s, cp)", localVar, localVar)
		} else {
			linef(sb, 4, "%s = append(%s, v)", localVar, localVar)
		}
		line(sb, 3, "}")
	}

	// Memberships, one channel at a time (ADR-0109 D3). membExpr[goField] is the
	// element-literal expression for each `@membership` field.
	membExpr := map[string]string{}
	for _, ch := range ts.Channels() {
		suffix := ch.AddMethodSuffix()
		accessor := fmt.Sprintf("%s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", membsVar, suffix)
		var chFields []mappingplan.TupleMembership
		for _, m := range ts.Memberships {
			if m.Channel == ch {
				chFields = append(chFields, m)
			}
		}
		if len(chFields) == 1 && chFields[0].IsSlice {
			// Slice-mode: the sole field on this channel takes the whole Seq.
			f := chFields[0]
			sliceVar := prefix + f.GoField + "Membs"
			linef(sb, 3, "var %s []%s", sliceVar, membElemGoType(f))
			linef(sb, 3, "for mv := range %s {", accessor)
			linef(sb, 4, "%s = append(%s, %s)", sliceVar, sliceVar, tupleMembDecodeElem("mv", f))
			line(sb, 3, "}")
			membExpr[f.GoField] = sliceVar
			continue
		}
		// Fixed-mode: collect the channel's values, require the exact count, then
		// index one per field in declaration order.
		rawVar := prefix + suffix + "Membs"
		wireElem := "uint64"
		if ch.EmbedsLiteralName() {
			wireElem = "[]byte"
		}
		linef(sb, 3, "var %s []%s", rawVar, wireElem)
		linef(sb, 3, "for mv := range %s {", accessor)
		linef(sb, 4, "%s = append(%s, mv)", rawVar, rawVar)
		line(sb, 3, "}")
		linef(sb, 3, "if len(%s) != %d {", rawVar, len(chFields))
		linef(sb, 4, `err = eb.Build().Str("section", %q).Str("channel", %q).Int("got", len(%s)).Int("want", %d).Errorf("membership count mismatch on read")`, g.Section, suffix, rawVar, len(chFields))
		line(sb, 4, "return")
		line(sb, 3, "}")
		for idx, f := range chFields {
			membExpr[f.GoField] = tupleMembDecodeElem(fmt.Sprintf("%s[%d]", rawVar, idx), f)
		}
	}

	linef(sb, 3, "%s = append(%s, %s{", elemsVar, elemsVar, ts.StructType)
	for _, m := range ts.Memberships {
		linef(sb, 4, "%s: %s,", m.GoField, membExpr[m.GoField])
	}
	for _, sc := range g.SubColumns {
		f := sc.Fields[0]
		linef(sb, 4, "%s: %s%sLocal,", f.GoFieldName, prefix, f.GoFieldName)
	}
	line(sb, 3, "})")
	line(sb, 2, "}")

	// Project the decoded elements onto the SoA column by cardinality (mirrors
	// marshallreflect.unmarshalTupleSection). One: exactly one attribute per row
	// (zero only when the section is all-container and spliced away); Optional:
	// at most one (Val/Has); Many: the whole element slice (nil when empty).
	switch ts.Cardinality {
	case mappingplan.AttrCardinalityOne:
		cond := fmt.Sprintf("len(%s) != 1", elemsVar)
		if len(g.ScalarSubColumns()) == 0 {
			cond = fmt.Sprintf("len(%s) > 1", elemsVar) // all-container: 0 or 1
		}
		linef(sb, 2, "if %s {", cond)
		linef(sb, 3, `err = eb.Build().Str("section", %q).Int("attrs", len(%s)).Errorf("cardinality-One nested section must carry exactly one attribute per row")`, g.Section, elemsVar)
		line(sb, 3, "return")
		line(sb, 2, "}")
		oneVar := prefix + ts.GoField + "One"
		linef(sb, 2, "var %s %s", oneVar, ts.StructType)
		linef(sb, 2, "if len(%s) == 1 {", elemsVar)
		linef(sb, 3, "%s = %s[0]", oneVar, elemsVar)
		line(sb, 2, "}")
		linef(sb, 2, "c.%s = append(c.%s, %s)", ts.GoField, ts.GoField, oneVar)
	case mappingplan.AttrCardinalityOptional:
		linef(sb, 2, "if len(%s) > 1 {", elemsVar)
		linef(sb, 3, `err = eb.Build().Str("section", %q).Int("attrs", len(%s)).Errorf("Optional nested section must carry at most one attribute per row")`, g.Section, elemsVar)
		line(sb, 3, "return")
		line(sb, 2, "}")
		valVar := prefix + ts.GoField + "Val"
		hasVar := prefix + ts.GoField + "Has"
		linef(sb, 2, "var %s %s", valVar, ts.StructType)
		linef(sb, 2, "%s := len(%s) == 1", hasVar, elemsVar)
		linef(sb, 2, "if %s {", hasVar)
		linef(sb, 3, "%s = %s[0]", valVar, elemsVar)
		line(sb, 2, "}")
		linef(sb, 2, "c.%sVal = append(c.%sVal, %s)", ts.GoField, ts.GoField, valVar)
		linef(sb, 2, "c.%sHas = append(c.%sHas, %s)", ts.GoField, ts.GoField, hasVar)
	default: // Many
		linef(sb, 2, "c.%s = append(c.%s, %s)", ts.GoField, ts.GoField, elemsVar)
	}
}

// tupleMembDecodeElem renders the expression converting one wire membership
// value (a loop var or an indexed raw value) to a tuple element field's element
// type: the uint64 id verbatim for a ref channel; string(x) for a string field;
// a defensive []byte copy for a []byte field (the value aliases the reused Arrow
// buffer and is retained inside the tuple slice).
func tupleMembDecodeElem(src string, m mappingplan.TupleMembership) string {
	var expr string
	switch m.GoType {
	case "uint64":
		expr = src
	case "[]byte":
		expr = "append([]byte(nil), " + src + "...)"
	default: // "string"
		expr = "string(" + src + ")"
	}
	// Wrap in the lw.* marker newtype (lw.Ref(v) / lw.Verbatim(string(v))); a
	// plain @membership field takes the underlying type directly.
	if m.MarkerGoType != "" {
		expr = m.MarkerGoType + "(" + expr + ")"
	}
	return expr
}

// membElemGoType is the Go type of one membership value in a decoded slice: the
// lw.* marker newtype when set (so a `[]lw.Ref` field is declared `[]lw.Ref`),
// else the plain underlying type.
func membElemGoType(m mappingplan.TupleMembership) string {
	if m.MarkerGoType != "" {
		return m.MarkerGoType
	}
	return m.GoType
}

// writeSectionMatchLoops emits the shared middle of a non-carrier
// single-sub-column section decode: per-field accumulator declarations,
// the attribute loop and the membership-match switch filling them. Both
// FillFromArrow (strict, SoA-appending tails) and ReadRow (presence-
// tolerant, row-assigning tails) build on it. Returns the non-const
// fields the caller must finish.
func writeSectionMatchLoops(sb *strings.Builder, g goplan.SectionGroup, attrsVar, membsVar, prefix string) (fields []mappingplan.TaggedField) {
	for _, f := range g.SubColumns[0].Fields {
		if f.IsConst {
			continue
		}
		fields = append(fields, f)
	}
	for _, f := range fields {
		writeFieldAccumulatorDecl(sb, f, prefix)
	}
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	if g.Channel().EmbedsLiteralName() {
		linef(sb, 3, "for membBytes := range %s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, g.Channel().AddMethodSuffix())
		line(sb, 4, "switch string(membBytes) {")
	} else {
		linef(sb, 3, "for membID := range %s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, g.Channel().AddMethodSuffix())
		line(sb, 4, "switch membID {")
	}
	for _, f := range fields {
		writeFieldMembCase(sb, f, prefix, attrsVar)
	}
	line(sb, 4, "}")
	line(sb, 3, "}")
	line(sb, 2, "}")
	return
}

// writeCarrierSectionDecode emits FillFromArrow decode for a mixed /
// parametrized (carrier-channel) section. PlanBuilder guarantees one
// membership — one value+carrier field — so every attribute belongs to it and
// there is no membership-id switch. The value comes from the section value
// accessor; the per-row carrier (id/name + params) comes from the combined
// Seq2 (mixed) or Seq (parametrized) membership accessor. The value field's
// shape selects the decode (ADR-0008 OQ#4): scalar / Option pair a single
// value with a scalar carrier; a container []T pairs N values (one attribute)
// with a scalar carrier; an exploded []T pairs N attributes (one value each)
// with a slice carrier.
func writeCarrierSectionDecode(sb *strings.Builder, g goplan.SectionGroup, attrsVar, membsVar, prefix string) (err error) {
	var f mappingplan.TaggedField
	found := false
	for _, ff := range g.SubColumns[0].Fields {
		if ff.Flags.Channel.UsesCarrier() {
			f = ff
			found = true
			break
		}
	}
	if !found {
		err = eb.Build().Str("section", g.Section).Errorf("carrier section has no value field")
		return
	}

	switch {
	case f.IsSlice():
		writeCarrierContainerDecode(sb, f, attrsVar, membsVar, prefix)
	case f.IsOption:
		writeCarrierOptionDecode(sb, f, attrsVar, membsVar, prefix)
	default:
		writeCarrierScalarDecode(sb, f, attrsVar, membsVar, prefix)
	}
	return
}

// carrierStructLiteral renders the marshalltypes carrier struct literal
// reconstructed from a per-attribute membership read. mvExpr is the
// membership-value loop expression for mixed channels; ignored for
// parametrized channels (whose CarrierValueField is "").
func carrierStructLiteral(f mappingplan.TaggedField, mvExpr, paramsExpr string) string {
	if vf := f.Flags.Channel.CarrierValueField(); vf != "" {
		return fmt.Sprintf("marshalltypes.%s{%s: %s, Params: append([]byte(nil), %s...)}", f.CarrierType, vf, mvExpr, paramsExpr)
	}
	return fmt.Sprintf("marshalltypes.%s{Params: append([]byte(nil), %s...)}", f.CarrierType, paramsExpr)
}

// carrierMembValExpr is the membership-value loop expression for a mixed
// channel ("mv", copied out of the Arrow buffer for verbatim []byte names),
// or "" for a parametrized channel (no separate membership value).
func carrierMembValExpr(f mappingplan.TaggedField) string {
	if f.Flags.Channel.CarrierValueField() == "" {
		return ""
	}
	if f.Flags.Channel.CarrierValueIsBytes() {
		return "append([]byte(nil), mv...)"
	}
	return "mv"
}

// writeCarrierMembLoopHeader emits, at the given depth, the `for … := range
// <membs>.<read>(…) {` line opening a per-attribute membership read. Mixed
// channels range over (mv, params); parametrized over (params) only.
func writeCarrierMembLoopHeader(sb *strings.Builder, depth int, f mappingplan.TaggedField, membsVar, readMethod string) {
	if f.Flags.Channel.CarrierValueField() == "" {
		linef(sb, depth, "for params := range %s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, readMethod)
	} else {
		linef(sb, depth, "for mv, params := range %s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, readMethod)
	}
}

// writeCarrierValueRead emits the read of a single section value into valVar at
// the given depth — a defensive copy for []byte / fixed-byte, straight
// assignment otherwise.
func writeCarrierValueRead(sb *strings.Builder, depth int, f mappingplan.TaggedField, valVar, valRead string) {
	switch goplan.CopyStrategy(f.GoType()) {
	case goplan.CopyFixedByte:
		linef(sb, depth, "copy(%s[:], %s)", valVar, valRead)
	case goplan.CopyBytes:
		linef(sb, depth, "%s = append([]byte(nil), %s...)", valVar, valRead)
	default:
		linef(sb, depth, "%s = %s", valVar, valRead)
	}
}

// writeCarrierScalarDecode decodes a scalar carrier value: exactly one
// attribute per row, value + carrier into scalar columns.
func writeCarrierScalarDecode(sb *strings.Builder, f mappingplan.TaggedField, attrsVar, membsVar, prefix string) {
	valVar := prefix + f.GoFieldName + "Val"
	carrierVar := prefix + f.GoFieldName + "Carrier"
	readMethod := "GetMembValue" + f.Flags.Channel.CarrierReadMethodSuffix()
	valRead := fmt.Sprintf("%s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", attrsVar, goplan.SingleValueReadAccessor(f))

	linef(sb, 2, "var %s %s", valVar, f.GoType())
	linef(sb, 2, "var %s marshalltypes.%s", carrierVar, f.CarrierType)
	linef(sb, 2, "%sCount := 0", prefix)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	writeCarrierMembLoopHeader(sb, 3, f, membsVar, readMethod)
	writeCarrierValueRead(sb, 4, f, valVar, valRead)
	linef(sb, 4, "%s = %s", carrierVar, carrierStructLiteral(f, carrierMembValExpr(f), "params"))
	linef(sb, 4, "%sCount++", prefix)
	line(sb, 3, "}")
	line(sb, 2, "}")
	linef(sb, 2, "if %sCount != 1 {", prefix)
	linef(sb, 3, "err = eb.Build().Int(\"row\", i).Str(\"field\", %q).Errorf(\"expected exactly one occurrence per row\")", f.GoFieldName)
	line(sb, 3, "return")
	line(sb, 2, "}")
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.GoFieldName, f.GoFieldName, valVar)
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.CarrierField, f.CarrierField, carrierVar)
}

// writeCarrierOptionDecode decodes an Option carrier value: 0 or 1 attribute.
// The carrier column gets one entry per row (zero when absent) to stay in
// lockstep with the Val / Has columns.
func writeCarrierOptionDecode(sb *strings.Builder, f mappingplan.TaggedField, attrsVar, membsVar, prefix string) {
	valVar := prefix + f.GoFieldName + "Val"
	carrierVar := prefix + f.GoFieldName + "Carrier"
	readMethod := "GetMembValue" + f.Flags.Channel.CarrierReadMethodSuffix()
	valRead := fmt.Sprintf("%s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", attrsVar, goplan.SingleValueReadAccessor(f))

	linef(sb, 2, "var %s %s", valVar, f.GoType())
	linef(sb, 2, "var %s marshalltypes.%s", carrierVar, f.CarrierType)
	linef(sb, 2, "%sCount := 0", prefix)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	writeCarrierMembLoopHeader(sb, 3, f, membsVar, readMethod)
	writeCarrierValueRead(sb, 4, f, valVar, valRead)
	linef(sb, 4, "%s = %s", carrierVar, carrierStructLiteral(f, carrierMembValExpr(f), "params"))
	linef(sb, 4, "%sCount++", prefix)
	line(sb, 3, "}")
	line(sb, 2, "}")
	linef(sb, 2, "if %sCount == 1 {", prefix)
	linef(sb, 3, "c.%sVal = append(c.%sVal, %s)", f.GoFieldName, f.GoFieldName, valVar)
	linef(sb, 3, "c.%sHas = append(c.%sHas, true)", f.GoFieldName, f.GoFieldName)
	line(sb, 2, "} else {")
	linef(sb, 3, "var zero %s", f.GoType())
	linef(sb, 3, "c.%sVal = append(c.%sVal, zero)", f.GoFieldName, f.GoFieldName)
	linef(sb, 3, "c.%sHas = append(c.%sHas, false)", f.GoFieldName, f.GoFieldName)
	line(sb, 2, "}")
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.CarrierField, f.CarrierField, carrierVar)
}

// writeCarrierContainerDecode decodes a container ([]T) carrier value: one
// attribute carrying N values, paired with a single scalar carrier. An empty
// container produces no attribute (splice) — the row gets an empty slice and a
// zero carrier.
func writeCarrierContainerDecode(sb *strings.Builder, f mappingplan.TaggedField, attrsVar, membsVar, prefix string) {
	sliceVar := prefix + f.GoFieldName + "Slice"
	carrierVar := prefix + f.GoFieldName + "Carrier"
	readMethod := "GetMembValue" + f.Flags.Channel.CarrierReadMethodSuffix()

	linef(sb, 2, "var %s []%s", sliceVar, f.GoType())
	linef(sb, 2, "var %s marshalltypes.%s", carrierVar, f.CarrierType)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	writeCarrierMembLoopHeader(sb, 3, f, membsVar, readMethod)
	linef(sb, 4, "%s = %s", carrierVar, carrierStructLiteral(f, carrierMembValExpr(f), "params"))
	line(sb, 3, "}")
	linef(sb, 3, "for v := range %s.GetAttrValueValue(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", attrsVar)
	if goplan.CopyStrategy(f.GoType()) == goplan.CopyBytes {
		line(sb, 4, "cp := make([]byte, len(v))")
		line(sb, 4, "copy(cp, v)")
		linef(sb, 4, "%s = append(%s, cp)", sliceVar, sliceVar)
	} else {
		linef(sb, 4, "%s = append(%s, v)", sliceVar, sliceVar)
	}
	line(sb, 3, "}")
	line(sb, 2, "}")
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.GoFieldName, f.GoFieldName, sliceVar)
	linef(sb, 2, "c.%s = append(c.%s, %s)", f.CarrierField, f.CarrierField, carrierVar)
}

func writeFieldAccumulatorDecl(sb *strings.Builder, f mappingplan.TaggedField, prefix string) {
	switch {
	case f.IsSlice():
		linef(sb, 2, "var %s%sSlice []%s", prefix, f.GoFieldName, f.GoType())
	case f.IsRoaring():
		linef(sb, 2, "var %s%sBitmap *roaring.Bitmap", prefix, f.GoFieldName)
	default:
		// Scalar or Option value: a Val slot plus an occurrence Count
		// (Option's Has bool is written at append time, not here).
		linef(sb, 2, "var %s%sVal %s", prefix, f.GoFieldName, f.GoType())
		linef(sb, 2, "var %s%sCount int", prefix, f.GoFieldName)
	}
}

func writeFieldAppend(sb *strings.Builder, f mappingplan.TaggedField, prefix string) {
	switch {
	case f.IsOption:
		linef(sb, 2, "if %s%sCount == 1 {", prefix, f.GoFieldName)
		linef(sb, 3, "c.%sVal = append(c.%sVal, %s%sVal)", f.GoFieldName, f.GoFieldName, prefix, f.GoFieldName)
		linef(sb, 3, "c.%sHas = append(c.%sHas, true)", f.GoFieldName, f.GoFieldName)
		line(sb, 2, "} else {")
		linef(sb, 3, "var zero %s", f.GoType())
		linef(sb, 3, "c.%sVal = append(c.%sVal, zero)", f.GoFieldName, f.GoFieldName)
		linef(sb, 3, "c.%sHas = append(c.%sHas, false)", f.GoFieldName, f.GoFieldName)
		line(sb, 2, "}")
	case f.IsSlice():
		linef(sb, 2, "c.%s = append(c.%s, %s%sSlice)", f.GoFieldName, f.GoFieldName, prefix, f.GoFieldName)
	case f.IsRoaring():
		linef(sb, 2, "c.%s = append(c.%s, %s%sBitmap)", f.GoFieldName, f.GoFieldName, prefix, f.GoFieldName)
	default:
		linef(sb, 2, "if %s%sCount != 1 {", prefix, f.GoFieldName)
		linef(sb, 3, "err = eb.Build().Int(\"row\", i).Str(\"field\", %q).Errorf(\"expected exactly one occurrence per row\")", f.GoFieldName)
		line(sb, 3, "return\n\t\t}")
		linef(sb, 2, "c.%s = append(c.%s, %s%sVal)", f.GoFieldName, f.GoFieldName, prefix, f.GoFieldName)
	}
}

func writeFieldMembCase(sb *strings.Builder, f mappingplan.TaggedField, prefix, attrsVar string) {
	if f.Flags.Channel.EmbedsLiteralName() {
		linef(sb, 4, "case %q:", f.LWMembership)
	} else {
		linef(sb, 4, "case %s:", f.KindVar())
	}
	// Single-value read accessor chosen by field shape, shared with the
	// reflect codec via goplan.SingleValueReadAccessor so the two
	// front-ends cannot pick different accessors for the same shape.
	singleVal := func() string {
		return fmt.Sprintf("%s.%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", attrsVar, goplan.SingleValueReadAccessor(f))
	}
	switch {
	case f.IsSlice():
		linef(sb, 5, "for v := range %s.GetAttrValueValue(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", attrsVar)
		if goplan.CopyStrategy(f.GoType()) == goplan.CopyBytes {
			line(sb, 6, "cp := make([]byte, len(v))")
			line(sb, 6, "copy(cp, v)")
			linef(sb, 6, "%s%sSlice = append(%s%sSlice, cp)", prefix, f.GoFieldName, prefix, f.GoFieldName)
		} else {
			linef(sb, 6, "%s%sSlice = append(%s%sSlice, v)", prefix, f.GoFieldName, prefix, f.GoFieldName)
		}
		line(sb, 5, "}")
	case f.IsRoaring():
		linef(sb, 5, "for v := range %s.GetAttrValueValue(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", attrsVar)
		linef(sb, 6, "if %s%sBitmap == nil {", prefix, f.GoFieldName)
		linef(sb, 7, "%s%sBitmap = roaring.New()", prefix, f.GoFieldName)
		line(sb, 6, "}")
		linef(sb, 6, "%s%sBitmap.Add(v)", prefix, f.GoFieldName)
		line(sb, 5, "}")
	default:
		// Scalar or Option value. Option diverges from a bare scalar only at
		// append time (writeFieldAppend writes the Has bool); the
		// per-attribute fill into <prefix><Field>Val / Count is identical, so
		// both shapes share this arm.
		linef(sb, 5, "val := %s", singleVal())
		switch goplan.CopyStrategy(f.GoType()) {
		case goplan.CopyFixedByte:
			linef(sb, 5, "copy(%s%sVal[:], val)", prefix, f.GoFieldName)
		case goplan.CopyBytes:
			line(sb, 5, "cp := make([]byte, len(val))")
			line(sb, 5, "copy(cp, val)")
			linef(sb, 5, "%s%sVal = cp", prefix, f.GoFieldName)
		default:
			linef(sb, 5, "%s%sVal = val", prefix, f.GoFieldName)
		}
		linef(sb, 5, "%s%sCount++", prefix, f.GoFieldName)
	}
}

func writeMultiSubColumnDecode(sb *strings.Builder, g goplan.SectionGroup, attrsVar, membsVar, prefix string) (err error) {
	subs, memb, err := writeMultiSubMatchLoops(sb, g, attrsVar, membsVar, prefix)
	if err != nil {
		return
	}
	if len(g.ScalarSubColumns()) > 0 {
		linef(sb, 2, "if %s%sCount != 1 {", prefix, memb.GoFieldName)
		linef(sb, 3, "err = eb.Build().Int(\"row\", i).Str(\"membership\", %q).Errorf(\"expected exactly one occurrence per row\")", memb.LWMembership)
	} else {
		// S = 0 tuple: a spliced row (every container empty on the write
		// side) carries no attribute and decodes to nil slices — mirror the
		// lone-container tolerance (ADR-0101 D2/D5).
		linef(sb, 2, "if %s%sCount > 1 {", prefix, memb.GoFieldName)
		linef(sb, 3, "err = eb.Build().Int(\"row\", i).Str(\"membership\", %q).Errorf(\"occurs more than once on the row\")", memb.LWMembership)
	}
	line(sb, 3, "return\n\t\t}")
	for _, s := range subs {
		linef(sb, 2, "c.%s = append(c.%s, %s%sVal)", s.Field.GoFieldName, s.Field.GoFieldName, prefix, s.Field.GoFieldName)
	}
	return
}

// multiSub is one sub-column of a multi-sub-column section during decode
// emission.
type multiSub struct {
	Field   mappingplan.TaggedField
	ColName string
}

// writeMultiSubMatchLoops emits the shared middle of a multi-sub-column
// section decode: per-sub value accumulators, the attribute loop reading
// every sub-column accessor and the single-membership match filling them
// plus a `<prefix><Memb>Count` occurrence counter. FillFromArrow and
// ReadRow attach their own tails.
func writeMultiSubMatchLoops(sb *strings.Builder, g goplan.SectionGroup, attrsVar, membsVar, prefix string) (subs []multiSub, memb mappingplan.TaggedField, err error) {
	if len(g.Memberships) != 1 {
		err = eb.Build().Str("section", g.Section).Errorf("multi-sub-column section with multiple memberships not supported on read side")
		return
	}
	for _, sc := range g.SubColumns {
		subs = append(subs, multiSub{Field: sc.Fields[0], ColName: sc.Name})
	}
	memb = g.Memberships[0]
	for _, s := range subs {
		if s.Field.IsSlice() {
			linef(sb, 2, "var %s%sVal []%s", prefix, s.Field.GoFieldName, s.Field.GoType())
		} else {
			linef(sb, 2, "var %s%sVal %s", prefix, s.Field.GoFieldName, s.Field.GoType())
		}
	}
	linef(sb, 2, "var %s%sCount int", prefix, memb.GoFieldName)
	linef(sb, 2, "n%s := %s.GetNumberOfAttributes(raruntime.EntityIdx(i))", prefix, attrsVar)
	linef(sb, 2, "for attrJ := int64(0); attrJ < n%s; attrJ++ {", prefix)
	for _, s := range subs {
		if !s.Field.IsSlice() {
			linef(sb, 3, "%s%sLocal := %s.GetAttrValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ))", prefix, s.Field.GoFieldName, attrsVar, mappingplan.UpperFirst(s.ColName))
			continue
		}
		// Container sub-column: drain the per-attribute Seq into a fresh
		// slice (nil for an empty container — an N=0 attribute reads back
		// as a nil slice, ADR-0101 D5). []byte elements are copied out of
		// the Arrow buffer like the lone-container fill path.
		linef(sb, 3, "var %s%sLocal []%s", prefix, s.Field.GoFieldName, s.Field.GoType())
		linef(sb, 3, "for v := range %s.GetAttrValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", attrsVar, mappingplan.UpperFirst(s.ColName))
		switch goplan.CopyStrategy(s.Field.GoType()) {
		case goplan.CopyBytes:
			line(sb, 4, "cp := make([]byte, len(v))")
			line(sb, 4, "copy(cp, v)")
			linef(sb, 4, "%s%sLocal = append(%s%sLocal, cp)", prefix, s.Field.GoFieldName, prefix, s.Field.GoFieldName)
		default:
			linef(sb, 4, "%s%sLocal = append(%s%sLocal, v)", prefix, s.Field.GoFieldName, prefix, s.Field.GoFieldName)
		}
		line(sb, 3, "}")
	}
	if memb.Flags.Channel.EmbedsLiteralName() {
		linef(sb, 3, "for membBytes := range %s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, memb.Flags.Channel.AddMethodSuffix())
		linef(sb, 4, "if string(membBytes) == %q {", memb.LWMembership)
	} else {
		linef(sb, 3, "for membID := range %s.GetMembValue%s(raruntime.EntityIdx(i), raruntime.AttributeIdx(attrJ)) {", membsVar, memb.Flags.Channel.AddMethodSuffix())
		linef(sb, 4, "if membID == %s {", memb.KindVar())
	}
	for _, s := range subs {
		linef(sb, 5, "%s%sVal = %s%sLocal", prefix, s.Field.GoFieldName, prefix, s.Field.GoFieldName)
	}
	linef(sb, 5, "%s%sCount++", prefix, memb.GoFieldName)
	line(sb, 4, "}")
	line(sb, 3, "}")
	line(sb, 2, "}")
	return
}

// --- ReadRow: presence-gated single-row read (ADR-0100 S2). ---

// ReadRowSupported reports whether <Kind>ReadRow is emitted for the plan,
// and the reason when it is not. Shared with downstream generators
// (recordstore/gen) so the store generator and this emission cannot
// disagree about coverage. Carrier (mixed / parametrized) channels are not
// covered yet; a plain-only kind has no sections to read, and a const-only
// kind is rejected because the match loops skip consts — presence could
// never be set, so the component would read back permanently absent.
func ReadRowSupported(plan *mappingplan.Plan) (ok bool, reason string) {
	if len(plan.Fields) == 0 {
		return false, "plain-only kind (no tagged sections)"
	}
	if !plan.HasNonConstField() {
		return false, "const-only kind (no non-const field can set presence)"
	}
	for _, f := range plan.Fields {
		if f.TupleField != "" {
			return false, fmt.Sprintf("field %s is a dynamic-membership tuple", f.TupleField)
		}
		if f.Flags.Channel.UsesCarrier() {
			return false, fmt.Sprintf("field %s uses a carrier channel", f.GoFieldName)
		}
	}
	return true, ""
}

// writeReadRowHelper emits <Kind>ReadRow: the presence-gated single-row
// twin of FillFromArrow. Where FillFromArrow decodes kind-homogeneous
// batches (a row lacking a scalar/unit field is an error), ReadRow reads
// one row of a FAT table on which the kind is an optional component
// (ADR-0075): a row carrying none of the kind's memberships yields
// present=false; a duplicated scalar field is an error, while duplicated
// container memberships concatenate. Fields bound to plain columns are
// left at their zero value — the caller owns the envelope.
func writeReadRowHelper(sb *strings.Builder, plan *mappingplan.Plan, mode EmitModeE) (err error) {
	kind := kindIdent(plan.KindType, mode)
	if ok, reason := ReadRowSupported(plan); !ok {
		linef(sb, 0, "// %sReadRow is not emitted: %s.\n", kind, reason)
		return
	}
	groups := goplan.ComputeGroups(plan)

	linef(sb, 0, "// %sReadRow reads row i as one optional %s component: presence-", kind, plan.KindType)
	line(sb, 0, "// gated (a row carrying none of the kind's memberships yields")
	line(sb, 0, "// present=false), membership-matched. A duplicated scalar field is")
	line(sb, 0, "// an error; duplicated container memberships concatenate. Plain-")
	line(sb, 0, "// bound fields stay zero — the caller owns the envelope. The")
	line(sb, 0, "// Attrs/Membs readers bind by type inference at the call site, as")
	line(sb, 0, "// with FillFromArrow.")
	linef(sb, 0, "func %sReadRow[", kind)
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttrs %s%sAttrsReadI,", method, kind, method)
		linef(sb, 1, "%sMembs %s%sMembsReadI,", method, kind, method)
	}
	line(sb, 0, "](")
	line(sb, 1, "i int,")
	for _, g := range groups {
		method := methodFor(g.Section)
		linef(sb, 1, "%sAttrs %sAttrs,", lowerFirst(method), method)
		linef(sb, 1, "%sMembs %sMembs,", lowerFirst(method), method)
	}
	linef(sb, 0, ") (row %s, present bool, err error) {", plan.KindType)

	for _, g := range groups {
		method := methodFor(g.Section)
		attrsVar := lowerFirst(method) + "Attrs"
		membsVar := lowerFirst(method) + "Membs"
		prefix := lowerFirst(method)
		linef(sb, 1, "// --- %s. ---", g.Section)
		if len(g.SubColumns) > 1 {
			var subs []multiSub
			var memb mappingplan.TaggedField
			subs, memb, err = writeMultiSubMatchLoops(sb, g, attrsVar, membsVar, prefix)
			if err != nil {
				return
			}
			linef(sb, 1, "if %s%sCount > 1 {", prefix, memb.GoFieldName)
			linef(sb, 2, "err = eb.Build().Int(\"row\", i).Str(\"membership\", %q).Errorf(\"occurs more than once on the row\")", memb.LWMembership)
			line(sb, 2, "return\n\t}")
			linef(sb, 1, "if %s%sCount == 1 {", prefix, memb.GoFieldName)
			for _, s := range subs {
				linef(sb, 2, "row.%s = %s%sVal", s.Field.GoFieldName, prefix, s.Field.GoFieldName)
			}
			line(sb, 2, "present = true")
			line(sb, 1, "}")
			continue
		}
		fields := writeSectionMatchLoops(sb, g, attrsVar, membsVar, prefix)
		for _, f := range fields {
			writeReadRowFieldFinish(sb, f, prefix)
		}
	}
	line(sb, 1, "return")
	line(sb, 0, "}\n")
	return
}

// writeReadRowFieldFinish emits the presence-tolerant tail for one field
// after writeSectionMatchLoops: assign into the row and mark the
// component present on a match; leave the zero value (never error) on
// absence; error on duplicate occurrences of a scalar-shaped field.
func writeReadRowFieldFinish(sb *strings.Builder, f mappingplan.TaggedField, prefix string) {
	switch {
	case f.IsOption:
		linef(sb, 1, "if %s%sCount > 1 {", prefix, f.GoFieldName)
		linef(sb, 2, "err = eb.Build().Int(\"row\", i).Str(\"field\", %q).Errorf(\"occurs more than once on the row\")", f.GoFieldName)
		line(sb, 2, "return\n\t}")
		linef(sb, 1, "if %s%sCount == 1 {", prefix, f.GoFieldName)
		// Field assignment, not option.Some — the generated file does not
		// import the option package (same idiom as Row / Append).
		linef(sb, 2, "row.%s.Val = %s%sVal", f.GoFieldName, prefix, f.GoFieldName)
		linef(sb, 2, "row.%s.Has = true", f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	case f.IsSlice():
		linef(sb, 1, "if %s%sSlice != nil {", prefix, f.GoFieldName)
		linef(sb, 2, "row.%s = %s%sSlice", f.GoFieldName, prefix, f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	case f.IsRoaring():
		linef(sb, 1, "if %s%sBitmap != nil {", prefix, f.GoFieldName)
		linef(sb, 2, "row.%s = %s%sBitmap", f.GoFieldName, prefix, f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	default:
		linef(sb, 1, "if %s%sCount > 1 {", prefix, f.GoFieldName)
		linef(sb, 2, "err = eb.Build().Int(\"row\", i).Str(\"field\", %q).Errorf(\"occurs more than once on the row\")", f.GoFieldName)
		line(sb, 2, "return\n\t}")
		linef(sb, 1, "if %s%sCount == 1 {", prefix, f.GoFieldName)
		linef(sb, 2, "row.%s = %s%sVal", f.GoFieldName, prefix, f.GoFieldName)
		line(sb, 2, "present = true")
		line(sb, 1, "}")
	}
}

// --- case helpers. ---

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'A' && s[0] <= 'Z' {
		return string(s[0]+32) + s[1:]
	}
	return s
}

// --- indentation-tracking emit helpers. ---
//
// EmitPlan runs the assembled source through go/format, which recomputes
// all indentation, so the depth passed to line / linef only needs to be
// structurally faithful: it keeps the pre-format text readable and
// replaces the fragile hand-counted "\t\t\t" string prefixes. A wrong
// depth cannot change the final emitted output.

// line writes s indented by depth tabs, followed by a newline.
func line(sb *strings.Builder, depth int, s string) {
	writeIndent(sb, depth)
	sb.WriteString(s)
	sb.WriteByte('\n')
}

// linef is line with a printf-style format string.
func linef(sb *strings.Builder, depth int, format string, a ...any) {
	writeIndent(sb, depth)
	fmt.Fprintf(sb, format, a...)
	sb.WriteByte('\n')
}

// blank writes a single empty line.
func blank(sb *strings.Builder) { sb.WriteByte('\n') }

func writeIndent(sb *strings.Builder, depth int) {
	for i := 0; i < depth; i++ {
		sb.WriteByte('\t')
	}
}
