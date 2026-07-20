// Package topo is the appliance topology vocabulary (ADR-0126): the
// component-marking contract and the compiled-in component registry.
//
// A component is a deliberately-run process family of the appliance — the
// GUI carrier, the deploy tool, the scraper, ClickHouse, NATS, the TLS
// front. Every such process carries its component token in the
// BOXER_COMPONENT environment variable, injected by whatever launched it
// (a unit-file Environment= line, a launcher script, a container env
// entry). Environment is inherited, so children — including
// extbin-spawned tools — attribute to their component for free unless a
// spawner scrubs Env.
//
// Two limits, accepted by the ADR: the mark is cooperative (operability
// identity, not a security boundary — uid and the cgroup attribute
// corroborate), and environment is exec-frozen (/proc/[pid]/environ is
// the exec-time image, so os.Setenv cannot retrofit a mark onto a
// running process; un-launched dev runs simply show none).
//
// The registry below is the declared half of the component vocabulary:
// the closed set of components the toolkit knows, in the extbin
// manifest mold — the one place to look, or grep, when auditing it. It
// is an inventory, not per-box desired state: which components should
// run on a given box is the deferred R1 desired-state store's question
// (ADR-0126 §SD6).
package topo

import (
	"regexp"
	"sort"
	"sync"

	"github.com/stergiotis/boxer/public/config/env"
)

// EnvVarName is the environment variable carrying the component mark.
// Unit files and launcher scripts reference it literally
// (`Environment=BOXER_COMPONENT=<token>`); keep the two in sync.
const EnvVarName = "BOXER_COMPONENT"

// Mark is the process's own component mark. Empty means unmarked (an
// un-launched run, or a supervisor that does not inject the variable).
var Mark = env.NewString(env.Spec{
	Name:        EnvVarName,
	Default:     "",
	Description: "component identity mark, injected by the supervisor (unit Environment= line, launcher script) and inherited by children; read by the topology layer (ADR-0126); empty = unmarked",
	Category:    env.CategorySystem,
})

// Self returns this process's component token, empty when unmarked. The
// value is reported verbatim — membership in the registry is a join
// performed by consumers, not a gate applied here.
func Self() (token string) {
	return Mark.Get()
}

// Component is one declared entry of the appliance inventory.
type Component struct {
	// Token is the registry key and the exact mark value
	// (`kind:name` graph nodes use it verbatim as name).
	Token string
	// Role is a one-line description of what the component is.
	Role string
	// Needs lists tokens of components this one depends on to function —
	// boxer's declared intent, the `component-needs` graph edges. It may
	// reference tokens declared later in this file; closure is asserted
	// by tests, not at Declare time.
	Needs []string
}

// tokenRe is the token shape: lower-spinal, led by an alphanumeric.
var tokenRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var (
	registryMu sync.RWMutex
	registry   = map[string]*Component{}
)

// Declare registers c and returns a stable handle to it. It panics on an
// empty or malformed token or a duplicate — declarations are package-init
// constants, so a clash is a programming error worth failing loudly.
func Declare(c Component) (handle *Component) {
	if !tokenRe.MatchString(c.Token) {
		panic("topo: Component.Token must be non-empty lower-spinal: " + c.Token)
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := registry[c.Token]; dup {
		panic("topo: duplicate component declaration: " + c.Token)
	}
	handle = &c
	registry[c.Token] = handle
	return
}

// Registry returns every declared component, sorted by Token. This is
// the machine-readable declared inventory (`keelson.components` serves
// it, ADR-0126 §SD5).
func Registry() (components []*Component) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	components = make([]*Component, 0, len(registry))
	for _, c := range registry {
		components = append(components, c)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Token < components[j].Token })
	return
}

// Lookup returns the declared component for token, nil when unknown.
func Lookup(token string) (c *Component) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[token]
}

// The appliance inventory. Marks in the kit unit files
// (showcase/onbox/, showcase/ansible/) must use these tokens verbatim.
var (
	// ImZero2Demo is the exposed GUI carrier — the keelson monolith
	// serving the headless pixel-streaming demo (ADR-0085).
	ImZero2Demo = Declare(Component{
		Token: "imzero2-demo",
		Role:  "exposed GUI carrier (headless pixel-streaming keelson host)",
	})

	// ImZero2Deploy is the pull-based release deploy tool — the timer
	// oneshot and both operator @-template variants share the token; the
	// cgroup attribute keeps the unit instances distinguishable.
	ImZero2Deploy = Declare(Component{
		Token: "imzero2-deploy",
		Role:  "pull-based release deploy tool (timer oneshot + operator variants)",
	})

	// Sysmetricsd is the standalone system-metrics scraper — the sole
	// /proc reader and the observed-topology collector (ADR-0090).
	Sysmetricsd = Declare(Component{
		Token: "sysmetricsd",
		Role:  "system-metrics scraper and observed-topology collector",
		Needs: []string{"nats"},
	})

	// ClickHouse is the loopback analytics server the box optionally
	// runs (facts history, play's default target).
	ClickHouse = Declare(Component{
		Token: "clickhouse",
		Role:  "loopback ClickHouse server (analytics store)",
	})

	// Caddy is the TLS + basic-auth front door proxying to the carrier.
	Caddy = Declare(Component{
		Token: "caddy",
		Role:  "TLS + basic-auth front door",
		Needs: []string{"imzero2-demo"},
	})

	// NATS is the core pub/sub bus (metric plane transport), run on-box by
	// showcase/onbox/nats.service (loopback NATS core, no JetStream). On an
	// airgapped box nats-server is built from vendored source (ADR-0026 SD4).
	NATS = Declare(Component{
		Token: "nats",
		Role:  "NATS core bus (metric plane transport)",
	})
)
