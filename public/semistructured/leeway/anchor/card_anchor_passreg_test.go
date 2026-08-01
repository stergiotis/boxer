package anchor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	"github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	"github.com/stretchr/testify/require"
)

// TestDqlPassRegistryGeneration wires the anchor pre-execute chain the way a
// keelson host does (ADR-0108): the standard set (passreg/defaults — the
// identity-macro expander plus the late-bound column-handle resolver factory)
// joins host-scoped entries at explicit orders, and the stage is applied via
// ApplyBestEffortBoundObserved with the anchor resolver as the factory
// binding. card_anchor_passreg.out.md records the registry catalog (the rows
// the keelson('sql_passes') introspection table serves, ADR-0094) and the
// per-pass observation trace for query 7, then the result is asserted
// identical to the directly-composed M1 chain — one registered seam, one
// behaviour (ADR-0108 §SD2).
func TestDqlPassRegistryGeneration(t *testing.T) {
	r := passreg.NewRegistry()

	// Host-scoped entries, at the orders the host convention gives them:
	// canonicalize ahead of the standard set (50, cf. play.RegisterPasses) so
	// later passes consume canonical shapes; comment stripping ahead of that;
	// table qualification between macro expansion (100) and handle resolution
	// (200) so the resolver sees qualified tables.
	for _, e := range []passreg.Entry{
		{
			Pass:        passes.StripComments,
			Stage:       passreg.StagePreExecute,
			Order:       40,
			Description: "strip comments from the shipped body",
			Provenance:  "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes",
		},
		{
			Pass:        passes.CanonicalizeFull(100),
			Stage:       passreg.StagePreExecute,
			Order:       50,
			Description: "rewrite the statement into canonical form",
			Provenance:  "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes",
		},
		{
			Pass:        passes.QualifyTables(`"anchor"`),
			Stage:       passreg.StagePreExecute,
			Order:       150,
			Description: "qualify unqualified table references with the anchor database",
			Provenance:  "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes",
		},
	} {
		require.NoError(t, r.Register(e))
	}
	// The standard set: identsql macro expansion (100) and the
	// ResolveColumnNames factory (200), late-bound to a per-consumer resolver.
	require.NoError(t, defaults.RegisterStandard(r))

	doc := &strings.Builder{}
	doc.WriteString(dqlDocHeader("anchor DQL — the pass registry seam (ADR-0108)", "TestDqlPassRegistryGeneration"))

	doc.WriteString("## catalog — what keelson('sql_passes') serves for this registry (ADR-0094)\n\n")
	doc.WriteString("| stage | order | name | late-bound | description |\n|---|---|---|---|---|\n")
	for _, row := range r.Catalog() {
		fmt.Fprintf(doc, "| %s | %d | `%s` | %v | %s |\n",
			row.Stage, row.Order, row.Name, row.LateBound, row.Description)
	}
	doc.WriteString("\n")

	resolver := NewDqlResolver()
	src := readDqlSource("./card_anchor_dql_query7.sql", t)

	var trace []passreg.ApplyObservation
	out := r.ApplyBestEffortBoundObserved(passreg.StagePreExecute, src, resolver, zerolog.Nop(),
		func(o passreg.ApplyObservation) { trace = append(trace, o) })

	doc.WriteString("## observation trace — query 7 through the stage\n\n")
	doc.WriteString("| order | pass | outcome | changed |\n|---|---|---|---|\n")
	for _, o := range trace {
		fmt.Fprintf(doc, "| %d | `%s` | %s | %v |\n", o.Order, o.Name, o.Outcome, o.Changed)
	}
	fmt.Fprintf(doc, "\n## result\n\n```sql\n%s\n```\n", out)

	// The registered seam and the directly-composed chain (M1) must agree —
	// a pass registered once behaves identically in every consumer of the
	// stage (ADR-0108 §SD2). The M1 chain ends in ValidateGrammar2 (a
	// non-rewriting stage), so equality also proves the registry output is
	// canonical; assert that directly as well.
	direct := src
	for _, st := range DqlPreExecuteStages(resolver, nil) {
		var err error
		direct, err = st.Run(direct)
		require.NoError(t, err)
	}
	require.Equal(t, direct, out, "registry-applied stage must match the directly-composed chain")
	_, err := nanopass.ParseCanonical(out)
	require.NoError(t, err, "registry output must be grammar2-canonical")

	writeFile("./card_anchor_passreg.out.md", doc.String(), t)
}
