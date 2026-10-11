package gen

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// Evidence is the evidence profile (ADR-0327 (proposed) §SD2): the
// properties a generated store opts into, each a component the generator
// emits into the store's package and the verbs over it. The components
// bind the facts data sections by name — `symbol`, `stringArray`,
// `u64Array` — so the table carries them, as a table built over
// factsschema's data sections does; and their memberships ride the
// configured Wrapper like every other component's, so a registry-backed
// wrapper must know the `evidence*` names and a declaration-order wrapper
// cannot serve the profile (its components share sections).
type Evidence struct {
	// Chain puts the chain component on every row and gives the store
	// AppendChained, Head, ChainRows, ChainRow and VerifyChain (law L31)
	// under the scheme in <Store>StoreConfig.Scheme. It needs a uint64 Key
	// as the scope and a uint64 Order as the ordinal, is refused with Cas,
	// and implies CanonWire: the chain digest is over the row's canonical
	// item (ADR-0325 §SD2).
	Chain bool
	// WrittenBy puts the written-by component on every row: the identity
	// the row was committed under, read off BeginCtx's context
	// (evidence.IdentityFrom), so who wrote a row is in the row and, on a
	// chained store, inside the digest (law L35).
	WrittenBy bool
	// Vault names the lw:-tagged DTO sources of the vaulted components
	// (ADR-0327 (proposed) §SD4), one kind per file, beside ComponentPaths
	// and not in it: they are the vault store's, and a ledger that could
	// scan and decode content it never stores would say the wrong thing
	// about itself. The generator emits a second store, <Store>VaultStore
	// over <table>vault — the same data sections, the envelope widened by
	// a `component` column — holding one row per (scope, ordinal,
	// component) with the component and its opening, and <Store>Vaulted,
	// the writer and reader over both: content first, the ledger row with
	// one commitment per component, reads that join and open, erasure per
	// component. Requires Chain, and the store's own scaffolding (no
	// SharedRA): the vault is a table of the store's.
	Vault []string
	// Shredded adds the profile's shredded component to the vault: a JSON
	// document exploded by jsonshred into one tuple element per leaf, on
	// the section of its value, the JSON Pointer as the mixed-channel
	// membership's name and the array indices as its parameters
	// (ADR-0326), with EvidenceShreddedOf and Attrs converting to and from
	// jsonshred's attributes. One such component per vault — two kinds on
	// the same verbatim slots would decode as each other — so every
	// shredded field of a row is one component, erased as one. Requires
	// Vault and the shredder's sections on the table: stringArray, symbol,
	// i64Array, f64Array, bool, null, emptyObject and emptyArray, each
	// taking the mixed verbatim name + parameters channel.
	Shredded bool
}

// The profile's component kinds and the files they are emitted to.
const (
	evidenceChainKind     = "EvidenceChain"
	evidenceWrittenByKind = "EvidenceWrittenBy"
	evidenceOpeningKind   = "EvidenceOpening"
	evidenceShreddedKind  = "EvidenceShredded"
	evidenceChainFile     = "evidencechain_dto.out.go"
	evidenceWrittenByFile = "evidencewrittenby_dto.out.go"
	evidenceOpeningFile   = "evidenceopening_dto.out.go"
	evidenceShreddedFile  = "evidenceshredded_dto.out.go"
	evidenceShreddedConv  = "evidenceshredded_conv.out.go"
	// vaultComponentColumn is the envelope column the vault adds: the
	// component a row holds, by its kind tag.
	vaultComponentColumn = "component"
)

// evidenceOn says the profile is set with anything in it.
func (inst Input) evidenceOn() bool {
	return inst.Evidence != nil && (inst.Evidence.Chain || inst.Evidence.WrittenBy || len(inst.Evidence.Vault) > 0)
}

func (inst Input) evidenceChain() bool     { return inst.Evidence != nil && inst.Evidence.Chain }
func (inst Input) evidenceWrittenBy() bool { return inst.Evidence != nil && inst.Evidence.WrittenBy }
func (inst Input) evidenceVault() bool     { return inst.Evidence != nil && len(inst.Evidence.Vault) > 0 }
func (inst Input) evidenceShredded() bool  { return inst.Evidence != nil && inst.Evidence.Shredded }

// vaultComponentPaths are the vault store's hand-written components and,
// with Shredded, the profile's shredded component.
func (inst Input) vaultComponentPaths() (paths []string) {
	paths = slices.Clone(inst.Evidence.Vault)
	if inst.evidenceShredded() {
		paths = append(paths, filepath.Join(inst.OutDir, evidenceShreddedFile))
	}
	return
}

// shredderSections are the sections the shredded component binds.
var shredderSections = []string{"stringArray", "symbol", "i64Array", "f64Array", "bool", "null", "emptyObject", "emptyArray"}

// vaultStoreName and vaultTableName name the vault store and its table.
func (inst Input) vaultStoreName() string { return inst.StoreName + "Vault" }
func (inst Input) vaultTableName() string { return inst.TableName + "vault" }

// vaultTableDesc is the ledger's description widened by the component
// column: a string EntityId beside the Key, which the generator carries as
// a pass-through envelope field.
func (inst Input) vaultTableDesc() (td common.TableDesc) {
	td = inst.Table
	td.DictionaryEntry.Name = naming.MustBeValidStylableName(inst.vaultTableName())
	td.PlainValuesNames = append(slices.Clone(td.PlainValuesNames), naming.MustBeValidStylableName(vaultComponentColumn))
	td.PlainValuesTypes = append(slices.Clone(td.PlainValuesTypes), ctabb.S)
	td.PlainValuesEncodingHints = append(slices.Clone(td.PlainValuesEncodingHints), encodingaspects.EmptyAspectSet)
	td.PlainValuesItemTypes = append(slices.Clone(td.PlainValuesItemTypes), common.PlainItemTypeEntityId)
	td.PlainValuesValueSemantics = append(slices.Clone(td.PlainValuesValueSemantics), valueaspects.EmptyAspectSet)
	return
}

// parseVaultPlans parses the vaulted components' sources.
func (inst Input) parseVaultPlans() (plans []*mappingplan.Plan, err error) {
	for _, in := range inst.vaultComponentPaths() {
		var plan *mappingplan.Plan
		if plan, err = marshallgen.ParsePlan(in); err != nil {
			err = eb.Build().Str("componentPath", in).Errorf("parse vaulted component: %w", err)
			return
		}
		plans = append(plans, plan)
	}
	return
}

