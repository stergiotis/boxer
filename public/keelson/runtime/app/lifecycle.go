package app

// SubjectInstanceClosed is the lifecycle announcement a bus makes when an
// instance's client closes (ADR-0240 §SD5) — the first member of the
// `runtime.*` lifecycle family ADR-0026 §SD3 reserved. The host closes a
// window's client after the app's Unmount (ADR-0188 §SD2), so by the time
// this goes out the instance has released what it could itself; a service
// that holds resources on the instance's behalf — datasets, handles —
// subscribes and releases the rest. The payload is [InstanceClosed]; the
// envelope's sender is the closed client's own identity. A client with no
// instance key (a service, a CLI, a test) announces nothing: there is no
// instance to own anything.
const SubjectInstanceClosed = "runtime.instance.closed"

// InstanceClosed is the payload of [SubjectInstanceClosed].
type InstanceClosed struct {
	// App is the closed client's app id.
	App AppIdT `json:"app"`
	// Instance is the host-minted window or embed key the client carried.
	Instance uint64 `json:"instance"`
}
