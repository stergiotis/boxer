// Package watchbill is durable work as facts (ADR-0223): a job is a row
// on a store-owned table, a worker claims it by one conditional update and
// reads its own mark back (§SD3), every transition is a guarded update
// followed by an event row (§SD2), a lease is the runtime heartbeat of the
// run that holds the job (§SD4), and the bus carries an id as a doorbell
// and nothing else (§SD5).
//
// A consumer registers a [HandlerI] for a kind, enqueues with [Enqueue],
// and the process's [Worker] — one per host, started by hostboot or by a
// CLI verb — runs it. Delivery is at-least-once and handlers are
// idempotent. To every observer a running job is an ordinary keelson task
// (ADR-0038): its progress rides the task's subjects, and the task's cancel
// is the job's.
//
// [StoreI] is the seam between the worker and the table: [SqlStore] is the
// production shape over a generated store, [MemStore] the test double.
package watchbill