// generateVault emits the vault store: the ledger's Input over the widened
// description, the vaulted components and the opening component, with
// CanonWire (the commitments are over the vault rows' canonical items) and
// no profile of its own.
func (inst Input) generateVault() (err error) {
	if err = inst.write(evidenceOpeningFile, []byte(inst.evidenceOpeningDTO())); err != nil {
		return
	}
	vault := Input{
		PackageName:           inst.PackageName,
		StoreName:             inst.vaultStoreName(),
		TableName:             inst.vaultTableName(),
		Database:              inst.Database,
		Table:                 inst.vaultTableDesc(),
		Roles:                 Roles{Key: inst.Roles.Key, Order: inst.Roles.Order},
		RowConfig:             inst.RowConfig,
		ComponentPaths:        append(inst.vaultComponentPaths(), filepath.Join(inst.OutDir, evidenceOpeningFile)),
		OutDir:                inst.OutDir,
		ImportPath:            inst.ImportPath,
		Flat:                  inst.Flat,
		FullCodecs:            inst.FullCodecs,
		Wrapper:               inst.Wrapper,
		ExternallyProvisioned: inst.ExternallyProvisioned,
		InsertSettings:        inst.InsertSettings,
		CanonWire:             true,
		passthroughCols:       true,
	}
	if err = vault.Generate(); err != nil {
		err = eh.Errorf("generate the vault store: %w", err)
	}
	return
}

// emitEvidenceDTOs writes the profile's component sources into OutDir and
// returns their paths, for the component pipeline to take as it takes a
// hand-written DTO. A hand-written component of the same kind is refused:
// the profile owns these shapes.
func (inst Input) emitEvidenceDTOs() (paths []string, err error) {
	for _, in := range inst.ComponentPaths {
		base := filepath.Base(in)
		if base == evidenceChainFile || base == evidenceWrittenByFile {
			err = eh.Errorf("ComponentPaths names %s, which the evidence profile emits itself", base)
			return
		}
	}
	if inst.evidenceChain() {
		if err = inst.write(evidenceChainFile, []byte(inst.evidenceChainDTO())); err != nil {
			return
		}
		paths = append(paths, filepath.Join(inst.OutDir, evidenceChainFile))
	}
	if inst.evidenceWrittenBy() {
		if err = inst.write(evidenceWrittenByFile, []byte(inst.evidenceWrittenByDTO())); err != nil {
			return
		}
		paths = append(paths, filepath.Join(inst.OutDir, evidenceWrittenByFile))
	}
	// The shredded component is the vault's, not the ledger's: written
	// here so the vault's plans can be parsed, returned to no one.
	if inst.evidenceShredded() {
		if err = inst.write(evidenceShreddedFile, []byte(inst.evidenceShreddedDTO())); err != nil {
			return
		}
		if err = inst.write(evidenceShreddedConv, []byte(inst.evidenceShreddedConversions())); err != nil {
			return
		}
	}
	return
}

func (inst Input) evidenceShreddedDTO() string {
	leaf := func(name, doc, value string) string {
		s := "// " + doc + "\ntype " + name + " struct {\n" +
			"\tPath   string   `lw:\"@membership,mixedLowCardVerbatim\"`\n" +
			"\tParams []uint64 `lw:\"@params\"`\n"
		if value != "" {
			s += "\t" + value + "\n"
		}
		return s + "}\n"
	}
	return `// Code generated by github.com/stergiotis/boxer/public/storage/recordstore/gen — DO NOT EDIT.

package ` + inst.PackageName + `

// EvidenceShreddedKind is the kind tag of EvidenceShredded, the component
// name a vault row holding it carries.
const EvidenceShreddedKind = "evidenceShredded"

// EvidenceShredded is the evidence profile's shredded component (ADR-0327
// (proposed) §SD4, ADR-0326): a JSON document exploded by jsonshred into
// one tuple element per leaf, on the facts section of its value, with the
// JSON Pointer as the mixed-channel membership's name and the array
// indices as its parameters. EvidenceShreddedOf and Attrs convert to and
// from jsonshred's attributes.
type EvidenceShredded struct {
	_ struct{} ` + "`kind:\"evidenceShredded\"`" + `

	Id uint64 ` + "`lw:\",id\"`" + `

	Strings      []EvidenceShreddedString ` + "`lw:\"stringArray\"`" + `
	Symbols      []EvidenceShreddedSymbol ` + "`lw:\"symbol\"`" + `
	I64s         []EvidenceShreddedI64    ` + "`lw:\"i64Array\"`" + `
	F64s         []EvidenceShreddedF64    ` + "`lw:\"f64Array\"`" + `
	Bools        []EvidenceShreddedBool   ` + "`lw:\"bool\"`" + `
	Nulls        []EvidenceShreddedLeaf   ` + "`lw:\"null\"`" + `
	EmptyObjects []EvidenceShreddedLeaf   ` + "`lw:\"emptyObject\"`" + `
	EmptyArrays  []EvidenceShreddedLeaf   ` + "`lw:\"emptyArray\"`" + `
}

` + leaf("EvidenceShreddedString", "EvidenceShreddedString is a string leaf.", "Value  string   `lw:\"stringArray:value,unit\"`") + "\n" +
		leaf("EvidenceShreddedSymbol", "EvidenceShreddedSymbol is a string leaf a schema hint marked low-cardinality.", "Value  string   `lw:\"symbol:value\"`") + "\n" +
		leaf("EvidenceShreddedI64", "EvidenceShreddedI64 is an integer leaf.", "Value  int64    `lw:\"i64Array:value,unit\"`") + "\n" +
		leaf("EvidenceShreddedF64", "EvidenceShreddedF64 is a number leaf that is not an integer.", "Value  float64  `lw:\"f64Array:value,unit\"`") + "\n" +
		leaf("EvidenceShreddedBool", "EvidenceShreddedBool is a boolean leaf.", "Value  bool     `lw:\"bool:value\"`") + "\n" +
		leaf("EvidenceShreddedLeaf", "EvidenceShreddedLeaf is a null, {} or [] leaf, which carries no value.", "")
}

