package loopback

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsHost(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "127.8.9.10", "::1", "[::1]", "localhost", "LocalHost"} {
		require.True(t, IsHost(host), host)
	}
	// "" binds every interface; a name is not resolved, so one that maps
	// to loopback only through a hosts file does not count.
	for _, host := range []string{"", "0.0.0.0", "::", "192.168.1.10", "example.com", "localhost.example.com"} {
		require.False(t, IsHost(host), host)
	}
}

func TestGateCheck(t *testing.T) {
	closed := Gate{Name: "x"}
	require.NoError(t, closed.Check("127.0.0.1:0"))
	require.NoError(t, closed.Check("[::1]:80"))
	require.NoError(t, closed.Check("localhost:80"))
	for _, addr := range []string{":8080", "0.0.0.0:80", "[::]:80", "192.168.1.10:80", "example.com:80"} {
		require.Error(t, closed.Check(addr), addr)
	}
	require.Error(t, closed.Check("no-port"))

	optIn := Gate{Name: "x", OptIn: "--allowNonLoopback"}
	err := optIn.Check(":8080")
	require.Error(t, err)
	require.Contains(t, err.Error(), "opt-in")
	optIn.AllowNonLoopback = true
	require.NoError(t, optIn.Check(":8080"))

	// An opt-in value means nothing on a listener that has no opt-in.
	closed.AllowNonLoopback = true
	require.Error(t, closed.Check(":8080"))
}

func TestGateListen(t *testing.T) {
	ln, err := Gate{Name: "x"}.Listen(t.Context(), "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close())
	_, err = Gate{Name: "x"}.Listen(t.Context(), ":0")
	require.Error(t, err)
}
