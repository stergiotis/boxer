// Package defaults aggregates the standard passreg entries (ADR-0108
// §SD4). Pass producers stay unaware of the registry — this package
// imports both sides, and hosts call RegisterDefaults once at wiring
// time. The set a process applies is therefore explicit at its wiring
// site, not implicit in the import graph.
package defaults

import (
	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/hmi/gloss/glosssql"
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/docsearchsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// RegisterStandard registers the standard set into r:
//
//   - distsql.ExpandDescriptiveStatistics (ADR-0161 §SD3) at
//     StagePreExecute, ordered before the LW_ID_* expansion so an LW_ID
//     macro inside a descriptiveStatistics argument is replicated into
//     the branches first and still expands.
//   - identsql.ExpandPass (LW_ID_* macros → bit arithmetic, ADR-0106
//     §SD5) at StagePreExecute. chlocal executors have no LW_ID_* UDFs
//     installed, so an unexpanded macro only works against a server
//     that carries them; expanding before execution serves both.
//   - constructsql.ExpandPass (LwConstructExpand, ADR-0181 §SD2/§SD7):
//     LW_PLAIN/LW_TV* constructor calls → `<expr> AS "<physical name>"`.
//     Client-only by design; a cheap marker pre-scan keeps it near-free
//     on queries without authoring calls. Ordered after identsql (100)
//     so an LW_ID_* macro inside a constructor's expression argument is
//     already expanded when the span is kept.
//   - glosssql.ExpandPass (GlossExpand, ADR-0186 §SD7): gloss(expr,
//     'media type', 'key', value…) → `<expr> AS "<label>@<media type>"`,
//     validated against the built-in gloss catalog. Same client-only
//     shape and marker pre-scan as the constructors; ordered after them
//     (140).
//   - ResolveColumnNames (friendly leeway column handles → physical
//     names, ADR-0116) at StagePreExecute, as a late-bound Factory
//     (ADR-0108 §SD7): it needs a per-consumer schema resolver, so it is
//     realised at the application site. See below.
//
// Deliberately NOT here: passes.ExposeSelectionConditions (ADR-0121). It changes a
// query's result schema, so it is opt-in per host rather than standard —
// play applies it from buildResidual behind a UI toggle, default off.
//
// Also deliberately NOT here: passes.CanonicalizeFull. It is result-schema-
// neutral but rewrites the whole statement, and pipelines that already
// canonicalise themselves (text2sql, genbuildertest) must not inherit a
// second copy via the registry. Hosts that want executed statements
// canonical register it at their wiring site — see play.RegisterPasses,
// which orders it ahead of the standard entries so they consume canonical
// shapes.
func RegisterStandard(r *passreg.Registry) (err error) {
	for _, e := range []passreg.Entry{
		{
			Pass:        distsql.ExpandDescriptiveStatistics,
			Stage:       passreg.StagePreExecute,
			Order:       75,
			Description: "expand descriptiveStatistics(cols…) into the ADR-0161 distribution result contract",
			Provenance:  "github.com/stergiotis/boxer/public/analytics/stats/distsql",
		},
		{
			// Ordered between the distribution macro (75) and identsql
			// (100). Nothing interacts — the expansion emits neither
			// macro family — the fixed slot is for determinism alone.
			// The keelson('…') references it emits are resolved later,
			// at the executor boundary (keelsonsql Bare/URL pass).
			Pass:        docsearchsql.ExpandPass,
			Stage:       passreg.StagePreExecute,
			Order:       80,
			Description: "expand docsearch('query') into the ADR-0164 documentation search UNION",
			Provenance:  "github.com/stergiotis/boxer/public/keelson/runtime/introspect/docsearchsql",
		},
		{
			Pass:        identsql.ExpandPass,
			Stage:       passreg.StagePreExecute,
			Order:       100,
			Description: "expand LW_ID_* identity-macro calls into bit arithmetic",
			Provenance:  "github.com/stergiotis/boxer/public/identity/identsql",
		},
		{
			// Ordered after identsql (100) and before handle resolution
			// (200). A minted alias is a physical leeway name, which the
			// handle pass never touches (a handle is exactly one colon), so
			// the 130 slot is for determinism, not correctness.
			Pass:        constructsql.ExpandPass,
			Stage:       passreg.StagePreExecute,
			Order:       130,
			Description: "expand LW_PLAIN/LW_TV* constructor calls into aliased expressions minting physical leeway column names",
			Provenance:  "github.com/stergiotis/boxer/public/semistructured/leeway/constructsql",
		},
		{
			// gloss(…) (ADR-0186 §SD7) — the display-side sibling of the
			// constructors: an alias declaring how a column renders, validated
			// against the built-in gloss catalog. Ordered after them (140) for
			// determinism only; neither emits the other's calls. An Entry, not
			// a Factory: the unbound /query path must expand it too, else the
			// call would reach the server as an unknown function.
			Pass:        glosssql.ExpandPass(nil),
			Stage:       passreg.StagePreExecute,
			Order:       140,
			Description: "expand gloss(expr, 'media type', 'key', value…) into a `label@media type;k=v` alias, validated against the gloss catalog",
			Provenance:  "github.com/stergiotis/boxer/public/hmi/gloss/glosssql",
		},
	} {
		err = r.Register(e)
		if err != nil {
			return
		}
	}

	// The lading store's three table-function macros (ADR-0198 §SD7):
	// fs(mount), fsdata(mount) and fssnap(mount) → a subquery over the store's
	// tables. A Factory and not an Entry, unlike the gloss and constructor
	// macros, because expanding one is an authorisation decision: which mounts
	// a caller may read is a MountVisibilityI, and a nil one refuses every
	// mount. Registered here rather than per host so it shows in
	// keelson('sql_passes') and behaves identically wherever it is bound; a
	// host that binds no visibility declines it, and the call reaches the
	// server as an unknown table function — which is the honest outcome, and
	// what "no policy was stated" should look like.
	//
	// The probe pass (empty Config, never Run) sources Name and Properties
	// from the real pass so they cannot drift from Build's output.
	ladingProbe := ladingsql.ExpandPass(ladingsql.Config{})
	err = r.RegisterFactory(passreg.Factory{
		Name:        ladingProbe.Name,
		Stage:       passreg.StagePreExecute,
		Order:       145,
		Description: "expand fs(mount) / fsdata(mount) / fssnap(mount) into a subquery over the lading snapshot store",
		Provenance:  "github.com/stergiotis/boxer/public/fs/lading/ladingsql",
		Properties:  ladingProbe.Properties,
		Build: func(binding any) (nanopass.Pass, bool) {
			vis, ok := binding.(ladingsql.MountVisibilityI)
			if !ok {
				return nanopass.Pass{}, false
			}
			return ladingsql.ExpandPass(ladingsql.Config{Visibility: vis}), true
		},
	})
	if err != nil {
		return
	}

	// Friendly leeway column-handle resolution (`geoPoint:pointLat`,
	// `symbol:*` → physical names) is a Factory rather than an Entry because
	// it needs a per-consumer ColumnResolverI — play binds its live
	// system.columns probe (ADR-0116 §SD6). It is realised at the application
	// site via ApplyBestEffortBound; a consumer without such a binding (the
	// /query path, which uses ApplyBestEffort) simply skips it. Registering it
	// here — not per client — is what makes it show in keelson('sql_passes')
	// and behave identically across StagePreExecute consumers. Ordered after
	// identsql (100 → 200) so it resolves already-macro-expanded SQL.
	//
	// The probe pass (nil resolver, never Run) sources the catalog's Name and
	// Properties from the real pass, so they cannot drift from Build's output.
	probe := passes.ResolveColumnNames(nil, "", nil)
	err = r.RegisterFactory(passreg.Factory{
		Name:        probe.Name,
		Stage:       passreg.StagePreExecute,
		Order:       200,
		Description: "resolve friendly leeway column handles to physical names",
		Provenance:  "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes",
		Properties:  probe.Properties,
		Build: func(binding any) (nanopass.Pass, bool) {
			resolver, ok := binding.(passes.ColumnResolverI)
			if !ok {
				return nanopass.Pass{}, false
			}
			return passes.ResolveColumnNames(resolver, "", nil), true
		},
	})
	if err != nil {
		return
	}

	// LW_COMPONENT / LW_COMPONENT_FILTER (ADR-0189 §SD3) is an Entry rather
	// than a Factory: unlike the schema-bound passes around it, what it needs
	// is a registry a host populates once at its wiring site (§SD7), not a
	// per-consumer binding. A host that registered no store gets a pass that
	// refuses any component call by name — which is the right answer, since
	// the alternative is a call that expands to nothing.
	//
	// Ordered at 110: after identsql (100), so an LW_ID_* macro around a
	// component call is already expanded, and before the extraction family
	// (120) so one statement may mix both. Neither emits the other's calls,
	// so the order is for determinism rather than dependency.
	err = r.Register(passreg.Entry{
		Pass:        constructsql.ComponentExpandPass(componentsql.Default, ""),
		Stage:       passreg.StagePreExecute,
		Order:       110,
		Description: "expand LW_COMPONENT/LW_COMPONENT_FILTER into a component's projection and conformance filter",
		Provenance:  "github.com/stergiotis/boxer/public/semistructured/leeway/constructsql",
	})
	if err != nil {
		return
	}

	// LW_GET/_NULL/_LIST extraction sugar (ADR-0181 §SD3) is a Factory for
	// the same reason handle resolution is: it needs a per-consumer schema
	// to turn a section name into lanes. Ordered at 120 — after identsql
	// (100), so an LW_ID_* macro in a surrounding expression is already
	// expanded, and before the constructors (130), which is arbitrary in the
	// sense that neither emits the other's calls, but fixed for determinism.
	//
	// Unlike the constructors, what this expands INTO is server-dependent:
	// the pack-form renderer calls the read-back helper family. That is the
	// dependency ADR-0174 §SD6 marks.
	extractProbe := constructsql.ExtractExpandPass(nil, "")
	err = r.RegisterFactory(passreg.Factory{
		Name:        extractProbe.Name,
		Stage:       passreg.StagePreExecute,
		Order:       120,
		Description: "expand LW_GET/LW_GET_NULL/LW_GET_LIST into leeway locate-and-extract expressions",
		Provenance:  "github.com/stergiotis/boxer/public/semistructured/leeway/constructsql",
		Properties:  extractProbe.Properties,
		Build: func(binding any) (nanopass.Pass, bool) {
			lanes, ok := binding.(constructsql.LaneSourceI)
			if !ok {
				return nanopass.Pass{}, false
			}
			// The membership registry is optional and asserted off the SAME
			// binding (ADR-0171 §SD4): a host that carries one lets a ref
			// channel take a name, one that does not keeps the id form.
			// Absent, this degrades rather than declining — the schema is
			// what the pass cannot work without.
			ids, _ := binding.(constructsql.MembershipIdsI)
			return constructsql.ExtractExpandPassWithIds(lanes, ids, ""), true
		},
	})
	if err != nil {
		return
	}

	// Target-adopting constructor expansion (ADR-0181 §SD8 M2) is a Factory
	// for the same reason the two above are: adopting an INSERT target's
	// naming needs a per-consumer schema. Ordered at 129 — one before the
	// unbound LwConstructExpand entry (130), so on a bound host THIS pass
	// consumes every constructor call and the entry's marker scan then finds
	// nothing; on an unbound host the factory declines and the entry mints
	// with fresh-table defaults exactly as before. Statements without an
	// INSERT wrapper expand identically under either.
	targetProbe := constructsql.ExpandPassWithTargetAdoption(nil, "")
	err = r.RegisterFactory(passreg.Factory{
		Name:        targetProbe.Name,
		Stage:       passreg.StagePreExecute,
		Order:       129,
		Description: "constructor calls adopt a resolved INSERT target's naming — segments, aspects and spelling",
		Provenance:  "github.com/stergiotis/boxer/public/semistructured/leeway/constructsql",
		Properties:  targetProbe.Properties,
		Build: func(binding any) (nanopass.Pass, bool) {
			schema, ok := binding.(constructsql.TargetSchemaI)
			if !ok {
				return nanopass.Pass{}, false
			}
			return constructsql.ExpandPassWithTargetAdoption(schema, ""), true
		},
	})
	return
}

// RegisterDefaults registers the standard set into passreg.Default.
func RegisterDefaults() error { return RegisterStandard(passreg.Default) }
