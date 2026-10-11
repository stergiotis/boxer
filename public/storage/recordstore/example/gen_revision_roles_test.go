package example

import (
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/storage/recordstore/gen"
	"github.com/stretchr/testify/require"
)

// The Revision role's generation-time gates: it names an existing plain
// column, one no other role binds, deriving to uint32.
func TestRevisionRoleRefusals(t *testing.T) {
	withRev := func(t *testing.T) *common.TableManipulator {
		manip := rolesManipulator(t)
		manip.PlainValueColumn(common.PlainItemTypeEntityLifecycle, "rv", ctabb.U32)
		return manip
	}
	_, err := generateRolesInto(t, withRev(t), gen.Roles{Revision: "nosuch"})
	require.ErrorContains(t, err, "Roles.Revision names no plain column")
	_, err = generateRolesInto(t, withRev(t), gen.Roles{Revision: "lca"})
	require.ErrorContains(t, err, "deriving to uint32", "a u8 column is not a revision")
	_, err = generateRolesInto(t, withRev(t), gen.Roles{Order: "idb", Revision: "idb"})
	require.ErrorContains(t, err, "another declared role already binds")
	_, err = generateRolesInto(t, withRev(t), gen.Roles{Revision: "rv"})
	require.NoError(t, err)
}
