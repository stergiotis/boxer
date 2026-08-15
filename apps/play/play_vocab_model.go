package play

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/analytics/stats/distsql"
	"github.com/stergiotis/boxer/public/hmi/gloss/glosssql"
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/docsearchsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/keelsonsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// play_vocab_model.go assembles what a buffer in this build may call, from
// the rosters the declaring packages publish (ADR-0174 §SD3). It is grouped
// by WHERE a function runs, not by which package declares it — that is the
// distinction that predicts how a call fails and what a user does about it
// (§SD1).

// vocabWhereE is the population a function belongs to.
type vocabWhereE uint8

const (
	// vocabServer is a SQL UDF: it exists only where it was installed, and
	// its absence is a provisioning fact.
	vocabServer vocabWhereE = iota
	// vocabClient is a nanopass macro expanded before the statement ships,
	// so it works against any endpoint — including one carrying no UDFs.
	vocabClient
	// vocabPlay is computed in play and never reaches a server.
	vocabPlay
)

func (inst vocabWhereE) String() string {
	switch inst {
	case vocabServer:
		return "server"
	case vocabClient:
		return "client"
	case vocabPlay:
		return "play"
	}
	return "unknown"
}

// title is the section heading.
func (inst vocabWhereE) title() string {
	switch inst {
	case vocabServer:
		return "Server — installed on this endpoint"
	case vocabClient:
		return "Client — expanded before the statement ships"
	case vocabPlay:
		return "play — computed here, never sent"
	}
	return "Unknown"
}

// blurb is the one line under the heading saying what the section's
// membership means for a user.
func (inst vocabWhereE) blurb() string {
	switch inst {
	case vocabServer:
		return "SQL user-defined functions. A missing one needs provisioning, not a different query."
	case vocabClient:
		return "Rewritten into ordinary SQL before it leaves play, so these work against any endpoint."
	case vocabPlay:
		return "Executed in play over the rows a sub-query returns; the server never sees the name."
	}
	return ""
}

// vocabEntry is one callable name as the panel presents it.
type vocabEntry struct {
	Name   string
	Params []string
	Doc    string
	Where  vocabWhereE
	// Family groups entries within a section and names their provenance.
	Family string
	// Declared is true when this build declares the function, which is what
	// makes "installed?" a meaningful question. Server functions found on the
	// endpoint that no roster claims are listed with Declared false.
	Declared bool
	// Available is false for a name the vocabulary reserves but does not
	// implement — it refuses rather than travelling (the `ts*` motif names).
	Available bool
	// Installed is meaningful for server entries only, and only once the
	// probe has answered — see vocabMarkInstalled, which is the only writer.
	Installed bool
	// Dependencies are server-side functions this entry's CLIENT-side
	// expansion emits (ADR-0174 §SD6). A client macro is portable only when
	// what it expands into is: LW_GET is expanded here and works on any
	// endpoint as a name, but the expression it becomes calls the read-back
	// family, so an endpoint without that family fails on the expansion.
	Dependencies []string
	// MissingDeps are the Dependencies the probe did not find. Written by
	// vocabMarkInstalled, and only when the probe has answered — an
	// unanswered probe leaves it empty, which reads as "not known" and not
	// as "all present".
	MissingDeps []string
}

// vocabMarkInstalled stamps the server entries with what the endpoint
// actually carries. Deliberately a separate pass over the declared roster
// rather than a parameter to vocabDeclared: the roster is a property of the
// build and the same for every endpoint, so computing it once and stamping it
// per endpoint keeps those two facts from being conflated in one function.
//
// installed nil (probe unanswered) leaves every entry false, which the render
// reads together with its own ready flag — false here means "not known",
// never "absent".
func vocabMarkInstalled(entries []vocabEntry, installed map[string]string) {
	if len(installed) == 0 {
		return
	}
	for i := range entries {
		// Expansion dependencies are marked for every population, not just
		// the server one: they are the reason a CLIENT entry can fail on an
		// endpoint, which is precisely the case a per-name "installed?"
		// column cannot express (ADR-0174 §SD6).
		entries[i].MissingDeps = nil
		for _, dep := range entries[i].Dependencies {
			if _, ok := installed[dep]; !ok {
				entries[i].MissingDeps = append(entries[i].MissingDeps, dep)
			}
		}
		if entries[i].Where != vocabServer {
			continue
		}
		_, ok := installed[entries[i].Name]
		entries[i].Installed = ok
	}
}

