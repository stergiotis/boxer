// Package defaults aggregates the standard passreg entries (ADR-0108
// §SD4). Pass producers stay unaware of the registry — this package
// imports both sides, and hosts call RegisterDefaults once at wiring
// time. The set a process applies is therefore explicit at its wiring
// site, not implicit in the import graph.
package defaults

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
)

// RegisterStandard registers the standard set into r:
//
//   - identsql.ExpandPass (LW_ID_* macros → bit arithmetic, ADR-0106
//     §SD5) at StagePreExecute. chlocal executors have no LW_ID_* UDFs
//     installed, so an unexpanded macro only works against a server
//     that carries them; expanding before execution serves both.
//   - ResolveColumnNames (friendly leeway column handles → physical
//     names, ADR-0116) at StagePreExecute, as a late-bound Factory
//     (ADR-0108 §SD7): it needs a per-consumer schema resolver, so it is
//     realised at the application site. See below.
func RegisterStandard(r *passreg.Registry) (err error) {
	for _, e := range []passreg.Entry{
		{
			Pass:        identsql.ExpandPass,
			Stage:       passreg.StagePreExecute,
			Order:       100,
			Description: "expand LW_ID_* identity-macro calls into bit arithmetic",
			Provenance:  "github.com/stergiotis/boxer/public/identity/identsql",
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
	return
}

// RegisterDefaults registers the standard set into passreg.Default.
func RegisterDefaults() error { return RegisterStandard(passreg.Default) }
