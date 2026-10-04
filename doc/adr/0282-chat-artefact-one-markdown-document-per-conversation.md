---
type: adr
status: proposed
date: 2026-10-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0282: The chat's artefact — one markdown document per conversation, edited by the model under the ceiling

## Context

A chat ([ADR-0265](./0265-chat-app-over-retained-model-calls.md)) produces
text only as answers in the transcript. Work that converges on a document — a
note, a plan, a write-up — has to be reassembled from several answers, and a
revision means the model repeats the whole text in a new answer. "Open in
mdedit" copies the transcript out once; nothing comes back.

The repository has the parsing half: goldmark with the Obsidian extensions
(`markdown/obsidian`), structure extraction with line numbers
(`markdown/mdextract`), and heading byte offsets in the markdown widget. It
lacks the editing half and a linter. mdedit's span, splice and find helpers
are unexported; `gov/doclint` checks this repository's documents against its
own conventions and knows nothing of Obsidian syntax.

## Design space (QOC)

**Q1 — How does editing the artefact relate to the ceiling
([ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md))?**

- *O1: grant-free, its own toggle*, like `ask_user` (ADR-0265 §SD7). The
  ladder never sees it; a chat set to Read could rewrite the document.
- *O2: scored as Edit.* A write is a document effect; below Edit the model
  gets the read tools only. **Chosen** by the owner.

**Q2 — Where does the artefact live?**

- *O1: in memory, with the conversation.* Same lifetime as the history and
  the title (ADR-0265 §SD2). **Chosen** by the owner.
- *O2: durable on `boxer.facts`, a kept kind per revision.* Nothing reads it
  back until resume exists (ADR-0265 §SD5).
- *O3: bound to a file.* Brings filesystem grants into a chat.

**Q3 — Who edits?**

- *O1: both, revision-checked.* A text editor in the panel; every concurrent
  case between a person's keystrokes and a model's write needs a rule.
- *O2: the model writes, the person reviews.* The panel shows, compares and
  reverts; a person who wants to type hands the document to mdedit.
  **Chosen** by the owner.

**Q4 — Where do the span and lint primitives live?**

- *O1: shared packages under `public/semistructured/markdown`*, the chat
  first among their callers. **Chosen** by the owner.
- *O2: inside the chat*, extracted later.

## Decision

The chat holds one markdown document per conversation, the artefact. The
model reads and edits it through tools; the person watches it in a panel,
accepts or rejects changes, and reverts. Two new packages carry the
primitives.

### SD1 — The artefact

- **One document per conversation**, empty until the first write. It starts
  and ends with the conversation: New conversation starts a new artefact.
- **Revisions.** Every accepted change makes a revision: the text, its
  number, the turn and tool call that made it, or "reverted to n". The list
  is kept in memory with the conversation.
- **Tied to the turn.** A turn records the revision current when it
  began. Taking it back restores that revision, and its revisions go with
  it; abandoning the edit puts them back, unless the person reverted in
  the meantime, in which case the turn stays taken back. A turn that failed
  or stopped keeps the revisions its calls made, as it keeps its calls. A
  revision number taken back is not handed out again.
- **The chat's properties.** The frontmatter carries `chat_*` properties
  the chat keeps: the conversation id, title, start time, model and
  endpoint, the host's run id, the app and window, the agent task when
  there is one, and Keep. They make a copy of the document traceable to
  the session and to the host's records keyed by the same ids. The chat
  stamps them into every write before it is proposed or committed, so they
  follow a rename at the next write; the model cannot set or delete them,
  and an edit of one is undone by the stamp. A write whose frontmatter is
  not valid YAML is refused, since the properties have nowhere to go.
  Flat keys rather than one nested map, because Obsidian's properties view
  shows nested maps as raw text.
- **Attachments, empty.** The artefact's shape is a document plus a set of
  attachments addressed by content hash. No verb adds one (SD7); the shape
  exists so that adding images later changes no tool's result.

### SD2 — The tools

