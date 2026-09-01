---
type: explanation
status: stable
reviewed-by: "@example"
reviewed-date: 2026-04-17
---

# Backticked paths

Existing, must pass: `doc/existing.md`, `doc/`, `./sibling.md`,
`public/pkg/file.go:12` (the pin is DL016's; the file exists).

Missing, must be flagged: `doc/phantom/2026_07_31__x.md`,
`./nope.md`.

Template, skipped: `doc/adr/NNNN-<slug>.md`, `public/*/README.md`,
`doc/{a,b}.md`, `scripts/$name.sh`, `doc/adr/0001-….md`.

Another checkout, skipped: `../boxer/doc/x.md`.

Not a path at all, skipped: `github.com/stergiotis/boxer/public/gov`,
`a/b` (no top-level dir named a), `doc` (no slash), `key: value/x`.

Link text, skipped (DL007 owns it): [`doc/existing.md`](./doc/existing.md).

```
fenced `doc/inside_fence.md` is skipped
```
