package lwsqlsurface_test

import (
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// localConn drives clickhouse-local as if it were a server, so Install and
// Reconcile run for real without one.
//
// State persists across invocations through --path: clickhouse-local writes
// created functions under that directory and reads them back on the next
// call. That is what makes this a lane rather than a shape check — the
// install really creates the whole surface, the marker really answers, and a
// DROP really removes something.
//
// Writes are buffered and flushed as one --multiquery script before the next
// read, because a process per statement put this lane at half a minute. The
// order statements execute in is unchanged and every write lands before
// anything observes it; what is given up is the attribution of an Exec error
// to the Exec call that caused it, which nothing here asserts and which the
// live lane still exercises. The tests below also share a server per
// scenario rather than per assertion.
//
// The integration lane (ADR-0171 verification plan) still owns the live
// server; what cannot be reproduced here is a server whose builtins differ,
// which is what the collision check is about.
type localConn struct {
	t       *testing.T
	bin     string
	path    string
	pending []string
}

func newLocalConn(t *testing.T) (conn *localConn) {
	t.Helper()
	bin, err := exec.LookPath("clickhouse-local")
	if err != nil {
		t.Skipf("clickhouse-local not on PATH, skipping (install ClickHouse to run surface install tests): %v", err)
	}
	return &localConn{t: t, bin: bin, path: t.TempDir()}
}

// run executes one statement or script, after any buffered writes.
func (inst *localConn) run(sql string) (out string, err error) {
	err = inst.flush()
	if err != nil {
		return
	}
	return inst.exec(sql)
}

func (inst *localConn) exec(script string) (out string, err error) {
	cmd := exec.Command(inst.bin, "--path", inst.path, "--multiquery", "--output-format", "TSV")
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		err = &localError{sql: script, stderr: stderr.String(), err: err}
		return
	}
	out = stdout.String()
	return
}

// flush runs the buffered writes as one script.
func (inst *localConn) flush() (err error) {
	if len(inst.pending) == 0 {
		return
	}
	script := strings.Join(inst.pending, ";\n") + ";\n"
	inst.pending = inst.pending[:0]
	_, err = inst.exec(script)
	return
}

func (inst *localConn) Exec(ctx context.Context, sql string) (err error) {
	inst.pending = append(inst.pending, strings.TrimSuffix(strings.TrimSpace(sql), ";"))
	return
}

func (inst *localConn) Query(ctx context.Context, sql string) (body io.ReadCloser, err error) {
	out, err := inst.run(sql)
	if err != nil {
		return
	}
	body = io.NopCloser(strings.NewReader(out))
	return
}

type localError struct {
	sql    string
	stderr string
	err    error
}

func (inst *localError) Error() string {
	return inst.err.Error() + ": " + inst.sql + "\n" + inst.stderr
}

// namespacedFunctions lists what the instance carries under the leeway
// namespace, independent of Reconcile — a check that shares no code with the
// thing it checks.
func namespacedFunctions(t *testing.T, conn *localConn) (names map[string]struct{}) {
	t.Helper()
	out, err := conn.run(`SELECT name FROM system.functions WHERE origin != 'System' AND name LIKE 'LW\_%' ORDER BY name`)
	require.NoError(t, err)
	names = make(map[string]struct{}, 64)
	for f := range strings.FieldsSeq(out) {
		names[f] = struct{}{}
	}
	return
}

// functionExists asks by exact name, so it also sees the pre-namespace
// spellings on the retired list — which namespacedFunctions cannot.
func functionExists(t *testing.T, conn *localConn, name string) (ok bool) {
	t.Helper()
	out, err := conn.run("SELECT count() FROM system.functions WHERE name = '" + name + "'")
	require.NoError(t, err)
	return strings.TrimSpace(out) != "0"
}

// TestSurfaceInstallLane is ADR-0171 §SD2's central claim as a test: one
// call, and all three families plus the marker are on the server at this
// build's revision — then again, unchanged, because a host reconciles on
// every startup and that is the common path, not an edge case.
func TestSurfaceInstallLane(t *testing.T) {
	conn := newLocalConn(t)
	ctx := context.Background()
	require.NoError(t, lwsqlsurface.Install(ctx, conn))

	installed := namespacedFunctions(t, conn)
	for _, f := range lwsqlsurface.DeclaredFunctions() {
		require.Containsf(t, installed, f.Name, "%s (%s) missing after install", f.Name, f.Family)
	}

	t.Run("marker answers this build's revision", func(t *testing.T) {
		out, err := conn.run("SELECT " + lwsqlsurface.VersionFunctionName + "()")
		require.NoError(t, err)
		require.Equal(t, strconv.Itoa(lwsqlsurface.Version), strings.TrimSpace(out))
	})

	t.Run("idempotent", func(t *testing.T) {
		require.NoError(t, lwsqlsurface.Install(ctx, conn))
		require.Equal(t, installed, namespacedFunctions(t, conn))
	})

	t.Run("reconcile reports in sync", func(t *testing.T) {
		rep, err := lwsqlsurface.Reconcile(ctx, conn, lwsqlsurface.ReconcileReport)
		require.NoError(t, err)
		require.Empty(t, rep.Missing)
		require.Empty(t, rep.Undeclared)
		require.Equal(t, lwsqlsurface.Version, rep.ServerVersion)
		require.True(t, rep.InSync())
	})
}

