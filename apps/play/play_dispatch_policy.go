package play

import (
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
)

// play_dispatch_policy.go — the resolver boxer ships for its own two
// endpoints (ADR-0141). It knows one fact worth routing on: whether a
// statement reaches into the introspection plane's namespace through the
// keelson() macro. Live in-process state exists only in this process (R2),
// so a query that names it has exactly one place it can run, and today the
// user has to know that and move the switcher by hand.
//
// Everything beyond that fact is somebody else's policy. There are no
// placement maps, no cluster rosters, and no balancing here — a system that
// needs those replaces the resolver.

// ignoredDatabases are database qualifiers that say nothing about
// placement. `system` resolves on either engine (the introspection plane
// answers through clickhouse-local, which has its own system tables), and
// `keelson` as a *database* name is an ordinary qualified table that merely
// shares a word with the macro — it is not a macro reference and must not
// be read as one.
var ignoredDatabases = map[string]struct{}{
	"system":  {},
	"keelson": {},
}

// maxNamedTables caps how many table names a reason string lists before it
// elides. The reason is read in a toolbar, not a log.
const maxNamedTables = 3

// keelsonResolver routes a read that names only keelson tables to the
// in-process introspection plane, leaves everything else on the pinned
// endpoint, and refuses the one case no endpoint can serve.
type keelsonResolver struct {
	// localEndpoint reports the in-process introspection /query URL, or ""
	// when this process publishes none. Injected so the policy is testable
	// without standing up an HTTP table source; nil means the real one.
	localEndpoint func() (url string)
}

var _ endpointResolverI = keelsonResolver{}

func (inst keelsonResolver) local() (url string) {
	if inst.localEndpoint != nil {
		url = inst.localEndpoint()
		return
	}
	url = introspect.LocalQueryEndpoint()
	return
}

func (inst keelsonResolver) resolve(residual string, base string, _ string) (dec dispatchDecision) {
	// R5, default-deny: only a provable read is ever moved. A mutation
	// addresses a deliberately chosen host — never "wherever a router felt
	// like sending it" — and an unknown kind is treated exactly as a
	// mutation, so both stay where the user pointed.
	kind := analysis.ClassifyStatementKind(residual)
	if kind != analysis.KindReadOnly {
		dec = dispatchDecision{
			targetURL: base,
			class:     dispatchClassManual,
			reason:    "not provably read-only (" + kind.String() + "), so not auto-routed",
		}
		return
	}

	macros := keelsonsql.References(residual)
	plain := plainTables(residual)

	switch {
	case len(macros) > 0 && len(plain) > 0:
		// Both sides named. The keelson side exists only in this process and
		// the plain side only on the server, so there is no endpoint that
		// could answer — say so rather than picking one and failing obscurely.
		dec = dispatchDecision{
			class: dispatchClassRefused,
			reason: "names keelson tables (" + nameList(macros) + ") and plain tables (" +
				nameList(plain) + "); no endpoint serves both",
		}
	case len(macros) > 0:
		ep := inst.local()
		if ep == "" {
			// The wall is still a wall — we just cannot get past it here.
			dec = dispatchDecision{
				targetURL: base,
				class:     dispatchClassManual,
				reason:    "names keelson tables (" + nameList(macros) + "), but this process publishes no introspection endpoint",
			}
			return
		}
		dec = dispatchDecision{
			targetURL: ep,
			class:     dispatchClassIntrospection,
			reason:    "names only keelson tables (" + nameList(macros) + ")",
		}
	default:
		// Plain-only, table-less, and unparseable all land here: nothing
		// says the introspection plane, so nothing moves.
		dec = dispatchDecision{
			targetURL: base,
			class:     dispatchClassManual,
			reason:    "names no keelson tables",
		}
	}
	return
}

// plainTables returns the ordinary table references in sql — the ones that
// live on a server rather than in this process — deduplicated in
// first-appearance order.
//
// It resolves through scopes rather than walking table identifiers,
// because a CTE reference is syntactically indistinguishable from a table
// and would otherwise be counted as one: `WITH e AS (SELECT * FROM
// keelson('env')) SELECT * FROM e` names no plain table at all, and reading
// `e` as one would turn every CTE over keelson data into a refusal.
// Subqueries and table functions are likewise not tables, and the
// databases in ignoredDatabases carry no placement meaning.
//
// Best-effort, in step with the rest of the client-side path: unparseable
// SQL returns nil, which routes to the pinned endpoint.
func plainTables(sql string) (names []string) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return
	}
	scopes, err := nanopass.BuildScopes(pr, "")
	if err != nil {
		return
	}
	seen := make(map[string]struct{})
	for _, scope := range nanopass.FlattenScopes(scopes) {
		for _, ts := range scope.Tables {
			if ts.IsCTE || ts.IsSubquery || ts.IsFunction {
				continue
			}
			if _, ignored := ignoredDatabases[ts.Database]; ignored {
				continue
			}
			name := ts.Table
			if ts.Database != "" {
				name = ts.Database + "." + ts.Table
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	return
}

// nameList renders table names for a reason a human reads, eliding the tail
// of a long list.
func nameList(names []string) (out string) {
	if len(names) > maxNamedTables {
		out = strings.Join(names[:maxNamedTables], ", ") + ", …"
		return
	}
	out = strings.Join(names, ", ")
	return
}

// sealedNames returns the sealed datasets a statement names, by asking the
// local introspection plane about each keelson() reference (ADR-0145 §SD3).
//
// A registry lookup, not a heuristic over SQL text: a name either resolves
// to a sealed provider on this process's plane or it does not. The residual
// is the right input because the ad-hoc alias→handle rewrite has already
// run by then (ADR-0134 §SD4), so what is asked about is the handle the
// server would receive.
func sealedNames(residual string) (names []string) {
	for _, ref := range keelsonsql.References(residual) {
		if introspect.IsLocalSealed(ref) {
			names = append(names, ref)
		}
	}
	sort.Strings(names)
	return
}