Offered when the Artefact option is on, in the tool loop of ADR-0265 §SD6.
The toggle is fixed at the first send, with Keep, Apps and Questions, for
their reason: the tools are described in the first request. Artefact alone
makes the turn the tool loop.

Read tools — offered whenever Artefact is on:

| Tool | Returns |
| --- | --- |
| `artefact_read` | the text or a line range, each line numbered, and the revision |
| `artefact_outline` | headings: level, text, slug, heading path, line span of the section |
| `artefact_find` | matches of a literal or a regular expression, with line and column |
| `artefact_inspect` | the parsed structure: frontmatter, wikilinks and embeds, links, tags, callouts, code blocks, footnotes, each with its line |
| `artefact_lint` | the findings of SD4 |

Write tools — offered only when the ceiling allows Edit (SD3):

| Tool | Does |
| --- | --- |
| `artefact_write` | replaces the whole text |
| `artefact_edit` | replaces a text that occurs exactly once; refused, with the count, otherwise |
| `artefact_insert` | inserts at a line, or at the start or end of a section named by heading path |
| `artefact_replace_section` | replaces a section's body, named by heading path |
| `artefact_set_frontmatter` | sets or deletes keys; the rest of the YAML is kept |

- **Every write names its base revision** and is refused if the artefact has
  moved since, with the current revision and outline. The model reads before
  it writes, as with the window operations' stale-write conflicts.
