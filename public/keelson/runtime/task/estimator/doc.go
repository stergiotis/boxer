// Package estimator is the throughput + humanized-form computation that
// gates emission inside a task.Handle. Per ADR-0038, a producer's
// h.Report(p) call publishes a TaskProgress only when the visible
// representation would change — this package owns the visible
// representation and the sliding-window throughput.
//
// Inputs are raw (Current, AtMs) samples; outputs are throughput in
// units/sec, ETA in ms (or -1 when unknown), and a humanized string
// formatted via dustin/go-humanize. The package has no bus or codec
// dependency — it is pure data and may be reused outside the task
// primitive if a second consumer ever needs the same gate.
package estimator
