package lwsqlsurface

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Dependent is a family of views defined outside leeway that inlines this
// surface: its bodies call LW_ functions, so — like the schema-decode views
// — each view is a snapshot of the function bodies as they were when it was
// created, and only re-creating it refreshes it.
//
// Registering one makes every install re-create it, last, after the
// surface has verified and the decode views are in place. That is the only
// moment its re-creation is both possible and sufficient: before step 3 the
// functions it inlines may be absent or stale, and any later moment leaves a
// window in which it answers from the previous revision.
//
// The family owns its own applicability. Statements runs against the server
// being installed, may query it, and returns nothing when the family does
// not belong there — a family over a table this server does not hold is not
// an error, it is absent.
type Dependent struct {
	// Name identifies the family in errors and in Dependents.
	Name string
	// Statements returns the statements that (re)create the family on conn,
	// in order, each one statement; empty when the family does not apply to
	// this server.
	Statements func(ctx context.Context, conn Conn) (stmts []string, err error)
}

var (
	dependentsMu sync.Mutex
	dependents   []Dependent
)

// RegisterDependent adds a family to every install that runs in this
// process, typically from the defining package's init — so a binary
// re-creates exactly the families it links. It returns the call that
// removes it again, for a test.
//
// An unnamed family, one without Statements, or a second family under a name
// already registered is a programming error and panics.
func RegisterDependent(d Dependent) (unregister func()) {
	if d.Name == "" || d.Statements == nil {
		log.Panic().Str("name", d.Name).Msg("lwsqlsurface: a dependent needs a name and its statements")
	}
	dependentsMu.Lock()
	defer dependentsMu.Unlock()
	for _, have := range dependents {
		if have.Name == d.Name {
			log.Panic().Str("name", d.Name).Msg("lwsqlsurface: dependent registered twice")
		}
	}
	dependents = append(dependents, d)
	return func() {
		dependentsMu.Lock()
		defer dependentsMu.Unlock()
		dependents = slices.DeleteFunc(dependents, func(have Dependent) bool { return have.Name == d.Name })
	}
}

// Dependents lists the registered families by name, in the order an install
// re-creates them.
func Dependents() (names []string) {
	for _, d := range sortedDependents() {
		names = append(names, d.Name)
	}
	return names
}

// sortedDependents is the registry by name: init order follows package
// import order, which is nothing an install should depend on.
func sortedDependents() (ds []Dependent) {
	dependentsMu.Lock()
	ds = slices.Clone(dependents)
	dependentsMu.Unlock()
	slices.SortFunc(ds, func(a, b Dependent) int { return strings.Compare(a.Name, b.Name) })
	return ds
}

// installDependents re-creates every registered family on conn. The first
// failure stops it and names the family: the surface and its own views are
// installed by then, so the error says what is left stale rather than
// suggesting the install did nothing.
func installDependents(ctx context.Context, conn Conn) (err error) {
	_, err = reconcileDependents(ctx, conn, true)
	return
}

// installDriftedDependents re-creates the families whose statements differ
// from the ones the server last had installed, and reports how many that
// was.
//
// What it compares is a digest of the family's statements and the surface
// revision they inline, which the previous install left on the server as a
// marker function (dependentMarkerName). A family therefore needs no
// revision of its own to be skipped, and a change to its definitions is
// picked up without anyone bumping one. What the digest cannot see is a view
// removed by hand on the server: the marker still matches, and it takes a
// full [Install] to put the view back.
func installDriftedDependents(ctx context.Context, conn Conn) (replaced int, err error) {
	return reconcileDependents(ctx, conn, false)
}

func reconcileDependents(ctx context.Context, conn Conn, force bool) (replaced int, err error) {
	for _, d := range sortedDependents() {
		var stmts []string
		stmts, err = d.Statements(ctx, conn)
		if err != nil {
			return replaced, eb.Build().Str("dependent", d.Name).Errorf("install dependent views (the surface is installed): %w", err)
		}
		if len(stmts) == 0 {
			continue
		}
		marker := dependentMarkerName(d.Name)
		digest := dependentDigest(stmts)
		if !force {
			// A marker that is absent fails the query, which reads as drift.
			have, qErr := queryTrimmed(ctx, conn, "SELECT "+marker+"()")
			if qErr == nil && have == digest {
				continue
			}
		}
		for _, stmt := range stmts {
			e := conn.Exec(ctx, stmt)
			if e != nil {
				return replaced, eb.Build().Str("dependent", d.Name).Str("statement", firstLine(stmt)).
					Errorf("install dependent views (the surface is installed): %w", e)
			}
		}
		// Last, so a family that stopped half way is re-created next time.
		e := conn.Exec(ctx, "CREATE OR REPLACE FUNCTION "+marker+" AS () -> '"+digest+"'")
		if e != nil {
			return replaced, eb.Build().Str("dependent", d.Name).Errorf("record the dependent's digest: %w", e)
		}
		replaced++
	}
	return replaced, nil
}

// dependentMarkerName is the function that reports the digest a family was
// last installed with. Like the feature probe it stays outside the `LW_`
// namespace, which Reconcile reads as the surface's own.
func dependentMarkerName(name string) (marker string) {
	var sb strings.Builder
	sb.WriteString("leewayDependentDigest_")
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// dependentDigest covers the statements and the surface revision they are
// expanded against.
func dependentDigest(stmts []string) (digest string) {
	h := sha256.New()
	_, _ = h.Write([]byte(strconv.Itoa(Version)))
	for _, stmt := range stmts {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(stmt))
	}
	return hex.EncodeToString(h.Sum(nil))
}
