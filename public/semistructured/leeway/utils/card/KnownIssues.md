# Known Issues & Technical Debt
## Text (`UnicodeCardEmitter`)

- **`runeWidth` assumes 1 rune = 1 column.** East Asian wide characters and combining marks are not accounted for. Would need `go.uber.org/runewidth` or equivalent for correct terminal alignment.

- **Plain sections render identically to tagged sections.** No visual distinction besides the section name being `itemType.String()`.

## JSON (`JsonCardEmitter`)

- **Set values wrapped in `{"set": [...]}`.** Adds a structural distinction from arrays but increases nesting. Consumers must handle this explicitly.

## HTML (`HtmlCardEmitter`)

- **No max-items or max-attributes cap.** Unlike the text emitter, the HTML emitter has no `MaxCollectionItems` or `MaxAttributesPerSection` limit. Large datasets will produce very large HTML.

- **No JavaScript.** The `<details>/<summary>` interactivity is HTML5-only. More complex interactions (filtering, search, sorting) would require JS.

- **Palette color collision.** The golden-ratio spacing `(idx*37) % span` can produce visually similar colors for certain section counts. Not a correctness issue but affects readability.
