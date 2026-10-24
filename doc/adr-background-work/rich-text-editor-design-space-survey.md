---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-24 as background to
> a decision not yet proposed: whether, and in what shape, `apps/mdedit`
> ([ADR-0178](../adr/0178-mdedit-markdown-editor.md)) grows from a
> source-beside-preview markdown editor into a rich-text editor a
> non-programmer can use. Nothing here is a decision; §9 maps the findings
> onto the shapes available in this tree and §10 lists the forks a design
> dialogue has to settle. §2 says where the tree stood on the compile date
> and will go stale as it moves; the ADRs it cites are authoritative.
>
> **Provenance — clean-room.** This page rests on product and API
> documentation, user manuals, help-centre pages, specifications and
> standards, published papers, vendor and author blog posts, changelogs and
> release notes, forum threads, and the *prose* of public issue and
> pull-request conversation pages. **No source file, repository code view,
> diff, commit page or package content of any surveyed project was read**,
> and where a permitted page carried code the behaviour is described and the
> code is not. The rule was applied to permissively-licensed projects exactly
> as to copyleft ones — the bar [ADR-0119 §SD6](../adr/0119-imzero2-pipelineview-widget.md)
> set: method-level extraction, everything re-derived. Two boundaries are
> drawn explicitly. egui is this tree's rendering substrate, not an
> inspiration source, and its *documentation* was read as a dependency's
> documentation is; its source was not consulted for this page. The tree's
> own code was read freely and is what §2 describes.
>
> The reading was done on the compile date by seven delegated passes, one per
> area (§§3–8 name them; the seventh, on end-user product decisions, is
> folded into §3.3 and §3.4), each held to the rule above and each required to
> tag every claim as read on a fetched page, seen only in a search snippet,
> recalled, or inferred. A claim appears below as fact only where a pass read
> it on a page; snippet-only and recalled claims are marked or left out, and
> inference is marked *(inference)*. **A number or a quoted rule below is a
> pass's reading of a page only where it sits inside quotation marks.** §11
> lists the pages consulted and the incidents under the protocol.

# A rich-text editor for end users — the design space, and what it meets in this tree

## 1 Question and scope

`apps/mdedit` is a markdown editor composed from seams that already existed:
a plain monospace `TextEdit` over the source, a `markdown.Doc` preview
reparsed when the buffer changes, an outline, find and replace, file I/O
through the Powerbox, and a formatting bar that wraps the selection in
markers ([ADR-0178](../adr/0178-mdedit-markdown-editor.md)). It is an editor
for someone who already writes markdown. The question here is what it would
take to make it an editor for someone who does not — who expects bold to
look bold while they type, a heading to be a heading rather than `## `, a
table to be a grid, and an image to be a picture — without giving up the
file on disk being markdown that other tools read unchanged.

"Rich text editor" is used here for the *experience*, not for a document
format: the document stays Obsidian-flavoured markdown, and the editor's
job is to present and edit it structurally. Three sub-questions:

1. **Which editing model** — a source editor with styling painted over it, a
   document rendered in place with markers revealed at the caret, a list of
   blocks each editable on its own, or a fully owned text engine — and what
   each costs against this tree's constraints (§2).
2. **Which behaviours** make such an editor read as solid to an end user:
   the caret, selection, undo, typing, clipboard and formatting conventions
   that platforms and the editors people trust have settled on (§6), and the
   ones that are contested.
3. **What must round-trip**: which markdown constructs can be rendered and
   edited in place without the editor rewriting source the user never
   touched, and where CommonMark and the Obsidian dialect make that hard (§7).

Out of scope: real-time collaboration and its data structures (named in §4
only where they constrain the document model), spell-checking engines,
and any decision about dependencies. ADR-0178 §Alternatives rejected "a
WYSIWYG or hybrid-preview editor" for its first cut on the ground that the
preview has no inverse mapping from geometry to source and that building one
is larger than the whole of M0. This page is the look at what "larger" means.

## 2 What the tree has, and where it stops (2026-10-24)

Read from the tree on the compile date. Each item is anchored to a symbol or
an ADR marker so a reader can check whether it still holds.

**The editing widget.** The only editable text surface is egui's `TextEdit`,
reached through the `textEdit` IDL node
([`egui2_definition_d_widgets.go`](../../public/thestack/imzero2/egui2/definition/egui2_definition_d_widgets.go)).
Its methods beyond egui's own are the seam ADR-0130 and its updates built:
`insertAtCursor` (replace the selection, caret after the splice —
[ADR-0063](../adr/0063-imzero2-textedit-insert-at-cursor.md)),
`highlightJob` (colour sections over the live buffer, reconciled across the
one-frame lag), `sectionStyled` (sparse overlays: underline, background,
strikethrough, italics — exactly the four `STYLE_*` flags in
`text_edit_highlight.rs`), `noWrapLayout`, `reportCursor` / `setCursor`
(the caret as a packed char range, out and in), `captureTab` and
`captureKeys`. Three limits of that seam matter here:

- **One font for the whole buffer.** The layouter resolves
  `TextStyle::Monospace` once and gives every section the same `font_id`;
  sections vary colour and the four style flags, never size, family or
  weight. egui's own `LayoutJob` can vary font, size, line height, letter
  spacing and vertical alignment per section (§5.1), so this is a seam
  limit, not a toolkit one — but lifting it is a Rust change, and any
  change under `rust/imzero2/` moves the browser host digest
  ([ADR-0278 §SD5](../adr/0278-tab-mode-for-downstream-apps.md)).
- **Every byte is laid out.** A `LayoutJob` covers the whole buffer; there is
  no way to hide a byte range (a marker) from the galley while keeping it in
  the text. ADR-0130's reconcile contract exists because a job that does not
  cover every byte drops glyphs.
- **Styling is one frame stale.** Go computes sections from the buffer it
  received at the previous frame's write-back; the live buffer may already
  hold this frame's keystrokes. The seam shifts spans past a single edit
  region to cover the gap. Anything that must react to the caret *this*
  frame — revealing markers as the caret enters a span — inherits the lag.

**The caret channel.** `reportCursor` is unconditional per frame and
`setCursor` positions but does not reveal (an off-screen selection is not
scrolled to — ADR-0130, update of 2026-08-08). There is no byte-range-to-rect
channel: nothing reports where a span of the buffer sits on screen, which is
what killed the tethered per-literal editor (ADR-0130, update of 2026-07-25
"the tethered per-literal editor phase is deferred") and would be needed for
any popover anchored to text — a link editor, a bubble toolbar over the
selection.

**The keyboard.** [ADR-0177](../adr/0177-imzero2-focus-scoped-keyboard-capture.md)
gives a focused widget a capture mask over a *subset* vocabulary — arrows,
Home, End, PageUp, PageDown, Enter, Space, Escape, Tab, Backspace, Delete
(§SD4). There are no letter keys, and the mask matches the key
modifier-agnostically with the modifier reported alongside (§SD5), so a
capture of `F` would eat every `f` typed — the reason Ctrl+F stays deferred in
mdedit (ADR-0178, update of 2026-09-01). Text input and type-ahead are out of
ADR-0177's scope by its §SD10. So Ctrl+B / Ctrl+I / Ctrl+K, the formatting
shortcuts every end-user editor has, have no channel yet; the formatting bar
is buttons.

