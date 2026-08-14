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
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/docsearchsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
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
	} {
		err = r.Register(e)
		if err != nil {
			return
		}
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
			return constructsql.ExtractExpandPass(lanes, ""), true
		},
	})
	return
}

// RegisterDefaults registers the standard set into passreg.Default.
func RegisterDefaults() error { return RegisterStandard(passreg.Default) }
