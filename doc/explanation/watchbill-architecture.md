---
type: explanation
audience: contributor touching watchbill, or an app that enqueues work
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-15
---

# How watchbill's pieces fit

Watchbill is durable work as rows on the one substrate: a job table claimed
by writing, a worker library, a client protocol on the bus, and surfaces
that read the same rows back. The decisions are in
[ADR-0223](../adr/0223-watchbill-durable-work-on-facts.md) (the table and
the claim), [ADR-0234](../adr/0234-watchbill-client-protocol-and-worker-presence.md)
(the client protocol), [ADR-0236](../adr/0236-watchbill-management-app.md)
(the management window) and [ADR-0237](../adr/0237-watchbill-worker-presence.md)
(worker presence). This page draws them as one picture and says how a job
travels, and in what encoding, from the app that asks for it to the row that
records it.

## The components

```
                                   one ClickHouse server = one cell
 ┌──────────────────────────────────────────────────────────────────────────────────────┐
 │  boxer.watchbill (job rows, updated in place)   boxer.watchbillevent (transitions)   │
 │       ▲ claim / transition / expire                    ▲ append                        │
 │       │  guarded UPDATE + read-back (rowcas)           │                               │
 │  boxer.facts                                                                          │
 │    runtime heartbeat rows (per run)   watchbillWorker rows (presence: started/stopped)│
 └───────▲───────────────────▲──────────────────────────────▲──────────────▲─────────────┘
         │                   │ liveness (sweep, presence)   │ append       │ read
         │                   │                              │              │
 ┌───────┴───────────────────┴──────────────────────────────┴──────┐  ┌────┴──────────────────┐
 │ process A (window host)                                         │  │ process B (headless)  │
 │                                                                 │  │  boxer watchbill run  │
 │  ┌───────────────── watchbill.Worker (runtime.watchbill) ─────┐ │  │  ┌──────────────────┐ │
 │  │ poll ─► claim ─► run as keelson task ─► settle + event    │ │  │  │ same Worker, no  │ │
 │  │ sweep (heartbeat join) · expire · honour cancels           │ │  │  │ bus; heartbeat + │ │
 │  │ presence: started / stopped                                │ │  │  │ presence rows    │ │
 │  │ serves  watchbill.job.{enqueue,cancel,retry,get,list}      │ │  │  └──────────────────┘ │
 │  │ hears   watchbill.wake        says  watchbill.changed      │ │  └───────────────────────┘
 │  └──────────▲──────────────────────────────▲──────────────────┘ │
 │             │ request/reply (codecs:        │ task.<jobid>.*     │
 │             │ watchbillRequest/Reply)       │ progress · cancel  │
 │  ═══════════╪══════════════ in-process bus (inprocbus) ═════════╪═══════════════════════   │
 │             │                               │                    │
 │  ┌──────────┴─────────────┐   ┌─────────────┴────────────┐   ┌──┴──────────────────────┐  │
 │  │ apps/watchbill (window)│   │ taskmonitor widget       │   │ apps/watchbilldemo      │  │
 │  │ watchbill.Client       │   │ (embedded in both apps)  │   │ watchbill.Client        │  │
 │  │ list · cancel · retry  │   │ live progress, cancel    │   │ enqueue demo.sleep      │  │
 │  │ table · trail · workers│   └──────────────────────────┘   │ registers HandlerI      │  │
 │  └──────────┬─────────────┘                                  └─────────────────────────┘  │
 │             │ SQL over keelson('watchbill'), keelson('watchbill_event'),                   │
 │             │          keelson('watchbill_worker')                                         │
 │  ┌──────────┴───────────────────────────────────────────────────────────────────────────┐ │
 │  │ introspection endpoint (local query, clickhouse-local) ◄── providers: jobs, events,  │ │
 │  │ workers = presence rows ⋈ heartbeat + this process's Worker.Status()                 │ │
 │  └──────────────────────────────────────────────────────────────────────────────────────┘ │
 │  sqlapplet book "watchbill": in flight · failed · by kind · abandoned · timeline · workers │
 └────────────────────────────────────────────────────────────────────────────────────────────┘

 Handlers (HandlerI, registered at init per binary) ─ run inside whichever Worker links them.
 Vocabulary: runtime vocab (job/event/presence memberships) · vdd (request/reply/launch codecs).
```

