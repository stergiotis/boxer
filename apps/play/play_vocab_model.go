package play

import (
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// play_vocab_model.go assembles what a buffer in this build may call, from
// the rosters the declaring packages publish (ADR-0174 §SD3). It is grouped
// by WHERE a function runs, not by which package declares it — that is the
// distinction that predicts how a call fails and what a user does about it
// (§SD1).

// The three populations the panel sections by, spelled locally so the panel
// code reads as it did before the registry owned the type (ADR-0174 §SD1).
const (
	vocabServer = sqlvocab.WhereServer
	vocabClient = sqlvocab.WhereClient
	vocabPlay   = sqlvocab.WhereHost
)

// vocabWhereTitle is the section heading.
func vocabWhereTitle(inst sqlvocab.WhereE) string {
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

// vocabWhereBlurb is the one line under the heading saying what the section's
// membership means for a user.
func vocabWhereBlurb(inst sqlvocab.WhereE) string {
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
	Where  sqlvocab.WhereE
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

// vocabDeclared returns everything this build knows how to offer, in the order
// the wiring site registered it: server rosters first (the population a user
// most often finds absent), then the client macros, then play's own.
//
// It reads the registry RegisterVocabulary populated (ADR-0190 §SD4) rather
// than unioning the rosters itself. The union used to live here, which meant
// the completion engine would have had to repeat it; one registry is what keeps
// a roster from reaching one surface and not the other.
//
// The LW_ID_* family appears in BOTH the server and client sections, and that
// is not a bug: it genuinely is both, installable as UDFs and expanded
// client-side, so listing it once would make one of the two answers wrong
// (§SD1). The registry carries it as two declarations, and a declaration whose
// Where names several populations fans out here for the same reason.
func vocabDeclared(r *sqlvocab.Registry) (out []vocabEntry) {
	fns := r.All()
	out = make([]vocabEntry, 0, len(fns))
	for _, f := range fns {
		for _, where := range sqlvocab.AllWheres {
			if f.Where&where == 0 {
				continue
			}
			out = append(out, vocabEntry{
				Name: f.Name, Params: sqlvocab.ParamNames(f.Params), Doc: f.Doc,
				Where: where, Family: f.Family,
				Declared: true, Available: f.Available,
				Dependencies: f.Dependencies,
			})
		}
	}
	return
}

// The two families holding what the endpoint carries and no roster declares.
// Named constants because the outline reads them back to decide what the panel
// opens closed (vocabExtraFamilies): they are the one population whose size
// this build does not bound, and a literal in two files would drift silently
// into a family that never collapses.
const (
	vocabFamilyUndeclared = "not declared by this build"
	vocabFamilyWithdrawn  = "withdrawn by this build"
)

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
				Name: name, Where: vocabServer, Family: vocabFamilyWithdrawn,
				Declared: false, Available: true,
				Doc: "a spelling this repository shipped and has since withdrawn — reinstalling the surface drops it",
			})
			continue
		}
		out = append(out, vocabEntry{
			Name: name, Where: vocabServer, Family: vocabFamilyUndeclared,
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
