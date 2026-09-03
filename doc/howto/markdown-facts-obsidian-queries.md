---
type: how-to
audience: engineer with a markdown vault and a ClickHouse host
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** The queries below are the ones
> `TestObsidianQueriesOverTheFixtureVault` (the `integration` lane of
> `mddocfacts`) executes against `clickhouse local`; that test, not this
> page, is the authority on whether they still answer.

# How to ingest a markdown vault and query it like Obsidian

Read a directory of Obsidian-flavoured markdown into `boxer.facts` with the
markdown ingestor (ADR-0218, proposed), then ask it for the link graph,
backlinks, tags, sections and frontmatter properties in SQL. It covers the
`semistructured/markdown` flavour only, and it does not cover rendering or
editing — mdedit does that.

## When to use this recipe

You have a vault — or any tree of `.md` files — and want its structure
queryable beside the rest of `boxer.facts`: which notes cite which, which
carry a tag, what a property holds, which code blocks are SQL. Every ingest
writes new rows; the document's content hash is the natural key that ties
re-ingests of identical text together, so "what changed since" is a query
too.

## Prerequisites

- A ClickHouse host reachable through the registered `CLICKHOUSE_*`
  variables ([doc/env-vars.md](../env-vars.md)), with the facts schema
  provisioned by `chstore` — the ingestor verifies it and never creates it.
- The leeway SQL surface installed on that host (`lwsqlsurface.Install`,
  which a boxer host runs at startup), because the reads below expand into
  its functions.
- A host that binds the `mddocfacts` store's component SQL and the
  `mddocvocab` registry, so `LW_COMPONENT('MdLink')` and
  `LW_GET(…, 'mdFrontmatterPath', …)` resolve. play does both.

## Steps

1. **See what will be stored.** `extract` prints the ingestor's reading as
   JSON, no database involved. Directories are walked for `.md` files;
   dot-directories (`.obsidian`, `.git`) are skipped.

   ```bash
   ./boxer.sh markdown extract path/to/vault | less
   ./boxer.sh markdown ingest --dry-run path/to/vault
   ```

2. **Ingest.** Each file becomes one `mdDoc` row, one row per heading,
   fenced code block, link, emphasised span and tag, and — when the file
   opens with a YAML block — one `mdFrontmatter` row. Files are stored under
   their path relative to the directory given, forward slashes, so
   `[[folder/note]]` matches.

   ```bash
   ./boxer.sh markdown ingest path/to/vault
   ```

3. **Query.** The item kinds are components: `LW_COMPONENT('<Kind>')` yields
   a named tuple per row, filtered to conforming rows. Every item carries
   `Doc` (the document row's id) and `DocHash` (the document's content hash),
   `Ordinal`, `Line` and `Section` (the enclosing heading's ordinal, NULL
   before the first heading).

## The queries

### The graph

One edge per internal link. Targets are stored as written; resolving them is
the join: by vault-relative path, by basename (Obsidian's shortest-path
rule), or by an alias out of the frontmatter row. A `LEFT JOIN` miss is an
empty string in ClickHouse, hence the `nullIf` on each candidate.

```sql
WITH docs AS (
  SELECT d.Id AS id, d.FileName AS file,
         lower(replaceRegexpOne(d.FileName, '\\.md$', '')) AS stem,
         lower(splitByChar('/', replaceRegexpOne(d.FileName, '\\.md$', ''))[-1]) AS base
  FROM (SELECT LW_COMPONENT('MdDoc') AS d FROM boxer.facts)
),
aliases AS (
  SELECT LW_GET('foreignKey', 'mdFrontmatterDoc', 'chan:low-card-ref') AS id,
         lower(one) AS alias
  FROM boxer.facts
  ARRAY JOIN LW_CO_GATHER("stringArray:value",
                          LW_SEL_ATTRS('stringArray', 'mdFrontmatterPath',
                                       'chan:low-card-ref-high-card-params', 'param:/aliases/_')) AS one
  WHERE LW_GET('symbol', 'mdFrontmatterKind', 'chan:low-card-ref') = 'mdFrontmatter'
),
links AS (
  SELECT l.Doc AS src, lower(replaceRegexpOne(l.Target, '\\.md$', '')) AS target
  FROM (SELECT LW_COMPONENT('MdLink') AS l FROM boxer.facts)
  WHERE NOT l.External
)
SELECT s.file AS source,
       coalesce(nullIf(t1.file, ''), nullIf(t2.file, ''), nullIf(t3.file, '')) AS target
FROM links
JOIN docs AS s ON s.id = links.src
LEFT JOIN docs AS t1 ON t1.stem = links.target
LEFT JOIN docs AS t2 ON t2.base = links.target
LEFT JOIN (SELECT a.alias AS alias, d.file AS file FROM aliases AS a JOIN docs AS d ON d.id = a.id) AS t3
       ON t3.alias = links.target
WHERE target IS NOT NULL
ORDER BY source, target
```

An unresolved target (an image, a note that does not exist) drops out; keep
it with `WHERE 1` and read `links.target` to list dangling links instead.

### Backlinks

Group the graph by target.

```sql
SELECT target, groupUniqArray(source) AS sources
FROM (<the graph query>)
GROUP BY target ORDER BY target
```

