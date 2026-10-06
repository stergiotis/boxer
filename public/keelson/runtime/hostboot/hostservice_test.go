package hostboot

import (
	"context"
	"errors"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestHostServicesStartInOrderAndStopAtClose(t *testing.T) {
	var events []string
	RegisterHostService("test.hostservice.a", func(_ context.Context, rt *Runtime) (stop func(), err error) {
		events = append(events, "start a")
		stop = func() { events = append(events, "stop a") }
		return
	})
	RegisterHostService("test.hostservice.broken", func(context.Context, *Runtime) (stop func(), err error) {
		err = errors.New("unreachable")
		return
	})
	RegisterHostService("test.hostservice.b", func(context.Context, *Runtime) (stop func(), err error) {
		events = append(events, "start b")
		return
	})
	require.Subset(t, RegisteredHostServices(), []string{"test.hostservice.a", "test.hostservice.broken", "test.hostservice.b"})

	rt := &Runtime{opts: Options{Log: zerolog.Nop()}}
	rt.bootHostServices(context.Background())
	require.Equal(t, []string{"start a", "start b"}, events)
	require.Contains(t, rt.HostServices, "test.hostservice.a")
	require.Contains(t, rt.HostServices, "test.hostservice.b")
	require.NotContains(t, rt.HostServices, "test.hostservice.broken")

	for i := len(rt.cleanups) - 1; i >= 0; i-- {
		rt.cleanups[i]()
	}
	require.Equal(t, "stop a", events[len(events)-1])
}

func TestRegisterHostServiceRefusesDuplicatesAndEmpties(t *testing.T) {
	noop := func(context.Context, *Runtime) (stop func(), err error) { return }
	RegisterHostService("test.hostservice.dup", noop)
	require.Panics(t, func() { RegisterHostService("test.hostservice.dup", noop) })
	require.Panics(t, func() { RegisterHostService("", noop) })
	require.Panics(t, func() { RegisterHostService("test.hostservice.nil", nil) })
}
