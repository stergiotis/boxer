package marshallgen

import (
	"go/format"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

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
// NoOpWrapper provides anchor-style emit (consts + no init + no pre/post).
// The schema-agnostic core compiles against any leeway DML / RA
// implementation whose method shapes satisfy the derived interfaces; Go type
// inference at the BuildEntities / FillFromArrow call site binds the type
// parameters from the concrete DML pointer. opts.Mode selects which pieces
// are rendered.
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
