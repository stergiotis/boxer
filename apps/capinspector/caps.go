package capinspector

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// CapId is the short identifier the carousel's status bar uses when
// asking the inspector to open with a pre-selected capability. The set
// is closed — exactly the entries in Registry.
type CapId = string

const (
	CapRun     CapId = "run"
	CapFacts   CapId = "facts"
	CapBus     CapId = "bus"
	CapFs      CapId = "fs"
	CapPersist CapId = "persist"
	CapTask    CapId = "task"
)

// BackendImpl is one realisation of a capability's contract. A cap
// can have several (e.g. Facts: InMemoryFactsStore vs chstore.Store);
// the carousel reports which one is effective via SetActiveBackend.
// The inspector renders every available impl side-by-side in the
// backend row, with the effective one highlighted.
type BackendImpl struct {
	// Id is the stable identifier the carousel uses with
	// SetActiveBackend. Short and lower-case (e.g. "chstore", "inmem").
	Id string
	// Display is the short label rendered inside the backend box.
	// Must fit in ~70px at 10pt when the cap has 2 impls; 152px at
	// 11pt when the cap has 1 impl.
	Display string
}

// CapSpec describes one capability for the inspector body. The schematic
// is live-generated from the registry by reading every Manifest's Caps
// and filtering with Matches; the prose fields are static and live
// here.
type CapSpec struct {
	// Id is the short capId the inspector key by; matches one of the
	// Cap* constants above.
	Id CapId
	// Display is the human label for the cap.
	Display string
	// SubjectFamily is the NATS-style subject pattern this capability
	// serves, or "(process identity)" / "(audit backend)" for the
	// non-subject ones.
	SubjectFamily string
	// Description is a single multi-line paragraph rendered above the
	// schematic. Keep concise; the schematic carries the live data.
	Description string
	// Backend names the package + key types that implement the cap.
	Backend string
	// AppFilter returns true when an app's SubjectFilter pattern
	// relates to this capability. Nil for non-subject caps (run,
	// facts) — those have no app-level wiring.
	AppFilter func(filter app.SubjectFilter) bool
	// HostInjected is the auto-grant pattern the windowhost minted
	// for an app declaring a related Manifest field (e.g. PersistedKeys
	// → runtime.persist.{ownAlias}.>). Empty when no host injection.
	HostInjected func(m app.Manifest) string
	// Backends is the set of available implementations of this cap.
	// One per shipping concrete impl. At runtime, the carousel calls
	// SetActiveBackend(capId, backendId) so the inspector knows which
	// one is in use; non-active impls render dimmed. Must be non-empty
	// (a cap with no impls would be a runtime bug, not a documentation
	// concern).
	Backends []BackendImpl
}

