package lwsqlsurface

import (
	"context"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Conn is the ClickHouse access Install and Reconcile need. chclient.Client
// satisfies it; the interface keeps this package free of a client
// dependency.
type Conn interface {
	Exec(ctx context.Context, sql string) (err error)
	Query(ctx context.Context, sql string) (body io.ReadCloser, err error)
}

// featureProbeName is created and dropped by the install-time probe; it is
// never part of the declared set.
//
// Deliberately OUTSIDE the `LW_` namespace: Reconcile reports every
// undeclared `LW\_%` function, and would otherwise race a concurrent
// install's probe and report it as drift. The probe is transient, so it must
// not look like surface state.
const featureProbeName = "leewaySurfaceFeatureProbe"

// Install reconciles the whole surface onto the server (ADR-0171 §SD2).
// Idempotent: every statement is CREATE OR REPLACE. Steps, each failing
// loudly rather than degrading silently:
//
//  1. feature probe — lambda-parameter SQL UDFs (the floor was verified on
//     ClickHouse 26.7; older servers are untested),
//  2. collision check — no declared name may resolve to a non-UDF server
//     function (e.g. a builtin arriving with a server upgrade),
//  3. CREATE OR REPLACE the three families in dependency order,
//  4. verify LW_SURFACE_VERSION() reports this build's Version,
//  5. drop the names this repository has retired (RetiredNames).
//
// Step 5 runs last, and only once the new roster has verified: until then
// the server's existing functions are the only working ones, and dropping
// them ahead of a failed install would take a working server down. It is
// also the one step that may fail without failing Install — see dropRetired.
//
// Installing all three families together is what makes the marker mean what
// the package doc says it means. A caller that wants one family provisions
// it from that family's own SQL and does not get a marker.
func Install(ctx context.Context, conn Conn) (err error) {
	err = probeLambdaSupport(ctx, conn)
	if err != nil {
		return
	}
	err = checkCollisions(ctx, conn)
	if err != nil {
		return
	}
	for _, stmt := range Statements() {
		e := conn.Exec(ctx, stmt)
		if e != nil {
			err = eb.Build().Str("statement", firstLine(stmt)).Errorf("install: %w", e)
			return
		}
	}
	got, qErr := queryTrimmed(ctx, conn, "SELECT "+VersionFunctionName+"()")
	if qErr != nil {
		err = eh.Errorf("post-install verify: %w", qErr)
		return
	}
	want := strconv.Itoa(Version)
	if got != want {
		err = eb.Build().Str("got", got).Str("want", want).Errorf("version marker mismatch after install")
		return
	}
	dropRetired(ctx, conn)
	return
}

// ReconcileModeE selects what Reconcile does about the drift it finds.
type ReconcileModeE uint8

const (
	// ReconcileReport finds drift and changes nothing. The default, and
	// what an automatic caller should use.
	ReconcileReport ReconcileModeE = iota + 1
	// ReconcileDrop additionally drops the undeclared `LW\_%` functions the
	// report names.
	ReconcileDrop
)

// Report is what a server carries, against what this build declares.
type Report struct {
	// Undeclared are leeway-namespaced functions on the server that no
	// roster in this build declares AND that this repository never shipped
	// — a hand-installed extra, or a spelling from a fork. Sorted by name.
	//
	// These are the only names ReconcileDrop removes, because they are the
	// only ones whose owner is unknown.
	Undeclared []string
	// Retired are names on the server that this repository shipped and
	// since withdrew. Reported apart from Undeclared because the two need
	// different actions: a retired name has a known fix — Install drops it
	// — while an undeclared one is somebody's, and the tool that cannot
	// tell them apart either nags about the fixable case or offers to
	// delete the unknown one. Sorted by name.
	//
	// Only the namespaced generations appear here: this listing asks the
	// server for `LW\_%`, so the pre-namespace spellings (`coLookup`,
	// `CO_GATHER`) are invisible to it. Install drops those anyway — it
	// issues an unconditional DROP per retired name rather than diffing a
	// listing — so they are removed without ever being reported.
	Retired []string
	// Missing are declared functions the server does not carry. Sorted by
	// name. This is the trial's failure in one field: three functions
	// absent, and nothing detecting it.
	Missing []string
	// Dropped is what ReconcileDrop removed; always empty under
	// ReconcileReport.
	Dropped []string
	// ServerVersion is the revision the server's marker reports, -1 when it
	// carries no surface marker. A server provisioned before the surface
	// marker existed reports -1 while being perfectly functional — read
	// PreSurfaceVersionFunctionName to tell that case from an empty server.
	ServerVersion int
	// MarkerUnreadable is true when the marker IS installed but its value
	// could not be read as a revision — a hand-edited body, or a transport
	// returning something other than the bare integer.
	//
	// It exists because ServerVersion's -1 already means "no marker", which
	// is the opposite fact: without this flag a server whose marker cannot
	// be parsed is reported as having none, sending someone to install over
	// a server whose real problem is that its marker was edited.
	MarkerUnreadable bool
}

// InSync reports whether the server carries what this build declares:
// nothing missing, no spelling this repository withdrew, and the marker at
// this Version.
//
// Undeclared is deliberately NOT part of the verdict. A name nobody here
// declared is somebody else's — that is the whole basis for reporting it
// rather than dropping it — and folding it in would make a deployment check
// permanently red on any shared server, with the only remedy being to delete
// a neighbour's function. Callers that want to act on extras read Undeclared
// directly.
func (inst Report) InSync() (ok bool) {
	ok = len(inst.Retired) == 0 && len(inst.Missing) == 0 &&
		inst.ServerVersion == Version && !inst.MarkerUnreadable
	return
}

// PreSurface reports a server provisioned before the surface marker existed:
// no marker, but the pack's retired one still answering. It is neither
// broken nor empty — every function it carries works — and an install is
// what moves it forward.
func (inst Report) PreSurface() (ok bool) {
	if inst.ServerVersion >= 0 {
		return false
	}
	return slices.Contains(inst.Retired, PreSurfaceVersionFunctionName)
}

// Reconcile compares the server's leeway-namespaced functions against the
// declared set.
//
// Dropping is opt-in, and the asymmetry against RetiredNames is deliberate:
// this repository may delete names it once shipped, but a name it never
// declared may belong to a fork or to a downstream consumer, and callers
// reconcile endpoints automatically at startup. Reporting is always safe;
// deleting someone else's function on a timer is not.
func Reconcile(ctx context.Context, conn Conn, mode ReconcileModeE) (rep Report, err error) {
	rep.ServerVersion = -1

	installed, err := serverFunctions(ctx, conn)
	if err != nil {
		return
	}
	declared := DeclaredNames()
	retired := make(map[string]struct{}, 64)
	for _, name := range RetiredNames() {
		retired[name] = struct{}{}
	}

	for name := range installed {
		if _, ok := declared[name]; ok {
			continue
		}
		if _, wasOurs := retired[name]; wasOurs {
			rep.Retired = append(rep.Retired, name)
			continue
		}
		rep.Undeclared = append(rep.Undeclared, name)
	}
	for name := range declared {
		if _, ok := installed[name]; !ok {
			rep.Missing = append(rep.Missing, name)
		}
	}
	slices.Sort(rep.Undeclared)
	slices.Sort(rep.Retired)
	slices.Sort(rep.Missing)

	if _, ok := installed[VersionFunctionName]; ok {
		got, qErr := queryTrimmed(ctx, conn, "SELECT "+VersionFunctionName+"()")
		if qErr != nil {
			err = eh.Errorf("reconcile: read version marker: %w", qErr)
			return
		}
		v, cErr := strconv.Atoi(strings.TrimSpace(got))
		if cErr == nil {
			rep.ServerVersion = v
		} else {
			// The name is installed — that is why this branch runs — so
			// leaving ServerVersion at the no-marker sentinel would report
			// the opposite of what was found.
			rep.MarkerUnreadable = true
		}
	}

	if mode != ReconcileDrop {
		return
	}
	for _, name := range rep.Undeclared {
		e := conn.Exec(ctx, "DROP FUNCTION IF EXISTS "+name)
		if e != nil {
			err = eb.Build().Str("function", name).Errorf("reconcile: drop: %w", e)
			return
		}
		rep.Dropped = append(rep.Dropped, name)
	}
	return
}

// serverFunctions lists the leeway-namespaced user-defined functions the
// server carries.
//
// `origin != 'System'` excludes builtins: a builtin that happens to be
// LW_-prefixed is not ours to report or drop, and the collision check is
// where that case is refused.
func serverFunctions(ctx context.Context, conn Conn) (names map[string]struct{}, err error) {
	sql := "SELECT name FROM system.functions WHERE origin != 'System' AND name LIKE '" +
		namespaceLike + "' ORDER BY name"
	out, err := queryTrimmed(ctx, conn, sql)
	if err != nil {
		err = eh.Errorf("list server functions: %w", err)
		return
	}
	names = make(map[string]struct{}, 32)
	for f := range strings.FieldsSeq(out) {
		names[f] = struct{}{}
	}
	return
}

// dropRetired removes the names this repository has withdrawn, so a server
// provisioned against an older build does not keep answering under them.
//
// Best-effort on purpose, and the only step of Install that is: the roster
// verified one statement earlier, so the server is already correct for every
// current caller. A DROP that fails means a stale function lingers — the
// state the server was in before this ran — which is worth neither failing
// an install nor taking a working endpoint down over. A caller that needs
// the drop to have happened asks the server, or calls Reconcile.
//
// A retired name that is also a current name is never dropped. Nothing
// re-uses a name today (ADR-0162 §SD5 forbids repurposing one), but the list
// is append-only and read by a future roster, so the guard is what keeps a
// re-introduced spelling from deleting itself right after installation.
func dropRetired(ctx context.Context, conn Conn) {
	current := DeclaredNames()
	for _, name := range RetiredNames() {
		if _, live := current[name]; live {
			continue
		}
		_ = conn.Exec(ctx, "DROP FUNCTION IF EXISTS "+name)
	}
}

// probeLambdaSupport creates, calls and drops a throwaway higher-order UDF.
// SQL UDFs predate lambda parameters; a server that rejects either step
// cannot host the surface, and the error says so instead of letting
// individual CREATEs fail with less attributable messages.
func probeLambdaSupport(ctx context.Context, conn Conn) (err error) {
	e := conn.Exec(ctx, "CREATE OR REPLACE FUNCTION "+featureProbeName+" AS (f, a) -> arrayMap(f, a)")
	if e == nil {
		var got string
		got, e = queryTrimmed(ctx, conn, "SELECT "+featureProbeName+"(x -> x + 1, [1])")
		if e == nil && got != "[2]" {
			e = eb.Build().Str("got", got).Errorf("unexpected probe result")
		}
	}
	// Best-effort: a leftover probe function is harmless and the next
	// install replaces it.
	_ = conn.Exec(ctx, "DROP FUNCTION IF EXISTS "+featureProbeName)
	if e != nil {
		err = eh.Errorf("server lacks lambda-parameter SQL UDF support (floor verified on ClickHouse 26.7): %w", e)
	}
	return
}

// checkCollisions refuses to install over any declared name that resolves to
// a function the surface does not own. The comparison is case-insensitive on
// purpose: our names are case-sensitive, but some builtin lookups are not,
// so a same-name-different-case builtin still shadows (ADR-0162 §SD2/§SD5).
// Declared names are [A-Za-z0-9_] only (enforced by test), so splicing them
// into the query is safe.
func checkCollisions(ctx context.Context, conn Conn) (err error) {
	fns := DeclaredFunctions()
	quoted := make([]string, 0, len(fns))
	for _, f := range fns {
		quoted = append(quoted, "'"+strings.ToLower(f.Name)+"'")
	}
	sql := "SELECT name FROM system.functions WHERE lower(name) IN (" +
		strings.Join(quoted, ", ") + ") AND origin != 'SQLUserDefined' ORDER BY name"
	out, qErr := queryTrimmed(ctx, conn, sql)
	if qErr != nil {
		err = eh.Errorf("collision check: %w", qErr)
		return
	}
	if out != "" {
		err = eb.Build().Str("names", strings.Join(strings.Fields(out), ", ")).
			Errorf("declared names collide with server-owned functions; refusing to shadow (ADR-0162 §SD5)")
	}
	return
}

func queryTrimmed(ctx context.Context, conn Conn, sql string) (out string, err error) {
	body, err := conn.Query(ctx, sql)
	if err != nil {
		return
	}
	defer func() {
		_ = body.Close()
	}()
	b, err := io.ReadAll(body)
	if err != nil {
		return
	}
	out = strings.TrimSpace(string(b))
	return
}

// firstLine keeps an install error attributable without pasting a whole
// multi-line CREATE into the message.
func firstLine(s string) (out string) {
	out = s
	if i := strings.IndexByte(out, '\n'); i >= 0 {
		out = out[:i]
	}
	return
}
