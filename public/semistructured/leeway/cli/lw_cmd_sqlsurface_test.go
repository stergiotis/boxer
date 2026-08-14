package cli

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

const testEndpoint = "http://example.invalid:8123/"

// TestFormatSurfaceStatusVersionCases pins the four things the version line
// can say, because they call for four different actions and a tool that
// blurs them sends someone to reprovision a healthy server — or leaves a
// broken one looking fine.
func TestFormatSurfaceStatusVersionCases(t *testing.T) {
	t.Run("matches", func(t *testing.T) {
		out := formatSurfaceStatus(lwsqlsurface.Report{ServerVersion: lwsqlsurface.Version}, testEndpoint)
		require.Contains(t, out, "matches this build")
		require.Contains(t, out, testEndpoint)
	})

	t.Run("older revision", func(t *testing.T) {
		out := formatSurfaceStatus(lwsqlsurface.Report{ServerVersion: lwsqlsurface.Version - 1}, testEndpoint)
		require.Contains(t, out, "older definitions")
		require.NotContains(t, out, "matches this build")
	})

	t.Run("pre-surface endpoint", func(t *testing.T) {
		// A server carrying the retired pack marker and no surface marker
		// works; it was provisioned before the families shared one. The
		// line says so instead of reporting an absence.
		out := formatSurfaceStatus(lwsqlsurface.Report{
			ServerVersion: -1,
			Retired:       []string{lwsqlsurface.PreSurfaceVersionFunctionName},
		}, testEndpoint)
		require.Contains(t, out, "pre-surface build")
		require.Contains(t, out, "install")
	})

	t.Run("nothing provisioned", func(t *testing.T) {
		out := formatSurfaceStatus(lwsqlsurface.Report{ServerVersion: -1}, testEndpoint)
		require.Contains(t, out, "no surface marker")
		require.NotContains(t, out, "pre-surface")
	})
}

// TestFormatSurfaceStatusSeparatesLeftovers pins the distinction the drop
// decision rests on: a withdrawn spelling has a known fix, an undeclared
// name is somebody's. The status output must route the reader to `install`
// for one and to a deliberate, separately named command for the other.
func TestFormatSurfaceStatusSeparatesLeftovers(t *testing.T) {
	out := formatSurfaceStatus(lwsqlsurface.Report{
		ServerVersion: lwsqlsurface.Version,
		Missing:       []string{"LW_CO_GATHER"},
		Retired:       []string{"LW_PACK_VERSION"},
		Undeclared:    []string{"LW_THEIR_HELPER"},
	}, testEndpoint)

	require.Contains(t, out, "LW_CO_GATHER")
	require.Contains(t, out, "LW_PACK_VERSION")
	require.Contains(t, out, "LW_THEIR_HELPER")
	require.Contains(t, out, "`install` drops them", "a withdrawn spelling has a known fix")
	require.Contains(t, out, "drop-undeclared", "an undeclared name needs a deliberate command")
	require.Contains(t, out, "left alone", "reporting is not deleting")
	require.NotContains(t, out, "in sync")
}

// TestFormatSurfaceStatusInSync pins the quiet case: no lists, one line
// saying how much was checked.
func TestFormatSurfaceStatusInSync(t *testing.T) {
	out := formatSurfaceStatus(lwsqlsurface.Report{ServerVersion: lwsqlsurface.Version}, testEndpoint)
	require.Contains(t, out, "in sync")
	require.Contains(t, out, strconv.Itoa(len(lwsqlsurface.DeclaredFunctions())))
}

// TestPrintedScriptInstallsWithTheMarker is why `print` exists.
//
// Provisioning by hand was possible before this command — pipe
// readback.HelperUDFsSQL() — but that script carries no version marker, so
// it produces a server that works and cannot say what it carries, which is
// the failure ADR-0171 §SD2's marker exists to prevent. This runs the
// printed script the way an operator would and asserts the marker answers
// afterwards.
func TestPrintedScriptInstallsWithTheMarker(t *testing.T) {
	bin, err := exec.LookPath("clickhouse-local")
	if err != nil {
		t.Skipf("clickhouse-local not on PATH, skipping: %v", err)
	}
	dir := t.TempDir()

	var script strings.Builder
	for _, stmt := range lwsqlsurface.Statements() {
		script.WriteString(stmt)
		script.WriteString(";\n")
	}

	run := func(t *testing.T, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, append([]string{"--path", dir, "--output-format", "TSV"}, args...)...)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		require.NoErrorf(t, cmd.Run(), "stderr:\n%s", stderr.String())
		return strings.TrimSpace(stdout.String())
	}

	install := exec.Command(bin, "--path", dir, "--multiquery", "--output-format", "TSV")
	install.Stdin = strings.NewReader(script.String())
	var stderr strings.Builder
	install.Stderr = &stderr
	require.NoErrorf(t, install.Run(), "the printed script must be executable as one script\nstderr:\n%s", stderr.String())

	require.Equal(t, strconv.Itoa(lwsqlsurface.Version), run(t, "--query", "SELECT "+lwsqlsurface.VersionFunctionName+"()"),
		"a hand-provisioned server must be able to say what it carries")
	require.Equal(t, strconv.Itoa(len(lwsqlsurface.DeclaredFunctions())),
		run(t, "--query", `SELECT count() FROM system.functions WHERE origin != 'System' AND name LIKE 'LW\_%'`),
		"the whole declared set lands, not just one family")
}

// TestFormatSurfaceStatusUnreadableMarker pins the state that used to be
// indistinguishable from an unprovisioned server: the marker is installed,
// its body is not a revision. Reporting "no surface marker" there sends
// someone to install over a server whose real problem is an edited function.
func TestFormatSurfaceStatusUnreadableMarker(t *testing.T) {
	out := formatSurfaceStatus(lwsqlsurface.Report{ServerVersion: -1, MarkerUnreadable: true}, testEndpoint)
	require.Contains(t, out, "has been edited")
	require.NotContains(t, out, "no surface marker")
}

// TestUndeclaredIsNotDrift pins ADR-0171 §SD2's asymmetry where it becomes a
// process exit code: a neighbour's function on a shared server is reported,
// but it is not this build being out of sync — otherwise a deployment check
// stays red until someone deletes it, which is what the asymmetry exists to
// prevent.
func TestUndeclaredIsNotDrift(t *testing.T) {
	rep := lwsqlsurface.Report{
		ServerVersion: lwsqlsurface.Version,
		Undeclared:    []string{"LW_THEIR_HELPER"},
	}
	require.True(t, rep.InSync(), "somebody else's function is not this build's drift")

	out := formatSurfaceStatus(rep, testEndpoint)
	require.Contains(t, out, "LW_THEIR_HELPER", "it is still reported")
	require.Contains(t, out, "left alone")

	// A withdrawn spelling, by contrast, IS this build's business.
	rep.Retired = []string{"LW_PACK_VERSION"}
	require.False(t, rep.InSync(), "a spelling this repository withdrew is drift it can fix")
}
