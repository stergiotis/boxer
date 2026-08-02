---
type: reference
audience: leeway/card maintainer; known-issue tracker
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
# Known Issues & Technical Debt
## Text (`UnicodeCardEmitter`)

- **`runeWidth` assumes 1 rune = 1 column.** East Asian wide characters and combining marks are not accounted for. Would need `go.uber.org/runewidth` or equivalent for correct terminal alignment.

- **Plain sections render identically to tagged sections.** No visual distinction besides the section name being `itemType.String()`.

## JSON (`JsonCardEmitter`)

- **Set values wrapped in `{"set": [...]}`.** Adds a structural distinction from arrays but increases nesting. Consumers must handle this explicitly.

## Topology sparks (`TopologySpark`, `BrailleSpark`, `TreemapSpark`)

- **All three assume 1 rune = 1 column**, like `UnicodeCardEmitter`. The braille
  and box-drawing glyphs they emit are single-width, but section *names* are
  passed through verbatim, so a wide East Asian character in a name still
  misaligns the row. `TreemapSpark` measures in runes rather than bytes since
  the fix below, which is correct for everything except double-width runes;
  closing that last gap needs a display-width table.
