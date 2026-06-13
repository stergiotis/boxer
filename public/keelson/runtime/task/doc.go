// Package task is keelson's bus-protocol primitive for long-running,
// cancellable, observable work — the M1 surface of ADR-0038.
//
// An app spawns a task via Spawn(parent, bus, opts). The returned HandleI
// is the producer-side API:
//
//	h, err := task.Spawn(ctx, bus, task.SpawnOpts{
//	    Kind:        "ch.export",
//	    Title:       "Export rows to parquet",
//	    Cancellable: true,
//	    EstimatedMs: 30_000,
//	})
//	for row := range rows {
//	    if h.Cancelled() {
//	        break
//	    }
//	    h.Report(task.ProgressReport{Current: n, Total: nTotal, Unit: task.UnitItems})
//	}
//	_ = h.Done(nil)
//
// The handle owns:
//
//   - A context.Context derived from the caller's parent, cancelled when
//     either parent cancels OR a task.<id>.cancel message arrives OR the
//     handle reaches a terminal state (Done/Error).
//   - An emission gate: Report is throttled by the humanized form of the
//     progress (e.g., "47% · 2m12s left") — a publish happens only when
//     the visible string would change, plus a 1 Hz heartbeat for
//     indeterminate-mode tasks.
//   - A cancel subscription on task.<id>.cancel that cancels the
//     internal context.
//
// Observers watch via WatchAll(bus, observer). The wire is canonical CBOR
// through runtime/buscodec; subjects are flat: task.<id>.<verb> with
// verbs created | progress | cancel | done | error.
//
// See ADR-0038 (background task primitive) for the rationale and
// EXPLANATION.md for the cancellation semantics and the emission gate
// design. The optional supervisor lands in M3 as a separate package.
package task
