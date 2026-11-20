package lwsqlsurface_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/chpack"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// surfaceDigestGolden is the content digest of this generation, written out.
// A change to any family's body, or to the views, moves Digest; the test
// below then fails until Version is bumped and this literal updated — so two
// builds of one generation never carry different bodies, which is what lets
// the generation alone decide which of two builds is newer.
const surfaceDigestGolden = "03035520cc49984c4126c7f71940bb82472cb6bd1d50b90e0af0c24ef252da94"

// surfaceDigestGeneration is the Version the golden digest belongs to.
const surfaceDigestGeneration = 3

func TestSurfaceDigestPinned(t *testing.T) {
	if lwsqlsurface.Digest() != surfaceDigestGolden || lwsqlsurface.Version != surfaceDigestGeneration {
		require.Failf(t, "the surface's bodies or generation moved",
			"digest %s at generation %d; pinned %s at generation %d. "+
				"A body change needs a Version bump: bump lwsqlsurface.Version, then set both constants here",
			lwsqlsurface.Digest(), lwsqlsurface.Version, surfaceDigestGolden, surfaceDigestGeneration)
	}
}

func TestDigestStatementPrecedesMarker(t *testing.T) {
	stmts := lwsqlsurface.Statements()
	require.Equal(t, lwsqlsurface.DigestStatement(), stmts[len(stmts)-2])
	require.Contains(t, stmts[len(stmts)-2], "'"+lwsqlsurface.Digest()+"'")
	require.Contains(t, string(lwsqlsurface.ViewStamp()), lwsqlsurface.Digest()[:16])
}

func TestDecide(t *testing.T) {
	cur := lwsqlsurface.Version
	dig := lwsqlsurface.Digest()
	cases := []struct {
		name  string
		stamp lwsqlsurface.ServerStamp
		want  lwsqlsurface.InstallActionE
	}{
		{"no marker", lwsqlsurface.ServerStamp{Generation: -1}, lwsqlsurface.ActionInstall},
		{"unreadable marker", lwsqlsurface.ServerStamp{Generation: -1, Unreadable: true}, lwsqlsurface.ActionInstall},
		{"older generation", lwsqlsurface.ServerStamp{Generation: cur - 1}, lwsqlsurface.ActionInstall},
		{"older generation, same digest text", lwsqlsurface.ServerStamp{Generation: cur - 1, Digest: dig}, lwsqlsurface.ActionInstall},
		{"this generation, no digest", lwsqlsurface.ServerStamp{Generation: cur}, lwsqlsurface.ActionInstall},
		{"this generation, other bodies", lwsqlsurface.ServerStamp{Generation: cur, Digest: strings.Repeat("0", 64)}, lwsqlsurface.ActionInstall},
		{"this generation, this digest", lwsqlsurface.ServerStamp{Generation: cur, Digest: dig}, lwsqlsurface.ActionNone},
		{"newer generation", lwsqlsurface.ServerStamp{Generation: cur + 1, Digest: "x"}, lwsqlsurface.ActionKeepNewer},
		{"newer generation, no digest", lwsqlsurface.ServerStamp{Generation: cur + 1}, lwsqlsurface.ActionKeepNewer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, lwsqlsurface.Decide(c.stamp), "%s", lwsqlsurface.Decide(c.stamp))
		})
	}
}

func TestParseStamp(t *testing.T) {
	s := lwsqlsurface.ParseStamp(" 3\n", "abc ")
	require.Equal(t, lwsqlsurface.ServerStamp{Generation: 3, Digest: "abc"}, s)
	s = lwsqlsurface.ParseStamp("x", "abc")
	require.True(t, s.Unreadable)
	require.Equal(t, -1, s.Generation)
	require.Equal(t, lwsqlsurface.ActionInstall, lwsqlsurface.Decide(s))
}

// TestRetiredGenerationsNotAhead keeps a retired name from recording a
// generation later than the build: such a name would never be dropped.
func TestRetiredGenerationsNotAhead(t *testing.T) {
	for _, r := range chpack.Retired() {
		require.LessOrEqualf(t, r.Generation, lwsqlsurface.Version, "%s", r.Name)
		require.GreaterOrEqualf(t, r.Generation, 1, "%s", r.Name)
	}
	require.Len(t, chpack.RetiredNames(), len(chpack.Retired()))
}

// TestNewerServerIsLeftInPlace is the monotonic install against a real
// instance: a server whose marker reports a later generation is neither
// re-installed nor stripped of retired names by InstallIfDrifted, and one
// that has dropped a function this build calls is refused by name.
func TestNewerServerIsLeftInPlace(t *testing.T) {
	conn := newLocalConn(t)
	require.NoError(t, lwsqlsurface.Install(t.Context(), conn))
	newer := strconv.Itoa(lwsqlsurface.Version + 1)
	require.NoError(t, conn.Exec(t.Context(), "CREATE OR REPLACE FUNCTION "+lwsqlsurface.VersionFunctionName+" AS () -> "+newer))
	require.NoError(t, conn.Exec(t.Context(), "CREATE OR REPLACE FUNCTION LW_PACK_VERSION AS () -> 0"))

	installed, err := lwsqlsurface.InstallIfDrifted(t.Context(), conn)
	require.NoError(t, err)
	require.False(t, installed)
	out, err := conn.run("SELECT " + lwsqlsurface.VersionFunctionName + "()")
	require.NoError(t, err)
	require.Equal(t, newer, strings.TrimSpace(out), "an older build must not put its generation back")
	require.True(t, functionExists(t, conn, "LW_PACK_VERSION"), "an older build drops nothing on a newer server")

	require.NoError(t, conn.Exec(t.Context(), "DROP FUNCTION LW_CO_GATHER"))
	_, err = lwsqlsurface.InstallIfDrifted(t.Context(), conn)
	require.ErrorContains(t, err, "lacks functions")
	require.ErrorContains(t, lwsqlsurface.VerifyServes(t.Context(), conn), "lacks functions")
}
