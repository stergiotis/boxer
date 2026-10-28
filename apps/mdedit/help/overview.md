---
type: explanation
audience: end-user
status: draft
title: Editing markdown
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Editing markdown

mdedit edits one markdown document at a time: the text you type, pasted in,
opened from a file, or loaded from a snapshot. Everything in the window reads
or writes that one string — the source pane, the preview, the outline, find
and replace, the block editor — so a change made in any of them is the same
change everywhere else. The dialect is Obsidian's: GFM with callouts,
wikilinks, highlights and frontmatter.

The window keeps a draft of its own. What you type is stored as you go and
comes back when the window next opens, whether or not it was ever saved to a
file; the *modified* badge says the buffer differs from the last point it
was copied out or restored. A document another app handed over, or one a
scene seeded, is kept apart from that draft and stays unsaved until you
save or copy it.

## Modes

The segmented control on the second bar row picks the layout. The mode is
the window's and does not persist.

- **Split** — the source beside the preview, the editing layout. The
  preview reparses as you type and scrolls to the section the caret is in.
- **Source** — the source alone, full width. The outline stays, since it
  moves the caret.
- **Read** — the preview alone. The formatting bar and the replace row go
  with the source pane; find still counts matches and takes the preview to
  them.
- **Blocks** — the document as a column of rendered blocks, one of which is
  open as text at a time. Its keys and its editors for tables, code,
  images and callouts are on the **Blocks mode and its keys** page.

## The bar

The first row is the document's gestures, then formatting. **Open**,
**Save** and **Save as** go through the system file picker; the app never
sees your filesystem, only the file you pick, and opening a file does not
bind it for saving — the first save asks where once. **Upload** persists
the document as rows another app can query; **Send to play** does the same
and opens the SQL playground on a query that reads it back. **Copy** puts
the whole document on the clipboard.

The formatting buttons wrap the selection — bold, italic, inline code,
strikethrough, highlight, link — or insert a placeholder to type over when
nothing is selected. The same wraps are on the keyboard as Ctrl+B, Ctrl+I,
Ctrl+E and Ctrl+K in both the source pane and a block.

The second row holds the mode control, the **Panel** toggle and find. The
last row, directly over the content, is the summary: which file the
document is bound to or which snapshot it was read from, whether it is
modified, whether the file moved under it on disk, a word count with a
reading estimate over the prose alone, and the status line.

The side panel shows one view at a time, chosen by its tabs: the outline or
the files. It needs a window wide enough to spare it, and hides itself
below that.

## The source pane

The source is syntax-coloured, with headings, code and emphasis set in their
own faces. Three keys do more than type:

- **Enter** continues a list, quote or callout line's marker on the next
  line and renumbers an ordered item; Enter on an item that is only its
  marker removes the marker and leaves the list. Shift+Enter is always a
  bare newline.
- **Tab** and **Shift+Tab** indent and outdent a list item by one tab, the
  unit Obsidian writes. On any other line Tab inserts a tab.
- **Ctrl+B / I / E / K** apply the formatting bar's wraps.
- **Ctrl+1 to Ctrl+6** make the caret's line a heading of that level, and
  **Ctrl+0** a paragraph.

A task box in the preview is a button: a click flips it in the source.

## Outline, find and replace

The **outline** lists the headings, nested by level; a section folds away.
Clicking one scrolls the preview to it and takes the caret with it.
*Renumber* writes numeric prefixes into every heading from the outline's own
nesting, and *Clear numbers* takes them out again; both rewrite the text
from outside the editor, so the editor's own undo does not describe them.
The column needs a window wide enough to spare it and hides below that.

**Find** is literal: no patterns, no whole-word option, and case folds
unless *Match case* is on. Matches are painted in the source pane; Enter in
the query goes to the next one, Shift+Enter to the previous. The source
pane cannot scroll itself to the caret, so a match far off-screen is
selected where you cannot see it, while the preview does scroll to its
section. **Replace** sits behind a toggle because its buttons rewrite the
buffer; it is hidden in Read mode, where the text it would rewrite is not
on screen.

## Files, proposals and the disk

The **files** view browses the lading snapshot store, read-only: a
document loaded from a snapshot is bound to no file, and *Save as* gives it
one. Such a document, and one handed over with its snapshot, offers
**rclone** in the summary row, which copies the command that mounts that
snapshot read-only.

A **proposal** is a change another program suggests for a range of the
document; it appears in a pane along the bottom with the proposed text
rendered. *Apply* replaces the range, and is refused if the buffer changed
since the proposal was made; *Discard* drops it; *Copy* takes the text to
the clipboard and leaves the buffer alone.

When the document is bound to a file, the file is watched. A change on disk
while the buffer holds unsaved edits is not loaded, and a badge says so:
save to overwrite the disk's version, or copy your work out and open to
take the disk's. A file deleted or renamed on disk leaves the buffer
untouched, and *Save as* gives it a new home.