func (inst Input) evidenceShreddedConversions() string {
	return `// Code generated by github.com/stergiotis/boxer/public/storage/recordstore/gen — DO NOT EDIT.

package ` + inst.PackageName + `

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/jsonshred"
)

// EvidenceShreddedOf is the shredded component holding attrs, as
// jsonshred.Explode produced them; an attribute whose value does not match
// its section is refused.
func EvidenceShreddedOf(attrs []jsonshred.Attr) (s EvidenceShredded, err error) {
	for _, a := range attrs {
		ok := true
		switch a.Section {
		case jsonshred.SectionString:
			var v string
			if v, ok = a.Value.(string); ok {
				s.Strings = append(s.Strings, EvidenceShreddedString{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionSymbol:
			var v string
			if v, ok = a.Value.(string); ok {
				s.Symbols = append(s.Symbols, EvidenceShreddedSymbol{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionI64:
			var v int64
			if v, ok = a.Value.(int64); ok {
				s.I64s = append(s.I64s, EvidenceShreddedI64{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionF64:
			var v float64
			if v, ok = a.Value.(float64); ok {
				s.F64s = append(s.F64s, EvidenceShreddedF64{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionBool:
			var v bool
			if v, ok = a.Value.(bool); ok {
				s.Bools = append(s.Bools, EvidenceShreddedBool{Path: a.Path, Params: a.Params, Value: v})
			}
		case jsonshred.SectionNull:
			s.Nulls = append(s.Nulls, EvidenceShreddedLeaf{Path: a.Path, Params: a.Params})
		case jsonshred.SectionEmptyObject:
			s.EmptyObjects = append(s.EmptyObjects, EvidenceShreddedLeaf{Path: a.Path, Params: a.Params})
		case jsonshred.SectionEmptyArray:
			s.EmptyArrays = append(s.EmptyArrays, EvidenceShreddedLeaf{Path: a.Path, Params: a.Params})
		default:
			err = eb.Build().Stringer("section", a.Section).Str("path", a.Path).Errorf("no section for a shredded attribute")
			return
		}
		if !ok {
			err = eb.Build().Stringer("section", a.Section).Str("path", a.Path).Errorf("a shredded attribute's value does not match its section")
			return
		}
	}
	return
}

// Attrs are the component's leaves as jsonshred's attributes, section by
// section in jsonshred's section order: what jsonshred.Compose takes.
func (inst EvidenceShredded) Attrs() (attrs []jsonshred.Attr) {
	for _, a := range inst.Strings {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionString, Value: a.Value})
	}
	for _, a := range inst.Symbols {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionSymbol, Value: a.Value})
	}
	for _, a := range inst.I64s {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionI64, Value: a.Value})
	}
	for _, a := range inst.F64s {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionF64, Value: a.Value})
	}
	for _, a := range inst.Bools {
		attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: jsonshred.SectionBool, Value: a.Value})
	}
	for _, leaves := range []struct {
		as  []EvidenceShreddedLeaf
		sec jsonshred.SectionE
	}{{inst.Nulls, jsonshred.SectionNull}, {inst.EmptyObjects, jsonshred.SectionEmptyObject}, {inst.EmptyArrays, jsonshred.SectionEmptyArray}} {
		for _, a := range leaves.as {
			attrs = append(attrs, jsonshred.Attr{Path: a.Path, Params: a.Params, Section: leaves.sec})
		}
	}
	return
}
`
}

func (inst Input) evidenceChainDTO() string {
	return `// Code generated by github.com/stergiotis/boxer/public/storage/recordstore/gen — DO NOT EDIT.

package ` + inst.PackageName + `

import "github.com/stergiotis/boxer/public/functional/option"

// EvidenceChainKind is the value of EvidenceChain.Kind.
const EvidenceChainKind = "evidenceChain"

// EvidenceChain is the evidence profile's chain component (ADR-0327
// (proposed) §SD2, §SD3): the row's place in its scope's chain, what its
// mark says about absent content, and the commitments to its vaulted
// components. AppendChained sets Prev, Digest and Scheme; the writer sets
// the mark and the vault sets the commitments.
type EvidenceChain struct {
	_ struct{} ` + "`kind:\"evidenceChain\"`" + `

	Id uint64 ` + "`lw:\",id\"`" + `
	// Kind's value is EvidenceChainKind; its membership is what a filter
	// finds the component by.
	Kind string ` + "`lw:\"evidenceKindChain,symbol\"`" + `
	// Prev is the predecessor's ordinal, zero for the first row; Digest the
	// chain digest over the predecessor's digest and this row's canonical
	// item with Prev and Digest at zero (law L31).
	Prev   uint64 ` + "`lw:\"evidenceChainPrev,u64Array,unit\"`" + `
	Digest string ` + "`lw:\"evidenceChainDigest,stringArray,unit\"`" + `
	// Scheme names the hash constructions the digest and the commitments
	// are under (evidence.Scheme.Name).
	Scheme string ` + "`lw:\"evidenceChainScheme,symbol\"`" + `
	// Mark is one of evidence.MarkE's names; MarkTarget the ordinal an
	// erased mark names, or the ordinal a keep-marked head row covers;
	// MarkComponent the one component an erased mark names, absent for all.
	Mark          string                ` + "`lw:\"evidenceChainMark,symbol\"`" + `
	MarkTarget    uint64                ` + "`lw:\"evidenceChainMarkTarget,u64Array,unit\"`" + `
	MarkComponent option.Option[string] ` + "`lw:\"evidenceChainMarkComponent,symbol\"`" + `
	// HeadDigest is the digest a keep-marked head row records for the
	// ordinal it covers (law L32).
	HeadDigest option.Option[string] ` + "`lw:\"evidenceChainHeadDigest,stringArray,unit\"`" + `
	// CommitComponents and Commitments are parallel: the vaulted components
	// of the row and the commitment to each (ADR-0327 (proposed) §SD4).
	CommitComponents []string ` + "`lw:\"evidenceChainCommitComponents,stringArray\"`" + `
	Commitments      []string ` + "`lw:\"evidenceChainCommitments,stringArray\"`" + `
}
`
}

func (inst Input) evidenceWrittenByDTO() string {
	return `// Code generated by github.com/stergiotis/boxer/public/storage/recordstore/gen — DO NOT EDIT.

package ` + inst.PackageName + `

import "github.com/stergiotis/boxer/public/functional/option"

// EvidenceWrittenByKind is the value of EvidenceWrittenBy.Kind.
const EvidenceWrittenByKind = "evidenceWrittenBy"

// EvidenceWrittenBy is the evidence profile's written-by component
// (ADR-0327 (proposed) §SD6, law L35): the identity the row was committed
// under, as evidence.IdentityFrom reads it off BeginCtx's context. The
// store fills it; a caller's value is replaced.
type EvidenceWrittenBy struct {
	_ struct{} ` + "`kind:\"evidenceWrittenBy\"`" + `

	Id uint64 ` + "`lw:\",id\"`" + `
	// Kind's value is EvidenceWrittenByKind.
	Kind string ` + "`lw:\"evidenceKindWrittenBy,symbol\"`" + `
	// Run, App and Instance are the call identity's origin; OriginBy says
	// how it was established (evidence.OriginByE's names).
	Run      option.Option[string] ` + "`lw:\"evidenceWrittenByRun,symbol\"`" + `
	App      option.Option[string] ` + "`lw:\"evidenceWrittenByApp,symbol\"`" + `
	Instance uint64                ` + "`lw:\"evidenceWrittenByInstance,u64Array,unit\"`" + `
	OriginBy string                ` + "`lw:\"evidenceWrittenByOriginBy,symbol\"`" + `
	// Principal is the claims' pseudonymous principal; PrincipalBy how far
	// to trust it (evidence.PrincipalBy*).
	Principal   option.Option[string] ` + "`lw:\"evidenceWrittenByPrincipal,stringArray,unit\"`" + `
	PrincipalBy string                ` + "`lw:\"evidenceWrittenByPrincipalBy,symbol\"`" + `
	// Purpose and Correlation are the claims'; Batch the insert batch the
	// row was committed in.
	Purpose     option.Option[string] ` + "`lw:\"evidenceWrittenByPurpose,symbol\"`" + `
	Correlation option.Option[string] ` + "`lw:\"evidenceWrittenByCorrelation,stringArray,unit\"`" + `
	Batch       option.Option[string] ` + "`lw:\"evidenceWrittenByBatch,stringArray,unit\"`" + `
}
`
}