**Undo.** Undo is egui's per-widget `Undoer`, which snapshots the whole
buffer string plus the cursor on a timer (§5.1 has the documented settings).
Every buffer rewrite mdedit does outside the widget's own edit path — replace
all, heading renumbering, an applied LLM proposal — is "just another state"
to it (ADR-0178 M3's standing caveat). Nothing in the tree owns a
document-level undo history, and nothing could span two widgets.

**The clipboard.** `clipboard.write` carries raw UTF-8 only; a rich-text or
image flavour is a documented extension point and not built
([ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md), clipboard
section). There is no `clipboard.read` subject — paste is egui's own
`Event::Paste` into the focused `TextEdit`, so the app never sees pasted
content before the widget has it, and HTML or RTF arriving on the clipboard
is not visible at all.

**The preview.** `markdown.Parse` lowers the goldmark AST into a Go-side
segment tree of retained blobs, and `Doc.Render` splices it per frame
(`widgets/markdown/EXPLANATION.md`). It renders headings, styled paragraphs,
code blocks, lists, blockquotes, callouts, wikilinks, embeds, images, GFM
tables (fixed row height, no wrapping cells), footnotes, task glyphs. Four
facts bear on an editor:

- The segment tree keeps the source byte offset of each heading's text
  (`HeadingInfo.ByteOffset`) and each code block's verbatim text, and nothing
  else of provenance. There is no mapping from rendered geometry back to
  source bytes — the reason task checkboxes are glyphs rather than controls
  ([ADR-0180](../adr/0180-markdown-rendering-fidelity-pass.md)).
- The parser does keep provenance: every goldmark block node carries
  `Lines()`, the source segments it was parsed from, and every text node a
  `Segment`. A block-to-source-range map is therefore available at parse
  time without a second parser; it is the *lowering* that discards it.
- Lists are hand-rolled from horizontal and vertical containers, and the
  package's own trade-offs note says "an editor with cursor placement would
  need a richer model".
- `Parse` may run on any goroutine; `Render` is render-thread only (ADR-0178,
  correction of 2026-08-09). A reparse per keystroke was measured at a low
  single-digit share of a frame at editor sizes (ADR-0178 §Context, 2026-08).

**Widgets inside text.** egui has no inline-widget text flow: links and
images in the preview are separate widgets in a `HorizontalWrapped`, which
breaks lines between runs rather than inside them (EXPLANATION.md,
trade-offs). An editable surface mixing text with images, checkboxes or
tables therefore cannot be one `TextEdit`.

**House rules that bound the options.** Design before code, and an ADR for a
new subsystem (AGENTS.md). Boxer's bar for reimplementing something that
exists is isomorphism or closed-loop observability, not sovereignty
([ADR-0069](../adr/0069-imzero2-layeredgraph-widget.md) Q2). Descope the
heavy peripheral piece rather than gate the design on it. A browser
dependency is not on the table: the tab host *is* the Rust host compiled to
wasm, and brings no DOM editing surface with it.

## 3 Hybrid markdown editors — what end-user products do

*Pass A: Obsidian, Typora, iA Writer, Bear 2, Ulysses, Zettlr, MarkText,
Logseq, Inkdrop, Craft, Joplin, Notion's shortcuts; and the mixed-audience
products that had to choose a default. Fifty pages; no incidents.*

### 3.1 What recurs

- **The markdown string stays the truth and the render is a view over it.**
  Every product that renders in place says so: Obsidian on its 1.5 tables —
  "Tables are still saved to plain text Markdown"; Joplin's rich editor is
  "still Markdown under the hood"; Zettlr calls its preview "only an
  approximation" of the export.
- **Reveal-on-caret is the dominant marker policy.** Markers hide until the
  caret enters the span and show for that span only. Obsidian: "When your
  cursor enters formatted content, the underlying syntax becomes visible for
  editing." Typora: "Moving the cursor to the middle of a span element will
  expand that element into the Markdown source." Bear 2: markup "reappears
  when the word is selected for editing." Zettlr tables: "The active cell
  shows its Markdown source."
- **A source mode is always one gesture away** — Obsidian Ctrl/Cmd+E and a
  toggle command, Typora Ctrl+/, Zettlr's status-bar item, Bear's "Hide
  Markdown" setting, a toolbar toggle in Discourse, Reddit and Stack Overflow.
- **Line-start typing shortcuts convert everywhere**, block editors included:
  `# `, `- `, `1. `, `[] `, `> `, a fence. Notion, Craft, Joplin ("Auto-format
  Markdown", switchable off), Discourse.
- **Paste a URL onto a selection and it becomes a link.** GitHub shipped it on
  2021-11-10; Inkdrop documents it; others cite Slack, Jira and Notion as the
  precedent.
- **Tables are the universal exception.** No product edits table *markup*
  with hidden pipes. Each either opens a grid sub-editor with Tab/Enter cell
  navigation and a context menu (Obsidian 1.5, Typora, Zettlr), drops the whole
  table to source (Obsidian before 1.5, Inkdrop, iA Writer), or inserts from a
  form (GitHub's `/table`).
- **Code fences are the second sub-editor**: a bordered block with a language
  field, Tab/Shift+Tab indenting selected lines, smart punctuation suppressed
  inside. Typora supports only fenced blocks there, not indented ones.
- **Enter continues a list; Enter on an empty item removes the marker.**
  Obsidian: "Hitting Enter is currently the only option to escape a list."
- **Smart punctuation is applied at render, or it leaks into source.** Typora
  distinguishes "Convert on Rendering" (ASCII stays in the file) from "Convert
  on Input" (the curly glyph is "saved in the Markdown source code"), and
  suppresses both "in YAML/code/math block or in source code mode". Its issue
  750 (2017, reopened 2019 and 2020) is the failure: quotes in a fence's
  option line converted on a mode switch in a file "opened and closed without
  saving", breaking R Markdown.
- **A slash menu and a floating toolbar are the discovery layer** for users
  who do not know the syntax (Notion, Craft, Inkdrop, GitHub's `/code
  /details /table /template /saved-replies`).
- **Front matter gets a form with a source fallback.** Obsidian's "Properties
  in document" setting is Visible / Hidden / Source, nested properties are
  pointed at source mode, and a JSON block "will be read, interpreted, and
  saved as YAML" — a normalisation the vendor documents.

### 3.2 What is contested

- **Whether to hide markers at all.** Ulysses: "it is not possible to hide
  all the markup in the editor natively", themes only dim it. iA Writer keeps
  syntax visible with highlighting. Inkdrop's author, asked for inline
  rendering: "I'm afraid to say no." Bear's developers were reported as
  conflicted about offering an opt-out of hiding.
- **Reveal granularity.** Span-only (Obsidian, Typora) against whole-block on
  focus (Logseq: "clicking on a block shows all hidden markdown syntax") and a
  requested hover or radius reveal that no product documents shipping.
- **Whether the rendered table can be opted out of.** Obsidian 1.5's grid
  editor has no plaintext toggle in Live Preview; the forum thread records Vim
  motions, multi-cursor and rectangular selection stopping inside it, and the
  workaround "Add a character above a table so Obsidian no longer treats it as
  a table".
- **Caret placement at the edge of a trailing span.** Clicking at the end of
  a line ending in `*~~x~~*` or `==**x**==` puts Obsidian's caret "before the
  end of the closing tag", and Enter then carries the formatting onto the next
  line — reproduced from 1.4.2 through 1.12.7, "will be fixed 1.14.0"
  (2026-09-03).
- **Line reflow on reveal.** Showing markers widens the line and shifts text;
  users describe it as making "continuous typing and reading really
  difficult". No vendor documents a mitigation.
- **Enter on an empty nested item: outdent or exit?** Users split; no ruling.
- **Normalising source.** Typora: "does not have built-in markdown convert,
  reformat or lint function", and its export drops "details that does not
  affect the final markdown representation" (reference links become inline).
  Joplin's rich editor rewrites reference links and adds an empty header row to
  headerless tables. These are the documented cases of an editor rewriting
  what the user did not touch.

### 3.3 Products that had to pick a default for a mixed audience

*Passes A and G together. No product that served a general audience and
started markdown-only stayed there; none that added a rich mode later removed
markdown.*

| Product | Started as | What changed | Stated reason | What followed |
| --- | --- | --- | --- | --- |
| Slack | markup composer | WYSIWYG toolbar for everyone, 2019-11-13 | "It was also difficult to tell what your messages would look like before you posted them … No fancy keystrokes required." | A developer's post on code spans breaking ("really, really, really bad"); the Hacker News thread at "2,776" points; Slack support quoted: "We made a mistake by forcing everyone into this feature without providing an opt-out"; by 2019-12-03 a preference, Preferences › Advanced › "Format messages with markup", with documented degraded behaviour ("Automatic formatting for bulleted, numbered, and indented lists won't be applied"). Toolbar stayed the default. |
| Reddit | markdown | Rich text ("Fancy Pants") default in the 2018 redesign | "for someone brand new to the site, it's the difference between finding the confidence to make that first post" | "Switch to markdown" per post, a "Default to Markdown" setting; the modes are not feature-identical ("Images are only supported in the rich text editor") |
| GitHub | markdown textarea | never left it; URL-on-selection → link (2021-11), pasted tables and links → markdown with Ctrl+Shift+V to bypass (2022-05), `/` "Markdown Helpers" beta (2023-03) | "reducing the typing required to create complex Markdown" | the one thing reverted was changing what paste does (2019-02, "reverted"); the slash popup drew "intrusive" and a Feature Preview opt-out |
| Stack Exchange | markdown + toolbar + preview | "Stacks editor" alpha with rich and markdown modes, c. 2021 | Teams customers "not comfortable writing their content in Markdown" *(snippet)* | alpha feedback wanted the preview back; a third "markdown with preview" mode *(snippet; the Meta pages were unreachable)* |
| Discourse | markdown + live preview | declined 2015–2020 ("killing a fly with a bazooka", 2015; "an enormous chasm between 'pretty good' and 'good enough'", 2018); a switch costed at a year and "$100,000–$120,000" (2020); experimental ProseMirror composer 2025-03-12; GA 2025-07-07 | "particularly those who are less tech-savvy or aren't familiar or comfortable with writing in Markdown" | "enabled by default for all communities", a toolbar toggle, the last mode "saved … as their individual preference"; underline refused because it "does not cook to an actual underline"; first defects were loose lists and uploads lost on mode switch |
| WordPress | TinyMCE WYSIWYG | Gutenberg blocks, 5.0, 2018-12-06 | "a single, easy-to-learn entry point"; "They'll never have to see a shortcode again" | the Gutenberg plugin at "2.1" of 5 ("2,491" one-star of "3,887"); Tenon: "fails to comply with all 30 of the WCAG 2.1 Success Criteria"; Classic Editor at "8+ million" installs and maintained in 2026 |
| Google Docs | WYSIWYG | opt-in "Automatically detect Markdown" (2022-03-29); import, export, copy and paste as markdown (2024-07-16) | "without having to learn keyboard shortcuts"; 2024: "particularly useful for technical content writers" | detection off by default; import and export on by default |
| Wikipedia | wikitext | VisualEditor (2013) | "people don't expect to have to learn markup to write something on a website" | randomised trials 2013 and 2015: "no effect on newcomer survival or productivity"; "35%" of newcomer edits through it by 2019 |

*(inference)* The end state is the same pair every time: a rich or hybrid
default for the majority, a persisted per-user markdown toggle for the
minority, markdown as the stored form. The complaints that forced vendor
changes were about **keyboard predictability, not rendering**: backspacing
through invisible structure and code spans no longer being literal (Slack),
the caret landing inside a closing marker (Obsidian), Enter doing something
other than new line (Discourse, Obsidian's tables), motions breaking inside a
sub-editor (Obsidian 1.5). The strongest sustained resistance of all came
from a WYSIWYG-to-*blocks* move (WordPress), and its audit found that
semantically clean output "does not imply a usable editing experience".

### 3.4 What end users format, and what the research says

*Pass G.* Per-feature telemetry from Google, Slack, Notion or Medium does not
exist in public. What does:

- **WordPress block usage** (gutenstats.blog, rolling three years, self-described
  as undercounting): Paragraph "43.7%", Image "10.7%", Heading "10.4%", List
  "4.2%", Columns "3.6%"; tables and quotes below "1.1%". *(inference)*
  Paragraph, heading, image and list are about seventy percent of blocks;
  tables and code are the constructs products single out for helpers (GitHub's
  `/table`, `/code`) because typing them is the pain, not because they are
  common.
- **The shared shortcut core of WYSIWYG-first apps** — Google Docs, Notion,
  Teams, Loop, Dropbox Paper, Atlassian, Craft, Bear — is `**` and `*`
  emphasis, `#` headings at line start, `-` / `*` / `1.` lists, `[]` checkbox,
  `>` quote, backticks, `---`. Divergences are real: Slack, Teams and legacy
  Confluence use a single `*` for bold; Bear's `~` is underline where Teams's
  is strikethrough; Notion's `>` makes a toggle and `"` a quote. Three vendors
  (Teams, Google, Dropbox) hedge or avoid the word "Markdown" when addressing
  end users — Paper's shortcut post "never uses the word".
- **Randomised evidence is thin and says little.** Wikimedia's 2015 trial
  (about 9.7k newcomers per arm): productive editors "18.3%" against "18.1%",
  survival "3.0%" against "2.9%", visual-editor users "~18 seconds longer" per
  edit and fewer reverts. The closest peer-reviewed markup-versus-WYSIWYG
  study (Word versus LaTeX, PLOS ONE 2014, n=40): markup users "were slower",
  "produced more … errors", and "more often report enjoying using their
  respective software". No study of markdown learnability for non-programmers
  was found.
- **Style guidance narrows the useful set further.** GOV.UK: "Only use bold to
  indicate interface elements … Do not use italics." Nielsen Norman's 1997
  finding that "79 percent of our test users always scanned any new page"
  argues for headings, lists and highlighted keywords over anything else.
- **The practitioners' positions.** Gruber: "Readability, however, is
  emphasized above all else", for "web writers". CommonMark's forum, 2014:
  "Users simply expect to press enter and get a new line, no matter what."
  iA (2025): "Markdown helps us focus on what we want to say instead of how
  it looks" — argued from writing quality, not usability data. Wikimedia's own
  page concedes the visual editor "does not increase the number of new users
  who become contributors". Typora and Obsidian both sell *hiding* the syntax
  rather than removing it; Bear exposes the same idea as a toggle.

## 4 Rich-text editor frameworks — the ideas, apart from the browser

*Pass B: ProseMirror, CodeMirror 6, Lexical, Slate, Tiptap, Quill, Milkdown,
Remirror, BlockNote, Editor.js, the Yjs and Automerge binding docs, and the
contenteditable literature. Read for an editor that has no DOM. No incidents;
two primary sources (Medium's contenteditable essay, Meta's Lexical launch
post) were unreachable and nothing from them is used as fact.*

### 4.1 Nine ideas every framework converges on

These hold with the browser removed, and are the checklist a native editor
owes itself.

1. **An editor-owned document model that is not the render tree.** ProseMirror:
   "This document representation is explicitly not HTML." Lexical: "the source
   of truth is not the DOM, but rather an underlying state model." Quill
   faults editors that "see their content through the same lens a web
   developer does: the DOM". *(inference)* Free for an immediate-mode widget,
   which has no retained render tree to confuse with the model — but it also
   means the model has to exist; the `TextEdit`'s string is not one.
2. **Immutable state, and changes as values.** ProseMirror's step is "an
   atomic change" that can `apply`, `invert`, map and merge; CodeMirror 6's
   "document and state data structures are immutable, and operations on them
   are pure functions"; Lexical's state is "locked and deemed immutable"
   after an update; Quill's Delta is a change and a document in one format.
   Undo, collaboration and plugins all consume the change value rather than
   diffing states.
3. **Position mapping through changes.** Anything that stores a position —
   selection, decoration, bookmark, undo entry — must be mappable across later
   edits (ProseMirror's `StepMap` of `[start, oldSize, newSize]` chunks; CM6's
   `ChangeSet.mapPos`). ADR-0130's reconcile shift is a one-edit special case
   of this *(inference)*.
4. **A schema or normalisation fixed point.** Slate's `normalizeNode` re-runs
   "until the node becomes valid" and a fix that does not resolve its own
   condition "causes an infinite loop"; Lexical's transforms carry the same
   warning; Quill: "There is only one valid DOM tree that can represent a
   given document". Canonical form is what makes equality, diffing and undo
   coalescing well defined.
5. **Decorations as a render overlay that is not document state.** Three
   kinds in ProseMirror (node, widget, inline), four in CM6 (mark, widget,
   replace, line); Slate: decorations "are not stored on editor state".
   ADR-0130's colour and styled sections are exactly this layer.
6. **Input rules for markdown shortcuts.** A regexp that "should end with
   `$`", tested "against the text before the cursor", with a handler; an
   `undoInputRule` that reverses it if it was "the user's most recent action";
   off inside code by default. Tiptap mirrors the shape with `undoable`
   defaulting to true.
7. **Undo as inverse changes with time-and-adjacency coalescing.** The
   documented defaults:

   | Framework | Group delay | Depth | Breaks a run |
   | --- | --- | --- | --- |
   | ProseMirror / Tiptap | "500" ms | "100" | changes not adjacent ⇒ "a new group is always started"; `closeHistory` |
   | CodeMirror 6 | "500" ms | "100" min | `joinToEvent` joins only when touching the last event's changed ranges; `isolateHistory` |
   | Lexical | "300" ms | unbounded (`null`) | hard return, paste, composition start, "Any change to the selection by the user" |
   | Quill | "1000" ms | "100" | `cutoff()`; `userOnly` to skip API changes |
   | Yjs UndoManager | "500" ms | — | `stopCapturing()`; only `trackedOrigins` undo |

   ProseMirror and CM6 store inverses and support selective undo rebased over
   others' later changes; Lexical and Draft.js store snapshots, cheap under
   copy-on-write.
8. **A command and extension layer with explicit precedence.** CM6 facets
   order "first by category, then by order"; Lexical has ten priority levels
   and a listener returning true "stops other listeners from receiving the
   event"; ProseMirror keymaps take precedence by order.
9. **Gap cursors.** A tree with leaf blocks (image, rule, table) has positions
   no text caret can express; ProseMirror's gap cursor is "a type of selection
   for focusing places that don't allow regular selection". Lexical's team
   said a horizontal caret "doesn't pass WCAG guidelines and doesn't work well
   on mobile".

### 4.2 What exists only to fight the browser — and what it turns into for us

DOM mutation observation and re-parse ("The editor notices DOM changes,
re-parses the changed region, and converts the difference into a
transaction"), DOM-to-model selection syncing, `beforeinput`, node-view
isolation: none of it is needed without a DOM. The `inputType` vocabulary of
Input Events Level 2 (insertParagraph, deleteWordBackward, formatBold,
historyUndo …) is still a useful platform-neutral list of editing intents.

The literature also says what the browser was *buying*. The frameworks keep
contenteditable "for focus, cursor motion, accessibility, and bidirectional
text" (ProseMirror); asked to drop it, its author answered that a
custom-layout editor is "a different project with different issues … It's
definitely not something that ProseMirror will ever do". The W3C EditContext
draft exists because custom-rendered editors had to use "offscreen editable
elements", with "negative implications for accessibility", and MDN lists what
such an editor must then provide: IME window placement, composition
formatting, screen-reader text. CKEditor's own analysis of the alternatives
ends "You were fired for delaying the project for a few months."

*(inference)* That is the honest cost list for the native route in this
tree. egui's `TextEdit` covers caret, selection, IME and AccessKit *inside one
widget*; the moment an editor spans several widgets or owns its own layout,
caret drawing, selection rendering, IME anchoring and the accessibility tree
are the editor's own, and a composition-in-progress state that suppresses
input rules and undo-group boundaries has to exist.

### 4.3 Position identity splits the field three ways

- **Flat offset into a token stream or text** — ProseMirror (open and close
  tokens count one each), CodeMirror 6, Quill. Mapping arithmetic is trivial
  and it is what the undo and collaboration machinery of ProseMirror and CM6
  assume.
- **Tree path plus offset** — Slate (`Point = {path, offset}`, always into a
  text leaf).
- **Stable id plus offset** — Lexical (`NodeKey`, runtime-only), BlockNote ("a
  block will keep the same ID from when it's created until it's removed"),
  Yjs relative positions, Automerge's span model with block markers. The CRDT
  bindings require this one: "In peer-to-peer editing, it is impossible to
  transform index positions so that everyone ends up with the same positions."

*(inference)* A (block id, offset into the block's flat text) hybrid matches
BlockNote and Automerge and lets each block keep CM6-style flat mapping — the
shape §8 and §9 return to.

### 4.4 The Live Preview recipe, as documented

Obsidian: "Obsidian uses CodeMirror 6 (CM6) to power the Markdown editor",
and its own decoration tutorial replaces list hyphens with a widget through a
replace decoration, limited to `view.visibleRanges`. In CM6's terms,
`Decoration.replace` "replaces the given range with a widget, or simply hides
it", and `EditorView.atomicRanges` is "used to provide ranges that should be
treated as atoms as far as cursor motion is concerned" — recommended for
replacing decorations "larger than one character". Caret motion is by grapheme
cluster. Only decorations provided directly may change the vertical layout,
because indirect sets "are only retrieved after the viewport has been
computed".

So Live Preview over a lossless markdown source is: a syntax tree over the
source, replace decorations hiding the markers, mark decorations styling the
rest, atomic ranges so the caret skips what is hidden, and the whole thing
recomputed for the visible range. §2 says which of those four the `TextEdit`
seam has (mark decorations) and lacks (replace, atomic ranges, variable fonts).

### 4.5 Markdown is lossy from any richer model

Every framework that stores a richer model and exports markdown documents the
loss. BlockNote's exporter is named `blocksToMarkdownLossy`: "children of
blocks which aren't list items are un-nested", "certain styles are removed".
Tiptap's markdown extension: comments "may be lost if replaced by Markdown
content", table cells hold one child because "the Markdown syntax can't
represent multiple child nodes". Discourse's composer refused underline
because it "does not cook to an actual underline" (§3.3). CM6 is lossless
trivially — the source *is* the document. The only ways to keep a markdown
file untouched where the user did not edit are to keep the source as the
model, or to keep the source *beside* the model and splice edits back by
range (§7.5).

## 5 Native toolkits and the immediate-mode ecosystem

*Pass C: egui 0.36.2's own documentation (the version the tree builds
against), the egui ecosystem, cosmic-text, parley and masonry, Zed's and
Helix's published design notes, Qt, TextKit 2, GTK4, RichEdit/TOM, Jetpack
Compose, Flutter's super_editor and flutter_quill, the Dear ImGui lineage.
No incidents; two search listings pointed at source files and were not
opened.*

### 5.1 What egui's `TextEdit` is, precisely

The widget edits a `TextBuffer` — "Most likely you will use a String" —
addressed by **character index** ("`char_index` is a _character index_, not a
byte index"); selection is a `CCursorRange`, "based on character count (NOT
byte count!)", with a saved horizontal position for vertical motion. Undo is
an `Undoer<(CCursorRange, String)>`: "Every frame you feed it the most recent
state", a point is created when the state "has remained stable for
`stable_time` seconds" — "`1.0` seconds" — or after "`30`" seconds unsettled,
"`100`" undos deep. *(inference)* Each snapshot is the whole string, and the
history belongs to one widget's state.

Styling is the `layouter` hook, "called at least once per frame", producing a
`LayoutJob` whose sections are gap-free byte ranges each with a `TextFormat`.
A section can vary `font_id` (family and size), colour, background, italics,
underline, strikethrough, `extra_letter_spacing`, `line_height` and `valign`.
Rows are sized per row, so **a heading-sized line and a body line can share
one galley**. What no section can express: a hidden or zero-width range
(sections must cover all of the text, and the galley text must equal the
buffer because cursors index it), an inline box of arbitrary size, link
metadata, or paragraph properties beyond a first-row `leading_space`. A
`Galley` is glyph rows only; the GitHub discussion "Rich Text Editor
Example" (2023) carries one hint and no answer, and an issue search for rich
text or images inside `TextEdit` returned nothing.

Input: `Event::Text`, `Event::Paste` (text; images only from 0.37's
`PasteImage`), `Event::Ime` with `Preedit` ("An empty preedit string indicates
that the IME has been dismissed") and `Commit`; 0.35 improved IME handling
and "proper visuals for IME composition". "Character" means Rust `char`
throughout — no grapheme-cluster or bidi wording appears on any egui text
page, and the only RTL mention in the changelog is a 0.32.3 fix. AccessKit is
a non-optional dependency, but write-back ("Support AccessKit `SetValue` and
`ReplaceSelectedText` in `TextEdit`") lands in 0.37; in 0.36 assistive
technology reads and cannot write. Default fonts "only support latin and
cryllic characters, and some emojis". Nothing virtualises: the layouter
produces the whole galley each frame; 0.34.2 "Optimize text selection
performance for large documents" is the one scale fix.

| Need | egui 0.36 |
| --- | --- |
| per-span font, size, colour, background, underline, strikethrough, italics, line height | yes, in the layouter |
| per-row variable height (a heading line among body lines) | yes |
| hidden byte ranges (markers, folds) | no |
| inline images or widgets in the text flow | no |
| styles that survive editing without a side table | no — the buffer is a `str`; styles are recomputed from text every frame |
| grapheme-cluster caret, bidi caret | not documented; `char`-based |
| undo across widgets or of an external model | no — per-widget snapshot of the whole string |
| assistive-technology write-back | 0.37 |
| image paste | 0.37 |

No egui crate edits rich text: `egui_commonmark` is "A commonmark viewer",
`egui_code_editor` a keyword-highlighted code field, and the search found
nothing WYSIWYG. The nearest Rust hybrid markdown editor, `writ`, is built on
winit, wgpu, Vello and parley, stores "raw markdown text using ropey" with
incremental tree-sitter, hides markers when the caret is elsewhere, and
recognises "Only **ATX** headings".

### 5.2 Three models recur across native toolkits

- **Attributed string plus layout manager** — TextKit 2, RichEdit/TOM,
  Compose (`TextFieldState` carries `textStyles` since Foundation 1.12, so
  "Styles live in `TextFieldState` alongside the text"), cosmic-text
  (per-line `AttrsList`, no background colour, no inline box), parley
  (`RangedBuilder`, and an `InlineBox` "to be laid out inline with text" at a
  byte index with a width and height). Buys one string, one integer cursor,
  styles as side ranges. Costs: blocks are implicit, and anything that is not
  text takes a **replacement character** — U+FFFC in GTK (`get_slice`
  "includes a 0xFFFC character whenever the buffer contains embedded
  images"), Qt's `QChar::ObjectReplacementCharacter`, TextKit's attachment
  character *(recalled)*.
- **Block tree edited through a cursor** — Qt (root frame → blocks →
  fragments, edited only via `QTextCursor`, whose `beginEditBlock` groups
  undo; images are "specially formatted text fragments"; tables are frames;
  `QPlainTextDocumentLayout` "replaces a pixel-exact height calculation with
  a line-by-line … scrolling approach" and so handles larger documents but
  "does not support tables nor embedded frames"), super_editor (`Document` =
  nodes with ids; a `DocumentPosition` is a node id plus a position whose
  type "depends upon the type of DocumentNode"; `AttributedText` keeps
  overlapping attributions), Quill's Delta. Buys first-class blocks and
  per-node positions; costs a two-part position and node-specific selection
  logic.
- **Rope plus layered display map** — Zed: a `Rope` as a sum tree of ≤64-char
  chunks, and over it `inlay_map` → `fold_map` → `tab_map` → `wrap_map` →
  `block_map` ("custom blocks such as diagnostics"), each its own tree.
  Helix: "A cursor is simply a single width selection." Buys huge documents,
  cheap snapshots, and coordinate transforms that are *exactly* the hide-the-
  markers (fold), rendered-decoration (inlay) and widget-between-lines
  (block) mechanisms. Costs a second coordinate system every feature must
  cross.

GTK sits between the first two: one buffer with tags over ranges, marks that
"float in the buffer" with gravity, and `GtkTextChildAnchor`s that host real
widgets. Its tag properties include `invisible`, `editable` and
`line-height` — a hidden range is a tag there.

Two toolkits document what the markdown boundary costs them. Qt's
`toMarkdown` "currently may include GitHub features even if you attempt to
disable them" and `setMarkdown` resets the undo history; Qt's GitHub dialect
treats underline as "distinct from italics; in CommonMark it's the same".
TextKit 2 falls back to TextKit 1 for "attributes not yet supported by
TextKit 2, such as tables", and "It's a one-way operation."

### 5.3 The Dear ImGui lineage, the nearest kin

Every highlighted or rich editor for Dear ImGui is a separate retained widget
(ImGuiColorTextEdit, Zep, `imgui_rich_md`); the original author's statement
that the editor "does not follow ImGui's immediate-mode model because the
editor has to maintain a relatively complex internal state" came through a
snippet only. The rewritten ImGuiColorTextEdit separated "Document and visual
positions" in 2026 to get word wrap and folding, and limits itself to the
Basic Multilingual Plane. Two issue-tracker searches for rich text inside
`InputTextMultiline` found nothing on topic. *(inference)* Immediate-mode
toolkits converge on the same answer egui's ecosystem has: the stock field
is single-style, and anything richer is a retained editor beside it.

*(inference)* For this tree the realistic shapes are the first model with the
style table fed into the existing layouter — one `TextEdit`, styles from a
parse, markers visible but dimmed because nothing can hide them — or a thin
version of the third model where "fold" is impossible but "block" rows are
separate egui widgets *between* `TextEdit`s rather than inside one. §9 costs
both.

## 6 What "solid" is made of — the interaction catalogue

*Pass D: Unicode annexes 9, 14, 29 and 51, Apple's text and undo
architecture documents and shortcut tables, Microsoft's Win32 and Fluent
guidance, GNOME's HIG, WCAG 2.2, WAI-ARIA and its toolbar pattern, NVDA's
guide, the help pages of Google Docs, Word, Pages, Notes, Notion, Obsidian,
Bear and iA Writer, and the latency literature. Two incidents (§11), neither
affecting a claim.*

The catalogue is long because an end user judges an editor by exactly these
things and never names them. The items are grouped by what they ask of the
toolkit versus the editor.

### 6.1 Caret and selection — what the toolkit owes

- **The caret moves by grapheme cluster, not code point.** UAX #29 names
  "text selection, arrow key movement, forward deletion" as the processes
  that treat clusters as units, while warning that clusters "can only provide
  an approximation of where to put cursors"; UTS #51: "there is never a
  grapheme cluster boundary within an emoji sequence … This affects editing
  operations, such as cursor movement or deletion." egui documents `char`
  (§5.1).
- **Word boundaries are locale work.** UAX #29 excludes the hyphen and
  leading or trailing apostrophes from the default, and Thai, Lao, Chinese
  and Japanese "requires the use of dictionary lookup". Platforms disagree on
  the landing point: Option-Right on macOS goes "to the end of the next
  word", Ctrl+Right on Windows to the start of the next *(recalled)*.
- **Affinity at a wrap point.** Apple: selection affinity "determines which
  glyph the insertion point displays near when the two glyphs defining the
  selected range are not adjacent … to place the insertion point at the end
  of one line or the beginning of the following line." *(inference)* End on a
  wrapped line landing at the start of the next is the most common "brittle"
  symptom, and it needs affinity carried beside the offset.
- **Bidi caret policy is unsettled.** Mozilla documents visual movement
  (macOS the same), Windows logical, Firefox's default "visual except if we
  are selecting", Safari "visual selection always"; the W3C list spent years
  "without reaching a consensus". Pick one per platform; make it a setting.
- **Click granularity and extension**: double-click word, triple-click
  paragraph; Shift+↑/↓ extends "to the nearest character at the same
  horizontal location on the line above/below" (sticky x); Word adds F8
  progressive selection. Notion's Escape selects the enclosing block (§8.2).
- **Modifier maps**: macOS Option = word, Command = line and document,
  Control = the Emacs set (A E F B N P K H D O T) from `StandardKeyBinding.dict`,
  which "include[s] a large number of Emacs-compatible control key
  bindings"; Windows Ctrl = word and paragraph, Home/End = line, Ctrl+Home/End
  = document. *(inference)* A macOS build that drops Control-A/E/K is noticed
  at once by the users most likely to be vocal.

### 6.2 Undo

Apple's architecture: operations within "a single cycle of the run loop"
form one group; "as soon as a new undo operation is registered, any existing
redo stack is cleared"; "The undo stack is unlimited by default"; in a field,
"Once the insertion point leaves the field or cell, prior operations cannot
be undone." Typing-run coalescing is documented nowhere Apple, Word or Docs
publish; the web frameworks' numbers (§4.1 item 7) are the only written
rules. Docs documents undo of autocorrect directly: "To change smartquotes
into standard straight quotes, press Ctrl-Z after smartquotes are created."
Redo is Ctrl+Shift+Z in Docs and GNOME, Ctrl+Y in Word — support both.
*(inference)* The rules to adopt: coalesce contiguous insertions at the
caret; break on caret move, paste, a formatting command, a newline and a time
gap; undo of a formatting command restores the selection it formatted; undo
of an input rule restores the typed form.

### 6.3 Input methods and international text

A text view "must … maintain a (possibly empty) range of marked text …
highlight text in this range in a distinctive way, and it should allow
selection within the marked text", and answer the IME's query for the rect of
a character range so it can place its candidate window; dead keys ("After
pressing Option-E, the accent mark appears in a highlighted box") ride the
same path. Microsoft: "Your app doesn't need to interact directly with the
IME" but must test that the "candidate window" does not occlude the UI. UAX
#14 yields "break opportunities" only; "selection of actual line break
positions … is not covered", dictionary scripts need lookup, hyphenation is
"outside the scope". *(inference)* For anything that is not egui's own
`TextEdit`, the contract is: expose the caret rect, accept marked text with a
selected sub-range, render the preedit underlined, commit or cancel, and never
let an input rule or a key handler see keystrokes while composition is
active.

### 6.4 Smart typing

- **List continuation and exit** are the consensus: iA Writer "a list marker
  of the same type will be created automatically when you hit return" and
  "Hitting return twice in a row removes the empty list item and exits the
  list"; Apple Notes "press Return twice"; Docs "press Enter twice"; Tab and
  Shift+Tab change level everywhere. Word's auto-list ("if you type an
  asterisk or 1., Word recognizes…") with its off-switch buried in AutoFormat
  As You Type is the canonical documented annoyance.
- **Smart punctuation** must be a setting, apply "only for new text that you
  type" (Pages), and stay out of the file: iA Writer separates Smart Quotes
  (changes the text) from Smart Punctuation (preview and PDF only), which is
  Typora's render-time option under another name (§3.1).
- **Auto-pair** is contested (§3.2); Obsidian offers two separate toggles,
  "Pair brackets and quotes automatically" and "Pair symbols automatically for
  bold, italic, code, and more", and its forum reports them not fully
  honoured.
- **Paste a URL over a selection** makes a link (Notion, GitHub, Inkdrop).
- **Spell-check** is a red underline with alternates "in a context menu when
  you tap or right-click", auto-correct only "when it's confident", and
  "Don't disable spell checking just because the current spell checking
  engine doesn't support your app language" (Fluent). The tree has no
  spell-checker; it is out of this page's scope and should be named as
  absent.

### 6.5 Clipboard

Obsidian: "Automatically convert HTML to Markdown when pasting and
drag-and-drop from web pages. Use Ctrl/Cmd+Shift+V to paste HTML without
converting." Bear reads the rich flavour and converts, with "Paste From ▸
Plain Text" as the escape, and offers "Copy As" Rich Text, Plain Text, HTML
or Markdown. Docs and Word: Ctrl+Shift+V pastes text only. *(inference)* The
convention set is: default paste converts HTML to markdown, the Shift variant
pastes plain, a "Paste From" menu exposes the flavour, and copy puts markdown
on the plain flavour *and* HTML on the rich one. §2: the tree's clipboard is
write-only and UTF-8 only, so none of this is reachable without the
[ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md) extension
point.

### 6.6 Formatting UI and shortcuts

Bubble toolbar on selection (Notion: "Highlight any text to see the editing
menu"; Medium *(snippet)*) against a persistent one (Word, Docs); a slash
menu for block insertion; the ARIA toolbar pattern (Tab in and out, arrows
within, "a documented shortcut key for moving focus from the relevant
context to its corresponding toolbar is recommended"). Keys: B, I, U
universal; K = link in Docs, Word, Notion and Medium; E = inline code
(Notion); strikethrough has no consensus (Docs Alt+Shift+5 on Windows,
Cmd+Shift+X on macOS; Notion Cmd+Shift+S). The Win32 guide: "Don't use
Ctrl+Alt combinations, because Windows interprets this combination in some
language versions as an AltGR key", and avoid "@ $ {} [] \ ~ | ^ ' < >" as
shortcut keys — which is why Docs' clear-formatting `Ctrl+\` is not portable.
Links: Ctrl+K; click shows "Remove" or "Change" (Docs). Tables: Word — "To
add a row at the end of a table, select the last cell of the last row, and
then press the Tab key"; Ctrl+Tab inserts a tab character. *(inference)* A
bubble toolbar and a link popover both need the selection's screen rect,
which §2 says the seam does not report.

### 6.7 Feel

- **Caret**: respect the system blink rate ("applications should respect the
  settings that the user has chosen"); GTK's cycle is "1200" ms and the caret
  "stops blinking" after "10" seconds, which also satisfies WCAG's
  five-second rule for automatic blinking. Typing resetting the blink phase
  is universal practice and documented nowhere fetched.
- **Latency**: Nielsen's "0.1" second for "instantaneously"; RAIL's "process
  user input events within 50 ms"; Dan Luu: "people can perceive latencies
  down to 2 ms or less", and current machines sit "in the 100ms to 200ms
  range" end to end; Fatin measured editors from "0.9" ms (GVim) to "49.4" ms
  (Atom). *(inference)* Budget keystroke-to-glyph at one frame and treat 50 ms
  as the ceiling; ADR-0178's reparse numbers are inside it.
- **Unsaved changes**: GNOME — "undo is typically a better option than a
  confirmation dialog"; cancel first, Return affirmative unless destructive.

### 6.8 Accessibility

WAI-ARIA's `textbox` with `aria-multiline="true"` is the role; ARIA carries
no formatting-run semantics — screen readers read formatting from the
platform text API, so a custom editor must expose text ranges with
attributes, not a string *(recalled)*. NVDA reports formatting "as you move
the cursor around documents" and has a slower proofreading mode that reports
"all the formatting changes on a line". Docs ships an explicit screen-reader
mode. WCAG: a visible focus indicator (2.4.7), targets "at least 24 by 24 CSS
pixels" (2.5.8), interaction-triggered motion that "can be disabled"
(2.3.3); Windows high-contrast themes: "Do not hard-code colors". AccessKit's
site "makes no statement" on text editing; §5.1: egui's write-back is 0.37.
Gutenberg's audit (§8.3) is the cautionary case for anything block-shaped.

### 6.9 Large documents

Docs: "Up to 1.02 million characters"; Word: "32 MB for the total document
text only"; Obsidian's forum: a 40k-line file "took over 20 sec" to close a
linked pane, a 7.5 MB file with embedded base64 images froze edit mode, and
the maintainer: Obsidian "does not currently support Markdown files
containing encoded or embedded data". *(inference)* Rendering, not storage,
is the limit everywhere; virtualise per block and keep images out of the
text buffer. The tree's preview already renders only what is visible per
segment; a single `TextEdit` lays out everything (§5.1).

### 6.10 A checklist for a scene

*(inference)* The items above that can be asserted by a headless scene
([ADR-0248](../adr/0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md))
and so can be the definition of "solid" for an ADR's verification plan:
Enter continues a list and Enter on an empty item exits it; Backspace at a
block start demotes then merges; ↑ from the first line lands in the previous
block; End on a wrapped line stays on that line; Ctrl+Z after an input rule
restores the typed text; Ctrl+Shift+V pastes plain; Tab in the last table
cell adds a row; a URL pasted over a selection becomes a link; the caret is
never placed inside a closing marker.

## 7 Round-trip fidelity and the Obsidian dialect

*Pass E: CommonMark 0.31.2 and GFM 0.29, the Obsidian help and developer
sites and forum, the Pandoc manual, remark and mdast's rendered READMEs,
goldmark's pkg.go.dev pages (v1 and v2), Qt, Prettier, cmarkit, CodeMirror's
guide and forum, rust-analyzer's syntax chapter, GitLab's content-editor
issue and Beid's wiki. No incidents; the GitLab issue page carried embedded
instructions to read repository files, which were ignored.*

### 7.1 The root problem

The spec defines input to HTML, not input to tree. "Conforming CommonMark
parsers need not store information about whether a particular character" was
an entity or a literal; the CommonMark forum thread on whether the spec
requires loss of input concludes that a tree which "losslessly contain[s] all
the information of the source" is "closer to a concrete syntax tree or a parse
tree", and that stripping whitespace counts as data loss only "for a use
such as a text editor or a syntax highlighter". That is this use.

### 7.2 Where CommonMark and GFM make re-emission dangerous

Each row is a place where the same rendered document has several source
spellings and a tree rebuilt from the AST picks one.

| Construct | Why re-emission changes what the user wrote |
| --- | --- |
| Emphasis `*` vs `_` | Not interchangeable: `_` "must be preceded by a Unicode punctuation character" when both-flanking, so intraword `_` is not emphasis while intraword `*` is. remark's default normalises to `*`; Pandoc documents `intraword_underscores`. The marker *and* the run length must be kept. |
| Hard breaks | Two spellings (two spaces, backslash) with one AST node; "Final spaces or tabs are stripped before inline parsing" at block end, so trailing spaces there are text, not semantics. |
| Soft breaks | "nonsemantic newlines" — the user's own wrapping; Obsidian's "Strict line breaks" setting changes what one newline means. |
| Lists | Marker char and each item's number ("The numbers of subsequent list items are disregarded"), marker width plus 1–4 spaces per item, lazy continuation lines ("deleting some or all of the indentation"), and loose versus tight — a property of blank lines across the whole list, so a local edit has a non-local effect. |
| Headings | ATX versus setext (whose content "may span more than one line"), indentation, an optional closing `#` run; `---` is a setext underline when it could be either. |
| Code | Indented versus fenced; fence char, length and indentation; the info string verbatim; an unclosed fence. |
| Link reference definitions | "A link reference definition does not correspond to a structural element"; they "affect the entire document"; "the first one takes precedence"; labels are case-folded and whitespace-collapsed. An "edit link URL" gesture has to know whether it edits the definition or inlines the link. |
| Escapes and entities | Per character, escaped, entity or literal; cmarkit's `*emph&nbsp;*` → `*emph *` stops being emphasis — entity-to-literal is not even semantics-preserving. |
| Tabs, trailing whitespace, thematic-break spelling, HTML blocks | Literal bytes the AST does not distinguish. |
| GFM tables | Alignment row, `\|` escaping "including inside other inline spans", missing or excess cells, rows are lines so no multi-line cells; every tool re-pads. |

### 7.3 The Obsidian dialect, and where Obsidian itself rewrites source

The dialect adds wikilinks with alias, heading and block targets, embeds
with `|WxH` sizes and PDF `#page=`, callouts (`[!type]`, case-insensitive,
`+`/`-` fold, nested, "any unsupported type defaults to the `note` type"),
tags ("must contain at least one non-numerical character", case-insensitive,
`/` nesting), `%%comments%%`, `==highlight==`, `^block-id` ("Latin letters,
numbers, and dashes"; trailing on a paragraph, on its own line after lists,
quotes, callouts and tables), footnotes (inline `^[text]` footnotes "only
work in Reading view, not Live Preview"), task boxes with "any character
inside the brackets", math, Mermaid fences, and YAML properties. One stated
deviation: "Obsidian does not render Markdown syntax inside HTML elements."

Live Preview's body is faithful because the buffer is the file (§4.4). But
Obsidian does rewrite source, and each case is a precedent that users both
want and complain about:

- **Links on rename.** "Automatically update internal links" rewrites every
  referrer, and under "Shortest path when possible" renaming `image.png`
  turned `![](images/image.png)` into `![](image1.png)` — the folder dropped;
  a moderator: relative-path mode with markdown links "is in an
  incomplete/broken state".
- **Ordered lists.** Since 1.8.4 changing one number renumbers "all
  descending numbered lists" unless separated by a rule — against the spec's
  "disregarded".
- **Tables and callouts** through context-menu actions (add or delete rows,
  sort, align, change callout type), each a source edit.
- **Frontmatter, the worst case.** "Properties are stored in YAML format at
  the top of the file", and the Properties UI round-trips them through "a JS
  object, which means it lose all of its YAML specific formatting and
  comments": quotes, comments, inline lists and type tags lost. Staff: "As
  long as things are semantically the same, it's not a bug and you should not
  expect for it to remain untouched"; in 2025, "there are no plans for
  changing this". A community plugin exists only to counter it. JSON
  frontmatter "will be read, interpreted, and saved as YAML". *(inference)* A
  frontmatter form needs a YAML concrete syntax tree or line-level patching,
  not an object round-trip.
- **Escapes in Live Preview** were hidden inconsistently (backslashes shown
  in Reading view, hidden in Live Preview); hiding trivia is itself a
  fidelity hazard, because the user cannot see what will be saved.

### 7.4 How the tools round-trip — none of them do

Pandoc: "one should not expect perfect conversions"; its `--wrap=preserve`
"will attempt to preserve the wrapping". remark-stringify is a canonical
writer with options (`bullet` default `*`, `emphasis` default `*`,
`incrementListMarker` true); mdast records no marker, heading style, fence
character or break spelling, and its position info is enough to *locate*
original text, never to regenerate it. Typora: "the Markdown source code for
tables is generated automatically by Typora", and issue 750 (§3.1). Qt's
`toMarkdown` and Prettier ("all the numbers will be fixed!", tables
"automatically … adjusted to fit their contents") are canonical writers by
design. cmarkit, the one renderer that *tries* for byte-equality, documents
its own losses — regularised indentation, lazy lines absorbed, one newline
style, eager escaping — which read as the checklist of trivia a faithful
design has to store. CodeMirror's author on WYSIWYG through a document model:
round-tripping "is, in any solution I'm aware of, going to change some aspects
of the Markdown text."

### 7.5 Three approaches that keep the file faithful

1. **Source as truth with a render overlay** — CodeMirror's shape (§4.4):
   nothing is ever re-serialised. Requires hiding ranges and atomic cursor
   motion, which §5.1 says one `TextEdit` cannot do.
2. **A lossless concrete syntax tree** — rust-analyzer's: "Syntax trees are
   lossless, or full fidelity. All comments and whitespace get preserved";
   "The original text can be recovered by concatenating the texts of all
   tokens in order." Applied to markdown the tokens must include
   indentation, lazy-line flags, marker runs, fence lines and blank lines as
   trivia.
3. **Position-preserving edits** — edit the byte range the node came from and
   reparse. GitLab's content-editor issue states the problem ("re-generates
   the entire Markdown source") and the remedy: keep the original source for
   nodes whose AST did not change, or load "only one block into the rich
   editor". Beid's wiki states the pure form: "parses Markdown into
   source-positioned nodes and edits the original bytes without normalizing
   untouched formatting"; "returns the original source rather than
   reserializing the tree"; tested against "all 652 official CommonMark 0.31.2
   examples for exact source round trips". The cost is that an edit whose
   *context* changes meaning — a loose/tight flip, `_` flanking, a label now
   shadowed, a `---` becoming a setext underline — needs a reparse-and-check,
   with the editor inserting an escape or a blank line only when the new text
   would parse differently from the intent.

**goldmark's support.** v1, which the tree uses, keeps `Segment{Start, Stop}`
on every text node and `Lines()` on every block node, plus `List.Marker`,
`IsTight`, `Start` and `AutoLink.AutoLinkType`; it records no heading style,
emphasis marker, fence character or reference-link kind. v2 "all nodes hold
the start position", "keeps which syntax was used for headings (ATX or
Setext)", distinguishes `you & me` from `you &amp; me`, and adds
`CodeBlockKind`, `ReferenceLinkKind`, `AutoLink.Text` "including any
surrounding syntax characters", `BlockNode.Source()` and `ListItem.Offset()`;
its README speaks of writing back "without losing less/any information" but
documents no markdown renderer. *(inference)* For approach 3 the missing
fields do not matter: what is needed is to locate every node's bytes, which
both versions give for blocks and v2 gives for everything. The tree's
obsidian extension would need the same per-node spans for wikilinks,
callouts, tags, highlights and comments; §2 notes the *lowering* is where
provenance is currently dropped, not the parse.

## 8 Block-structured editors

*Pass F: Notion, WordPress Gutenberg, Craft, Logseq, Anytype, AppFlowy (docs
only), BlockNote, Editor.js, with Dropbox Paper and Google Docs' pageless mode
for the "looks continuous" question. No incidents; Logseq's documentation
site could not be fetched, so its claims rest on forum threads.*

### 8.1 Invariants every block editor shares

- **A document is an ordered list of typed blocks with stable ids, and blocks
  may nest.** Notion's block carries `id`, `type`, `has_children`, `parent`;
  Gutenberg's a `clientId`, `attributes`, `innerBlocks`; BlockNote's "a block
  will keep the same ID from when it's created until it's removed". Craft: "In
  Craft, everything you create is a block." Notion lets a *paragraph* have
  children; nesting is not reserved for lists.
- **Inline formatting is a run array inside a block and never crosses one.**
  Notion's `rich_text` is an array of `text` / `mention` / `equation` objects
  with `annotations` for bold, italic, strikethrough, underline, code and
  colour; BlockNote's `InlineContent` is styled text or a link, and "does not
  include content from any nested blocks". *(inference)* This is exactly what a
  per-byte-range styleable text field can render.
- **Some blocks have no text flow, or a plain one.** BlockNote: images have
  `content: undefined`, code blocks "store plain text: unstyled, with no
  links", tables and columns "use different content shapes".
- **Tables are grids of cells, each an inline run — not nested blocks.**
  Notion's `table_width` "can only be set when the table is first created";
  BlockNote's advanced table features are "disabled by default to keep the
  default table experience easy to use".
- **Serialisation is structural, and markdown is a lossy export.** Gutenberg's
  HTML-comment grammar keeps `originalContent` for a block that fails
  validation; Notion's export: "Callout blocks will be exported as HTML, as
  there is no Markdown equivalent"; Obsidian's importer: "We recommend that you
  do not use Notion's Markdown export as it omits important data"; BlockNote's
  exporter is named lossy (§4.5). Logseq's `((uuid))` block references and
  `id::` lines leak into the file and read as "a string of unintelligible
  ((UUID))" outside the app; Obsidian's `^block-id` is the same trade with a
  shorter token.
- **Hard limits are documented.** Notion: `text.content` ≤ "2000" characters,
  arrays ≤ "100" elements, a request ≤ "1000 block elements and 500KB overall",
  "1,000 blocks per workspace" on the free plan. Craft: "limits block
  indentation to 5 levels deep", "intentional, not a technical limitation".

### 8.2 What happens at the seams

The gestures a block list has to answer, and what the documented products
do. Where a cell says *recalled* the behaviour was not found on a page.

| Gesture | Documented answers |
| --- | --- |
| Enter mid-paragraph | split into two blocks at the caret (Notion: "Press `enter` to insert a line of text"; Editor.js hands Enter to the core unless a tool sets `enableLineBreaks`) |
| Soft break | Shift+Enter "to create a line break within a block of text" (Notion) |
| Backspace at block start | *recalled, not documented:* a list item or heading first reverts to a paragraph, then merges into the previous text block; a non-mergeable neighbour (image) is selected instead. Editor.js makes merge a per-type hook that "receives data in the same format as `save` output" |
| Arrow ↑ / ↓ out of a block | Gutenberg: "Pressing Up Arrow on the first line of a block moves focus into the block above"; each block "is a real focus change", and a screen-reader user notes "Sometimes screen readers don't speak the focus transition". Craft: "arrow-key block navigation is macOS-only". BlockNote exposes `prevBlock` / `nextBlock` on its cursor |
| Select text across blocks | Notion only since 2022-01-19 ("Click & drag to select, cut, copy & paste partial text across paragraphs, bullet lists, callouts & more", "supported on all platforms except Firefox"); drag from the margin selects whole blocks instead. Gutenberg: Shift+click, drag, Shift+arrows; Ctrl+A selects the block's text, "Press again to select parent block or all blocks". BlockNote's selection is a start block and an end block, running from the start of the first to the end of the last |
| Escape | Notion: "Press `esc` to select the block you're currently in", Shift+↑/↓ extends. Gutenberg: Escape leaves Edit mode for Navigation mode |
| Drag a block | a handle (`⋮⋮`, `⠿`) on hover, a drop guide; Notion makes columns by dropping beside; BlockNote accepts a drop "up to 250px outside" and snaps to the nearest position |
| Copy across blocks | BlockNote recommends `JSON.stringify(editor.document)` as the non-lossy clipboard form beside a text rendering; the others are *recalled* |
| `/` menu | Notion: `/bullet`, `/heading`, Esc dismisses; BlockNote: "If the block with the text caret is empty, the call changes that block's type. Otherwise, it inserts a new block below"; Craft: `/3x4` makes a table "up to 9x9" |
| Turn into | Notion: "Any block can be converted to any other type"; Gutenberg's transforms "also work on multi-block selections"; Editor.js shows its conversion toolbar only when the user "selects almost all of the Block's content" |
| Paste multi-paragraph text | Gutenberg: "If you copy and paste markdown into Gutenberg, it will automatically convert the whole document to blocks"; Editor.js has per-tool `pasteConfig` of tags, files and patterns |
| Focused block look | Notion: no focus chrome, handles on hover. Gutenberg's "faint grey border" and "too much reliance on hover effects" were criticised; "Spotlight mode" dims every block but the current one as an option |

The special-cased blocks are the same everywhere: tables (grid, Tab and
Enter between cells, Tab in the last cell adds a column and Enter in the last
row adds a row — Craft), code (plain text, its own Enter and Tab), images (a
caption as a separate small field), equations (both an inline run type and a
block), callouts and toggles (a header run plus a nested block list). Toggle
headings in Notion "must be opened and closed manually, with no option to do
so all at once". Gutenberg allows bold and links *inside* a code block.

### 8.3 The costs, as documented

- **Reception.** The Gutenberg plugin's average fell from "2.7" to "2.3" stars
  across 2018; the quoted complaints were "the user must click the + sign
  every single time", "it was all too easy to add a block by mistake", "Why
  does every little thing have to be in a separate block?", "I had entire
  blocks just disappear on me". Classic Editor installs went "from 7,000 to
  60,000" in weeks and stand at "8+ million" (§3.3).
- **Accessibility.** WPCampus commissioned Tenon for "$31,200": a "329-page
  technical report", "84" GitHub issues; at WordCamp US 2018 even "setting a
  title and writing paragraph content" was "painfully difficult" with a screen
  reader. The repair was a *mode*: Navigation mode (Gutenberg 6.3, 2019-08)
  where "a single `Tab` press" moves between blocks, Enter edits, "The
  `Escape` key allows you to move back to the Navigation Mode", and arrows
  later gained nested-level semantics. A screen-reader walkthrough describes
  the announcements it needs: "Heading block, level 2, editing".
- **Cross-block selection.** Notion shipped it four years after launch and
  without Firefox; users still report "Selecting multiple blocks is unreliable
  - especially if there's a table involved" and "the cursor jumps around" when
  a bullet becomes a heading.
- **Markdown lossiness, caps and columns** as in §8.1; Notion's columns are
  "available on tablet but not mobile".

### 8.4 What a block list that must read as one document has to do

Derived from the above; the precedent for each is in parentheses.

1. **One caret, many fields.** The editor owns the caret, not the field
   widget: ↑ on a block's first line lands in the previous block at the same x
   (Gutenberg's "real focus change", with its screen-reader cost), and the
   caret is re-placed after every split, merge and conversion ("the cursor
   jumps around" is the documented failure).
2. **Enter splits, Backspace-at-start merges, with type demotion first**, and
   the pair are inverses in one undo entry *(inference)*. Merge is a per-type
   decision — an image does not merge, it is selected (Editor.js's hook).
3. **Two selection models, one look.** Whole-block selection (Escape,
   Shift+↑/↓, margin drag) and partial text selection spanning blocks. A
   continuous highlight means every intermediate block paints a full-range
   selection though only the first and last carry a text anchor *(inference
   from BlockNote's start-and-end-block model)*.
4. **Chrome hidden until hover or focus** (Notion, BlockNote); a dimming
   "spotlight" only as an option (Gutenberg).
5. **Clipboard carries structure and text** — a native form beside a markdown
   rendering; paste splits paragraphs into blocks and honours markdown
   prefixes (Gutenberg).
6. **Special blocks are not text fields**: tables, code, image captions,
   equations, callout and toggle headers each get their own editor (§8.2).
7. **A keyboard mode for accessibility.** Gutenberg's Navigation/Edit split is
   the only documented pattern that made a block editor usable with a screen
   reader; Notion's Escape-selects-block is the lighter equivalent. Announce
   block type and level on focus change.
8. **A nesting policy**: a depth cap (Craft's five), and whether paragraphs
   nest (Notion: yes).
9. **Stable ids**, and an early decision on whether they leak into the file
   (Logseq and Obsidian both do, with complaints).
10. **Markdown round-trip is lossy from a block model unless the source is
    kept beside it** (§4.5, §7.5). Every block product keeps a native format
    as the truth and treats markdown as an export; a markdown-file editor
    cannot, which is the problem §9 is about.

## 9 The design space, mapped onto this tree

*(inference throughout, from §§2–8.)* Five shapes cover the field. Each is
described by what it renders, what the keys do at its seams, how the file
stays faithful, and which of §2's limits it runs into.

### 9.1 The shapes

**S0 — a better source editor.** Markers stay visible, as in Ulysses, iA
Writer and Inkdrop (§3.2). What changes is behaviour: Enter continues a list
and exits on an empty item, Tab indents, line-start shortcuts, Ctrl+B/I/K,
URL-over-selection, a find that scrolls to the match. Everything stays in one
`TextEdit`; the file is the buffer. Runs into: no letter keys in the
ADR-0177 vocabulary and a modifier-agnostic mask (Ctrl+B would eat every b);
line-aware rewrites leave egui's edit path and its undo (ADR-0178
§Alternatives). Does not meet the bar §3.3 sets — every mixed-audience
product moved to a rich default — but every later shape needs its behaviours
anyway.

**S1 — decorated source: one `TextEdit`, styled from the parse.** The
highlight seam grows per-section font, size and line height (§5.1 says egui
supports it; §2 says it is a Rust change), so headings are large, bold is
bold, code is monospace inside a proportional paragraph, and markers are
dimmed rather than hidden. The CodeMirror recipe (§4.4) minus its two load-
bearing halves: no replace decorations and no atomic ranges, because every
byte is laid out and the caret indexes the buffer. Reveal-on-caret is
therefore unnecessary — markers never leave — which also removes the reflow-
on-reveal and caret-inside-closing-marker complaints of §3.2. Tables, code,
images, callouts and embeds remain source; a bubble toolbar and link popover
need the byte-range-to-rect channel the seam lacks. The file is the buffer,
lossless by construction. One-frame-stale styling shows as a frame of
mis-coloured text after a keystroke, as it does today. The cheapest shape
that an end user would call formatted, and short of what they would call an
editor for pictures and tables.

**S2 — a block list over the source.** The document is the parse's top-level
blocks, each known by its `Lines()` byte range (§2, §7.5). Unfocused blocks
render as the preview already renders them; the focused block is a
`TextEdit` over its own source slice (S1's styling applies inside it);
tables, code fences, images, callouts and frontmatter get the sub-editors
every product gives them (§3.1, §8.2). An edit is a byte-range splice into
the one source string followed by a reparse — Beid's and GitLab's approach
(§7.5) — so untouched bytes are never rewritten. This is the "per-block
editing model" ADR-0178 named as larger than its M0, and the pass-F list
(§8.4) is its cost sheet: the editor owns the caret across fields
(`setCursor`, `reportCursor`, `requestFocus` exist; arrows, Enter, Backspace
and Escape are in the key vocabulary); Enter splits and Backspace-at-start
demotes then merges, as one undo entry; cross-block text selection is the
hard item (egui selection is per widget — Notion took four years and still
excluded Firefox); clipboard across blocks is write-only here; undo must be
the editor's own, with egui's per-widget `Undoer` reduced to within-block
typing; a Navigation/Edit keyboard mode for accessibility; stable block
identity across reparses (the outline's `slug#ord` problem, ADR-0178 update
2026-08-09, generalised). Reveal granularity is whole-block, Logseq's model
(§3.2), which users who wanted span-level reveal found "doesn't cut it" —
but it is the only granularity a `TextEdit` can give, and S1 inside the
focused block narrows the gap. Reparse per keystroke is already licensed; the
preview's segment tree would be rendered per block rather than whole. No Rust
change is strictly required for a first cut, and the S1 seam makes the
focused block much better.

**S3 — an owned text engine on the painter lane.** The ImPlot and Leaflet
route ([ADR-0149](../adr/0149-implot-core-port-painter-lane.md),
[ADR-0204](../adr/0204-leaflet-map-core-port.md)): a Go document model, a
layout that asks egui for glyph metrics, a caret, selection and IME of its
own. It is the only shape that can hide markers, place an image inline, and
move the caret by grapheme cluster. Its cost is §4.2's honest list — caret,
selection rendering, IME anchoring (egui's `Event::Ime` is not exposed over
the FFI), the accessibility tree, platform key bindings — plus text
measurement that is only reachable through fetchers at frame end
(`widgets/markdown/EXPLANATION.md`). It fails ADR-0069's bar: neither
isomorphism nor closed-loop observability asks for it. parley's `InlineBox`
(§5.2) shows the layout primitive it would want and the tree does not have.
Recorded so it is not re-proposed without a trigger.

**S4 — a rich document model with markdown export.** Notion's, BlockNote's,
Qt's shape. Rejected by §4.5, §7.4 and §8.1: every such exporter documents
loss, and the file on disk is the contract with every other tool that reads
the vault.

### 9.2 Against the criteria

`++` strong positive, `+` positive, `−` negative, `−−` strong negative.

| | S0 source | S1 decorated | S2 blocks | S3 engine | S4 model |
| --- | --- | --- | --- | --- | --- |
| C1 — reads as formatted to a non-programmer (§3.3) | −− | + | ++ | ++ | ++ |
| C2 — tables, images, code as end users expect (§3.1, §8.2) | −− | − | ++ | ++ | ++ |
| C3 — file untouched where not edited (§7) | ++ | ++ | + | + | −− |
| C4 — keyboard predictability at seams (§3.3, §6.10) | ++ | ++ | − | + | + |
| C5 — reuses the tree's seams; no new subsystem | ++ | + | − | −− | −− |
| C6 — Rust / IDL change | none | font seam | none needed; font seam helps | large | large |
| C7 — accessibility and IME inherited from egui | ++ | ++ | + (per block) | −− | − |
| C8 — undo | egui's | egui's | editor's own | editor's own | editor's own |

S2 is the shape every documented product converged on once the toolkit could
not hide markers inside a text flow (§5.3: every immediate-mode rich editor
is a retained widget beside the stock field; §8.1: block editors exist
because one contenteditable "causes permanent bugs"). Its two negatives —
seams and cross-block selection — are exactly the documented failure modes
of its genre, and §8.4 is the list of what mitigates them. S1 is not an
alternative to S2 but its focused-block rendering, and can ship first.

### 9.3 Seams the shapes need, independent of which is chosen

Ordered by how many shapes need them. Each is a Tier 1 surface or an
existing ADR's extension point, so each is an ADR update or a small ADR, not
an app change.

1. **Modifier-qualified letter keys** in the ADR-0177 vocabulary, with a
   mask that can say "Ctrl+B but not b". Needed by S0 through S3; today's
   mask "consumes the bare key".
2. **Per-section font, size and line height** in the highlight seam
   (ADR-0130). S1 and S2's focused block; §5.1 confirms egui supports it;
   moves the browser host digest.
3. **A document-level undo** owned in Go: inverse edits with the §4.1
   coalescing rules, egui's `Undoer` left to within-widget typing. S2 and S3.
4. **A composition-in-progress signal** (is the focused `TextEdit` in IME
   preedit), so input rules and undo-group boundaries hold off. S0 onward,
   as soon as an input rule exists.
5. **Clipboard read, and an HTML flavour** — the extension point ADR-0026
   names. HTML-to-markdown paste (§6.5) and cross-block copy. S2.
6. **Byte-range-to-rect** from the editor galley. Bubble toolbar, link
   popover, tethered anything. S1 and S2; the same channel ADR-0130's
   deferred tethered-editor entry asked for.
7. **AccessKit write-back** arrives with egui 0.37 (§5.1); an upgrade, not a
   seam.

### 9.4 A proposal for the dialogue to take or leave

Build toward S2, in this order, each step useful on its own and each
measurable by a scene (§6.10):

- **S0's behaviours first**, inside today's editor: Enter list continuation
  and exit (Enter is already capturable), Tab indent, line-start shortcuts
  through `insertAtCursor`, and the letter-key vocabulary for Ctrl+B/I/K.
  They are needed in every later shape and they are what the §3.3 complaints
  were about.
- **The font seam**, and with it S1: a proportional, sized, dimmed-marker
  source pane. At this point mdedit's split view can default to "formatted
  source beside preview" and the preview becomes optional for prose.
- **S2 behind a mode**, Split / Source / Read gaining "Edit" — blocks over
  the same source string, S1 inside the focused block, the three sub-editors
  (table grid, code fence, image caption) and frontmatter as a form over
  line-level patching. Source mode stays one keystroke away, which §3.1 says
  every product keeps and §3.3 says the minority will demand.
- **Cross-block text selection deferred** past the first cut, with
  whole-block selection (Escape, Shift+↑/↓) in its place — the order Notion
  took.

Default mode for a window is the one fork this page will not pre-empt
(§10 F1).

## 10 Open forks for a design dialogue

- **F1 — the default.** Rich (S2) by default with a persisted per-user
  source toggle is what every mixed-audience product landed on (§3.3); the
  tree's users today are markdown writers. Per window, per app, or per
  document kind?
- **F2 — block granularity.** goldmark's top-level blocks, or list items and
  callout bodies as blocks of their own (Notion nests paragraphs; Craft caps
  depth at five). Decides what Enter and Backspace mean inside a list.
- **F3 — what "faithful" promises.** Untouched bytes never change
  (approach 3, §7.5), or Obsidian's "semantically the same" for the pieces a
  sub-editor owns — tables, ordered-list numbers, frontmatter. The table
  grid in particular will re-pad.
- **F4 — cross-block selection**: first cut, or deferred behind whole-block
  selection.
- **F5 — undo ownership** once edits come from two places: all through the
  Go history, or egui's for typing and Go's for structure, with the handoff
  rule written down.
- **F6 — the key vocabulary** (seam 1): letters with modifiers only, or a
  general key channel. ADR-0177 argued for a subset on purpose.
- **F7 — clipboard** (seam 5): a read subject and a rich flavour are
  Powerbox decisions ADR-0026 reserved; who takes them.
- **F8 — frontmatter**: a form over line-level patching, or source only with
  Obsidian's "Properties in document: Source" as the model. The documented
  failure (§7.3) argues against the object round-trip whatever else is
  chosen.
- **F9 — the sub-editors' scope.** Tables: Tab and Enter navigation, add row
  and column, no merged cells (BlockNote's "easy to use" default). Code: a
  language field and Tab. Images: caption only. Callouts: type menu. Which
  ship in the first cut.
- **F10 — the measure of "solid".** The §6.10 scene list as the ADR's
  verification plan, or a hand-driven checklist.
- **F11 — mdedit's existing surfaces** — outline, find and replace, LLM
  proposals, send-to-play, the files pane, follow-on-disk — each assumes one
  buffer and one `TextEdit`. Which keep working unchanged in S2 (those that
  rebind the whole buffer do), and which need the block model to know about
  them (find's painted matches).

## 11 Sources and incidents

Seven delegated passes, 2026-10-24. Each pass's full report, with every
claim tagged and every page listed, was kept with the session and is not
committed; the pages cited in this document are listed here by pass. Where a
primary source was unreachable the text above says so and uses nothing from
it as fact.

**Protocol incidents.** Passes A, B, C, E, F and G: none. Pass D: two,
neither affecting a claim — fetching a conference paper's PDF caused the
fetch tool to save the file locally (not opened; the paper's figure is cited
as a snippet only), and for three Apple pages that returned only a title the
fetch tool volunteered paraphrases, which were tagged recalled rather than
read. Near-misses recorded by passes C, D and E: search listings that pointed
at code views (Zed, ImGuiColorTextEdit, Chromium's selection model) were not
opened; a GitLab issue page carried embedded instructions to read repository
files, which were ignored; the Beid wiki was read as documentation and no
file link followed. Unreachable throughout: medium.com, meta.stackexchange.com,
reddit.com and its mirrors, docs.logseq.com (over the fetch size limit), and
several Apple and Material reference pages that render client-side.

**Pass A — hybrid editors.** obsidian.md/help (edit-and-read, properties,
Advanced formatting syntax), obsidian.md/changelog (2021-11-10, 2023-11-20),
forum.obsidian.md threads 109155, 37377, 73866, 35542, 41517, 114024, 922,
2890; support.typora.io (Markdown-Reference, Auto-Pair, Markdown-Export,
Code-Fences, Shortcut-Keys, SmartyPants, What's-New-0.11); typora-issues
issue 750 (prose); ia.net/writer/support (markdown-guide); joplinapp.org
rich_text_editor; discourse.joplinapp.org 2724; docs.zettlr.com (appearance,
tables); docs.inkdrop.app, forum.inkdrop.app 1764; bear.app/faq, blog.bear.app
2023-07; help.ulysses.app (markup, first-steps-markdown-xl); notion.com/help
keyboard-shortcuts; support.craft.do markdown-shortcuts; github.com/marktext
landing README; discuss.logseq.com 1650, 2531; meta.discourse.org 352347,
326070, 148687; ubuntu-mate.community 30550; docs.github.com
about-slash-commands; github.blog changelogs 2021-11-10, 2022-02-02;
vice.com, tech.slashdot.org and howtogeek.com on Slack 2019;
markdownguide.org/tools/reddit.

**Pass B — frameworks.** prosemirror.net (guide, ref); marijnhaverbeke.nl
(prosemirror, collaborative-editing, facets); codemirror.net (guide, ref,
examples/decoration); docs.obsidian.md (Decorations, Editor extensions);
lexical.dev (intro, editor-state, selection, history, commands, nodes,
transforms, key-management, lexical-markdown); discuss.prosemirror.net 4557,
1766; docs.slatejs.org (interfaces, nodes, locations, operations, rendering,
normalizing, migrating); tiptap.dev (introduction, input-rules, markdown,
undo-redo); quilljs.com (delta, designing-the-delta-format, why-quill,
history, cloning-medium-with-parchment); github.com/Milkdown landing README;
remirror.io extension concepts; blocknotejs.org (document-structure,
export/markdown); editorjs.io base-concepts; docs.yjs.dev (undo-manager,
relative-positions, prosemirror binding); automerge.org rich-text schema and
blog; w3.org input-events-2, w3c.github.io contentEditable and edit-context,
MDN EditContext; ckeditor.com blog; lists.w3.org 2015Aug/0731.

**Pass C — native toolkits.** docs.rs egui and epaint 0.36 (TextEdit,
TextEditOutput, TextEditState, TextBuffer, LayoutJob, LayoutSection,
TextFormat, TextWrapping, Galley, PlacedRow, Row, Undoer, Settings, Event,
ImeEvent, text_selection, CCursorRange, TextCursorState, accesskit_text);
github.com/emilk/egui releases pages, discussion 2806, issue search;
docs.rs egui_commonmark, egui_extras syntax_highlighting, egui_code_editor,
writ, cosmic-text (Buffer, Attrs, AttrsList, Editor, Edit, Action, Motion,
Wrap), parley (InlineBox, editing, PlainEditor, PlainEditorDriver, Cursor,
Selection), iced text_editor, floem-editor-core, floem text_editor;
linebender.org tmil-13, tmil-25; zed.dev/blog zed-decoded-rope-sumtree;
docs.helix-editor.com (usage, keymap); doc.qt.io (richtext-structure,
qtextobjectinterface, qtextcursor, qtextcharformat, qtextdocument, richtext,
qabstracttextdocumentlayout, qplaintextedit, richtext-advanced-processing);
developer.apple.com WWDC21 10061, WWDC22 10090; docs.gtk.org gtk4
(section-text-widget, TextTag, TextBuffer, TextChildAnchor, get_slice,
TextView); learn.microsoft.com (about-text-object-model,
about-rich-edit-controls); developer.android.com compose text (user-input,
rich-text-input); pub.dev super_editor, flutter_quill;
github.com/superlistapp/super_editor landing; github.com/ocornut/imgui wiki
Useful-Extensions; BalazsJako, goossens and Rezonality landing READMEs.

**Pass D — interaction.** unicode.org tr9, tr14, tr29, tr51;
unicode-org.github.io icu boundaryanalysis; support.apple.com 102650,
mh35735, pages tanad45f9cce, notes apd93c815aa0; developer.apple.com
archive (UndoManager, AppKitUndo, CocoaTextArchitecture TextEditing,
TextDefaultsBindings); support.google.com docs 179738, 12022089, 6282736,
1696711, 45893, 97447, 3300615, drive 37603; workspaceupdates.googleblog.com
2010-02; support.microsoft.com Word shortcuts, automatic bullets, table rows
1eacc486; learn.microsoft.com (inter-keyboard, about-carets,
input-method-editors, high-contrast-themes, rich-edit-box, text-controls,
text-box, word operating-parameter-limitation); w3c.github.io/aria;
w3.org WAI APG toolbar, WCAG22 Understanding (target-size-minimum,
pause-stop-hide, animation-from-interactions, focus-appearance,
focus-visible); developer.gnome.org HIG (keyboard, dialogs); docs.gtk.org
gtk-cursor-blink-time, -timeout; danluu.com (keyboard-latency, input-lag);
pavelfatin.com typing-with-pleasure; nngroup.com response-times; web.dev
rail; obsidian.md/help/settings; forum.obsidian.md 13782; notion.com/help
(keyboard-shortcuts, writing-and-editing-basics, understanding-block-usage);
bear.app/faq copy-portions; community.bear.app 18764; ia.net smart-automation;
wiki.mozilla.org Bidi_editing; lists.w3.org www-international 2018JanMar/0117;
bugzilla.mozilla.org 1198600 (prose); download.nvaccess.org userGuide;
accesskit.dev.

**Pass E — round trip.** spec.commonmark.org 0.31.2; github.github.com/gfm;
obsidian.md/help (links, embeds, callouts, tags, syntax, advanced-syntax,
properties, obsidian-flavored-markdown, edit-and-read,
plugins/format-converter, settings); obsidian.md/changelog 2023-11-20;
docs.obsidian.md (processFrontMatter, Decorations, Editor extensions);
forum.obsidian.md 65851, 104006, 95911, 82045, 34110, 6748, 106794, 66502;
community.obsidian.md preserve-frontmatter-formatting; talk.commonmark.org
3832; discuss.codemirror.net 2997; codemirror.net guide; pandoc.org MANUAL;
github.com remark-stringify, mdast, unist, goldmark, cmark landing READMEs;
pkg.go.dev goldmark ast, text, v2, v2/ast; doc.qt.io qtextdocument;
prettier.io (options, rationale, 1.8.0 post); support.typora.io
(Markdown-Reference, Strict-Mode), support.typora.com.cn Line-Break,
typora-issues 750; gitlab.com gitlab issue 338338 (prose);
rust-analyzer.github.io syntax; ocaml.org cmarkit Cmarkit_commonmark;
github.blog 2021-09-30 footnotes; github.com/noxdea/beid wiki.

**Pass F — block editors.** developers.notion.com (block, rich-text,
request-limits); notion.com/help (what-is-a-block, keyboard-shortcuts,
writing-and-editing-basics, export-your-content, import-data-into-notion,
math-equations, columns-headings-and-dividers, code-blocks,
simple-tables-vs-databases, understanding-block-usage), notion.com/releases
2022-01-19; developer.wordpress.org block-editor (key-concepts, data-flow,
block-deprecation, accessibility); wordpress.org documentation
(keyboard-shortcuts, wordpress-block-editor, working-with-blocks,
moving-blocks, paragraph-block, table-block, code-block), plugins
classic-editor; make.wordpress.org 2019-08-14; automattic.design;
marcozehe.de; wptavern.com (two articles); wpcampus.org 2018-12 audit
results; craft-support.mintlify.app (blocks-and-pages, indent-limits, tables,
toggles, export); obsidian.md/help/import (notion, craft); discuss.logseq.com
1558, 2746, 12872; doc.anytype.io (editor, block-types, import-export);
docs.appflowy.io import-from-notion; blocknotejs.org (document-structure,
blocks, tables, math, export/import markdown, side-menu, suggestion-menus,
cursor-selections); editorjs.io (base-concepts, saving-data, tools-api,
blocks), github.com/codex-team/editor.js landing README; help.dropbox.com
formatting, blog.dropbox.com paper-101; howtogeek.com on Docs pageless;
community.latenode.com thread; mkaz.blog.

**Pass G — end-user evidence.** slack.com (help 360039953113, blog
2019-11-13); quuxplusone.github.io 2019-11-20; news.ycombinator.com 21589647
and hn.algolia.com; tech.slashdot.org 2019-11-25; mjtsai.com 2018-04-11;
markdownguide.org/tools/reddit; github.blog changelogs 2019-02-05,
2021-11-10, 2022-05-19, 2023-03-15; docs.github.com (about-slash-commands,
about-saved-replies); github.com/orgs/community discussion 40299;
stackoverflow.design editor; stackoverflowteams.com/help/formatting;
meta.discourse.org 31340, 68246, 326070, 352347, 352704, 369779,
choosing-the-default-composer-mode; blog.discourse.org 3.5; ma.tt 2017-08;
wordpress.org/news 2018-12, plugins (classic-editor, gutenberg); wptavern.com;
workspaceupdates.googleblog.com 2022-03-29, 2024-07-16; notion.com/help
keyboard-shortcuts; support.microsoft.com (Teams markdown, Loop shortcuts);
help.dropbox.com formatting, blog.dropbox.com paper-101;
confluence.atlassian.com (Jira Cloud markdown, x/V4gJF); support.craft.do
markdown-shortcuts; bear.app/faq; typora.io; obsidian.md (home,
blog/live-preview-update); cultofmac.com iOS 27 Notes; ia.net (formatting
fetish essay, format-text); daringfireball.net markdown syntax;
commonmark.org, talk.commonmark.org 833; nngroup.com how-users-read;
guidance.publishing.service.gov.uk style guide; journals.plos.org PLOS ONE
2014 0115069; meta.wikimedia.org (VisualEditor May 2015 and June 2013
studies, Research:Visual_Editor, VisualEditor/Why, 2020 July newsletter,
2016-17 survey Q153); gutenstats.blog; ckeditor.com blog 2026-03.
