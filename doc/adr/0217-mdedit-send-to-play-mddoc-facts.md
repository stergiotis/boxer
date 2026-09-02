---
type: adr
status: proposed
date: 2026-09-02
---

# ADR-0217: send-to-play — markdown documents as a boxer.facts kind

## Context

mdedit ([ADR-0178](./0178-mdedit-markdown-editor.md)) edits a markdown buffer;
play is the SQL playground. The wish: hand the document to play in one
gesture — and do it through the `boxer.facts` data integration rather than a
bespoke viewer channel, so a sent document is a fact with a timestamp, a hash
and a name, "what did I send and when" is a query, and play renders it with
machinery it already has.

Every seam this needs exists. Facts-bound generated record stores are the
standing way to persist a new kind
(doc/explanation/facts-bound-record-stores.md; sysmfacts is the worked
example, [ADR-0184](./0184-sysmetrics-persistence-tee.md) §SD2's
externally-provisioned rule included). Cross-app launching is
`windowhost.open` with a `playLaunch` config
([ADR-0135](./0135-app-launch-requests.md)); component reads are
`LW_COMPONENT` over an explicitly registered store
([ADR-0189](./0189-component-sql-authoring-surface.md)); and a result column glossed
`text/markdown` renders as a document in play's Detail pane
([ADR-0123](./0123-play-content-typed-detail-cells.md) /
[ADR-0186](./0186-play-gloss-catalog.md)). What this ADR decides is the kind,
its home, and the handover's exact shape.

## Decision

**SD1 — a new vocabulary and kind, in neutral packages.**
`public/semistructured/markdown/mddocvocab` claims tag value 2178316 (the
width-32 class, ADR-0183 D0; assignments golden beside it) and names six
memberships: `mddocKind`, `mddocTitle`, `mddocFileName`, `mddocContent`,
`mddocContentHash`, `mddocWords`. `mddocfacts` beside it holds the one DTO
(`MdDoc`, kind `mdDoc`) and the store storegen emits over it. Neutral
territory is load-bearing: play registers the store's component SQL and
mdedit writes rows, and neither may import the other — importing an app
package drags its registering `init()` (the ADR-0017 §SD4 hazard).

**SD2 — append-shaped, with two-level identity.** Every send is a new row:
`Id` hashes (content, send time) and is the launch query's filter key, while
`NaturalKey` (and the queryable `mddocContentHash` column) hash the content
alone, so re-sending identical text is visibly the same entity across sends.
Nothing is updated or deleted — the shape facts-bound stores support today,
and the right one for a send log. The store never provisions:
`VerifySchema` refuses a host that has not run chstore's DDL, and the sender
surfaces that as a status line (ADR-0184 §SD2's posture, restated at the new
call site).

**SD3 — the handover is a launch query, not shipped bytes.** mdedit ingests
one row, flushes, and opens play via `windowhost.open` with

```sql
SELECT gloss(tupleElement(LW_COMPONENT('MdDoc'), 'Content'), 'text/markdown', 'label', 'doc')
FROM boxer.facts
WHERE "id:id" = <row.Id>
LIMIT 1
```

`AutoRun: true, Tab: "detail"` — play executes it and the Detail pane renders
the `doc@text/markdown` column as a document. The content type is declared
via the gloss macro, never sniffed (ADR-0123's rule); `tupleElement` is the
pinned canonical authoring form; the component read carries the kind's own
conformance filter, so the id filter narrows an exact read rather than
defining one. The alternative — an `adhocdata` Arrow publish over the
introspection endpoint (writingstylescope's handover) — ships content without
persisting it, which is exactly the half this flow wants kept: the fact row
IS the feature, the play window is its view.

**SD4 — explicit registration, wiring-site reviewed.** play's
`RegisterComponents` registers `mddocfacts.MddocComponentSQL` beside
sysmetrics and the lading policy — never by package init (link-set
determinism), and pinned by a kind-roster test so a kind disappearing from
the host is a test failure, not a silently dark query surface. mdedit's
manifest grows exactly one cap: Pub on `windowhost.open`, with the cap-list
tripwire test updated (the growth-is-deliberate rule).

**SD5 — the whole pipeline runs off the render thread, bounded.** Connect
(`chclient.ConfigFromEnv` — the registered `CLICKHOUSE_*` variables, no new
env surface), ping, verify, ingest, flush, launch: one goroutine, one
timeout, failures into the status line. The button renders unconditionally
and drops clicks while a send is in flight — the house rule — rather than
hiding when ClickHouse is unconfigured, the same posture tally takes.

## Consequences

- `boxer.facts` gains its second generated-store kind family and the tree its
  second facts-bound store; the explanation doc's "worked example" singular
  becomes a pattern with two instances.
- The sent document is retained in ClickHouse under the facts TTL/lifecycle
  regime — a deliberate property (it is the point), worth knowing when the
  buffer holds something that should not persist: send is a publish.
- The launch query depends on play binding its column-name resolver (the
  friendly `"id:id"` handle) and registering the component — both are
  standing wiring, both now test-pinned.
- Deferred: a picker over previously sent documents (the rows are already
  queryable in play by hand), sending selections rather than whole documents,
  and any read path in mdedit itself.

## Verification

Vocabulary assignments golden; storegen generation test; a component-SQL
expansion golden plus a launch-shaped read test (the caller's id filter
survives beside the injected conformance filter); pure tests over the row's
identity rules and the launch SQL; play's kind-roster test. The end-to-end
path needs a live ClickHouse with the facts DDL applied and stays manual:
set `CLICKHOUSE_ENDPOINT`, open mdedit in the imzero2 host, Send to play.
