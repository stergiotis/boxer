// Package storegen generates a record store bound to the `boxer.facts`
// table: it hands [github.com/stergiotis/boxer/public/storage/recordstore/gen]
// the facts schema and a registry-resolved membership-id snapshot, so the
// store's baked ids agree with the vocabulary its writers use.
//
// This is ADR-0105 D2 (the store-side sibling of `codec/factswrapper`) and
// ADR-0183 D2. Where `factswrapper` resolves membership ids at package init,
// a store must state them at *generation* time — they reach the emitted Scan
// filter SQL and the `<Store>MembershipIds` cross-check map, and an id baked
// from any source other than the codec's own would match nothing on read,
// silently. [MembershipIds] is that one source.
//
// # The store does not own the table
//
// `boxer.facts` is provisioned by `factsstore/chstore`'s SetupTable, which is
// its sole DDL author. A store generated here therefore must not run its own
// EnsureTable against a live server; ADR-0184 SD2 suppresses the verb at
// generation time. The emitted DDL file remains the schema of record for
// review and diffing.
//
// # Not vdd-only
//
// The registry is a parameter, not a constant. `boxer.facts` is shared by
// several vocabularies kept apart by tag value (keelson vdd at 1, the runtime
// vocabulary at 2, capmap at 16, per ADR-0168 §SD6), and a store binds
// whichever one its components are written against.
//
// # Generation lane
//
// Driven by a gen-test in the target package, the way `recordstore/example`
// and `recordstore/sharedsection` are driven. No CLI command exists — the
// repo has none for any record store, and the gen test is its proven lane.
package storegen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"iter"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/registry"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
)

// NaturalKeySourceI is the minimal view of a natural-key registry
// [MembershipIds] needs. Declared as a local interface rather than taking
// [registry.HumanReadableNaturalKeyRegistry] so a caller need not thread the
// registry's contract type parameter through, and so a test can supply a
// synthetic vocabulary.
type NaturalKeySourceI interface {
	IterateAll() iter.Seq2[naming.StylableName, registry.RegisteredNaturalKey]
}

// MembershipIds materializes src as the membership name → id map
// [marshallgen.FixedIdsWrapper] takes.
//
// Registries spell natural keys in their own naming style (lower-spinal, for
// every vocabulary in this tree); a component DTO's `lw:` tag spells the same
// membership lowerCamel. This function is the one seam where that conversion
// happens, so the ids a store bakes and the names its DTOs carry cannot
// disagree about which spelling is which.
//
// # Why the conversion is safe, and what is still checked
//
// A registry validates and normalizes at registration: `MustBegin("foo2Bar")`
// stores `foo2-bar`, and a name that is not a valid stylable name is refused
// there rather than here. Every key reaching this function is therefore
// already canonical, and spinal↔lowerCamel is a bijection over canonical
// names — so the documented lossy case (an internal digit-then-letter run,
// which only the upper-case styles mangle) cannot arrive, and two distinct
// keys cannot converge on one lowerCamel spelling. TestMembershipIds_
// RoundTripsEveryRegisteredName pins that over the whole vdd vocabulary,
// which is where ADR-0183 D1 asks for the check.
//
// The two conditions below are consequently *not* reachable through a
// registry. They are checked anyway because [NaturalKeySourceI] is an
// interface — a test, a synthetic vocabulary, or a future non-registry source
// can yield anything — and because the failure they prevent is silent: a
// duplicate map key overwrites, and a zero id matches no row while looking
// like an ordinary number.
//
//   - Two names converging on one lowerCamel spelling is an error naming both.
//   - A zero id is an error. Tag value zero is reserved as invalid
//     (ADR-0106 §SD8), so a zero here means a partially built or zero-value
//     key, never a legitimate assignment.
//
// The snapshot's extent is link-set dependent: a registry populates by
// package-init side effects, so which names appear depends on which
// vocabulary packages the generating binary links. Their *values* do not —
// ids are fixed by registration, not by what else is linked.
func MembershipIds(src NaturalKeySourceI) (ids map[string]uint64, err error) {
	if src == nil {
		err = eh.Errorf("storegen: nil natural-key source")
		return
	}
	ids = make(map[string]uint64)
	// Registered spelling per emitted key, so a collision can name both sides.
	from := make(map[string]naming.StylableName)
	for nk, reg := range src.IterateAll() {
		camel := string(naming.ConvertNameStyle(nk, naming.LowerCamelCase))
		if prev, dup := from[camel]; dup {
			err = eb.Build().Str("membership", camel).
				Str("registered", string(nk)).Str("alsoRegistered", string(prev)).
				Errorf("storegen: two registered names converge on one lowerCamel membership spelling")
			ids = nil
			return
		}
		id := reg.GetId().Value()
		if id == 0 {
			err = eb.Build().Str("naturalKey", string(nk)).
				Errorf("storegen: membership resolves to id zero, which is reserved as invalid (ADR-0106 §SD8)")
			ids = nil
			return
		}
		ids[camel] = id
		from[camel] = nk
	}
	return
}