Top to bottom: the cell holds four kinds of rows, two on the store-owned
watchbill tables and two on the facts table. Every process that links a
handler stands a worker over them. The window host's worker also serves the
client protocol on the in-process bus, which the two apps drive through the
typed client. Live progress and cancel ride the task subjects, because every
run is a keelson task with the job's id. Reads that are not client verbs, the
trail and the workers, go through the introspection endpoint, which the book
and the window both query.

What crosses a process boundary is the table and nothing else: the bus is
in-process, so a job enqueued in one process is claimed in another at that
worker's next poll, a cancel from one process reaches another's worker
through the row, and a worker's presence and liveness are rows the other
reads. The doorbell and the announcements are conveniences within a
process, never the truth.

## How a job travels

1. An app holding `ClientCaps` calls the typed client, which publishes a
   request on `watchbill.job.enqueue` and waits for the reply.
2. The worker serving that subject writes a queued row, wakes itself,
   announces the row on `watchbill.changed`, and replies with the row as
   written. The owner app on the row is the bus envelope's sender, never a
   field the caller wrote.
3. On its next pass the worker reads the queue, claims the row by one
   guarded update, reads it back, spawns the run as a keelson task with the
   job's id, and calls the handler registered for the kind.
4. The handler returns; the worker settles the row by one more guarded
   update and appends the transition row. A failure with attempts left
   re-queues under the policy; a re-queue that is already due wakes the
   worker again.
5. A cancel from anywhere is either the row set to `cancel` for the holder
   to honour within a poll, or the task's own cancel subject, which the
   worker turns into the same transition.

## What is serialized, and how

Three encodings, none of them free-form:

- **A verb on the bus is a facts-shaped row in sparse CBOR.** The request
  and reply DTOs (`codec/watchbillrequest`, `codec/watchbillreply`) are
  generated from their `lw:` tags on the same path as every launch request
  and task payload: each field is a vocabulary membership on a section of
  the facts schema, so the DTO marshals as one row of that table through
  the sparse-CBOR wire form, which carries only the sections the kind
  populates. The op is carried twice, in the subject's last token and in the
  payload, and the worker refuses a mismatch. A reply lays its jobs out as
  parallel arrays, one entry per job by index. The encoding is canonical,
  with times as RFC 3339 nanoseconds in UTC, so the same value is the same
  bytes.
- **The job row is leeway columns, one section per attribute.** Nothing on
  the table is a serialized command. The generated record store writes each
  attribute into its own tagged-value section, so every mutable field is a
  one-element array in its own column, which is what lets a claim rewrite
  one element and guard on another with a lightweight update
  ([the consistency page](./watchbill-consistency-model.md) says what that
  buys). The transition row is the same encoding, append-only.
- **Args, when a job has no subject row, are facts-CBOR of a declared
  kind.** The `Args` column is a blob and `ArgsKind` names what it claims.
  `WithArgs` takes a generated facts DTO, encodes it with the same codec, and
  stamps the kind from the codec itself; `ArgsOf` refuses a mismatch before
  decoding. A struct without a generated codec cannot be attached at all,
  which is [ADR-0135](../adr/0135-app-launch-requests.md) §SD2 enforced by
  construction. Replies never carry the bytes back, only the kind. Prefer a
  subject: the row in the consumer's own table that says what to do.

Two edges worth knowing: the transition row's error is the formatted text of
the error chain, not the marshalled chain, so surfaces show it as text; and
the launch config that opens the management window on a job
(`watchbillLaunch`) is one more DTO on the same codec path.

## Where to read next

- [doc/howto/watchbill-jobs.md](../howto/watchbill-jobs.md) — write a handler, enqueue, watch, from an app or a shell.
- [watchbill-consistency-model.md](./watchbill-consistency-model.md) — what the claim guarantees, in Jepsen's terms.
- [facts-bound-record-stores.md](./facts-bound-record-stores.md) — the lane the presence store was added on.
