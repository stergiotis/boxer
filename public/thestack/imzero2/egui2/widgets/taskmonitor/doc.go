//go:build llm_generated_opus47

// Package taskmonitor is the M4 of ADR-0038 — a reusable widget that
// observes task.> over the bus and renders an in-flight list (with
// progress bar + per-row cancel button) and a rolling history of
// finished tasks. Failures expand into an errorview chain renderer
// directly, so an eh.MarshalError-shaped TaskError payload reads back
// with the same look as anywhere else in the runtime.
//
// Construction follows the producer-handle pattern: callers pass a
// task.TaskApiI (typically from task.ForApp(MountCtx)), an
// IdStack-scoped idPrefix, and an Opts struct. Start subscribes the
// widget to task.> and (optionally) seeds the in-flight map from the
// supervisor's task.list.inflight snapshot. Stop tears down cleanly.
// Render is a single call inside a panel / scroll area.
//
// The widget is intentionally stateless about identity — it does not
// know its consumer's AppId or RunId. Audit + identity propagation
// happen at the producer side via task.ForApp; the monitor's job is
// purely presentation.
package taskmonitor
