---
type: reference
audience: end-user
status: draft
title: Blocks mode and its keys
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Blocks mode and its keys

In Blocks mode the document is a column of rendered blocks — paragraphs,
headings, lists, quotes, tables, code, images, callouts — and one of them is
open as text at a time. Click a block to open it; the others stay rendered.
There is no second model behind the blocks: the open block is a window on
the same source string the other modes edit, and what it writes lands in the
source byte for byte. Markdown you type into a block is markdown — a line
starting with `# `, `- `, `1. `, `> ` or a fence becomes what it says the
moment the block closes, since the document is reparsed after every change.

Undo and redo are the document's, not the open block's: Ctrl+Z and Ctrl+Y
walk every change made in this mode, across blocks, including a table's
reshapes. Typing coalesces into one entry while it is continuous; a key
gesture or a menu operation is an entry of its own.

## In an open block

| Key | What it does |
| --- | --- |
| Enter at the end of a paragraph or heading | splits off a new paragraph below and moves into it |
| Enter inside a paragraph | splits the block at the caret |
| Enter in a list, quote or callout line | continues the line's marker; Enter on an item that is only its marker removes the marker and leaves the list |
| Shift+Enter | a soft line break inside the block |
| Tab, Shift+Tab on a list item | indents or outdents the item by one tab; elsewhere Tab inserts a tab |
| Backspace at the start of a block | a heading or list item becomes a paragraph first; a paragraph joins the text block above it; a block that cannot be joined (an image, a rule, a table) is selected instead |
| Up with the caret at the block's start, Down at its end | moves into the block above or below, caret at its end or start. The text's edge stands in for the edge row, so Up on the second row of a wrapped line leaves the block |
| Ctrl+B / I / E / K | wraps the selection in bold, italic, inline code or a link, as the formatting bar does |
| Ctrl+Z, Ctrl+Y | undo and redo, over the whole document |
| Escape | closes the block and selects it |

## A selected block

Escape, or Backspace against a block that cannot be joined, leaves text
editing with the block selected.

| Key | What it does |
| --- | --- |
| Up, Down | selects the block above or below |
| Shift+Up, Shift+Down | extends the selection over neighbouring blocks |
| Enter | opens the selected block, caret at its end |
| Delete, Backspace | removes the selected blocks |
| Ctrl+C | copies the selected blocks' source to the clipboard |
| Ctrl+Z, Ctrl+Y | undo and redo |
| Escape | clears the selection |

## Tables

A table opens as a grid of cells with a handle above every column, before
every row, and at the top left for the table. A handle's menu holds the
structural operations: insert a row or column beside this one, move it,
delete it, align a column left, centre or right, sort the body by a column,
copy a row or the table as tab-separated text, add a row or column at the
end, reset the column widths. The columns resize by dragging the header
borders; a width is the window's and is not written into the document.

Sorting is stable and numeric-aware — numbers before text, text without
regard to case, empty cells last either way. The header row stays first
through every operation: nothing is inserted above it and it does not move.

| Key | What it does |
| --- | --- |
| Tab, Shift+Tab | the next or previous cell, wrapping at a row's ends; Tab past the last cell adds a row |
| Enter | the cell below; Enter in the last row adds a row |
| Up, Down | the cell above or below; Up from the header and Down from the last row leave the table for the neighbouring block |
| Left, Right at the text's edges | the previous or next cell, wrapping at a row's ends; past the first or the last cell, the neighbouring block |
| Ctrl+Up, Ctrl+Down | moves the caret's row up or down, below the header |
| Ctrl+Left, Ctrl+Right | moves the caret's column left or right |
| Ctrl+B / I / E / K, Ctrl+Z, Ctrl+Y | as in any block |
| Escape | closes the table and selects it |

Pasting a block of cells copied from a spreadsheet into a cell fills the
cells to the right and below from that position, adding rows and columns as
needed. A pipe typed into a cell is escaped in the source.

What a GFM table cannot carry, the grid does not offer: merged cells, more
than one line in a cell, several header rows, captions, cell colours. The
alignment a column is given is written into the delimiter row, where the
rendered table and other renderers read it; the grid's own cells stay left
while the table is open.

## Code, images and callouts

A **fenced code block** opens as a language field over a monospace body; the
fence lines themselves are kept out of reach. Tab in the body inserts a tab.

An **image** opens as its picture over fields for the alt text, the target
and the size, in whichever spelling — the markdown image or the wikilink
embed — the source used.

A **callout** opens as a type menu, a fold switch and a title over its body,
with the `> ` prefixes stripped from the body and put back when it closes.

In each, Escape closes the block and selects it, and Ctrl+Z and Ctrl+Y walk
the document's undo.

## What stays the same

The outline follows the caret from block to block, and clicking a heading
in it opens the block holding it. Find paints nothing in this mode but
still counts matches and opens the block holding the current one. The
formatting bar and the replace row go with the source pane, as they do in
Read mode.
