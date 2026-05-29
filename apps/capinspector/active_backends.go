//go:build llm_generated_opus47

package capinspector

import "sync"

// activeMu guards activeBackends — the carousel writes once at boot;
// inspector windows read on every frame. RW lock to keep the read
// path cheap.
var (
	activeMu       sync.RWMutex
	activeBackends = map[CapId]string{}
)

// SetActiveBackend records the implementation the runtime selected
// for one capability. Called by the carousel after each service is
// constructed (chstore.NewWithFallback resolves facts; fsbroker.NewService
// resolves fs; etc.). backendId must match one of the CapSpec.Backends[].Id
// values for the cap — the inspector renders an unknown id by leaving
// every backend dim, which is the right signal that the carousel and
// the registry drifted.
//
// Re-calling SetActiveBackend overwrites the previous value; useful
// only in tests since the carousel sets each cap exactly once per
// boot.
func SetActiveBackend(capId CapId, backendId string) {
	activeMu.Lock()
	defer activeMu.Unlock()
	activeBackends[capId] = backendId
}

// ActiveBackend returns the recorded effective backend for capId, or
// "" when the carousel didn't set one (a degraded mode — the cap's
// service likely failed NewService and is unbound).
func ActiveBackend(capId CapId) (backendId string) {
	activeMu.RLock()
	defer activeMu.RUnlock()
	backendId = activeBackends[capId]
	return
}

// resetActiveBackends is a test-only helper. Production code mutates
// the map exactly once per boot; tests need a clean slate between
// table entries.
func resetActiveBackends() {
	activeMu.Lock()
	defer activeMu.Unlock()
	activeBackends = map[CapId]string{}
}