// TestInstallDropsWithdrawnNames reproduces the jsonbench trial's finding: a
// server carrying a name this repository withdrew keeps answering under it
// until something drops it, because CREATE OR REPLACE cannot remove a
// renamed function.
//
// Both generations are planted — a pre-namespace spelling and the pack's own
// marker, which this milestone retires — because they are dropped by the
// same list and only the second is visible to a `LW\_%` listing.
func TestInstallDropsWithdrawnNames(t *testing.T) {
	conn := newLocalConn(t)
	ctx := context.Background()

	retired := lwsqlsurface.RetiredNames()[0]
	require.NoError(t, conn.Exec(ctx, "CREATE OR REPLACE FUNCTION "+retired+" AS (x) -> x"))
	require.NoError(t, conn.Exec(ctx,
		"CREATE OR REPLACE FUNCTION "+lwsqlsurface.PreSurfaceVersionFunctionName+" AS () -> 4"))
	require.True(t, functionExists(t, conn, retired), "precondition: the stale name is there")

	require.NoError(t, lwsqlsurface.Install(ctx, conn))

	require.Falsef(t, functionExists(t, conn, retired), "install must drop the retired %s", retired)
	require.False(t, functionExists(t, conn, lwsqlsurface.PreSurfaceVersionFunctionName),
		"the pack marker is retired: exactly one name answers the revision question")
	require.Contains(t, namespacedFunctions(t, conn), lwsqlsurface.VersionFunctionName)
}

// TestReconcileDrift covers both halves of the drift question and the
// decision between them (ADR-0171 §SD2): an undeclared leeway-namespaced
// function is reported and left alone — it may be a fork's, and play
// reconciles endpoints automatically at startup — while a declared function
// that is absent is the trial's own failure, now detected.
func TestReconcileDrift(t *testing.T) {
	conn := newLocalConn(t)
	ctx := context.Background()
	require.NoError(t, lwsqlsurface.Install(ctx, conn))

	const theirs = "LW_SOMEBODY_ELSES_HELPER"
	require.NoError(t, conn.Exec(ctx, "CREATE OR REPLACE FUNCTION "+theirs+" AS (x) -> x + 1"))

	t.Run("reported, not dropped", func(t *testing.T) {
		rep, err := lwsqlsurface.Reconcile(ctx, conn, lwsqlsurface.ReconcileReport)
		require.NoError(t, err)
		require.Equal(t, []string{theirs}, rep.Undeclared)
		require.Empty(t, rep.Dropped)
		require.Contains(t, namespacedFunctions(t, conn), theirs, "report mode must not delete")
		// Reported, but NOT counted as this build being out of sync: the
		// surface is fully provisioned and the extra name is somebody
		// else's. Folding it into the verdict would make a deployment
		// check red until someone deleted a neighbour's function.
		require.True(t, rep.InSync(), "an undeclared name is not this build's drift")
	})

	t.Run("install leaves it alone too", func(t *testing.T) {
		// Only the retired list is automatic, and that list holds names
		// this repository itself shipped.
		require.NoError(t, lwsqlsurface.Install(ctx, conn))
		require.Contains(t, namespacedFunctions(t, conn), theirs)
	})

	t.Run("drop mode removes it", func(t *testing.T) {
		rep, err := lwsqlsurface.Reconcile(ctx, conn, lwsqlsurface.ReconcileDrop)
		require.NoError(t, err)
		require.Equal(t, []string{theirs}, rep.Dropped)
		require.NotContains(t, namespacedFunctions(t, conn), theirs)

		// The declared set survives: the filter is "undeclared", not
		// "everything under LW_".
		after, err := lwsqlsurface.Reconcile(ctx, conn, lwsqlsurface.ReconcileReport)
		require.NoError(t, err)
		require.True(t, after.InSync())
	})

	t.Run("a missing declared function is reported", func(t *testing.T) {
		const victim = "LW_VALUE_BY_TAG_EQUAL"
		require.NoError(t, conn.Exec(ctx, "DROP FUNCTION IF EXISTS "+victim))
		rep, err := lwsqlsurface.Reconcile(ctx, conn, lwsqlsurface.ReconcileReport)
		require.NoError(t, err)
		require.Equal(t, []string{victim}, rep.Missing)
		require.False(t, rep.InSync())
	})
}

