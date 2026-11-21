package hostboot

import (
	"context"
	"slices"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/registration"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
// its own without owning the Boot call. An empty or repeated name or a nil
// start is recorded as a registration failure, which stops Boot
// (ADR-0396 §2).
func RegisterHostService(name string, start HostServiceStartFunc) {
	registration.Fail("hostboot.RegisterHostService", AddHostService(name, start))
}

// AddHostService is RegisterHostService returning the refusal instead of
// recording it.
func AddHostService(name string, start HostServiceStartFunc) (err error) {
	if name == "" || start == nil {
		err = eb.Build().Str("name", name).Errorf("hostboot: a host service needs a name and a start func")
		return
	}
	hostServices.mu.Lock()
	defer hostServices.mu.Unlock()
	if slices.Contains(hostServices.names, name) {
		err = eb.Build().Str("name", name).Errorf("hostboot: host service %q is registered twice", name)
		return
	}
	hostServices.names = append(hostServices.names, name)
	hostServices.starts = append(hostServices.starts, start)
	return
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
			rt.onClose(stop)
		}
	}
}
