package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
)

// A lading macro whose mount is a `{name:Type}` slot reads the slot's value
// from the statement's `SET param_…` prelude at expansion (ADR-0200 §SD6).
// ExtractParams takes the prelude off before the pre-execute stage, so the
// stage used to see an unbound slot, skip the expansion, and send `fs(…)` to
// the server as an unknown table function — every lading book applet that
// takes its mount as a knob did. The stage gets the prelude back now, and the
// body sent still carries none.
func TestPreExecuteStageSeesTheParameterPrelude(t *testing.T) {
	cl := newTestClientWithStandardSet(t)
	cl.passBinding = &clientPassBinding{MountVisibilityI: ladingsql.VisibleAll{}}

	body, params := cl.BuildStatement("SET param_m = '*';\nSET param_dir = '.';\nSELECT name FROM fs({m:String}) WHERE dir = {dir:String}")
	require.Equal(t, map[string]string{"param_m": "*", "param_dir": "."}, params)
	require.NotContains(t, strings.ToLower(body), "fs(", "the macro expanded")
	require.Contains(t, body, ladingschema.TableNameMeta)
	require.NotContains(t, body, "SET ", "the prelude travels as URL parameters, not in the body")
	require.Contains(t, body, "{dir:String}", "a slot the macro does not take stays for the server to bind")

	// a statement that binds nothing goes through the stage as before
	body, params = cl.BuildStatement("SELECT name FROM fs('*')")
	require.Empty(t, params)
	require.NotContains(t, strings.ToLower(body), "fs(")
}