// call renders the entry as a call template for the Insert action.
func (inst vocabEntry) call() string {
	return inst.Name + "(" + strings.Join(inst.Params, ", ") + ")"
}

// vocabFamilyLabel names the declaring family for the panel's Family
// column, with the ADR a reader follows to find out what the family is for.
func vocabFamilyLabel(fam lwsqlsurface.FamilyE) (label string) {
	switch fam {
	case lwsqlsurface.FamilyPack:
		label = "co/ragged pack (ADR-0162)"
	case lwsqlsurface.FamilyReadback:
		label = "leeway read-back (ADR-0066)"
	case lwsqlsurface.FamilyIdentity:
		label = "identity (ADR-0106) — also expanded client-side"
	case lwsqlsurface.FamilySurface:
		label = "surface marker (ADR-0171)"
	default:
		label = "leeway SQL surface"
	}
	return
}

// vocabDeclared returns everything this build knows how to offer, in
// presentation order: server rosters first (the population a user most often
// finds absent), then the client macros, then play's own.
//
// The LW_ID_* family appears in BOTH the server and client sections, and that
// is not a bug: it genuinely is both, installable as UDFs and expanded
// client-side, so listing it once would make one of the two answers wrong
// (§SD1).
func vocabDeclared() (out []vocabEntry) {
	out = make([]vocabEntry, 0, 48)

	// The server population IS the declared set (ADR-0171 §SD2), taken from
	// the one place that holds all three families plus the marker. Looping
	// the three rosters separately here is what let the marker be declared
	// in one list and probed against another; a name the surface installs
	// and this panel does not know would show up as an "extra" on a
	// correctly provisioned server.
	for _, f := range lwsqlsurface.DeclaredFunctions() {
		out = append(out, vocabEntry{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: vocabServer, Family: vocabFamilyLabel(f.Family),
			Declared: true, Available: true,
		})
	}

	for _, f := range identsql.Functions() {
		out = append(out, vocabEntry{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: vocabClient, Family: "identity (ADR-0106)",
			Declared: true, Available: true,
		})
	}
	// The statement- and expression-level macros the pre-execute registry
	// expands. Each names its own spelling, so this list carries no string
	// literals that could drift from the pass that implements them.
	out = append(out,
		vocabEntry{
			Name: distsql.FuncName, Params: []string{"cols…"},
			Doc:   "expand into the distribution result contract — count, mean, quantiles per column (ADR-0161)",
			Where: vocabClient, Family: "statistics (ADR-0161)", Declared: true, Available: true,
		},
		vocabEntry{
			Name: docsearchsql.FuncName, Params: []string{"'query'"},
			Doc:   "search this build's documentation corpus; expands into a UNION over the doc tables (ADR-0164)",
			Where: vocabClient, Family: "documentation (ADR-0164)", Declared: true, Available: true,
		},
		vocabEntry{
			Name: keelsonsql.FuncName, Params: []string{"'table'"},
			Doc:   "table position only: introspection tables served in-process or over HTTP (ADR-0094)",
			Where: vocabClient, Family: "introspection (ADR-0094)", Declared: true, Available: true,
		},
	)
	// The leeway constructor family (ADR-0181 §SD2/§SD7): client-only by
	// design — expansion mints identifier-position aliases no server-side
	// function can reach.
	for _, f := range constructsql.Functions() {
		out = append(out, vocabEntry{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: vocabClient, Family: "leeway authoring (ADR-0181)",
			Declared: true, Available: true,
		})
	}
	// The gloss(…) macro (ADR-0186 §SD7): client-only like the constructors,
	// an alias declaring how a column renders.
	for _, f := range glosssql.Functions() {
		out = append(out, vocabEntry{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: vocabClient, Family: "glosses (ADR-0186)",
			Declared: true, Available: true,
		})
	}
	// The extraction family is client-expanded like the constructors, but
	// unlike them it expands INTO server-side calls — so it declares those
	// as dependencies and is marked against the same probe (ADR-0181 §SD7,
	// ADR-0174 §SD6). This is the entry the dependency column exists for.
	for _, f := range constructsql.ExtractFunctions() {
		out = append(out, vocabEntry{
			Name: f.Name, Params: f.Params, Doc: f.Doc,
			Where: vocabClient, Family: "leeway extraction (ADR-0181)",
			Declared: true, Available: true,
			Dependencies: constructsql.ExtractExpansionDependencies(),
		})
	}

	for _, f := range tsFuncs {
		params := make([]string, 0, len(f.Args))
		for _, a := range f.Args {
			params = append(params, a.Name)
		}
		out = append(out, vocabEntry{
			Name: f.Name, Params: params, Doc: f.Doc,
			Where: vocabPlay, Family: "time series (ADR-0163)",
			Declared: true, Available: f.Shipped,
		})
	}
	return
}

