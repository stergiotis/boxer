---
type: reference
audience: contributor
status: draft
vizeval:
  size: 1600x1000
  intent: "See which services call which, find the busiest caller and the most depended-on service, and tell the tiers apart."
  sinks: [graph, card, unicode]
  settleMs: 3000
  questions:
    - id: busiest-caller
      prompt: "Which service calls the most other services?"
      answer: "SELECT name FROM base ORDER BY length(calls) DESC, name LIMIT 1"
    - id: orders-calls
      prompt: "Which services does orders call?"
      answer: "SELECT arrayJoin(calls) AS c FROM base WHERE name = 'orders' ORDER BY c"
      compare: set
    - id: most-depended-on
      prompt: "Which service is called by the most other services?"
      answer: "SELECT c FROM (SELECT arrayJoin(calls) AS c FROM base) GROUP BY c ORDER BY count() DESC, c LIMIT 1"
  gates:
    text.overlap_pairs: {max: 0}
    text.clipped: {max: 0}
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Service graph

Fourteen services in three tiers — edge, app, data — each an entity with a
name, a tagged section `tier` whose membership names its tier, and a tagged
section `calls` whose values name the services it calls, each call under a
membership naming its protocol. The reader wants the call structure: who
depends on whom, which service is most depended on, which tier a service is
in. A table can answer every question by reading row after row; a picture
of the graph can answer them at a glance, or hide them in a tangle.

```sql base
SELECT * FROM values(
  'name String, tier String, calls Array(String), protos Array(String)',
  ('gateway',    'edge', ['web', 'mobile-api', 'search'],          ['http', 'http', 'http']),
  ('web',        'edge', ['orders', 'users', 'catalog'],           ['grpc', 'grpc', 'grpc']),
  ('mobile-api', 'edge', ['orders', 'users', 'recommend'],         ['grpc', 'grpc', 'grpc']),
  ('orders',     'app',  ['orders-db', 'payments', 'queue', 'catalog'], ['sql', 'grpc', 'amqp', 'grpc']),
  ('users',      'app',  ['users-db', 'cache'],                    ['sql', 'resp']),
  ('catalog',    'app',  ['search-index', 'cache'],                ['http', 'resp']),
  ('payments',   'app',  ['orders-db', 'queue'],                   ['sql', 'amqp']),
  ('search',     'app',  ['search-index'],                         ['http']),
  ('recommend',  'app',  ['catalog', 'users', 'cache'],            ['grpc', 'grpc', 'resp']),
  ('orders-db',  'data', [],                                       []),
  ('users-db',   'data', [],                                       []),
  ('cache',      'data', [],                                       []),
  ('queue',      'data', [],                                       []),
  ('search-index', 'data', [],                                     [])
)
```

```sql
SELECT
  LW_PLAIN(toUInt64(rowNumberInAllBlocks()), 'id', 'u64', 'item:id'),
  LW_PLAIN(name, 'natural-key', 's', 'item:id'),
  LW_TV([tier], 'tier', 'name', 's'),
  LW_TV_MEMB([tier], 'tier', 'low-card-verbatim'),
  LW_TV_SUPPORT([toUInt64(1)], 'tier', 'lvcard'),
  LW_TV(calls, 'calls', 'target', 's'),
  LW_TV_MEMB(protos, 'calls', 'low-card-verbatim'),
  LW_TV_SUPPORT(arrayMap(x -> toUInt64(1), protos), 'calls', 'lvcard')
FROM base
```