func (inst Input) evidenceOpeningDTO() string {
	return `// Code generated by github.com/stergiotis/boxer/public/storage/recordstore/gen — DO NOT EDIT.

package ` + inst.PackageName + `

// EvidenceOpeningKind is the value of EvidenceOpening.Kind.
const EvidenceOpeningKind = "evidenceOpening"

// EvidenceOpening is the evidence profile's opening component on a vault
// row (ADR-0327 (proposed) §SD4): the opening the ledger's commitment to
// the row's component opens under, as hex. It is destroyed with the row
// and is never in the row's canonical item: the commitment is over the
// item without it.
type EvidenceOpening struct {
	_ struct{} ` + "`kind:\"evidenceOpening\"`" + `

	Id uint64 ` + "`lw:\",id\"`" + `
	// Kind's value is EvidenceOpeningKind.
	Kind string ` + "`lw:\"evidenceKindOpening,symbol\"`" + `
	// Opening is evidence.OpeningText of the opening.
	Opening string ` + "`lw:\"evidenceOpeningValue,stringArray,unit\"`" + `
}
`
}

// emitVaulted writes <Store>Vaulted, the writer and reader over the ledger
// and its vault store (ADR-0327 (proposed) §SD4).
func (inst emitter) emitVaulted(p func(string, ...any)) {
	st, ent, chain := inst.storeType(), inst.entityType(), evidenceChainKind
	vst, vent, venv := inst.vaultStoreName()+"Store", inst.vaultStoreName()+"Entity", inst.vaultStoreName()+"Envelope"
	wrapped := inst.StoreName + "Vaulted"
	rowType := lowerFirst(inst.StoreName) + "ChainRow"
	ord := inst.orderEntityField()
	p("// %s is the store a host runs under the profile's vault (ADR-0327", wrapped)
	p("// (proposed) §SD4): a ledger that never holds content and a vault with one")
	p("// row per (scope, ordinal, component), each with the opening the ledger's")
	p("// commitment opens under. Append writes the content first, then the ledger")
	p("// row with one commitment per component; reads join the two and hand the")
	p("// laws the openings; erasure is per component, a rewrite of the vault's")
	p("// part. Safe for concurrent use as far as its stores are: Append, Flush and")
	p("// Close are the single writer's; the reads take the stores' executors.")
	p("type %s struct {", wrapped)
	p("\tledger *%s", st)
	p("\tvault  *%s", vst)
	p("}")
	p("")
	p("// New%s is the ledger and its vault; the scheme is the ledger's.", wrapped)
	p("func New%s(ledger *%s, vault *%s) *%s { return &%s{ledger: ledger, vault: vault} }", wrapped, st, vst, wrapped, wrapped)
	p("")
	p("// Ledger and Vault are the stores underneath.")
	p("func (inst *%s) Ledger() *%s { return inst.ledger }", wrapped, st)
	p("func (inst *%s) Vault() *%s  { return inst.vault }", wrapped, vst)
	p("")
	p("// %sVaultComponents are the kind tags of the vaulted components, the", inst.StoreName)
	p("// names the ledger's commitments and the vault's component column carry.")
	p("var %sVaultComponents = []string{", inst.StoreName)
	for _, vp := range inst.vaultPlans {
		p("\t%q,", vp.KindName)
	}
	p("}")
	p("")
	p("// Split prepares content for the row at ordinal: for each content row the")
	p("// component it holds names it, the opening is drawn, the commitment to the")
	p("// row's canonical item without the opening goes onto ent's chain component,")
	p("// and the row gains its opening. A content row must hold exactly one")
	p("// vaulted component. ent and content are updated in place.")
	p("func (inst *%s) Split(ent *%s, ordinal uint64, content []*%s) (err error) {", wrapped, ent, vent)
	p("\tif inst.ledger.cfg.Scheme.IsZero() {")
	p("\t\terr = eh.Errorf(\"split: the ledger has no scheme (%sStoreConfig.Scheme)\")", inst.StoreName)
	p("\t\treturn")
	p("\t}")
	p("\tch := ent.%s.Val", chain)
	p("\tch.CommitComponents, ch.Commitments = nil, nil")
	p("\tseen := make(map[string]bool, len(content))")
	p("\tfor _, v := range content {")
	p("\t\tcomponent := \"\"")
	for _, vp := range inst.vaultPlans {
		p("\t\tif v.%s.Has {", vp.KindType)
		p("\t\t\tif component != \"\" {")
		p("\t\t\t\terr = eh.Errorf(\"split: a content row holds more than one vaulted component\")")
		p("\t\t\t\treturn")
		p("\t\t\t}")
		p("\t\t\tcomponent = %q", vp.KindName)
		p("\t\t}")
	}
	p("\t\tif component == \"\" {")
	p("\t\t\terr = eh.Errorf(\"split: a content row holds no vaulted component\")")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tif seen[component] {")
	p("\t\t\terr = eh.Errorf(\"split: two content rows hold the same vaulted component; a row holds one of each at most\")")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tseen[component] = true")
	p("\t\tv.ID, v.%s = ent.ID, ordinal", ord)
	p("\t\tv.%s.Component = component", venv)
	p("\t\tv.%s = option.None[%s]()", evidenceOpeningKind, evidenceOpeningKind)
	p("\t\titem, e := inst.vault.CanonicalItem(v)")
	p("\t\tif e != nil {")
	p("\t\t\terr = eh.Errorf(\"split: %%w\", e)")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\topening := evidence.NewOpening()")
	p("\t\tv.%s = option.Some(%s{Kind: %sKind, Opening: evidence.OpeningText(opening)})", evidenceOpeningKind, evidenceOpeningKind, evidenceOpeningKind)
	p("\t\tch.CommitComponents = append(ch.CommitComponents, component)")
	p("\t\tch.Commitments = append(ch.Commitments, inst.ledger.cfg.Scheme.Commit(opening, item))")
	p("\t}")
	p("\tent.%s = option.Some(ch)", chain)
	p("\treturn")
	p("}")
	p("")
	p("// Prepare is Split and the ledger's PrepareChained: the content rows named,")
	p("// opened and committed to on ent's chain component, and ent made the next")
	p("// row of its scope's chain, nothing written. A writer that must know the")
	p("// digest before it acts prepares, acts, and writes (ADR-0314 §SD1).")
	p("func (inst *%s) Prepare(ctx context.Context, head evidence.Head, ent *%s, content []*%s) (digest string, err error) {", wrapped, ent, vent)
	p("\tif err = inst.Split(ent, head.Ordinal+1, content); err != nil {")
	p("\t\treturn")
	p("\t}")
	p("\treturn inst.ledger.PrepareChained(ctx, head, ent)")
	p("}")
	p("")
	p("// Check says ent and content are what Prepare makes of them after the")
	p("// predecessor at prevDigest: each content row is keyed by ent's scope and")
	p("// ordinal, holds the one component it is named by, opens the commitment")
	p("// ent's chain component carries for it in order, and ent checks as a")
	p("// chained row (CheckChained). What a service checks on rows a client")
	p("// prepared, before Write.")
	p("func (inst *%s) Check(prevDigest string, ent *%s, content []*%s) (err error) {", wrapped, ent, vent)
	p("\tif err = inst.ledger.CheckChained(prevDigest, ent); err != nil {")
	p("\t\treturn")
	p("\t}")
	p("\tch := ent.%s.Val", chain)
	p("\tif len(ch.CommitComponents) != len(content) || len(ch.Commitments) != len(content) {")
	p("\t\treturn eh.Errorf(\"check: the content rows and the commitments differ in number\")")
	p("\t}")
	p("\tseen := make(map[string]bool, len(content))")
	p("\tfor i, v := range content {")
	p("\t\tif v.ID != ent.ID || v.%s != ent.%s {", ord, ord)
	p("\t\t\treturn eh.Errorf(\"check: a content row is not the ledger row's\")")
	p("\t\t}")
	p("\t\tcomponent := \"\"")
	for _, vp := range inst.vaultPlans {
		p("\t\tif v.%s.Has {", vp.KindType)
		p("\t\t\tif component != \"\" {")
		p("\t\t\t\treturn eh.Errorf(\"check: a content row holds more than one vaulted component\")")
		p("\t\t\t}")
		p("\t\t\tcomponent = %q", vp.KindName)
		p("\t\t}")
	}
	p("\t\tif component == \"\" || component != v.%s.Component || component != ch.CommitComponents[i] {", venv)
	p("\t\t\treturn eh.Errorf(\"check: a content row is not named by the component it holds\")")
	p("\t\t}")
	p("\t\tif seen[component] {")
	p("\t\t\treturn eh.Errorf(\"check: two content rows hold the same vaulted component\")")
	p("\t\t}")
	p("\t\tseen[component] = true")
	p("\t\tif !v.%s.Has {", evidenceOpeningKind)
	p("\t\t\treturn eh.Errorf(\"check: a content row carries no opening\")")
	p("\t\t}")
	p("\t\topening := evidence.OpeningOf(v.%s.Val.Opening)", evidenceOpeningKind)
	p("\t\tc := *v")
	p("\t\tc.%s = option.None[%s]()", evidenceOpeningKind, evidenceOpeningKind)
	p("\t\titem, e := inst.vault.CanonicalItem(&c)")
	p("\t\tif e != nil {")
	p("\t\t\treturn eh.Errorf(\"check: %%w\", e)")
	p("\t\t}")
	p("\t\tif !inst.ledger.cfg.Scheme.Opens(opening, item, ch.Commitments[i]) {")
	p("\t\t\treturn eh.Errorf(\"check: a content row does not open its commitment\")")
	p("\t\t}")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// Write commits prepared rows: the content rows into the vault, then the")
	p("// ledger row as it stands, in that order — a ledger row whose content is")
	p("// missing reads as erased and breaks law L31 unless the chain explains it,")
	p("// while a vault row without its ledger row is dead weight nothing reads.")
	p("// Both stores buffer; Flush makes the rows durable, vault first.")
	p("func (inst *%s) Write(ctx context.Context, ent *%s, content []*%s) (err error) {", wrapped, ent, vent)
	p("\tfor _, v := range content {")
	p("\t\tb := inst.vault.BeginCtx(ctx, v.ID, v.%s, v.%s)", ord, venv)
	for _, vp := range inst.vaultPlans {
		p("\t\tif v.%s.Has {", vp.KindType)
		p("\t\t\tb.Add%s(v.%s.Val)", vp.KindType, vp.KindType)
		p("\t\t}")
	}
	p("\t\tif v.%s.Has {", evidenceOpeningKind)
	p("\t\t\tb.Add%s(v.%s.Val)", evidenceOpeningKind, evidenceOpeningKind)
	p("\t\t}")
	p("\t\tif err = b.Commit(); err != nil {")
	p("\t\t\treturn eh.Errorf(\"write: vault: %%w\", err)")
	p("\t\t}")
	p("\t}")
	p("\treturn inst.ledger.WriteChained(ctx, ent)")
	p("}")
	p("")
	p("// Append is Prepare and Write.")
	p("func (inst *%s) Append(ctx context.Context, head evidence.Head, ent *%s, content []*%s) (digest string, err error) {", wrapped, ent, vent)
	p("\tif digest, err = inst.Prepare(ctx, head, ent, content); err != nil {")
	p("\t\treturn")
	p("\t}")
	p("\tif err = inst.Write(ctx, ent, content); err != nil {")
	p("\t\tdigest = \"\"")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// Flush makes the buffered rows durable, the vault's before the ledger's.")
	p("func (inst *%s) Flush(ctx context.Context) (err error) {", wrapped)
	p("\tif _, err = inst.vault.Flush(ctx); err != nil {")
	p("\t\treturn eh.Errorf(\"flush: vault: %%w\", err)")
	p("\t}")
	p("\tif _, err = inst.ledger.Flush(ctx); err != nil {")
	p("\t\treturn eh.Errorf(\"flush: ledger: %%w\", err)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// Close closes both stores.")
	p("func (inst *%s) Close() {", wrapped)
	p("\tinst.vault.Close()")
	p("\tinst.ledger.Close()")
	p("}")
	p("")
	p("// Join is the ledger rows as the laws read them with the vault's openings")
	p("// and items attached, and the content rows grouped by ordinal. Of several")
	p("// vault rows for one (ordinal, component) the last listed stands; the")
	p("// laws say whether it opens.")
	p("func (inst *%s) Join(ledger []*%s, content []*%s) (rows []evidence.RowI, byOrdinal map[uint64][]*%s) {", wrapped, ent, vent, vent)
	p("\tbyOrdinal = make(map[uint64][]*%s)", vent)
	p("\tfor _, v := range content {")
	p("\t\tbyOrdinal[v.%s] = append(byOrdinal[v.%s], v)", ord, ord)
	p("\t}")
	p("\tfor _, e := range ledger {")
	p("\t\trow := inst.ledger.ChainRow(e).(*%s)", rowType)
	p("\t\tvaults := make(map[string]*%s)", vent)
	p("\t\tfor _, v := range byOrdinal[e.%s] {", ord)
	p("\t\t\tif v.ID != e.ID {")
	p("\t\t\t\tcontinue")
	p("\t\t\t}")
	p("\t\t\tvaults[v.%s.Component] = v", venv)
	p("\t\t}")
	p("\t\trow.openings = make(map[string][]byte, len(vaults))")
	p("\t\tfor component, v := range vaults {")
	p("\t\t\tif v.%s.Has {", evidenceOpeningKind)
	p("\t\t\t\tif o := evidence.OpeningOf(v.%s.Val.Opening); o != nil {", evidenceOpeningKind)
	p("\t\t\t\t\trow.openings[component] = o")
	p("\t\t\t\t}")
	p("\t\t\t}")
	p("\t\t}")
	p("\t\trow.vaultItem = func(component string) ([]byte, error) {")
	p("\t\t\tv, ok := vaults[component]")
	p("\t\t\tif !ok {")
	p("\t\t\t\treturn nil, eh.Errorf(\"vault item: no row for the component\")")
	p("\t\t\t}")
	p("\t\t\tc := *v")
	p("\t\t\tc.%s = option.None[%s]()", evidenceOpeningKind, evidenceOpeningKind)
	p("\t\t\treturn inst.vault.CanonicalItem(&c)")
	p("\t\t}")
	p("\t\trows = append(rows, row)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// Rows reads scope's rows from an ordinal on, ledger and vault, and joins")
	p("// them.")
	p("func (inst *%s) Rows(ctx context.Context, scope uint64, from uint64) (rows []evidence.RowI, byOrdinal map[uint64][]*%s, err error) {", wrapped, vent)
	p("\tvar ledger []*%s", ent)
	p("\tfor e, er := range inst.ledger.Replay(ctx, scope, from, recordstore.ReplayOptsU64{}) {")
	p("\t\tif er != nil {")
	p("\t\t\terr = eh.Errorf(\"rows: ledger: %%w\", er)")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tledger = append(ledger, e)")
	p("\t}")
	p("\tvar content []*%s", vent)
	p("\tfor v, er := range inst.vault.Replay(ctx, scope, from, recordstore.ReplayOptsU64{}) {")
	p("\t\tif er != nil {")
	p("\t\t\terr = eh.Errorf(\"rows: vault: %%w\", er)")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\tcontent = append(content, v)")
	p("\t}")
	p("\trows, byOrdinal = inst.Join(ledger, content)")
	p("\treturn")
	p("}")
	p("")
	p("// VerifyChain is law L31 over scope from seed, the vault's openings in hand.")
	p("func (inst *%s) VerifyChain(ctx context.Context, scope uint64, seed string) (broken uint64, why evidence.BreakE, err error) {", wrapped)
	p("\trows, _, err := inst.Rows(ctx, scope, 0)")
	p("\tif err != nil {")
	p("\t\treturn")
	p("\t}")
	p("\treturn evidence.Verify(inst.ledger.cfg.Scheme, seed, rows)")
	p("}")
	p("")
	p("// Erase rewrites the vault rows at ordinals out of the table, all their")
	p("// components or only those named: ALTER TABLE … DELETE with")
	p("// mutations_sync = 2, so the bytes are gone when it returns, not masked")
	p("// for a later merge (ADR-0320 §SD4). The erased mark that explains the")
	p("// absence is the writer's to append first (ADR-0327 (proposed) §SD3).")
	p("func (inst *%s) Erase(ctx context.Context, scope uint64, ordinals []uint64, components []string) (err error) {", wrapped)
	p("\tif len(ordinals) == 0 {")
	p("\t\treturn")
	p("\t}")
	p("\twhere := %sColKey + \" = \" + strconv.FormatUint(scope, 10) + \" AND \" + %sColOrder + \" IN (\" + evidenceOrdinals(ordinals) + \")\"", inst.vaultStoreName(), inst.vaultStoreName())
	p("\tif len(components) > 0 {")
	p("\t\tvar names string")
	p("\t\tif names, err = evidenceComponentLiterals(components); err != nil {")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\twhere += \" AND \" + %sColComponent + \" IN (\" + names + \")\"", inst.vaultStoreName())
	p("\t}")
	p("\treturn inst.mutate(ctx, where)")
	p("}")
	p("")
	p("// Destroy rewrites scope's vault rows out of the table but those at keep.")
	p("func (inst *%s) Destroy(ctx context.Context, scope uint64, keep []uint64) (err error) {", wrapped)
	p("\twhere := %sColKey + \" = \" + strconv.FormatUint(scope, 10)", inst.vaultStoreName())
	p("\tif len(keep) > 0 {")
	p("\t\twhere += \" AND \" + %sColOrder + \" NOT IN (\" + evidenceOrdinals(keep) + \")\"", inst.vaultStoreName())
	p("\t}")
	p("\treturn inst.mutate(ctx, where)")
	p("}")
	p("")
	p("func (inst *%s) mutate(ctx context.Context, where string) (err error) {", wrapped)
	p("\tif inst.vault.exec == nil {")
	p("\t\treturn eh.Errorf(\"the vault has no executor\")")
	p("\t}")
	p("\tif err = inst.vault.exec.Exec(ctx, \"ALTER TABLE \"+inst.vault.writeTable()+\" DELETE WHERE \"+where+\" SETTINGS mutations_sync = 2\"); err != nil {")
	p("\t\terr = eh.Errorf(\"erase vault rows: %%w\", err)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// evidenceOrdinals are ordinals as the order column's literals.")
	p("func evidenceOrdinals(ordinals []uint64) string {")
	p("\tparts := make([]string, 0, len(ordinals))")
	p("\tfor _, o := range ordinals {")
	p("\t\tparts = append(parts, strconv.FormatUint(o, 10))")
	p("\t}")
	p("\treturn strings.Join(parts, \", \")")
	p("}")
	p("")
	p("// evidenceComponentLiterals are kind tags as string literals; a tag is a")
	p("// Go identifier, so anything else is refused rather than quoted.")
	p("func evidenceComponentLiterals(components []string) (s string, err error) {")
	p("\tparts := make([]string, 0, len(components))")
	p("\tfor _, c := range components {")
	p("\t\tfor _, r := range c {")
	p("\t\t\tif !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {")
	p("\t\t\t\terr = eh.Errorf(\"a component name is not a kind tag\")")
	p("\t\t\t\treturn")
	p("\t\t\t}")
	p("\t\t}")
	p("\t\tparts = append(parts, \"'\"+c+\"'\")")
	p("\t}")
	p("\ts = strings.Join(parts, \", \")")
	p("\treturn")
	p("}")
	p("")
}

