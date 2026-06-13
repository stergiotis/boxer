package ddl

import (
	"testing"

	canonicaltypes "github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
	"github.com/stretchr/testify/require"
)

// Regression for review C-2: naming.Key values (coSectionGroup / streamingGroup)
// are embedded verbatim into the physical column name, but checkNameComponent
// was applied only to the section and column names. A key containing the
// structural separator (the production ":") would render an identical physical
// name for distinct logical columns and break re-parse. checkKeyComponent now
// rejects separator-bearing keys at composition time, while still allowing the
// common empty-key case.
func TestComposeTaggedValuesColumnRejectsSeparatorInKey(t *testing.T) {
	conv, err := NewHumanReadableNamingConvention(":")
	require.NoError(t, err)

	ctp := canonicaltypes.NewParser()
	ct := ctp.MustParsePrimitiveTypeAst("s")

	compose := func(coGroup, streamGroup naming.Key) error {
		_, e := conv.composeTaggedValuesColumn(
			"sec", useaspects.EmptyAspectSet, "col", ct,
			encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet,
			common.ColumnRoleValue, common.TableRowConfigMultiAttributesPerRow,
			coGroup, streamGroup)
		return e
	}

	// Empty keys (the common no-group case) must pass.
	require.NoError(t, compose("", ""))
	// Separator-free keys must pass.
	require.NoError(t, compose(naming.Key("groupA"), naming.Key("groupB")))
	// A separator-bearing co-section group must be rejected.
	require.Error(t, compose(naming.Key("a:b"), ""), "co-section group containing the separator must be rejected")
	// A separator-bearing streaming group must be rejected.
	require.Error(t, compose("", naming.Key("x:y")), "streaming group containing the separator must be rejected")
}

// The collision the fix prevents: ("a","b:c") vs ("a:b","c") rendered the same
// physical name before C-2. With composition rejecting separator-bearing keys,
// neither degenerate key can be embedded, so the collision is unreachable.
func TestComposeTaggedValuesColumnKeyCollisionUnreachable(t *testing.T) {
	conv, err := NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ctp := canonicaltypes.NewParser()
	ct := ctp.MustParsePrimitiveTypeAst("s")

	_, errA := conv.composeTaggedValuesColumn("sec", useaspects.EmptyAspectSet, "col", ct,
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet,
		common.ColumnRoleValue, common.TableRowConfigMultiAttributesPerRow,
		naming.Key("a"), naming.Key("b:c"))
	_, errB := conv.composeTaggedValuesColumn("sec", useaspects.EmptyAspectSet, "col", ct,
		encodingaspects.EmptyAspectSet, valueaspects.EmptyAspectSet,
		common.ColumnRoleValue, common.TableRowConfigMultiAttributesPerRow,
		naming.Key("a:b"), naming.Key("c"))
	require.Error(t, errA)
	require.Error(t, errB)
}
