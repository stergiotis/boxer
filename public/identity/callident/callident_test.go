package callident

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallIdentityFrom_Absent(t *testing.T) {
	_, ok := CallIdentityFrom(context.Background())
	assert.False(t, ok)
	_, ok = CallIdentityFrom(WithCallIdentity(context.Background(), CallIdentity{}))
	assert.False(t, ok, "a zero identity reads as none")
}

func TestWithClaims_KeepsOrigin(t *testing.T) {
	host := WithCallIdentity(context.Background(), CallIdentity{
		Origin: Origin{Run: "r1", App: "a.b", Instance: 7},
		Claims: Claims{Principal: "p-old"},
	})
	app := WithClaims(host, Claims{Principal: "p1", Purpose: "billing", Correlation: "c1"})
	ci, ok := CallIdentityFrom(app)
	require.True(t, ok)
	assert.Equal(t, Origin{Run: "r1", App: "a.b", Instance: 7}, ci.Origin)
	assert.Equal(t, Claims{Principal: "p1", Purpose: "billing", Correlation: "c1"}, ci.Claims)

	// Claims on a context with no origin stand alone.
	ci, ok = CallIdentityFrom(WithClaims(context.Background(), Claims{Purpose: "x"}))
	require.True(t, ok)
	assert.Equal(t, Origin{}, ci.Origin)
}