- **Every write returns** the new revision (or the proposal's id, SD3), the
  changed line span, and the lint findings for that span.
- **Results are the model's own text**, not app-sourced: the artefact holds
  only what the model wrote or the person accepted, so reading it does not
  taint the conversation. A future attachment from outside changes that
  (SD7).

### SD3 — Under the ceiling

The chat's Allow and Changes settings (ADR-0280 §SD5) apply to the artefact
as they do to a window, and are read whether Apps is on or not.

- **Allow below Edit:** the read tools only. A write tool called anyway is
  refused with the limit, as the host refuses above the ceiling.
- **Ask first:** a write becomes a proposal. The panel shows it as a diff
  with Accept and Reject; the tool call waits inside the turn until the
  person decides, the way `ask_user` does, and returns the verdict. Cancel
  withdraws it.
- **Apply directly:** a write lands as a revision at once; the person reverts
  from the panel.
- **The scale.** With Artefact on, the settings' marker is at least the
  level the artefact allows (Read or Edit). The task marker stays what the
  host granted.
- **The chat enforces this, not the host.** ADR-0280 places the limit in the
  host's dispatcher so that it does not rest on the coordinator restraining
  itself. The artefact is the chat's own state and never reaches the
  dispatcher, so here the chat is both the holder and the enforcer. The bound
  is as strong as the chat's code, and it covers nothing outside the window.

### SD4 — `markdown/mdlint`

A linter for Obsidian-flavoured markdown, over the `obsidian` parse.

- **Findings:** rule id, severity (info, warn, error), line, column, end,
  message — the shape of `gov/doclint`'s `Finding`, with a span.
- **Rules work on one document.** A wikilink to another page cannot be
  resolved and is not reported; a link to a heading of the same document
  (`[[#heading]]`, `[[#^block]]`) is.
- **First rule set:** an unresolved same-document heading or block link; a
  heading that skips a level; two headings with one anchor; frontmatter
  that is not valid YAML; an unclosed code fence; an unknown callout type;
  a footnote reference with no definition and a definition never
  referenced; a malformed tag; an empty link target.
- **Rules are registered** and selectable by id, so the chat and a later
  caller can turn a rule off.

### SD5 — `markdown/mdspan`

Byte-range primitives the tools of SD2 are built on:

- a section's range by heading path, from the heading line to the next
  heading of the same or higher level;
- line and byte conversion, both ways;
- a splice that replaces a range and reports the changed line span;
- the frontmatter block's range.

The span is computed from the parse, not by scanning for `#`, so a `#` line
inside a code fence is not a heading.

### SD6 — The panel

A right panel beside Settings and Statistics, toggled from the bar.

- The rendered document, frontmatter included; a switch to the source.
- The revision list: who made each revision (the model, a revert), its turn.
  Selecting one shows its diff against the previous; Revert makes it current
  as a new revision.
- A pending proposal (SD3) at the top, as a diff, with Accept and Reject.
- The lint findings, by line and column.
- Copy, and Open in mdedit, which hands over the current text as the
  transcript is handed over (ADR-0265 §SD4).

### SD7 — Deferred, recorded

- **Images.** An upload adds an attachment (SD1); `![[name]]` resolves
  through `obsidian/resolver`'s `ResolverI`. A pasted image carries a
  sensitivity label, and an attachment from outside would taint the
  conversation as an app's text does. Decided with the upload.
- **Keeping the artefact** beyond the window, and resuming it — with resume
  (ADR-0265 §SD5). Under Keep, the artefact's text still reaches
  `boxer.facts` inside the tool calls and results that carry it.
- **The person typing in the panel** — mdedit is the editor (Q3).
- **mdedit on the new packages.** Its own helpers stay until it is next
  revised.
- **Rules across a vault**: resolving wikilinks to other pages needs a vault
  index.
- **More than one artefact per conversation.**
- **The panel's outline, and a finding that scrolls to its line.** The
  model has both through `artefact_outline` and the findings' positions;
  the panel lists them without navigation.

## Surfaces — Tier 1

| Surface | Change |
| --- | --- |
| `markdown/mdlint` | new |
| `markdown/mdspan` | new |
| chat tools | `artefact_*`, ten |
| artefact frontmatter | `chat_*` properties, kept by the chat |
| chat settings | Artefact option; Allow and Changes read without Apps |
| chat panel | new Artefact panel |

## Alternatives

- **The artefact as a window of mdedit, driven by operations
  ([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)).**
  It would be host-enforced and the person could type, but it needs Apps, a
  grant and a launch for every conversation, and mdedit would need a
  catalog. It stays possible beside this one.
- **The model answers with the whole document each time.** No tools, and a
  long document costs its whole length per revision in output and in every
  later request.
- **Diff-format edits (unified patches).** Models produce patches with
  wrong line counts often enough that the exact-text replace is the
  common choice; a patch also needs a fuzzy applier.

## Consequences

### Positive

- A conversation can converge on a document instead of on a last answer.
- Revisions follow the turns, so rewind and branch need no rule of their own.
- The linter and span primitives are reusable outside the chat.

### Negative

- The ceiling binds here in the chat, not the host (SD3).
- More tools in every request with Artefact on; ten schemas.
- The document dies with the window unless handed to mdedit or copied.
- The person cannot type in the artefact; a fix of one word goes through
  the model or through mdedit, and mdedit's copy does not come back.

### Neutral

- A conversation with Artefact off is unchanged.

## Migration — Tier 1

None: new packages, and a new option that is off by default. ADR-0265 is
proposed and gains a reference.

## Verification plan — Tier 1

- `mdspan`: section ranges across levels, a `#` in a fence, frontmatter, a
  document with no headings; splice reports the right span.
- `mdlint`: one fixture per rule, and a clean fixture with no findings.
- The tools: a stale base revision is refused; `artefact_edit` with zero and
  two matches is refused; Allow below Edit offers no write tool and refuses
  one called; Ask first waits for the verdict and Cancel withdraws it.
- Rewind and branch restore the revision of the turn.
- A scene on the headless host with a scripted model: a write under Ask
  first, Accept, the panel showing revision 1, then Revert.

## Status

Proposed 2026-10-04, built. The verification plan holds: the unit tests of
`mdspan`, `mdlint` and the chat's tools, revisions, policy and rewind, a
turn under Ask first against a scripted model, and the scene
[chat-artefact](../../apps/chat/scenes/chat-artefact.scene.md) on the
headless host. Not verified: the desktop host, and a real model's use of
the tools.

## Updates

None.

## References

- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) §SD2, §SD5, §SD6, §SD7 — the chat, its lifetime, the tool loop, `ask_user`.
- [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) — the ceiling and the ladder.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) — what Keep retains.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD5 — operation effects.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — the widget contract, for the panel's parts.
