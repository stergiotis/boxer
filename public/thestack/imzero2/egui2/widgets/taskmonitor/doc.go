// Package taskmonitor is a semi-retained imzero2 widget (ADR-0267), the M4 of
// ADR-0038: it observes task.> over the bus and renders an in-flight list
// (with progress bar + per-row cancel button) and a rolling history of
// finished tasks. Close is required once Start succeeded — it releases the
// bus subscription. Failures expand into an errorview chain renderer
// directly, so an eh.MarshalError-shaped TaskError payload reads back
// with the same look as anywhere else in the runtime.
//
// Construction follows the producer-handle pattern: callers pass the host's
// id stack and a scope key, a task.TaskApiI (typically from
// task.ForApp(MountCtx)), and an Options struct re-read every frame through
// Monitor.Opts. Start subscribes the widget to task.> and (optionally) seeds
// the in-flight map from the supervisor's task.list.inflight snapshot. Close
// tears down cleanly. Render is a single call inside a panel / scroll area
// and reports the cancels the user requested.
//
// The widget is intentionally stateless about identity — it does not
// know its consumer's AppId or RunId. Audit + identity propagation
// happen at the producer side via task.ForApp; the monitor's job is
// purely presentation.
package taskmonitor