// emitEvidenceVerbs writes the profile's verbs: the written-by filler and
// the chain's AppendChained, Head, ChainRows, ChainRow, VerifyChain and
// the row adapter the laws read.
func (inst emitter) emitEvidenceVerbs(p func(string, ...any)) {
	if inst.evidenceWrittenBy() {
		p("// writtenByOf is the written-by component of a row committed under ctx")
		p("// (ADR-0327 (proposed) §SD6): the call identity's origin and claims, the")
		p("// batch and the attestation on ctx, as evidence.IdentityFrom reads them.")
		p("func (inst *%s) writtenByOf(ctx context.Context) (row %s) {", inst.storeType(), evidenceWrittenByKind)
		p("\tid := evidence.IdentityFrom(ctx)")
		p("\trow = %s{Kind: %sKind, Instance: id.Instance, OriginBy: id.OriginBy.String(), PrincipalBy: id.PrincipalBy}", evidenceWrittenByKind, evidenceWrittenByKind)
		for _, f := range []struct{ field, src string }{{"Run", "id.Run"}, {"App", "id.App"}, {"Principal", "id.Principal"}, {"Purpose", "id.Purpose"}, {"Correlation", "id.Correlation"}, {"Batch", "id.Batch"}} {
			p("\tif %s != \"\" {", f.src)
			p("\t\trow.%s = option.Some(%s)", f.field, f.src)
			p("\t}")
		}
		p("\treturn")
		p("}")
		p("")
	}
	if !inst.evidenceChain() {
		return
	}
	st, ent, chain := inst.storeType(), inst.entityType(), evidenceChainKind
	rowType := lowerFirst(inst.StoreName) + "ChainRow"
	hasPT := len(inst.model.passthrough) > 0
	beginArgs := "ent.ID, ent." + inst.orderEntityField()
	if hasPT {
		beginArgs += ", ent." + inst.StoreName + "Envelope"
	}
	p("// PrepareChained makes ent the next row of its scope's chain without")
	p("// writing it (ADR-0327 (proposed) §SD2, law L31): the ordinal after head's,")
	p("// the chain component's Prev, Digest and Scheme set by the store — the mark")
	p("// is the caller's — the written-by component the context's, whatever ent")
	p("// carried, and the digest over the row's canonical item with Prev and")
	p("// Digest at zero, chained to head's digest (the seed for an empty scope).")
	p("// A writer that must know the digest before it acts prepares, acts, and")
	p("// writes (ADR-0314 §SD1). ent is updated in place; digest is what its chain")
	p("// component now records.")
	p("func (inst *%s) PrepareChained(ctx context.Context, head evidence.Head, ent *%s) (digest string, err error) {", st, ent)
	p("\tif inst.cfg.Scheme.IsZero() {")
	p("\t\terr = eh.Errorf(\"prepare chained: the store has no scheme (%sStoreConfig.Scheme)\")", inst.StoreName)
	p("\t\treturn")
	p("\t}")
	p("\tif ent == nil {")
	p("\t\terr = eh.Errorf(\"prepare chained: nil entity\")")
	p("\t\treturn")
	p("\t}")
	p("\tent.%s = head.Ordinal + 1", inst.orderEntityField())
	p("\tch := ent.%s.Val", chain)
	p("\tch.Kind, ch.Prev, ch.Digest, ch.Scheme = %sKind, head.Ordinal, \"\", inst.cfg.Scheme.Name()", chain)
	p("\tif ch.Mark == \"\" {")
	p("\t\tch.Mark = evidence.MarkNone.String()")
	p("\t}")
	p("\tent.%s = option.Some(ch)", chain)
	if inst.evidenceWrittenBy() {
		p("\tent.%s = option.Some(inst.writtenByOf(ctx))", evidenceWrittenByKind)
	}
	p("\titem, err := inst.chainItem(ent)")
	p("\tif err != nil {")
	p("\t\terr = eh.Errorf(\"prepare chained: %%w\", err)")
	p("\t\treturn")
	p("\t}")
	p("\tdigest = inst.cfg.Scheme.ChainDigest(head.Digest, item)")
	p("\tch.Digest = digest")
	p("\tent.%s = option.Some(ch)", chain)
	p("\treturn")
	p("}")
	p("")
	p("// CheckChained says ent is what PrepareChained makes of a row after the")
	p("// predecessor at prevDigest: it names that predecessor, carries the scheme,")
	p("// and its digest is the chain's over its canonical item. What a service")
	p("// checks on a row a client prepared, before WriteChained; the written-by")
	p("// component is checked as content, and whether its origin is the sender's")
	p("// is the service's to compare.")
	p("func (inst *%s) CheckChained(prevDigest string, ent *%s) (err error) {", st, ent)
	p("\tif inst.cfg.Scheme.IsZero() {")
	p("\t\treturn eh.Errorf(\"check chained: the store has no scheme (%sStoreConfig.Scheme)\")", inst.StoreName)
	p("\t}")
	p("\tif ent == nil || !ent.%s.Has {", chain)
	p("\t\treturn eh.Errorf(\"check chained: the row carries no chain component\")")
	p("\t}")
	p("\tch := ent.%s.Val", chain)
	p("\tif ch.Scheme != inst.cfg.Scheme.Name() {")
	p("\t\treturn eh.Errorf(\"check chained: the row is under another scheme\")")
	p("\t}")
	p("\tif ch.Prev+1 != ent.%s {", inst.orderEntityField())
	p("\t\treturn eh.Errorf(\"check chained: the row does not follow its predecessor's ordinal\")")
	p("\t}")
	p("\titem, err := inst.chainItem(ent)")
	p("\tif err != nil {")
	p("\t\treturn eh.Errorf(\"check chained: %%w\", err)")
	p("\t}")
	p("\tif ch.Digest != inst.cfg.Scheme.ChainDigest(prevDigest, item) {")
	p("\t\treturn eh.Errorf(\"check chained: the row's digest is not the chain's\")")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// WriteChained commits a prepared row as it stands: no field is set, no")
	p("// digest recomputed. The row is buffered as Commit buffers; Flush makes it")
	p("// durable.")
	p("func (inst *%s) WriteChained(ctx context.Context, ent *%s) (err error) {", st, ent)
	p("\tif ent == nil || !ent.%s.Has || ent.%s.Val.Digest == \"\" {", chain, chain)
	p("\t\treturn eh.Errorf(\"write chained: the row is not prepared\")")
	p("\t}")
	if inst.evidenceWrittenBy() {
		p("\tb := inst.beginCtx(ctx, %s, false)", beginArgs)
	} else {
		p("\tb := inst.BeginCtx(ctx, %s)", beginArgs)
	}
	for _, c := range inst.comps {
		p("\tif ent.%s.Has {", c.Kind)
		p("\t\tb.Add%s(ent.%s.Val)", c.Kind, c.Kind)
		p("\t}")
	}
	p("\tif err = b.Commit(); err != nil {")
	p("\t\terr = eh.Errorf(\"write chained: %%w\", err)")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// AppendChained is PrepareChained and WriteChained.")
	p("func (inst *%s) AppendChained(ctx context.Context, head evidence.Head, ent *%s) (digest string, err error) {", st, ent)
	p("\tif digest, err = inst.PrepareChained(ctx, head, ent); err != nil {")
	p("\t\treturn")
	p("\t}")
	p("\tif err = inst.WriteChained(ctx, ent); err != nil {")
	p("\t\tdigest = \"\"")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// chainItem is the canonical item of ent with the chain's Prev and Digest")
	if inst.model.revision != nil {
		p("// at zero, and the revision too — a superseding row at a later revision")
		p("// is the same row to the chain: what its chain digest is over.")
	} else {
		p("// at zero: what its chain digest is over.")
	}
	p("func (inst *%s) chainItem(ent *%s) (item []byte, err error) {", st, ent)
	p("\tc := *ent")
	p("\tif c.%s.Has {", chain)
	p("\t\tch := c.%s.Val", chain)
	p("\t\tch.Prev, ch.Digest = 0, \"\"")
	p("\t\tc.%s = option.Some(ch)", chain)
	p("\t}")
	if inst.model.revision != nil {
		p("\tc.%sEnvelope.%s = 0", inst.StoreName, inst.model.revision.pascal)
	}
	p("\treturn inst.CanonicalItem(&c)")
	p("}")
	p("")
	p("// Head is the chain head of scope: the latest row's ordinal and digest.")
	p("// found is false for a scope with no row; a scope whose latest row carries")
	p("// no chain component is an error.")
	p("func (inst *%s) Head(ctx context.Context, scope uint64) (h evidence.Head, found bool, err error) {", st)
	p("\tent, found, err := inst.Latest(ctx, scope)")
	p("\tif err != nil || !found {")
	p("\t\treturn")
	p("\t}")
	p("\tif !ent.%s.Has {", chain)
	p("\t\terr = eh.Errorf(\"head: the latest row of the scope carries no chain component\")")
	p("\t\treturn")
	p("\t}")
	p("\th = evidence.Head{Ordinal: ent.%s, Digest: ent.%s.Val.Digest}", inst.orderEntityField(), chain)
	p("\treturn")
	p("}")
	p("")
	p("// ChainRows reads the rows of scope from an ordinal on, in order, as the")
	p("// laws read them.")
	p("func (inst *%s) ChainRows(ctx context.Context, scope uint64, from uint64) (rows []evidence.RowI, err error) {", st)
	p("\tfor ent, e := range inst.Replay(ctx, scope, from, recordstore.ReplayOptsU64{}) {")
	p("\t\tif e != nil {")
	p("\t\t\terr = e")
	p("\t\t\treturn")
	p("\t\t}")
	p("\t\trows = append(rows, inst.ChainRow(ent))")
	p("\t}")
	p("\treturn")
	p("}")
	p("")
	p("// VerifyChain is law L31 over scope from seed: the first ordinal that")
	p("// breaks the chain and why, or zero when it holds.")
	p("func (inst *%s) VerifyChain(ctx context.Context, scope uint64, seed string) (broken uint64, why evidence.BreakE, err error) {", st)
	p("\trows, err := inst.ChainRows(ctx, scope, 0)")
	p("\tif err != nil {")
	p("\t\treturn")
	p("\t}")
	p("\treturn evidence.Verify(inst.cfg.Scheme, seed, rows)")
	p("}")
	p("")
	p("// ChainRow is ent as the laws read it. It implements evidence.HeadRowI,")
	p("// answering a head for a keep-marked row that records one. Openings and")
	p("// vault items come from the vault; without one every commitment reads as")
	p("// absent content.")
	p("func (inst *%s) ChainRow(ent *%s) evidence.RowI { return &%s{store: inst, ent: ent} }", st, ent, rowType)
	p("")
	p("type %s struct {", rowType)
	p("\tstore *%s", st)
	p("\tent   *%s", ent)
	p("\t// openings are the vault's, by component; vaultItem is the canonical")
	p("\t// item of a component's vault row without its opening.")
	p("\topenings  map[string][]byte")
	p("\tvaultItem func(component string) ([]byte, error)")
	p("}")
	p("")
	p("var _ evidence.HeadRowI = (*%s)(nil)", rowType)
	p("")
	p("func (inst *%s) chain() %s { return inst.ent.%s.Val }", rowType, chain, chain)
	p("func (inst *%s) Ordinal() uint64 { return inst.ent.%s }", rowType, inst.orderEntityField())
	p("func (inst *%s) Prev() uint64    { return inst.chain().Prev }", rowType)
	p("func (inst *%s) Digest() string  { return inst.chain().Digest }", rowType)
	p("func (inst *%s) CanonicalItem() ([]byte, error) { return inst.store.chainItem(inst.ent) }", rowType)
	p("func (inst *%s) Mark() evidence.Mark {", rowType)
	p("\tch := inst.chain()")
	p("\tm, _ := evidence.ParseMark(ch.Mark)")
	p("\treturn evidence.Mark{Mark: m, Target: ch.MarkTarget, Component: ch.MarkComponent.Val}")
	p("}")
	p("func (inst *%s) Commitments() (cs []evidence.Commitment) {", rowType)
	p("\tch := inst.chain()")
	p("\tfor i, component := range ch.CommitComponents {")
	p("\t\tc := evidence.Commitment{Component: component}")
	p("\t\tif i < len(ch.Commitments) {")
	p("\t\t\tc.Commitment = ch.Commitments[i]")
	p("\t\t}")
	p("\t\tif o, ok := inst.openings[component]; ok {")
	p("\t\t\tc.Opening, c.Present = o, true")
	p("\t\t}")
	p("\t\tcs = append(cs, c)")
	p("\t}")
	p("\treturn")
	p("}")
	p("func (inst *%s) VaultItem(component string) ([]byte, error) {", rowType)
	p("\tif inst.vaultItem == nil {")
	p("\t\treturn nil, eh.Errorf(\"vault item: the row was read without its vault\")")
	p("\t}")
	p("\treturn inst.vaultItem(component)")
	p("}")
	p("func (inst *%s) Head() (ordinal uint64, digest string, ok bool) {", rowType)
	p("\tch := inst.chain()")
	p("\tif ch.Mark != evidence.MarkKeep.String() || !ch.HeadDigest.Has {")
	p("\t\treturn")
	p("\t}")
	p("\treturn ch.MarkTarget, ch.HeadDigest.Val, true")
	p("}")
	p("")
	if inst.evidenceVault() {
		inst.emitVaulted(p)
	}
}