// TestReconcileOnAnEmptyServerIsAllMissing pins that a server nobody has
// provisioned reports the whole declared set as missing and no version,
// rather than erroring — the state a first-run host is in.
func TestReconcileOnAnEmptyServerIsAllMissing(t *testing.T) {
	conn := newLocalConn(t)
	rep, err := lwsqlsurface.Reconcile(context.Background(), conn, lwsqlsurface.ReconcileReport)
	require.NoError(t, err)
	require.Len(t, rep.Missing, len(lwsqlsurface.DeclaredFunctions()))
	require.Empty(t, rep.Undeclared)
	require.Equal(t, -1, rep.ServerVersion)
}

// TestReconcileSeparatesRetiredFromUndeclared pins the distinction the CLI's
// status output rests on: a name this repository shipped and withdrew has a
// known fix — install drops it — while a name nobody here ever declared is
// somebody else's. Reporting them in one bucket makes a tool either nag
// about the fixable case or offer to delete the unknown one.
func TestReconcileSeparatesRetiredFromUndeclared(t *testing.T) {
	conn := newLocalConn(t)
	ctx := context.Background()

	// A pre-surface server: the pack's retired marker, and someone else's
	// helper, and nothing of the current surface.
	require.NoError(t, conn.Exec(ctx,
		"CREATE OR REPLACE FUNCTION "+lwsqlsurface.PreSurfaceVersionFunctionName+" AS () -> 4"))
	require.NoError(t, conn.Exec(ctx, "CREATE OR REPLACE FUNCTION LW_THEIR_HELPER AS (x) -> x"))

	rep, err := lwsqlsurface.Reconcile(ctx, conn, lwsqlsurface.ReconcileReport)
	require.NoError(t, err)
	require.Equal(t, []string{lwsqlsurface.PreSurfaceVersionFunctionName}, rep.Retired)
	require.Equal(t, []string{"LW_THEIR_HELPER"}, rep.Undeclared)
	require.Equal(t, -1, rep.ServerVersion)
	require.True(t, rep.PreSurface(), "a marker-less server carrying the pack marker is pre-surface, not empty")
	require.False(t, rep.InSync())

	// Dropping touches only the unknown one; the retired name is Install's
	// business, not a destructive opt-in's.
	rep, err = lwsqlsurface.Reconcile(ctx, conn, lwsqlsurface.ReconcileDrop)
	require.NoError(t, err)
	require.Equal(t, []string{"LW_THEIR_HELPER"}, rep.Dropped)
	require.True(t, functionExists(t, conn, lwsqlsurface.PreSurfaceVersionFunctionName))

	// And an install is what clears it, which is what PreSurface means.
	require.NoError(t, lwsqlsurface.Install(ctx, conn))
	after, err := lwsqlsurface.Reconcile(ctx, conn, lwsqlsurface.ReconcileReport)
	require.NoError(t, err)
	require.False(t, after.PreSurface())
	require.True(t, after.InSync())
}

// strictConn is a localConn that does NOT buffer: every Exec is its own
// clickhouse-local invocation with a single --query.
//
// The default harness batches writes into one `--multiquery` script for
// speed, which silently tolerates something a real server does not.
// ClickHouse's HTTP interface takes ONE statement per request, and
// readback.FamilyStatements exists to cut the embedded .sql into exactly
// that — by a lexical `;` split its own doc calls safe "for this file and
// only this file". If a future body carries a `;` inside a string literal,
// the batched lane still passes while the live one fails with
// "Multi-statements are not allowed". This exercises the contract for real,
// on one install, so the cost is paid once.
type strictConn struct{ *localConn }

func (inst strictConn) Exec(ctx context.Context, sql string) (err error) {
	_, err = inst.localConn.exec(sql)
	return
}

// TestInstallHonoursOneStatementPerExec runs the install with every Exec
// issued separately, which is what the real client does.
func TestInstallHonoursOneStatementPerExec(t *testing.T) {
	conn := strictConn{newLocalConn(t)}
	ctx := context.Background()
	require.NoError(t, lwsqlsurface.Install(ctx, conn),
		"every statement must stand alone: a body carrying a `;` would fail here and pass the batched lane")

	installed := namespacedFunctions(t, conn.localConn)
	for _, f := range lwsqlsurface.DeclaredFunctions() {
		require.Containsf(t, installed, f.Name, "%s missing", f.Name)
	}
}
