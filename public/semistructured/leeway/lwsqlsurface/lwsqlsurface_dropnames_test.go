package lwsqlsurface_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsqlsurface"
)

// listingConn answers the function listing with a fixed body and records
// every Exec; every other query answers empty.
type listingConn struct {
	listing string
	execs   []string
}

func (inst *listingConn) Exec(ctx context.Context, sql string) (err error) {
	inst.execs = append(inst.execs, sql)
	return
}

func (inst *listingConn) Query(ctx context.Context, sql string) (body io.ReadCloser, err error) {
	out := ""
	if strings.Contains(sql, "FROM system.functions") {
		out = inst.listing
	}
	body = io.NopCloser(strings.NewReader(out))
	return
}

// Regression: the listing was split on whitespace, so a server function
// named `LW_x helper` was reported as `LW_x` and `helper`, and a drop ran
// DROP FUNCTION IF EXISTS helper — a function outside the namespace.
func TestReconcileDropRefusesNamesThatAreNotIdentifiers(t *testing.T) {
	conn := &listingConn{listing: "LW_x helper\n"}
	rep, err := lwsqlsurface.Reconcile(t.Context(), conn, lwsqlsurface.ReconcileReport)
	require.NoError(t, err)
	require.Equal(t, []string{"LW_x helper"}, rep.Undeclared)

	_, err = lwsqlsurface.Reconcile(t.Context(), conn, lwsqlsurface.ReconcileDrop)
	require.Error(t, err)
	for _, e := range conn.execs {
		require.NotContains(t, e, "DROP")
	}
}
