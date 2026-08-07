package writingstylescope

// The built-in example pair. Two short engineering notes on different
// subjects, written so the app has something to show on first open and so the
// screenshot tour has a deterministic scene with no I/O.
//
// The pair is constructed to teach how the readouts are meant to be read,
// which takes three kinds of section, not two:
//
//   - "Retry budgets" in document B is a lightly reworded copy of the same
//     section in document A. It is the finding: NCD ≈ 0.27 against a
//     background that runs 0.78–0.88, detached from the bulk rather than
//     merely at its edge.
//   - "Timeouts" in both documents is the distractor: the same topic,
//     independently written. It lands around 0.82 — the 25th percentile of
//     the background, so lower than a random pair but plainly inside the
//     bulk. It is what a fixed threshold gets wrong in one direction or the
//     other, and the reason the app reports a quantile instead.
//   - Everything else is unrelated prose forming the background the other two
//     are read against.
const (
	sampleDocA = `---
title: Client resilience notes
status: draft
---

# Client resilience notes

Notes collected while making the ingest client behave under a flaky upstream.
Nothing here is specific to one transport; the same failure shapes turned up
over HTTP and over the message bus, and the fixes moved across unchanged.

## Connection pooling

The pool is sized against the upstream's concurrency limit, not against our own
core count. Sizing it larger does not increase throughput once the upstream
starts queueing — it only moves the queue from their side to ours, where it is
invisible to their metrics and to ours. We settled on a fixed pool with a small
overflow allowance and a hard ceiling, and we log every time the ceiling is
reached rather than growing silently. Idle connections are reaped on a timer
that is deliberately shorter than the upstream's own idle timeout, so the
close is always initiated by us and never surfaces as a mid-request reset.

## Retry budgets

A retry policy without a budget is an outage amplifier. When the upstream
degrades, every client retries, and the retries are themselves the load that
keeps it degraded. We cap retries as a fraction of the request rate rather
than as a per-request count: a client may spend at most ten percent of its
outgoing requests on retries, measured over a sliding window, and once that
budget is exhausted failures are returned to the caller immediately. The
per-request count still exists, but it is the second limit to bind, not the
first. The budget is what turns a retry storm back into a plain error rate.

## Timeouts

Every outgoing call carries a deadline derived from the caller's remaining
budget, not a constant. A constant timeout on a chain of three services means
the innermost call is still running long after the outermost caller gave up,
burning capacity on work whose result nobody will read. We pass the deadline
down explicitly and refuse to start a call whose remaining budget is below the
observed median latency for that route — failing fast beats failing late.

## Backpressure

The client signals saturation upward rather than buffering. An unbounded queue
in front of a slow consumer converts a latency problem into a memory problem
and then into a crash, and the crash discards work that a rejection would have
left with the caller. Our queues are bounded and reject at the head; the
rejection carries a retry-after hint derived from the current drain rate.

## What we did not do

We did not implement adaptive concurrency control. It was attractive on paper
and every prototype we built oscillated under bursty load without a damping
term we could justify. The fixed pool plus the retry budget covered the
failure modes we had actually seen, so the adaptive controller stayed out.
`

	sampleDocB = `---
title: Batch scheduler design
status: draft
---

# Batch scheduler design

How the nightly batch tier decides what to run, in what order, and what it does
when a job misbehaves. The tier is deliberately dumber than the online path:
everything it does is restartable, and nothing it does is urgent.

## Job graph

Jobs declare their inputs and outputs as dataset names, and the scheduler
derives the dependency edges from the declarations rather than from a
hand-maintained graph. This has been worth the extra bookkeeping: a job that
quietly stops reading an input loses the edge automatically, and the graph
never accumulates dependencies that stopped being real. Cycles are rejected at
registration time with the offending path printed, which turned out to matter
more than we expected — most cycles are introduced by a rename, and the path
makes the rename obvious.

## Retry budgets

A retry policy without a budget amplifies an outage. When the upstream starts
to degrade, every client retries, and those retries are themselves the load
that keeps it degraded. So we cap retries as a fraction of the request rate
instead of as a per-request count: a job may spend at most ten percent of its
outgoing requests on retries, measured over a sliding window, and once the
budget is spent, failures go back to the caller immediately. A per-request
count still exists, but it is the second limit to bind rather than the first.
The budget is what turns a retry storm back into an ordinary error rate.

## Timeouts

Batch jobs get a wall-clock ceiling and a no-progress ceiling, and the second
one does the real work. A job that is slow but advancing is usually worth
letting finish overnight; a job that has not written a row in twenty minutes is
almost always wedged on a lock or a dead connection. We kill on the progress
signal and let the wall-clock ceiling stay generous, which cut our false
cancellations to nearly none without lengthening the tail.

## Placement

Jobs are placed by their historical peak memory rather than by a declared
request, because declarations rot and the history does not. The scheduler keeps
a rolling maximum per job and pads it; a job whose padded maximum no longer
fits its class is promoted automatically and the promotion is logged. Placement
never packs a node past its memory ceiling, even when the CPU is idle, because
the failure mode of over-packing memory is a kill and the failure mode of
under-packing CPU is a slower night.

## Observability

Every run emits a row with its inputs, its outputs, its resource peaks, and its
exit reason. The rows are the only thing anyone looks at during an incident, so
they are written before the artifacts are published rather than after — a run
that failed to publish must still explain itself.
`
)
