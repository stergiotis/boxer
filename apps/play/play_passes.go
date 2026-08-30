package play

import (
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/fs/lading/ladingpolicy"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/hmi/gloss/glosssql"
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/docsearchsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// RegisterHostSql makes every SQL registration a host owes play, against the
// process-wide default registries: the standard pass set, play's own passes,
// the component registry the LW_COMPONENT pass reads, and the vocabulary the
// Vocabulary tab and the completion engine read.
//
// Any host that runs play calls this — the standalone binary for itself, a
// window host for the embedded app (ADR-0108 §SD4). It is one function so the
// set is stated once: a host that assembles it by copying another host's call
// sequence silently lacks whatever the next registration adds, and nothing
// compares the copy against the original.
//
// Best-effort, by the rule each registration carries individually: a failure
// costs the surface that registration feeds — a macro expansion, an
// LW_COMPONENT kind, a listing and its completions — and never blocks a boot.
// The logger receives one warning per failed registration.
func RegisterHostSql(logger zerolog.Logger) {
	if err := passregdefaults.RegisterDefaults(); err != nil {
		logger.Warn().Err(err).Msg("play: standard pass registration failed")
	}
	if err := RegisterPasses(passreg.Default); err != nil {
		logger.Warn().Err(err).Msg("play: host pass registration failed")
	}
	if err := RegisterComponents(componentsql.Default); err != nil {
		logger.Warn().Err(err).Msg("play: component registration failed")
	}
	if err := RegisterVocabulary(sqlvocab.Default); err != nil {
		logger.Warn().Err(err).Msg("play: vocabulary registration failed")
	}
}

// RegisterPasses adds play's host-scoped entries to the shared pre-execute
// stage, beyond the standard set (passreg/defaults): a play-hosting process
// canonicalises every executed statement. Hosts (the standalone binary, the
// carousel) call this at their wiring site next to defaults.RegisterDefaults,
// keeping the process's rewrite set reviewable there (ADR-0108 §SD4).
//
// CanonicalizeFull runs first (Order 50, ahead of the standard entries), so
// the stage's later passes consume canonical shapes — the nanopass contract
// that downstream passes target canonical form. The quoted spellings it
// emits stay matchable and executable: identsql's expander and the
// column-handle resolver compare identifiers through DecodeIdentifier, and
// ClickHouse accepts quoted function and table-function names
// ("left"('a', 1), FROM "numbers"(1)) — verified against the server. The
// converse cost is that later passes' own output (macro expansions) ships
// uncanonicalised; that output is machine-generated and already uniform.
//
// The rewrite is result-schema-neutral: ClickHouse derives result column
// names from the parsed AST, so a sugared spelling and its canonical form
// name their columns identically ([1,2] and array(1,2) both name "[1, 2]";
// quoted function names do not leak into names). Like every entry of the
// stage it rewrites the shipped body only — editor and preview surfaces keep
// the user's original text.
func RegisterPasses(r *passreg.Registry) (err error) {
	return r.Register(passreg.Entry{
		Pass:        passes.CanonicalizeFull(100),
		Stage:       passreg.StagePreExecute,
		Order:       50,
		Description: "rewrite the statement into canonical form (sugar to function calls, canonical quoting and keyword case)",
		Provenance:  "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes",
	})
}

// RegisterComponents publishes the generated stores play can read through
// LW_COMPONENT into a component registry (ADR-0189 §SD7).
//
// Explicit rather than by package init: a registry populated by import has an
// extent that depends on the link set, so the same query would resolve a kind
// in one binary and not another. Listing the stores here keeps what play knows
// about reviewable at the wiring site, next to RegisterPasses.
//
// Only stores whose tables play actually reads belong here. sysmetrics is the
// first — its rows are what ADR-0184's tee writes, and what loadstudy reads.
func RegisterComponents(r *componentsql.Registry) (err error) {
	if err = r.Register(sysmfacts.SysmetricsComponentSQL); err != nil {
		return
	}
	// The lading mount policy (ADR-0198 §SD2) is the one lading kind that
	// lives in boxer.facts rather than behind an fs*() macro; registering it
	// is what lets a statement name a mount by its declared name
	// (ADR-0200 §SD6).
	return r.Register(ladingpolicy.PolicyComponentSQL)
}

// RegisterVocabulary publishes every SQL function this build declares into one
// vocabulary registry (ADR-0190 §SD4).
//
// The rosters say what a family contains; this says where each family runs and
// what to call it — the two facts that are a property of the *host*, not of the
// declaring package. Explicit for the same reason RegisterComponents is: a
// registry populated by import knows about a different vocabulary in one binary
// than in another.
//
// It is the single source both surfaces read. The Vocabulary tab's declared set
// and the completion engine's signatures come from here, so a roster cannot
// reach one and not the other.
//
// The LW_ID_* family is registered twice on purpose — once as the server UDFs
// lwsqlsurface installs, once as the client macro identsql expands — because it
// genuinely is both, and reporting only one of the two would make the other
// answer wrong (ADR-0174 §SD1).
func RegisterVocabulary(r *sqlvocab.Registry) (err error) {
	// The server population IS the declared set (ADR-0171 §SD2), taken from
	// the one place that holds all three families plus the marker.
	for _, f := range lwsqlsurface.DeclaredFunctions() {
		err = r.Register(sqlvocab.Function{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: sqlvocab.WhereServer, Family: vocabFamilyLabel(f.Family), Available: true,
		})
		if err != nil {
			return
		}
	}

	for _, f := range identsql.Functions() {
		err = r.Register(sqlvocab.Function{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: sqlvocab.WhereClient, Family: "identity (ADR-0106)", Available: true,
		})
		if err != nil {
			return
		}
	}

	// The statement- and expression-level macros the pre-execute registry
	// expands. Each names its own spelling, so this list carries no string
	// literals that could drift from the pass that implements them.
	err = r.Register(
		sqlvocab.Function{
			Name: distsql.FuncName, Params: sqlvocab.Exprs("cols…"),
			Doc:   "expand into the distribution result contract — count, mean, quantiles per column (ADR-0161)",
			Where: sqlvocab.WhereClient, Family: "statistics (ADR-0161)", Available: true,
		},
		sqlvocab.Function{
			Name: docsearchsql.FuncName, Params: sqlvocab.Exprs("'query'"),
			Doc:   "search this build's documentation corpus; expands into a UNION over the doc tables (ADR-0164)",
			Where: sqlvocab.WhereClient, Family: "documentation (ADR-0164)", Available: true,
		},
		sqlvocab.Function{
			Name:   ladingsql.FuncEntries,
			Params: []sqlvocab.Param{sqlvocab.Expr("mount"), sqlvocab.Expr("snapshot")},
			Doc:    "table position only: one row per node of a mount's snapshot — the newest complete one unless a snapshot is given (ADR-0198)",
			Where:  sqlvocab.WhereClient, Family: "snapshot store (ADR-0198)", Available: true,
		},
		sqlvocab.Function{
			Name:   ladingsql.FuncBlocks,
			Params: []sqlvocab.Param{sqlvocab.Expr("mount"), sqlvocab.Expr("snapshot")},
			Doc:    "table position only: one row per stored block of a mount's snapshot, with its line numbering (ADR-0198)",
			Where:  sqlvocab.WhereClient, Family: "snapshot store (ADR-0198)", Available: true,
		},
		sqlvocab.Function{
			Name:   ladingsql.FuncSnapshots,
			Params: []sqlvocab.Param{sqlvocab.Expr("mount")},
			Doc:    "table position only: one row per complete snapshot of a mount, with its totals (ADR-0198)",
			Where:  sqlvocab.WhereClient, Family: "snapshot store (ADR-0198)", Available: true,
		},
		sqlvocab.Function{
			Name:   keelsonsql.FuncName,
			Params: []sqlvocab.Param{sqlvocab.Lit("'table'", sqlvocab.DomainIntrospectionTable)},
			Doc:    "table position only: introspection tables served in-process or over HTTP (ADR-0094)",
			Where:  sqlvocab.WhereClient, Family: "introspection (ADR-0094)", Available: true,
		},
	)
	if err != nil {
		return
	}

	// The leeway constructor family (ADR-0181 §SD2/§SD7): client-only by
	// design — expansion mints identifier-position aliases no server-side
	// function can reach.
	err = registerRoster(r, constructsql.Functions(), sqlvocab.WhereClient, "leeway authoring (ADR-0181)", nil)
	if err != nil {
		return
	}
	// The gloss(…) macro (ADR-0186 §SD7): client-only like the constructors,
	// an alias declaring how a column renders.
	for _, f := range glosssql.Functions() {
		err = r.Register(sqlvocab.Function{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: sqlvocab.WhereClient, Family: "glosses (ADR-0186)", Available: true,
		})
		if err != nil {
			return
		}
	}
	// The extraction family is client-expanded like the constructors, but
	// unlike them it expands INTO server-side calls — so it declares those as
	// dependencies and is marked against the same probe (ADR-0181 §SD7,
	// ADR-0174 §SD6).
	err = registerRoster(r, constructsql.ExtractFunctions(), sqlvocab.WhereClient,
		"leeway extraction (ADR-0181)", constructsql.ExtractExpansionDependencies())
	if err != nil {
		return
	}
	// The component family (ADR-0189 §SD8). The two names differ in what they
	// need installed, which is why only one carries dependencies:
	// LW_COMPONENT_FILTER expands to ClickHouse built-ins and runs against any
	// endpoint, while LW_COMPONENT expands to the named-tuple projection, which
	// calls the read-back helpers.
	for _, f := range constructsql.ComponentFunctions() {
		e := sqlvocab.Function{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: sqlvocab.WhereClient, Family: "leeway components (ADR-0189)", Available: true,
		}
		if f.Name == constructsql.NameComponent {
			e.Dependencies = constructsql.ComponentExpansionDependencies()
		}
		err = r.Register(e)
		if err != nil {
			return
		}
	}

	for _, f := range tsFuncs {
		params := make([]sqlvocab.Param, 0, len(f.Args))
		for _, a := range f.Args {
			params = append(params, sqlvocab.Expr(a.Name))
		}
		err = r.Register(sqlvocab.Function{
			Name: f.Name, Params: params, Doc: f.Doc,
			Where: sqlvocab.WhereHost, Family: "time series (ADR-0163)", Available: f.Shipped,
		})
		if err != nil {
			return
		}
	}
	return
}

// registerRoster is the shape the constructsql rosters share: one Where, one
// family label, one dependency list for the whole family.
func registerRoster(r *sqlvocab.Registry, fns []constructsql.Function, where sqlvocab.WhereE, family string, deps []string) (err error) {
	for _, f := range fns {
		err = r.Register(sqlvocab.Function{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: where, Family: family, Available: true, Dependencies: deps,
		})
		if err != nil {
			return
		}
	}
	return
}
