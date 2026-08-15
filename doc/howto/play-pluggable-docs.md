---
type: how-to
audience: engineer with a specific task
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to plug a custom Docs source into the `play` app

The [`play` app](../../apps/play/)'s **Docs** tab answers "what is this?" for
the name under the SQL editor's caret. By default it answers from
ClickHouse's own `system.documentation`. A library that re-uses
`play.PlayApp` can point it at a different corpus — its own vocabulary table,
a different table's documentation, or something with no ClickHouse
involvement at all — without forking the app.

The seam is one interface and one method:

```go
// DocsSourceI is how the Docs pane finds documentation for a name and makes
// sense of its corpus's own links.
type DocsSourceI interface {
	Lookup(name string) (entries []DocsEntry, ready bool, err error)
	LinkClaimed(url string) bool
	LinkCandidates(label, url string) []string
	AbsolutiseLinks(md string) string
	AbsoluteURL(url string) string
	EmptyHint() string
	ExplainError(err error) string
	Close()
}

// Install your source; nil restores ClickHouse's own system.documentation.
func (inst *PlayApp) SetDocsSource(src DocsSourceI)
```

play ships `ClickHouseDocsSource` — the source an unconfigured `PlayApp` with
a live client already uses — as a ready-to-use `DocsSourceI`, not just a
reference implementation: a ClickHouse-backed re-user (Pattern A) typically
reconfigures it rather than writing a new one. A corpus that is not a
ClickHouse query away implements the interface directly (Pattern B).

## Install the source

Set it once, right after you build the `PlayApp` — the same place
[`PlayLauncher.Mount`](../../apps/play/app_register.go) does its own
post-construction wiring. It takes effect on the next frame; there is
nothing to unregister:

```go
inner := play.NewLivePlayApp(client, initSQL, 100, rules) // rules: a *gloss.Repository, or nil for play's default
inner.SetDocsSource(mySource) // or a reconfigured play.ClickHouseDocsSource
```

## Pattern A — point ClickHouseDocsSource at your own table (most common)

If your corpus is a ClickHouse table shaped like `system.documentation` (or
can be queried into that shape), reuse the ready-made source and change only
what it queries:

```go
src := play.NewClickHouseDocsSource(client)
src.Query = `SELECT term AS name, 'Ontology' AS type, description, '' AS source
             FROM dspl.ontology_terms WHERE lower(term) = lower({n:String})`
src.SiteBase = "" // this corpus's descriptions carry no links to absolutise
inner.SetDocsSource(src)
```

`Query` must select four columns aliased `name`, `type`, `description`,
`source`, accept the looked-up name as `{n:String}`, and order results so the
preferred kind sorts first when a name is ambiguous — see
[`ClickHouseDocsSource`](../../apps/play/play_docs_clickhouse.go)'s doc
comment for the exact shape `decodeDocRows` expects.

Want both ClickHouse's builtins and your own vocabulary in the same pane?
`UNION ALL` them into one `Query` — `ClickHouseDocsSource` never needs to know
about more than one source, and the pane's existing kind selector already
handles a name whose entries come from both halves:

```sql
SELECT name, toString(type) AS type, description, source FROM system.documentation
WHERE lower(name) = lower({n:String})
UNION ALL
SELECT term AS name, 'Ontology' AS type, description, '' AS source FROM dspl.ontology_terms
WHERE lower(term) = lower({n:String})
ORDER BY name = {n:String} DESC, type
```

## Pattern B — implement DocsSourceI from scratch

For a corpus that is not one ClickHouse query away — an embedded static
corpus, an in-memory map, a different service entirely — implement the
interface directly. A source with nothing worth intercepting as an in-pane
link can stub the four link methods to inert values, which just means every
link in a body opens in a browser instead of being followed in place:

```go
type myDocs struct{ /* ... */ }

func (s *myDocs) Lookup(name string) (entries []play.DocsEntry, ready bool, err error) {
	// ready=false means "still working, ask again next frame" — see below.
	// ...
	return entries, true, nil
}
func (s *myDocs) LinkClaimed(url string) bool               { return false }
func (s *myDocs) LinkCandidates(label, url string) []string { return nil }
func (s *myDocs) AbsolutiseLinks(md string) string           { return md }
func (s *myDocs) AbsoluteURL(url string) string              { return url }
func (s *myDocs) EmptyHint() string                          { return "Put the caret on a name." }
func (s *myDocs) ExplainError(err error) string               { return err.Error() }
func (s *myDocs) Close()                                      {}
```

## The Lookup polling contract

`Lookup` is driven by the pane's own debounce — called at most once per
frame, after the caret holds still on a name — and **must not block the
render thread**. If the answer is not ready yet (a request you started is
still in flight), return `ready=false`; the driver calls `Lookup` again next
frame, and the last-good answer for a *different*, already-resolved name
keeps showing in the meantime. This is the same shape `ClickHouseDocsSource`
gets for free from the query lane (`nodeLane.demand`): start the work once,
poll for it, let the last-good result ride until a fresh one lands. A source
backed by something synchronous and cheap (an in-memory map) can simply
always return `ready=true`.

## Further reading

- [`DocsSourceI`](../../apps/play/play_docs_source.go) — the full interface
  contract.
- [`ClickHouseDocsSource`](../../apps/play/play_docs_clickhouse.go) — the
  ready-to-use default, and what a reconfigured `Query` must produce.
- [The Docs pane's engine](../../apps/play/play_docs.go) — the debounce,
  cache and caret-to-candidate-name walk that sit above whatever source is
  installed.
- [play-gloss-rules.md](./play-gloss-rules.md) — the standing gloss rules a
  host hands play at construction
- [play-pluggable-detail.md](./play-pluggable-detail.md) — the sibling seam
  (`SetDetailContent`) this one was modelled on.
- [ADR-0147](../adr/0147-sqleditor-widget-and-completion.md) — the
  caret-entity seam the Docs pane's candidate walk is built on.
