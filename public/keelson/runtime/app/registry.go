//go:build llm_generated_opus47

package app

import (
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AppCtor produces a fresh AppI instance. Hosts that want true
// multi-instance dispatch (e.g., the M3 DockHost opening the same app
// in two tiles with isolated state) invoke the ctor once per dispatch.
// For apps registered via the legacy Register(a AppI) path, the ctor is
// a singleton closure that returns a on every call — i.e., multi-instance
// dispatch on a singleton-registered app yields shared state, matching
// the pre-M3 behaviour. Apps that want isolated per-tile state migrate
// to RegisterFactory and avoid package-level state in their AppI.
//
// Ctors should be lightweight — heavyweight resource acquisition belongs
// in Mount, not the ctor.
type AppCtor func() (a AppI, err error)

// entry is what the Registry stores: a static Manifest and a factory
// that produces AppI instances. Singletons register a closure that
// returns the same a; factories register a closure that allocates fresh.
type entry struct {
	manifest Manifest
	ctor     AppCtor
}

// Registry is the canonical list of apps in this process. Apps register
// themselves from init() via the package-level Register or
// RegisterFactory; the runtime hosts (DockHost, CliHost, ScreenshotHost)
// consume the iteration and lookup API.
//
// Manifest validation runs at registration; invalid manifests are rejected
// with an error. Duplicate Ids are rejected — first registration wins,
// subsequent attempts are logged at Warn level by the package-level helpers
// and returned as an error by the method forms.
type Registry struct {
	mu      sync.RWMutex
	entries []entry
	byId    map[AppIdT]int
}

// NewRegistry returns an empty Registry. Tests use this for isolation;
// production code uses DefaultRegistry.
func NewRegistry() (inst *Registry) {
	inst = &Registry{
		byId: make(map[AppIdT]int, 16),
	}
	return
}

// DefaultRegistry is the process-wide registry that init()-time registrations
// populate.
var DefaultRegistry = NewRegistry()

// Register adds a singleton app to DefaultRegistry. All Open() calls
// return the same a, preserving pre-M3 behaviour. Errors are logged at
// Warn and dropped — appropriate from init(), where the caller has no
// error channel. Tests and code that need the error should call
// DefaultRegistry.Register directly.
func Register(a AppI) {
	err := DefaultRegistry.Register(a)
	if err != nil {
		log.Warn().Err(err).Msg("app.Register: dropping app")
	}
}

// RegisterFactory adds a factory-backed app to DefaultRegistry. Each
// Open(id) invokes ctor() for a fresh AppI instance; multi-instance
// dispatch yields isolated state when the AppI is implemented without
// package-level state. Errors are logged at Warn and dropped.
func RegisterFactory(m Manifest, ctor AppCtor) {
	err := DefaultRegistry.RegisterFactory(m, ctor)
	if err != nil {
		log.Warn().Err(err).Str("id", string(m.Id)).
			Msg("app.RegisterFactory: dropping app")
	}
}

// Register inserts a singleton app, preserving sorted-Id iteration order.
// Internally creates a ctor that returns a on every call.
func (inst *Registry) Register(a AppI) (err error) {
	if a == nil {
		err = eh.Errorf("registry: nil app")
		return
	}
	m := a.Manifest()
	ctor := func() (singleton AppI, ctorErr error) {
		singleton = a
		return
	}
	err = inst.RegisterFactory(m, ctor)
	return
}

// RegisterFactory inserts a factory-backed app, preserving sorted-Id
// iteration order. The ctor is held until Open(id) is invoked.
func (inst *Registry) RegisterFactory(m Manifest, ctor AppCtor) (err error) {
	if ctor == nil {
		err = eb.Build().Str("id", string(m.Id)).Errorf("registry: nil ctor")
		return
	}
	err = m.Validate()
	if err != nil {
		err = eh.Errorf("registry: invalid manifest: %w", err)
		return
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	_, exists := inst.byId[m.Id]
	if exists {
		err = eb.Build().Str("id", string(m.Id)).Errorf("registry: duplicate Id")
		return
	}
	// Distinct Ids must also produce distinct SubjectAliases: the alias is
	// what keys persisted state and the auto-injected persist cap
	// (runtime.persist.{alias}.>), so a collision would silently share one
	// app's cold state and permissions with another. The Id-uniqueness check
	// above does not catch this (e.g. ".../foo/play" and ".../bar/play" both
	// alias to "play").
	newAlias := m.Id.SubjectAlias()
	for i := range inst.entries {
		if inst.entries[i].manifest.Id.SubjectAlias() == newAlias {
			err = eb.Build().Str("id", string(m.Id)).Str("alias", newAlias).
				Str("collidesWith", string(inst.entries[i].manifest.Id)).
				Errorf("registry: SubjectAlias %q collides with already-registered app %s", newAlias, string(inst.entries[i].manifest.Id))
			return
		}
	}
	idx := sort.Search(len(inst.entries), func(i int) bool {
		return inst.entries[i].manifest.Id >= m.Id
	})
	inst.entries = append(inst.entries, entry{})
	copy(inst.entries[idx+1:], inst.entries[idx:])
	inst.entries[idx] = entry{manifest: m, ctor: ctor}
	for i := idx; i < len(inst.entries); i++ {
		inst.byId[inst.entries[i].manifest.Id] = i
	}
	return
}

// Open invokes the registered ctor for id and returns the resulting AppI
// instance. Hosts that dispatch apps (DockHost tiles, CLI invocations,
// screenshot tours) call Open. For singleton-registered apps the same
// instance comes back every call; for factory-registered apps each call
// yields a fresh instance.
func Open(id AppIdT) (a AppI, err error) {
	a, err = DefaultRegistry.Open(id)
	return
}

// Open returns a fresh AppI instance from the registered ctor for id.
func (inst *Registry) Open(id AppIdT) (a AppI, err error) {
	inst.mu.RLock()
	idx, exists := inst.byId[id]
	if !exists {
		inst.mu.RUnlock()
		err = eb.Build().Str("id", string(id)).Errorf("registry: id not found")
		return
	}
	ctor := inst.entries[idx].ctor
	inst.mu.RUnlock()
	a, err = ctor()
	if err != nil {
		err = eb.Build().Str("id", string(id)).Errorf("registry: ctor failed: %w", err)
		return
	}
	if a == nil {
		err = eb.Build().Str("id", string(id)).Errorf("registry: ctor returned nil AppI")
		return
	}
	return
}

// LookupManifest returns just the static Manifest for an Id. Use this
// for enumeration / menu rendering / cap auditing — anything that wants
// metadata without instantiating an AppI. Returns ok=false if absent.
func LookupManifest(id AppIdT) (m Manifest, ok bool) {
	m, ok = DefaultRegistry.LookupManifest(id)
	return
}

// LookupManifest returns the registered Manifest for an Id, or ok=false.
func (inst *Registry) LookupManifest(id AppIdT) (m Manifest, ok bool) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	idx, exists := inst.byId[id]
	if !exists {
		return
	}
	m = inst.entries[idx].manifest
	ok = true
	return
}

// AllManifests returns the registered manifests in sorted-Id order from
// DefaultRegistry. Returned slice is a fresh copy; callers may mutate.
func AllManifests() (manifests []Manifest) {
	manifests = DefaultRegistry.AllManifests()
	return
}

// AllManifests returns registered manifests in sorted-Id order. The
// returned slice is a fresh copy.
func (inst *Registry) AllManifests() (manifests []Manifest) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	manifests = make([]Manifest, len(inst.entries))
	for i, e := range inst.entries {
		manifests[i] = e.manifest
	}
	return
}