### Tag resolution

Body tags and frontmatter `tags` are one kind, told apart by `Source`. A
parent tag resolves its children by prefix.

```sql
SELECT g.Name, groupUniqArray(d.FileName) AS files
FROM (SELECT LW_COMPONENT('MdTag') AS g FROM boxer.facts) AS tags
JOIN (SELECT LW_COMPONENT('MdDoc') AS d FROM boxer.facts) AS docs ON d.Id = g.Doc
GROUP BY g.Name ORDER BY g.Name;

SELECT count() FROM (SELECT LW_COMPONENT('MdTag') AS g FROM boxer.facts)
WHERE g.Name = 'meta' OR startsWith(g.Name, 'meta/')
```

### Headings and sections

The tree is carried twice: `Parent` for a walk, `Path` (the ancestors'
texts) for a filter. `Slug` is the fragment key — lower-cased, spaces to
hyphens, or the explicit `{#anchor}` — and Obsidian also resolves
`[[note#Heading]]` by the heading's text.

```sql
SELECT h.Level, h.Text, h.Slug, h.Path
FROM (SELECT LW_COMPONENT('MdHeading') AS h FROM boxer.facts) AS hs
JOIN (SELECT LW_COMPONENT('MdDoc') AS d FROM boxer.facts) AS docs ON d.Id = h.Doc
WHERE d.FileName = 'alpha.md' ORDER BY h.Ordinal;

-- which code blocks sit under which heading
SELECT c.Language, h.Text
FROM (SELECT LW_COMPONENT('MdCodeBlock') AS c FROM boxer.facts) AS cs
JOIN (SELECT LW_COMPONENT('MdHeading') AS h FROM boxer.facts) AS hs
  ON h.Doc = c.Doc AND h.Ordinal = c.Section
```

### Frontmatter properties

The frontmatter row has no component. Its leaves ride the mixed channel:
the membership `mdFrontmatterPath` carries the path as its parameter — with
array positions elided to `_` — and `mdFrontmatterParams` carries the elided
indices. Name the path with `param:`; read `*Array` sections through
`LW_GET_LIST`, `bool` through `LW_GET`; a YAML or ISO 8601 date is in
`timeArray` (zone-less values as UTC); the value-less markers `null`, `[]`
and `{}` are strings in `symbolArray`.

```sql
SELECT LW_GET_LIST('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/title')[1]  AS title,
       LW_GET_LIST('f64Array',    'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/rating')[1] AS rating,
       LW_GET('bool',             'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/draft')     AS draft,
       LW_GET_LIST('symbolArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/notes')[1]  AS notes,
       LW_GET_LIST('timeArray',   'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/created')[1] AS created
FROM boxer.facts
WHERE LW_GET('symbol', 'mdFrontmatterKind', 'chan:low-card-ref') = 'mdFrontmatter'
```

A list property is several attributes under one template; the selector pair
gathers them, and the parameter lane gives the index back:

```sql
SELECT LW_CO_GATHER("stringArray:value",
                    LW_SEL_ATTRS('stringArray', 'mdFrontmatterPath',
                                 'chan:low-card-ref-high-card-params', 'param:/tags/_')) AS tags,
       arrayMap((p, a) -> ("stringArray:mrhp"[p], "stringArray:value"[a]),
                LW_SEL('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/reviewers/_/roles/_'),
                LW_SEL_ATTRS('stringArray', 'mdFrontmatterPath', 'chan:low-card-ref-high-card-params', 'param:/reviewers/_/roles/_')) AS roles
FROM boxer.facts
WHERE LW_GET('symbol', 'mdFrontmatterKind', 'chan:low-card-ref') = 'mdFrontmatter'
```

`tags: project, alpha` — the comma-string spelling — is one string leaf at
`/tags`, not a list at `/tags/_`; the `MdTag` rows are where both spellings
meet.

## Verification

`./boxer.sh markdown ingest --dry-run` reports the row count it would write;
after an ingest, `SELECT count() FROM boxer.facts WHERE …` over the kind
marker of each kind should add up to it. The integration test
`TestObsidianQueriesOverTheFixtureVault` runs every query above over the
fixture vault under `mdextract/testdata/vault`:

```bash
go test -tags "$(cat tags) integration" -run TestObsidianQueriesOverTheFixtureVault ./public/semistructured/markdown/mddocfacts/
```

## Troubleshooting

- **Symptom:** `verify facts schema` fails on ingest.
  **Cause:** the host has never run chstore's DDL for `boxer.facts`.
  **Fix:** start a boxer host against it once, or apply the DDL the way the
  host does; the ingestor will not provision.
- **Symptom:** `LW_GET` refuses with "channel is required".
  **Cause:** every `boxer.facts` section carries more than one channel, so
  `chan:` is mandatory; the frontmatter leaves are on
  `low-card-ref-high-card-params`, everything else on `low-card-ref`.
- **Symptom:** a `[[Note]]` edge is missing from the graph.
  **Cause:** the target was written with a different case or path than the
  stored file name, or through an alias the frontmatter does not declare.
  **Fix:** read `links.target` beside the resolved column; the stored target
  is as written.