// Input parameterizes one facts-bound store generation. The schema, table
// name, database and row config are not parameters — they are what makes the
// store facts-bound.
type Input struct {
	// PackageName is the Go package the emitted files declare.
	PackageName string
	// StoreName is the exported name prefix (e.g. "Sysmetrics" yields
	// SysmetricsStore, SysmetricsEntity).
	StoreName string
	// ComponentPaths are the lw:-tagged DTO sources, one kind per file.
	ComponentPaths []string
	// OutDir receives the emitted files.
	OutDir string
	// ImportPath is the generated package's own import path; the store file
	// reaches its internal/lowlevel scaffolding through it.
	ImportPath string
	// Ids is the membership-id snapshot, from [MembershipIds]. Every
	// ref-channel membership of every component must appear in it — a missing
	// name fails generation rather than baking a wrong id.
	Ids map[string]uint64
	// DDL optionally overrides the emitted table clauses, including the
	// ADR-0181 §SD4 skip-index policy. Note that the emitted DDL is a
	// statement of intent here and not applied by the store: `chstore` owns
	// this table's DDL (ADR-0184 SD2/SD5).
	DDL *clickhouse.TableOptions
}

// sharedRA is not an option either.
//
// The read-access classes are a function of the table, so a facts-bound
// store's own copy would duplicate `factsschema/ra` — ~206 KB, which
// ADR-0184 measured and recorded as the follow-up this closes. Binding the
// existing package is strictly better here: it is the same generator over
// the same TableDesc, and it is the package `codec/factswrapper` already
// emits into every keelson wire codec, so a store and a codec decoding the
// same row now share one definition of what its columns are.
//
// Not a parameter because there is no second answer. A facts-bound store
// that emitted its own RA would be duplicating a package it links anyway.
//
// The DML is *not* shared. Its entity-frame control surface is walled by
// the internal/lowlevel import barrier (ADR-0100 SD6), and `factsschema/dml`
// exports that surface for the hand-written facts writers — so binding it
// would drop the wall. That is a decision for ADR-0100, not a default here.
var sharedRA = gen.Scaffold{
	ImportPath: "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/ra",
	Package:    "ra",
	// factsschema/codegen generates under the bare table name, where this
	// generator's default would be "facts_table".
	Stylable: factsschema.TableName,
}

// externallyProvisioned is not an option.
//
// `boxer.facts` is provisioned by `factsstore/chstore`'s SetupTable, which is
// its sole DDL author, so every store generated here is externally
// provisioned by construction: the emitted store carries no EnsureTable, no
// embedded DDLCreate and no DDLTail. A store that cannot run DDL cannot be
// wired up to run it by a later caller who did not read ADR-0184 §SD2, which
// is the point of deciding it here rather than at each call site.
//
// The transport agrees independently: the ClickHouse HTTP interface rejects
// the multi-statement script EnsureTable emits, so a facts-bound store bound
// to `keelson/data/storeexec` could not provision itself even if allowed to.
//
// The DDL *file* is still written — it is the physical schema the store
// decodes positionally, and whoever does provision the table needs it.
const externallyProvisioned = true

// Generate emits the store package against the `boxer.facts` schema.
func (inst Input) Generate() (err error) {
	if len(inst.Ids) == 0 {
		err = eh.Errorf("storegen: empty membership-id snapshot — pass the result of MembershipIds over the vocabulary the components are written against")
		return
	}
	err = inst.checkComponentPackages()
	if err != nil {
		return
	}
	manip, err := factsschema.GetSchemaInManipulator()
	if err != nil {
		err = eh.Errorf("storegen: facts schema: %w", err)
		return
	}
	td, err := manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("storegen: build facts table desc: %w", err)
		return
	}
	err = gen.Input{
		PackageName:    inst.PackageName,
		StoreName:      inst.StoreName,
		TableName:      factsschema.TableName,
		Database:       factsschema.DatabaseName,
		Table:          td,
		RowConfig:      factsschema.TableRowConfig,
		ComponentPaths: inst.ComponentPaths,
		OutDir:         inst.OutDir,
		ImportPath:     inst.ImportPath,
		Wrapper:        marshallgen.FixedIdsWrapper{Ids: inst.Ids},
		SharedRA:       &sharedRA,
		DDL:            inst.DDL,
		// Not a parameter — see the externallyProvisioned doc.
		ExternallyProvisioned: externallyProvisioned,
	}.Generate()
	if err != nil {
		err = eh.Errorf("storegen: generate %s store: %w", inst.StoreName, err)
	}
	return
}

// checkComponentPackages refuses a component whose own package clause differs
// from PackageName.
//
// The emitted per-component codec takes its package from the DTO source, while
// the store file takes its from PackageName, so a mismatch emits two packages
// into one directory. That fails at `go build` with "found packages x and y",
// naming neither the DTO nor the setting that caused it — and it fails only
// once someone compiles the output, which for a freshly generated package can
// be much later. Hence the check here, where both values are in hand.
//
// This is a property of recordstore/gen generally, not of facts-bound stores;
// it lives here because that generator is shared and this milestone does not
// own it. Lifting it is recorded as a candidate in ADR-0184.
func (inst Input) checkComponentPackages() (err error) {
	fset := token.NewFileSet()
	for _, path := range inst.ComponentPaths {
		var f *ast.File
		f, err = parser.ParseFile(fset, path, nil, parser.PackageClauseOnly)
		if err != nil {
			err = eb.Build().Str("component", path).Errorf("storegen: parse component package clause: %w", err)
			return
		}
		if got := f.Name.Name; got != inst.PackageName {
			err = eb.Build().Str("component", path).Str("componentPackage", got).
				Str("packageName", inst.PackageName).
				Errorf("storegen: component declares package %q but the store is generated as package %q — the emitted codec keeps the component's own package clause, so the two would land in one directory as two packages", got, inst.PackageName)
			return
		}
	}
	return
}
