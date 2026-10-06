package hostboot

import (
	"context"
	"slices"
	"sync"
)

// HostServiceStartFunc starts a service on a booted runtime: the bus, the
// facts store and the task supervisor exist, no window is open yet. It
// returns the func that stops the service, which may be nil.
type HostServiceStartFunc func(ctx context.Context, rt *Runtime) (stop func(), err error)

var hostServices struct {
	mu     sync.Mutex
	names  []string
	starts []HostServiceStartFunc
}

// RegisterHostService registers a service every Boot in this process starts,
// in registration order. It is meant for init functions of packages a host
// links, so a host built from another module's command can carry services of
// its own without owning the Boot call. It panics on an empty or repeated
// name or a nil start.
func RegisterHostService(name string, start HostServiceStartFunc) {
	if name == "" || start == nil {
		panic("hostboot: RegisterHostService needs a name and a start func")
	}
	hostServices.mu.Lock()
	defer hostServices.mu.Unlock()
	if slices.Contains(hostServices.names, name) {
		panic("hostboot: host service registered twice: " + name)
	}
	hostServices.names = append(hostServices.names, name)
	hostServices.starts = append(hostServices.starts, start)
}

// RegisteredHostServices returns the names of the registered host services
// in registration order.
func RegisteredHostServices() (names []string) {
	hostServices.mu.Lock()
	names = slices.Clone(hostServices.names)
	hostServices.mu.Unlock()
	return
}

// bootHostServices starts every registered host service. A service that
// fails to start is logged and left out, like every optional service; one
// that starts has its stop run at Close.
func (rt *Runtime) bootHostServices(ctx context.Context) {
	hostServices.mu.Lock()
	names := slices.Clone(hostServices.names)
	starts := slices.Clone(hostServices.starts)
	hostServices.mu.Unlock()
	for i, start := range starts {
		stop, err := start(ctx, rt)
		if err != nil {
			rt.opts.Log.Warn().Err(err).Str("service", names[i]).Msg("host service failed to start; continuing without it")
			continue
		}
		rt.HostServices = append(rt.HostServices, names[i])
		if stop != nil {
			rt.cleanups = append(rt.cleanups, stop)
		}
	}
}
