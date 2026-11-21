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
	loopbackAddrs := []string{"127.0.0.1:0", "[::1]:80", "localhost:80"}
	otherAddrs := []string{":8080", "0.0.0.0:80", "[::]:80", "192.168.1.10:80", "example.com:80"}

	// A listener binds the address it is given.
	open := Gate{Name: "x"}
	for _, addr := range loopbackAddrs {
		exposure, err := open.check(addr)
		require.NoError(t, err, addr)
		require.Equal(t, ExposureLoopback, exposure, addr)
	}
	for _, addr := range otherAddrs {
		exposure, err := open.check(addr)
		require.NoError(t, err, addr)
		require.Equal(t, ExposureUndeclared, exposure, addr)
	}
	require.Error(t, open.Check("no-port"))

	// One marked loopback-only refuses every other address, whatever else
	// it carries.
	local := Gate{Name: "x", LoopbackOnly: true}
	for _, addr := range loopbackAddrs {
		require.NoError(t, local.Check(addr), addr)
	}
	for _, addr := range otherAddrs {
		err := local.Check(addr)
		require.Error(t, err, addr)
		require.Contains(t, err.Error(), "refusing non-loopback")
	}
	local.Exposure = &Exposure{Serves: "someone", Reason: "something"}
	require.Error(t, local.Check(":8080"))
}

func TestGateListen(t *testing.T) {
	ln, err := Gate{Name: "x"}.Listen(t.Context(), "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, ln.Close())
	_, err = Gate{Name: "x", LoopbackOnly: true}.Listen(t.Context(), ":0")
	require.Error(t, err)
}