// codecFileName is the per-component codec file for a DTO source: the
// source's base with .out.go in place of .go, or, for a source the
// generator emitted itself (already .out.go), with _codec.out.go.
func codecFileName(in string) string {
	base := filepath.Base(in)
	if strings.HasSuffix(base, ".out.go") {
		return strings.TrimSuffix(base, ".out.go") + "_codec.out.go"
	}
	return strings.TrimSuffix(base, ".go") + ".out.go"
}

// evidencePreconditions are the profile's generation-time gates, checked
// as soon as the role types are known so they report ahead of the
// component and Cas gates a wrong schema would also trip.
func (inst Input) evidencePreconditions(keyGoType, orderGoType string) (err error) {
	if !inst.evidenceChain() {
		return
	}
	switch {
	case keyGoType != "uint64":
		err = fmt.Errorf("Evidence.Chain needs a uint64 Key as the scope; this schema's Key is %s", keyGoType)
	case orderGoType != "uint64":
		err = fmt.Errorf("Evidence.Chain needs a uint64 Order as the ordinal (Roles.Order on a plain uint64 column); this schema's Order is %s", orderGoType)
	case inst.Cas:
		err = fmt.Errorf("Evidence.Chain is refused with Cas: a row rewritten in place has no chain")
	}
	return
}

// vaultPreconditions are the vault's generation-time gates.
func (inst Input) vaultPreconditions() (err error) {
	if !inst.evidenceVault() {
		return
	}
	switch {
	case !inst.evidenceChain():
		err = fmt.Errorf("Evidence.Vault needs Evidence.Chain: the commitments ride the chain component")
	case inst.SharedRA != nil:
		err = fmt.Errorf("Evidence.Vault is refused with SharedRA: the vault is a table of the store's own, with scaffolding of its own")
	}
	if err != nil || !inst.evidenceShredded() {
		return
	}
	have := make(map[string]bool, len(inst.Table.TaggedValuesSections))
	for _, sec := range inst.Table.TaggedValuesSections {
		have[string(sec.Name)] = true
	}
	for _, name := range shredderSections {
		if !have[name] {
			err = fmt.Errorf("Evidence.Shredded needs the shredder's sections on the table, each taking the mixed verbatim name + parameters channel; %q is missing", name)
			return
		}
	}
	return
}
