---
type: reference
audience: contributor
status: draft
vizeval:
  size: 1600x1000
  intent: "See where storage goes: which department, and within it which team and project, holds the most."
  sinks: [hierarchy, chart, card]
  questions:
    - id: biggest-department
      prompt: "Which top-level department uses the most storage in total?"
      answer: "SELECT splitByChar('/', path)[1] AS d FROM base GROUP BY d ORDER BY sum(hot + cold) DESC LIMIT 1"
    - id: platform-top-team
      prompt: "Within platform, which team uses the most storage?"
      answer: "SELECT splitByChar('/', path)[2] AS t FROM base WHERE startsWith(path, 'platform/') GROUP BY t ORDER BY sum(hot + cold) DESC LIMIT 1"
    - id: research-teams
      prompt: "How many teams does research have?"
      answer: "SELECT uniqExact(splitByChar('/', path)[2]) FROM base WHERE startsWith(path, 'research/')"
  gates:
    text.overlap_pairs: {max: 0}
    text.clipped: {max: 0}
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Storage hierarchy

Sixteen storage projects, each an entity whose natural key is its path —
department, team, project — with a tagged section `usage` holding its hot and
cold gigabytes under a verbatim membership. The reader wants the shape of the
whole: which department dominates, how it divides. The paths make it a tree
and the sizes make it a weighted one; a table lists the numbers, a treemap or
an icicle shows the proportions, a sankey shows how the total splits level by
level.

```sql base
SELECT
  path,
  round(20 + (cityHash64(path, 1) % 4000) / 10, 1) AS hot,
  round((cityHash64(path, 2) % 9000) / 10, 1) AS cold
FROM values('path String',
  'research/vision/datasets', 'research/vision/models', 'research/nlp/corpora',
  'research/nlp/models', 'research/robotics/sim', 'platform/infra/logs',
  'platform/infra/backups', 'platform/data/warehouse', 'platform/data/lake',
  'product/web/assets', 'product/web/builds', 'product/mobile/builds',
  'product/mobile/crash-dumps', 'finance/reports/quarterly', 'finance/archive/scans',
  'legal/contracts/signed')
```

```sql
SELECT
  LW_PLAIN(toUInt64(rowNumberInAllBlocks()), 'id', 'u64', 'item:id'),
  LW_PLAIN(path, 'natural-key', 's', 'item:id'),
  LW_TV([hot, cold], 'usage', 'gigabytes', 'f64'),
  LW_TV_MEMB(['hot', 'cold'], 'usage', 'low-card-verbatim'),
  LW_TV_SUPPORT([toUInt64(1), 1], 'usage', 'lvcard')
FROM base
ORDER BY path
```
