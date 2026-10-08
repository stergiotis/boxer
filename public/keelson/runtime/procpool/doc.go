// Package procpool keeps warm, pre-spawned worker processes for callers that
// cannot afford a process start per request (ADR-0285).
//
// The pool knows nothing about what a worker runs. A [SpawnerI] makes one and
// returns it once it can serve; the worker frees its own resources in Close
// and then calls the [ReleaseFunc] it was spawned with. The pool counts a
// slot as free only after that call, which is what lets MaxConcurrent stand
// for a scarce resource — a licence seat, a sandbox — rather than a process
// count.
//
// What a worker is used for between AcquireE and Close is the caller's
// business: one request and exit, as
// [github.com/stergiotis/boxer/public/keelson/data/chlocalpool] does
// (ADR-0028 §SD3), or a conversation of many round trips.
package procpool
