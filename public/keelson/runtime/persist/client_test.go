package persist_test

import (
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/persist"
)

// newClientSetup wires bus + service + a per-app bus client with the
// matching runtime.persist.{alias}.> cap, then builds a persist.Client
// over the bus. Mirrors the carousel's Phase A+C wiring for tests that
// don't need a windowhost.
func newClientSetup(t *testing.T, appId app.AppIdT) (svc *persist.Service, cli *persist.Client, cleanup func()) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	backend := persist.NewMemoryBackend()
	s, err := persist.NewService(bus, zerolog.Nop(), backend)
	require.NoError(t, err)

	alias := appId.SubjectAlias()
	caps := []app.SubjectFilter{
		{Pattern: persist.SubjectPrefix + alias + ".>", Direction: app.CapDirectionPub,
			Reason: "test fixture: own state"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionSub,
			Reason: "Request must subscribe to its reply inbox"},
	}
	busC := bus.NewClient(appId, caps)
	c, err := persist.NewClient(busC, appId)
	require.NoError(t, err)

	svc = s
	cli = c
	cleanup = func() {
		s.Close()
	}
	return
}

func TestClient_NewClient_RejectsNilBus(t *testing.T) {
	_, err := persist.NewClient(nil, "test.x")
	require.Error(t, err)
}

func TestClient_SetGetRoundTrip(t *testing.T) {
	_, cli, cleanup := newClientSetup(t, "github.com/example/test.app")
	defer cleanup()

	err := cli.Set("editorFont", []byte("Iosevka 14"))
	require.NoError(t, err)

	got, found, err := cli.Get("editorFont")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []byte("Iosevka 14"), got)
}

func TestClient_GetMissingKey_FoundFalse(t *testing.T) {
	_, cli, cleanup := newClientSetup(t, "github.com/example/test.app")
	defer cleanup()

	_, found, err := cli.Get("neverSet")
	require.NoError(t, err)
	assert.False(t, found, "missing key must return found=false, not an error")
}

func TestClient_DeleteThenGet_FoundFalse(t *testing.T) {
	_, cli, cleanup := newClientSetup(t, "github.com/example/test.app")
	defer cleanup()

	require.NoError(t, cli.Set("x", []byte("v")))
	require.NoError(t, cli.Delete("x"))
	_, found, err := cli.Get("x")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestClient_EmptyValueRoundTrip(t *testing.T) {
	// Empty []byte must round-trip as found=true, distinct from
	// "never set" (found=false). The wire encoding via PersistReply
	// must preserve the distinction.
	_, cli, cleanup := newClientSetup(t, "github.com/example/test.app")
	defer cleanup()

	require.NoError(t, cli.Set("empty", []byte{}))
	got, found, err := cli.Get("empty")
	require.NoError(t, err)
	require.True(t, found, "empty-value set must distinguish from never-set")
	assert.Empty(t, got)
}

func TestClient_PermissionDenied_NoCap(t *testing.T) {
	// An app with no runtime.persist.{ownAlias}.> cap must see every
	// Storage call error on the permission gate before reaching the
	// service. This is the lockdown shape Phase A established and
	// Phase C must preserve.
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(500 * time.Millisecond)
	backend := persist.NewMemoryBackend()
	s, err := persist.NewService(bus, zerolog.Nop(), backend)
	require.NoError(t, err)
	defer s.Close()

	// No caps at all.
	busC := bus.NewClient("github.com/example/nocaps", nil)
	cli, err := persist.NewClient(busC, "github.com/example/nocaps")
	require.NoError(t, err)

	err = cli.Set("any.key", []byte("v"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission")
}