// vocabExtras lists functions the endpoint carries that no roster in this
// build declares — a hand-installed helper, or a spelling left behind by a
// rename this build no longer performs. They are the other half of the drift
// question ADR-0171 §SD2 asks, and the only half a client can see today.
//
// installed nil (probe unanswered) yields nothing, never a claim.
func vocabExtras(installed map[string]string, declared []vocabEntry) (out []vocabEntry) {
	if len(installed) == 0 {
		return
	}
	known := make(map[string]struct{}, len(declared))
	for _, e := range declared {
		if e.Where == vocabServer {
			known[e.Name] = struct{}{}
		}
	}
	// A name this repository shipped and withdrew is not an unknown extra:
	// it has a known fix, and the next install drops it. Reporting it as
	// "not in any roster this build carries" sends a reader to investigate
	// a name we are about to delete ourselves — and on a pre-surface
	// endpoint it would contradict the panel's own skew line, which names
	// the retired marker as the reason the endpoint is pre-surface
	// (ADR-0171 §SD2 splits the two for the same reason).
	retired := make(map[string]struct{}, 64)
	for _, name := range lwsqlsurface.RetiredNames() {
		retired[name] = struct{}{}
	}
	for name := range installed {
		if _, ok := known[name]; ok {
			continue
		}
		if _, wasOurs := retired[name]; wasOurs {
			out = append(out, vocabEntry{
				Name: name, Where: vocabServer, Family: "withdrawn by this build",
				Declared: false, Available: true,
				Doc: "a spelling this repository shipped and has since withdrawn — reinstalling the surface drops it",
			})
			continue
		}
		out = append(out, vocabEntry{
			Name: name, Where: vocabServer, Family: "not declared by this build",
			Declared: false, Available: true,
			Doc: "on this endpoint but not in any roster this build carries — see the Docs tab for its definition",
		})
	}
	sortVocabByName(out)
	return
}

// sortVocabByName orders entries alphabetically, case-insensitively so a
// server mixing spellings does not split into two runs.
func sortVocabByName(entries []vocabEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && strings.ToLower(entries[j].Name) < strings.ToLower(entries[j-1].Name); j-- {
			entries[j], entries[j-1] = entries[j-1], entries[j]
		}
	}
}

// vocabSurfaceSkew describes the surface revision the endpoint carries
// against the one this build writes (ADR-0171 §SD2). ok=false when there is
// nothing to say — the probe has not answered, or neither marker is present
// (which the per-function "missing" marks already report, more precisely).
//
// The pre-surface case is its own sentence rather than a missing marker.
// A server carrying the retired LW_PACK_VERSION is not broken and not
// empty: it was provisioned by a build from before the three families
// shared one marker, and every function it carries still works. What it
// cannot tell us is whether the other two families are there — which is
// the whole reason the surface marker exists, so saying "unknown" would
// throw away the one thing this reading does establish.
func vocabSurfaceSkew(surface int, preSurface int) (line string, ok bool) {
	if surface < 0 {
		if preSurface < 0 {
			return "", false
		}
		return "pre-surface endpoint: pack v" + strconv.Itoa(preSurface) +
			", no surface marker — reinstall to reconcile all three families", true
	}
	if surface == lwsqlsurface.Version {
		return "surface v" + strconv.Itoa(surface) + " — matches this build", true
	}
	if surface > lwsqlsurface.Version {
		// Ahead, not behind: the endpoint was provisioned by a NEWER build,
		// so the definitions are newer than this client's, not older. The
		// marker number went 4 → 1 when the surface replaced the pack's, so
		// this is not a hypothetical direction.
		return "surface v" + strconv.Itoa(surface) + " on this endpoint, v" + strconv.Itoa(lwsqlsurface.Version) +
			" in this build — the endpoint was provisioned by a newer build", true
	}
	return "surface v" + strconv.Itoa(surface) + " on this endpoint, v" + strconv.Itoa(lwsqlsurface.Version) +
		" in this build — the names below may resolve to older definitions", true
}