// Registry is the lookup table the inspector reads to render any cap
// detail page. The set is intentionally small (one entry per shipped
// M2 cap); adding a new cap means appending here.
var Registry = map[CapId]CapSpec{
	CapRun: {
		Id:            CapRun,
		Display:       "Run identity",
		SubjectFamily: "(process identity)",
		Description: "Per-process identifier minted by runinfo.Init() at boot " +
			"and exported as PEBBLE2_RUN_ID for child processes (incl. the Rust " +
			"client). Every audit row carries this id so a session's activity " +
			"groups under one runtime-start fact. ADR-0026 §SD12 / 2026-05-12 " +
			"runtime-run amendment.",
		Backend: "runtime/runinfo + runtime/heartbeat",
		Backends: []BackendImpl{
			// runinfo holds the identity; heartbeat is a sibling
			// emitter — collaborators, not alternatives. Modelled as
			// one impl since they always ship together.
			{Id: "runinfo", Display: "runinfo + heartbeat"},
		},
	},
	CapFacts: {
		Id:            CapFacts,
		Display:       "Audit + state backend",
		SubjectFamily: "(audit backend — not a subject)",
		Description: "Where grants, audit records, app-lifecycle rows, " +
			"heartbeats, and persist state land. chstore.NewWithFallback " +
			"returns a live ClickHouse-backed Store when reachable; otherwise " +
			"InMemoryFactsStore. Read paths: LookupRunStart, LifecyclesByRun, " +
			"LastHeartbeatForRun, RecentLogs.",
		Backend: "runtime/factsstore + runtime/factsstore/chstore",
		// Two true alternatives — the canonical example of the
		// "available vs effective" visual. The carousel picks one at
		// boot via chstore.NewWithFallback.
		Backends: []BackendImpl{
			{Id: "inmem", Display: "InMem"},
			{Id: "chstore", Display: "chstore"},
		},
	},
	CapBus: {
		Id:            CapBus,
		Display:       "In-proc subject router",
		SubjectFamily: "(all subjects)",
		Description: "inprocbus.Inst routes Publish/Subscribe/Request between " +
			"per-app inprocbus.Client instances minted from Manifest.Caps. M4 " +
			"swaps the in-proc transport for NATS; the BusI surface is stable " +
			"across the swap. Every allowed call lands an audit row via the " +
			"factsstore.AsAuditSink sink the carousel attaches.",
		Backend: "runtime/inprocbus",
		// Matches every app — bus is the universal substrate.
		AppFilter: func(_ app.SubjectFilter) bool { return true },
		// One impl today; M4 lands NATS and that becomes a second
		// entry here (Id: "nats"). The inspector then shows both
		// with whichever the carousel constructed highlighted.
		Backends: []BackendImpl{
			{Id: "inprocbus", Display: "inprocbus"},
		},
	},
	CapFs: {
		Id:            CapFs,
		Display:       "fs.* Powerbox",
		SubjectFamily: "fs.dialog.{read|write|bundle|watch}, fs.handle.{uuid}.{read|close|watch|unwatch|event}",
		Description: "User-mediated filesystem access. Apps publish " +
			"fs.dialog.read to request a file pick; fsbroker.Service queues " +
			"the request; the picker overlay resolves with a user-selected " +
			"path; the broker mints an opaque handle uuid and augments the " +
			"app's caps with fs.handle.{uuid}.>. The path is never exposed " +
			"to the app — only the handle subject is. fs.dialog.watch adds " +
			"streaming directory-change notifications on the conventional " +
			"fs.handle.{uuid}.event subject; the broker grants Pub+Sub on " +
			"the per-uuid handle pattern so the app can subscribe. Watch " +
			"requests carry an optional WatchRequest.Recursive flag — when " +
			"set, events fire for the entire subtree and Name carries the " +
			"forward-slash relpath under the watch root.",
		Backend: "runtime/fsbroker (inotify primary, poller fallback for inotify-blind FS) + runtime/fsbroker/pickerbridge",
		AppFilter: func(f app.SubjectFilter) bool {
			return strings.HasPrefix(f.Pattern, "fs.")
		},
		// One Powerbox impl today; the inotify/poller dichotomy lives
		// *inside* fsbroker as a per-watch strategy choice, not as
		// alternative cap-broker implementations. A future "remote-fs"
		// Powerbox (e.g. routing through an SSH agent) would land here
		// as a second entry alongside fsbroker.
		Backends: []BackendImpl{
			{Id: "fsbroker", Display: "fsbroker"},
		},
	},
	CapPersist: {
		Id:            CapPersist,
		Display:       "runtime.persist.* state",
		SubjectFamily: "runtime.persist.{appAlias}.{key}.{get|set|delete}",
		Description: "Per-app key-value cold-state surface. " +
			"persist.NewClient(busC, appId) is the StorageI MountCtx hands to " +
			"every app; the wire subject is runtime.persist.{ownAlias}.{key}.{op}. " +
			"Keys must be a single NATS token (no dots). The windowhost auto- " +
			"injects the runtime.persist.{ownAlias}.> cap when Manifest.PersistedKeys " +
			"is non-empty — apps declare keys, not caps.",
		Backend: "runtime/persist (Service + Client + MemoryBackend)",
		AppFilter: func(f app.SubjectFilter) bool {
			return strings.HasPrefix(f.Pattern, "runtime.persist.")
		},
		HostInjected: func(m app.Manifest) string {
			if len(m.PersistedKeys) == 0 {
				return ""
			}
			return "runtime.persist." + m.Id.SubjectAlias() + ".>"
		},
		// One impl today (mem). The amendment trail names two future
		// alternatives — disk-backed and facts-backed — that would
		// each land as a sibling entry here.
		Backends: []BackendImpl{
			{Id: "mem", Display: "MemBackend"},
		},
	},
	CapTask: {
		Id:            CapTask,
		Display:       "task.* background primitive",
		SubjectFamily: "task.<id>.{created|progress|cancel|done|error}, task.list.inflight",
		Description: "Cancellable, observable background work over the bus " +
			"(ADR-0038). Apps spawn handles through task.ForApp(MountCtx); the " +
			"API auto-injects OwnerAppId / OwnerTileKey / OwnerRunId so audit " +
			"rows join back to AppLifecycleRow + RuntimeStartRow. " +
			"task.PatternAll (\"task.>\") is the universal observer subscription; " +
			"task.SubjectListInflight (\"task.list.inflight\") is a request/reply " +
			"the M3 supervisor serves with a buscodec-encoded snapshot. The " +
			"runtime/task package owns the protocol; runtime/task/supervisor is " +
			"the opt-in audit + heartbeat layer that lands rows in factsstore.",
		Backend: "runtime/task + runtime/task/supervisor",
		AppFilter: func(f app.SubjectFilter) bool {
			return strings.HasPrefix(f.Pattern, "task.")
		},
		// task and supervisor are co-deployed: the supervisor is the
		// audit hook for the producer surface, not an alternative impl.
		// Modeled as one entry covering both packages, matching how
		// runinfo + heartbeat compose under CapRun.
		Backends: []BackendImpl{
			{Id: "task", Display: "task"},
			{Id: "supervisor", Display: "+supervisor"},
		},
	},
}

// allCapIdsOrdered returns the canonical render order so the
// inspector picker UI doesn't shuffle entries across frames (Go map
// iteration is randomised).
func allCapIdsOrdered() (ids []CapId) {
	ids = []CapId{CapRun, CapFacts, CapBus, CapFs, CapPersist, CapTask}
	return
}
