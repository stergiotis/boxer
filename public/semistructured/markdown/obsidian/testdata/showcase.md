---
title: Feature Showcase
tags:
  - demo
  - obsidian
aliases:
  - showcase-doc
cssclasses:
  - wide
author:
  name: Alice
  email: alice@example.com
publish: true
version: 3
---

# Feature Showcase

This document demonstrates all supported Obsidian-flavored Markdown features.

## Wikilinks

- Simple link: [[MyPage]]
- Link with alias: [[MyPage|click here]]
- Link with heading: [[MyPage#Introduction]]
- Link with heading and alias: [[MyPage#Getting Started|get started]]
- Spaces in name: [[My Cool Page]]
- Broken link: [[NonExistent]]

## Embeds

- Image embed: ![[photo.png]]
- SVG embed: ![[diagram.svg]]
- Note embed: ![[SomeNote]]
- Note section embed: ![[SomeNote#Details]]

## Highlights

This sentence has ==highlighted text== in the middle.

You can use highlights for ==key terms== and ==important phrases==.

## Comments

This is visible %%but this is hidden%% and this is visible again.

Another paragraph with %%a secret note%% embedded.

## Tags

Inline tags: #project #status/active #priority-high

Nested tags work too: #area/frontend/components

## Callouts

> [!note]
> This is a standard note callout with default title.

> [!warning] Watch Out
> This callout has a custom title.

> [!tip]- Collapsed Tip
> This content is hidden by default. Click to expand.

> [!example]+ Expanded Example
> This foldable callout starts open.
> 
> It can contain **bold**, *italic*, and `code`.

> [!danger] Critical Issue
> Something went very wrong here.

> [!quote] Albert Einstein
> Imagination is more important than knowledge.

## GFM Features

### Tables

| Feature     | Status | Priority |
|-------------|--------|----------|
| Wikilinks   | Done   | High     |
| Embeds      | Done   | High     |
| Callouts    | Done   | Medium   |
| Math        | Planned| Low      |

### Task Lists

- [x] Implement wikilinks
- [x] Implement embeds
- [x] Implement callouts
- [ ] Implement math rendering
- [ ] Add dark theme

### Strikethrough

This feature is ~~no longer supported~~ deprecated.

## Combined Features

A paragraph with ==highlights==, [[wikilinks]], #tags, and %%hidden comments%% all together.

> [!info] Integration Note
> You can use ==highlights== and #tags inside callouts too.

## Code

Inline `code` and a block:

```go
func main() {
    fmt.Println("Hello, Obsidian!")
}
```
