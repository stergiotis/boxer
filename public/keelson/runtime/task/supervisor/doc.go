// Package supervisor is the ADR-0038 M3 audit + heartbeat layer for the
// task primitive. Apps using the M1 / M2 surface need no supervisor;
// hosts that want a durable record of started / finished / cancelled /
// abandoned tasks instantiate one through New() and call Start().
//
// What the supervisor does:
//
//   - Subscribes task.> via task.WatchAll and writes one factsstore
//     LogRow per terminal verb (created / done / error / cancel) plus
//     one row per abandoned-by-heartbeat detection. Progress events
//     are observed but not persisted (too high-frequency for an audit
//     trail).
//   - Maintains an in-memory snapshot of currently in-flight tasks
//     and serves it on a request/reply subject (default
//     "task.list.inflight"). UI status panels and supervisors of
//     supervisors (M4 NATS cluster bridges) query this without
//     reconstructing from history.
//   - Runs a heartbeat watchdog: an in-flight task whose last
//     observed emission is older than HeartbeatThresholdMs is
//     promoted to InflightStateAbandoned. The state surfaces in the
//     snapshot and a final audit row is written; no synthetic bus
//     event is emitted (the abandoned producer may yet recover).
//
// The supervisor does NOT own bus client construction — the host
// passes an app.BusI permissioned according to Caps(). This keeps the
// supervisor unit-testable over any BusI implementation and matches
// the M1 / M2 idiom.
package supervisor