// Lookup returns an AppI for the given Id from DefaultRegistry by
// invoking the ctor. Backward-compat shim — new code should call
// LookupManifest for metadata or Open for dispatch.
func Lookup(id AppIdT) (a AppI, ok bool) {
	a, ok = DefaultRegistry.Lookup(id)
	return
}

// Lookup invokes the ctor for id. For singleton-registered apps this is
// the singleton; for factory-registered apps each Lookup allocates fresh.
// Prefer LookupManifest or Open in new code.
func (inst *Registry) Lookup(id AppIdT) (a AppI, ok bool) {
	a, err := inst.Open(id)
	if err != nil {
		return
	}
	ok = true
	return
}

// All returns AppI instances for every registered Id by invoking each
// ctor. Backward-compat shim — prefer AllManifests for metadata
// enumeration (avoids spurious ctor calls for factory-registered apps).
func All() (apps []AppI) {
	apps = DefaultRegistry.All()
	return
}

// All invokes the ctor for every registered Id in sorted-Id order.
func (inst *Registry) All() (apps []AppI) {
	inst.mu.RLock()
	ctors := make([]AppCtor, len(inst.entries))
	for i, e := range inst.entries {
		ctors[i] = e.ctor
	}
	inst.mu.RUnlock()
	apps = make([]AppI, 0, len(ctors))
	for _, ctor := range ctors {
		a, err := ctor()
		if err != nil || a == nil {
			continue
		}
		apps = append(apps, a)
	}
	return
}

// Len returns the registered app count.
func (inst *Registry) Len() (n int) {
	inst.mu.RLock()
	defer inst.mu.RUnlock()
	n = len(inst.entries)
	return
}
