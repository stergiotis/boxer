---
type: explanation
audience: contributor
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

## Treemap layout primitives

Pure computation — no UI dependency. Used by the `treemap` widget package
for tile placement.

- **Squarified treemap** algorithm (Bruls, Huizing, van Wijk 2000), inlined
  from `capmap` (MIT).
- `ComputeLayout(root, w, h)` and `ComputeLayoutAt(root, bounds)` produce a
  `*Layout` with `RectOf(node)` lookups.
- `Node`: `Name`, `Size`, `Children`. `TotalSize()` recursively sums.
- `Rect`: `X, Y, W, H` in `float64`.

The painter-based cushion-rendering layer (`treemap_cushion.go`) and the
original interactive demo were removed once the Frame-based widget in the
parent `treemap` package became the sole consumer; see git history for the
original two-layer design.
