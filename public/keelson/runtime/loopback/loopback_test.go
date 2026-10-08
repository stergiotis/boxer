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
