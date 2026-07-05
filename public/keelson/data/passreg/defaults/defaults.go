// Package defaults aggregates the standard passreg entries (ADR-0108
// §SD4). Pass producers stay unaware of the registry — this package
// imports both sides, and hosts call RegisterDefaults once at wiring
// time. The set a process applies is therefore explicit at its wiring
// site, not implicit in the import graph.
package defaults

import (
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
)

// RegisterStandard registers the standard set into r:
//
//   - identsql.ExpandPass (LW_ID_* macros → bit arithmetic, ADR-0106
//     §SD5) at StagePreExecute. chlocal executors have no LW_ID_* UDFs
//     installed, so an unexpanded macro only works against a server
//     that carries them; expanding before execution serves both.
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
	return
}

// RegisterDefaults registers the standard set into passreg.Default.
func RegisterDefaults() error { return RegisterStandard(passreg.Default) }
