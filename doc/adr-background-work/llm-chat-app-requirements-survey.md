---
type: explanation
audience: designer of the chat app (ADR-0265) and of retained model conversations (ADR-0264)
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-27 to feed [ADR-0265](../adr/0265-chat-app-over-retained-model-calls.md) and [ADR-0264](../adr/0264-retained-model-conversations-on-facts.md); nothing here is a decision. The survey is clean-room: it rests on public product documentation and help pages, and no third-party source code was read. Each claim about a product is tagged either `[verified: URL]` (fetched on the compile date and says so) or `[from general knowledge]` (not re-read; check before building on it).

# LLM chat applications — a requirements survey

The question: of what chat applications commonly do, what does a minimal
desktop chat over `llm.retain.complete` need in its first version, what
can wait, and — the part that costs most to get wrong — which
requirements would force a change to ADR-0264's storage model: one
append-only row per message, calls linked by conversation id and parent
call id, and only the messages new since the parent kept, found by a
prefix check against the parent's hashes.

Caveats first. Help pages describe intended behaviour, not what a
product does in every client or plan; several of the pages read here
were updated within the last month and will move again. Help centres
document privacy and data handling far more carefully than interaction
details, so the matrix is thinner on keyboard, accessibility and
branch display than on retention. One vendor's help centre refused
direct fetches; those pages were read through a public reader proxy and
are marked in the References. `[verified: KEY]` below is a
reference-style link; each key resolves to its URL in the References
list.

Products surveyed: three hosted (ChatGPT, Claude.ai, Gemini), one local
desktop app (LM Studio), one local desktop app with local files as its
store (Jan), and two self-hosted web front ends (Open WebUI, LibreChat).

## 1 Feature matrix

`v` = a fetched page says so; `gk` = from general knowledge, not
re-read; `—` = no public page read says either way. Sources per cell are
in §2.

| Feature | ChatGPT | Claude.ai | Gemini | LM Studio | Open WebUI | LibreChat | Jan |
| --- | --- | --- | --- | --- | --- | --- | --- |
| History list + search | yes v | list v, search gk | yes v | folders v | yes, content v | yes v | threads v |
| Edit own message | yes v | yes gk | yes v | yes gk | yes v | yes v | yes v |
| Edit assistant reply | — | no gk | no gk | yes gk | yes v | yes gk | yes v |
| Regenerate, versions kept | yes gk | yes gk | latest only v | yes v | yes v | yes gk | yes v |
| Branch to new chat | yes v | no gk | yes v | yes v | yes v | yes v | — |
| Stop generation | yes v | yes gk | yes gk | yes gk | yes v | yes gk | yes gk |
| Streaming | yes gk | yes gk | yes gk | yes gk | yes gk | yes v | yes gk |
| Over-long context | — | summarise v | — | policy v | error / opt-in compaction v | — | setting gk |
| Per-chat system prompt / params | projects gk | projects gk | Gems gk | yes v | yes v | yes gk | yes gk |
| Attachments | yes v | yes gk | yes gk | docs + RAG v | yes v | yes v | — |
| Copy code block | shortcut v | yes gk | yes gk | yes gk | shortcut v | yes v | yes gk |
| Export / import | ZIP export v | JSON export v | — | — | JSON/txt/PDF; import v | import v | files on disk v |
| Share link | snapshot v | snapshot v | yes gk | no gk | yes v | yes v | no gk |
| Delete + retention | 30 d v | 30 d v | 3/18/36 mo v | local gk | local v | local gk | local v |
| Temporary chat | yes, 30 d v | yes, 30 d v | yes, 72 h v | n/a | yes, nothing written v | yes, 30 d default v | — |
| Shortcut list | yes v | gk | — | some v | rebindable v | some v | — |
| Token / context display | no gk | no gk | no gk | yes v | yes gk | yes gk | yes gk |
| Multi-window / side by side | — | — | — | split view v | two models v | multi-tab sync v | background threads gk |
| Offline / local model | no | no | no | yes v | yes v | yes gk | yes gk |

## 2 Requirements

Each entry: what the surveyed products do, then a verdict for this app —
**v1**, **defer**, or **n/a** — with one line of reason. "The ADR" means
ADR-0265 unless ADR-0264 is named.

### 2.1 Functional — the core loop

**F1 Send a turn, see the reply, continue.** Universal. ChatGPT converts a
paste over 10k characters into an attachment so it does not consume the
context window [verified: [OA6]]. **v1** — the ADR's SD3 turn loop.

**F2 Stop / cancel.** ChatGPT added "stop generating" as an early
release-note item [verified: [OA6]]; Open WebUI binds it to Escape in the
input and makes that binding fixed [verified: [OW4]]. In every product
read, stop acts on a stream: the partial answer stays visible
[from general knowledge]. **v1** as the ADR has it (abandon the wait) —
but see §3.6: without streaming, cancel leaves a completed call on the
record that the user never saw.

**F3 Per-turn errors in place.** Jan renders an error card under the user
message that failed, with a Regenerate button, and persists the error per
thread across restart [verified: [JN3]]. **v1** — the ADR's failed bubble;
ADR-0264 §SD4 keeps a failed call's messages.

**F4 New conversation.** Universal [from general knowledge]; Open WebUI lists editing and
regenerating among its core chat features [verified: [OW9]]. **v1**.

### 2.2 Functional — history

**F5 Conversation list, search, rename, pin, folders.** Claude.ai lists,
renames and deletes from the sidebar [verified: [CL1]]; Gemini searches,
pins, renames and deletes, and requires Keep Activity to be on for any of
it [verified: [GE3]]; Open WebUI searches titles and message content with
`tag:`/`folder:`/`pinned:` prefixes, and groups by time [verified: [OW5]];
LM Studio has nested folders and chat duplication [verified: [LM1]].
**defer** — ADR-0265 SD2/SD5: past conversations are read in play; a list
needs a read surface and titles.

**F6 Titles.** Every hosted product shows one; Open WebUI's temporary chat
explicitly writes no title [verified: [OW8]]. **defer** (ADR-0264 §SD7) —
a title is a non-call event.

**F7 Resume a past conversation.** Universal among products with history.
**defer** — ADR-0265 SD5; it makes the app rely on the rows.

### 2.3 Functional — edit, regenerate, branch

**F8 Edit an earlier user message and resend.** Gemini: edit, then
Update, and the response is regenerated [verified: [GE1]]. LibreChat
distinguishes Save (no rerun) from Update & rerun (Ctrl/Cmd+Enter)
[verified: [LC5]]. Claude.ai keeps the old continuation and shows arrows
between versions [from general knowledge]. **defer** — ADR-0265 SD5; the
storage already takes it as a call with an earlier parent (§3.2).

**F9 Regenerate / alternative answers.** Jan: each edit or regeneration
creates a version, a `< n/m >` control navigates them, switching a
version re-threads everything downstream, and branches persist as "a
parent-pointer tree in message metadata" [verified: [JN1]]. LM Studio:
multiple generations per chat with arrow navigation [verified: [LM3]].
Gemini offers other drafts only for the latest response
[verified: [GE1]]. ChatGPT shows `n/m` arrows on regenerated and edited
messages [from general knowledge]. **defer** — same reason as F8.

**F10 Branch into a separate conversation.** ChatGPT: "Branch in new chat"
from a message's More actions [verified: [OA6]]. Gemini lists branching
into new chats [verified: [GE3]]. Open WebUI forks from any assistant
response [verified: [OW2]]. LibreChat offers three copy scopes — visible
path only, path plus branches along it, or everything to/from the target
[verified: [LC2]]. **defer**; note that in LibreChat and ChatGPT a branch
is a *copy* into a new conversation, where ADR-0264 would record a new
call in the same conversation (§3.2).

**F11 Edit an assistant reply.** Jan and Open WebUI allow it (Open WebUI
including replies holding tool calls) [verified: [JN1], [OW2]]; LM Studio
allows insert/edit/continue for either role [from general knowledge].
**n/a for v1**, and the one edit the storage model cannot take
cheaply (§3.3).

### 2.4 Functional — context-window management

**F12 What happens when the conversation outgrows the model.** Four
distinct behaviours:

- *Summarise, keep the full record.* Claude.ai summarises earlier
  messages near the limit, keeps the full history "so Claude can
  reference it", shows "organizing its thoughts", only with code
  execution enabled, and at extra usage cost [verified: [CL3]].
- *Opt-in compaction or refusal.* Open WebUI "does **not** silently
  truncate your conversation": the provider rejects the request, unless
  an admin enables Context Compaction (default threshold 80,000 tokens) or
  a filter function caps messages or tokens [verified: [OW6]].
- *A named policy.* LM Studio's `contextOverflowPolicy` is `stopAtLimit`
  (stop reason `contextLengthReached`), `truncateMiddle` (keep the system
  prompt and first user message) or `rollingWindow` [verified: [LM2]].
- *A context-length setting, silent forgetting.* Jan's model settings
  warn that a short context "might forget" earlier parts
  [from general knowledge; seen only as a search snippet of Jan's
  model-settings page].

**v1: fail with the provider's error** (ADR-0265 SD3), which matches Open
WebUI's default. **defer** windows and summaries (§3.1).

**F13 Show how much context is used.** LM Studio displays current tokens
against total context [verified: [LM3]]; the hosted products do not show
a count [from general knowledge]. **defer, but cheap**: the reply's token
counts are already on the `llmCall` row; a running total beside the
composer would make F12's failure less of a surprise.

### 2.5 Functional — settings

**F14 System prompt and parameters per conversation.** Open WebUI has a
three-level hierarchy — per chat over per account over per model — and a
per-chat parameter "is the one sent" [verified: [OW7]]; folders become
project workspaces with their own system prompts [verified: [OW2]].
LM Studio remembers per-model load settings [verified: [LM3]]. Hosted
products hang instructions on projects, Gems or custom instructions
[from general knowledge]. **defer**, or v1 only as a field fixed before
the first turn: changing it mid-conversation rewrites message 0 (§3.4).

**F15 Model choice.** Open WebUI runs two models side by side and switches
model mid-conversation [verified: [OW1]]. **n/a for v1** — the host
decides the model (`llm.describe`); the ADR shows it, does not choose it.

### 2.6 Functional — content handling

**F16 Markdown and code blocks with copy.** ChatGPT: ⌘/Ctrl+Shift+; copies
the last code block, ⌘/Ctrl+/ lists shortcuts [verified: [OA6]]; Open WebUI
has the same copy-last-code-block and copy-last-response bindings
[verified: [OW4]]; LibreChat code blocks copy, run or download
[verified: [LC5]]. **v1** for rendering (widgets/markdown); **v1 if cheap**
for a copy button — a model's answer is mostly copied out.

**F17 Attachments.** LM Studio puts a short document's full text in
context and falls back to retrieval of "relevant bits" for long ones
[verified: [LM4]]; ChatGPT lets messages with image or file attachments be
edited [verified: [OA6]] and may save uploaded files to a separate Library
with its own retention [verified: [OA3]]. **defer** (§3.7).

### 2.7 Functional — export, share, delete

**F18 Export / import.** ChatGPT exports a ZIP by email link, up to 7 days,
link valid 24 h [verified: [OA4]]; Claude.ai a ZIP by email, not importable
into another account [verified: [CL4]]. Open WebUI exports JSON, text or
PDF per chat and imports its own and ChatGPT's exports; its JSON keeps the
branch tree with parent and child ids and a `history.currentId` for the
active branch, and re-import creates duplicates [verified: [OW3]]. LibreChat imports
ChatGPT and other exports [verified: [LC1]]. Jan keeps each thread as a
folder holding a line-delimited `messages.jsonl` and a `thread.json` of
settings [verified: [JN2]] — the closest of the seven to an append-only
per-message store. **n/a** — SQL over `boxer.facts` is the export.

**F19 Share.** A share is a snapshot of messages sent before sharing, in
Claude.ai (reshare to update) [verified: [CL5]] and ChatGPT personal
accounts ("Update link"), while ChatGPT workspace links include later
messages [verified: [OA5]]; LibreChat publishes "the selected branch's
exact persisted tail" and can refresh at the same URL [verified: [LC6]].
**n/a** for a single-user desktop app.

**F20 Delete a conversation.** ChatGPT: gone from view at once, scheduled
for permanent deletion within 30 days, not restorable; archive is a
separate, non-deleting action [verified: [OA2]]. Claude.ai: removed at
once, deleted from back-end systems within 30 days [verified: [CL1]].
Gemini: auto-delete after 3, 18 or 36 months, or never [verified: [GE2]].
Open WebUI deletes single chats or all history, and archives
[verified: [OW5]]. **defer** — but the expectation is universal, and
ADR-0265 already tells the user their conversation outlives the window
(§3.5).

**F21 Temporary / incognito chat.** Four different meanings of
"temporary":

| Product | Kept? | Visible later? |
| --- | --- | --- |
| ChatGPT | copy up to 30 days; can be *saved* into a regular chat later [verified: [OA1]] | no, unless saved |
| Claude.ai | 30 days or org policy; cannot be converted [verified: [CL2]] | no; in org exports |
| Gemini | up to 72 hours [verified: [GE2]] | no [verified: [GE1]] |
| LibreChat | in the database until retention ends, default 30 days, 1 h–1 y [verified: [LC3]] | not in sidebar or search |
| Open WebUI | nothing written: no chat record, messages, titles or tags [verified: [OW8]] | no |

**v1-cheap if wanted** (§3.5): send on `llm.complete` instead of
`llm.retain.complete`.

### 2.8 Non-functional

**N1 Perceived latency.** Every product read streams [from general
knowledge]; LibreChat goes further and resumes an interrupted stream from
recorded deltas and syncs it across tabs and devices [verified: [LC4]].
**v1 without streaming** (ADR-0265): the placeholder with elapsed time is
the whole mitigation; for a local model on long answers it is the app's
largest usability gap.

**N2 Persistence is honest.** Jan persists branches and errors across
restart [verified: [JN1], [JN3]]; Gemini's history features switch off
entirely when Keep Activity is off [verified: [GE3]]. **v1** — ADR-0265's
per-conversation "not kept" notice is the equivalent disclosure.

**N3 Privacy and retention are visible.** Every hosted product states
deletion delay and temporary-chat retention in its help pages (F20, F21);
LM Studio states that nothing typed leaves the device and lists which
features do use the network [verified: [LM5]] and claims no telemetry
[verified: [LM3]]. **v1** — the app should say where text goes (model
host) and where it is kept (`boxer.facts`), since the latter has no TTL
(ADR-0264 §SD7).

**N4 Keyboard.** Open WebUI: about twenty rebindable shortcuts and eight
fixed ones, a master toggle, Escape stops [verified: [OW4]]. LibreChat:
Ctrl/Cmd+Enter to update and rerun an edit [verified: [LC5]]. The hosted
products send on Enter and newline on Shift+Enter
[from general knowledge]. **v1** — ADR-0265's Ctrl+Enter is the
multi-line-editor convention and matches play's Run; the Enter-sends
habit from hosted products is the expectation it goes against, so the
composer should show the chord.

**N5 Accessibility.** No page read documents screen-reader support for
the transcript; this is the thinnest area of the survey. **v1** only to
the extent chatview already exposes names to the ADR-0154 tree.

**N6 Multi-window.** LM Studio splits two chats side by side
[verified: [LM1]]; Jan keeps a thread generating while the user is
elsewhere [from general knowledge; seen only as a search snippet of Jan's
changelog]. **v1** as the ADR has it — each window its own
conversation, Ctrl+Enter gated by focus.

**N7 Offline / local model.** LM Studio runs fully offline once a model
is downloaded [verified: [LM5]]; its REST API can keep conversation state
server-side and branch by `previous_response_id`, or not (`store: false`)
[verified: [LM6]]. **n/a** as a feature — the host decides; relevant only
in that local answers are slow (N1) and contexts short (F12).

## 3 What touches the storage model

The model under test (ADR-0264 §SD2–SD3): calls append-only; a
`llmMessage` row per message, at a logical ordinal; a turn keeps only
what follows its parent's messages, found by per-message hash; a declared
omission (`OmitFrom`, `OmitTo`) says what a window left out; anything
that matches neither is kept in full from ordinal 0 and so marked.

### 3.1 Context truncation and compaction

- *Truncation, rolling window, truncate-middle* (LM Studio's three
  policies, Open WebUI's filters): **covered.** Each is a declared
  omission; truncate-middle is one range, a rolling window a growing one.
  Keeping the system message is keeping ordinal 0 outside the range.
- *Summary in place of the omitted range* (Claude.ai, Open WebUI
  compaction): **not covered.** ADR-0264 §SD7 names it: a summary is
  context the model saw, not a turn, and until it has a role or flag it
  makes the turn a rewrite, kept in full. What it would need: a message
  role or flag ("stands in for ordinals a..b"), and the prefix check
  taught to accept it. Claude.ai's "full history preserved" is exactly
  the logical conversation plus that flagged message.
- *A per-call view of what the model saw* is what Open WebUI's refusal
  default avoids needing; the call row's omission range already gives it.

### 3.2 Edit, regenerate and branch display

- *Edit a user message, regenerate*: **covered.** A new call whose parent
  is the call before the edited turn; its request matches the parent's
  prefix, so only the new user message and reply are kept. One cost: a
  plain regenerate re-keeps the unchanged user message on every
  regeneration, since it is "new since the parent". Harmless for
  correctness; a reader grouping versions groups by parent.
- *Branch display* (`n/m` arrows in Jan, LM Studio, ChatGPT): **covered
  as a tree, not as a selection.** Siblings are calls sharing a parent.
  What the model lacks is the *active* branch — Open WebUI's
  `history.currentId`. In a session-only app that is in-memory state; a
  resumed conversation would either take "latest leaf" as the rule or
  need a non-call event recording the choice (the same verb titles
  need).
- *Branch into a new conversation* (ChatGPT, LibreChat copy scopes):
  **not the model's shape.** ADR-0264 branches within a conversation. A
  copy into a new conversation id would store its whole request (no
  parent in that conversation) unless the parent may cross conversations.
  Worth deciding before resume ships; not needed in v1.

### 3.3 Editing an assistant reply, or Save without rerun

**Not covered cheaply.** Changing a reply the model gave (Jan, Open
WebUI, LM Studio) makes the next request disagree with the parent's
hashes: the turn is kept in full and marked as a rewrite — correct, but
it forfeits the dedup for that turn, and the record cannot say which
message was edited. LibreChat's Save-without-rerun makes no call at all,
so nothing records it until the next turn. What it would need: an
"edited message" event, or acceptance that edits are rewrites. The
verdict **n/a for v1** avoids the question.

### 3.4 Per-conversation settings changed mid-way

Changing the system prompt after turn 1 rewrites ordinal 0: the next
turn is kept in full and marked, later turns dedupe against it. Model
or parameter changes are per call and live on the `llmCall` row, so
they cost nothing. **Covered, with a one-turn penalty**; fixing the
system prompt before the first send avoids it.

### 3.5 Deletion, retention, temporary chats

- *Delete a conversation* (universal in hosted products, with a stated
  30-day back-end window): **not covered as a user action.** The SD6
  ditch deletes by kind and app; the columns a per-conversation delete
  would select by are on every row. What it would need: a
  conversation-scoped lightweight delete, a decision on whether the
  `llmCall` counts go too, and a record that the delete happened —
  ADR-0264 §SD7's purge policy. Append-only rows make "hide from list"
  cheap (a non-call event) and "erase" a separate mechanism; the
  products read keep the two apart too (ChatGPT archive vs delete).
- *Retention period* (Gemini's 3/18/36 months): `boxer.facts` has no
  TTL; a per-row expiry is a table-level change, not a chat change.
- *Temporary chat*: **covered by not retaining.** Sending on
  `llm.complete` keeps counts and no text — Open WebUI's semantics, the
  strictest of the five. ChatGPT's "save it later" is **not covered**:
  it would need a backfill write of a conversation no retained call
  carried. Claude.ai and LibreChat's "kept 30 days but hidden" would
  need a TTL.

### 3.6 Cancel without streaming

**A gap the products do not have.** Where a stream is stopped, the
partial answer is what the user saw. Here, cancel abandons the wait but
the host call completes and is kept (ADR-0254's limit); the next turn's
parent is the previous successful reply, so the abandoned call becomes a
dead-end sibling in the tree — indistinguishable in SQL from a
regenerate the user chose not to follow. What it would need: a way to
mark the call abandoned (a cancel that reaches the service, or a
non-call event), or reader guidance that a childless leaf may be a
cancel. Small, but it touches v1.

### 3.7 Attachments

Images are kept as hash and size (ADR-0264 §SD3), so the record cannot
reproduce the turn. Documents injected as text are ordinary message
content and dedupe once sent. Retrieval-style attachments (LM Studio's
RAG fallback) insert *different* excerpts each turn; if they are spliced
into earlier messages, every turn is a rewrite. What it would need: the
excerpts carried as a new message of their own each turn, or a blob
store for bytes — ChatGPT keeps files in a Library with retention
separate from chats [verified: [OA3]], which is that shape.

### 3.8 Summary

| Requirement | Covered by append-only + parent + prefix dedup? |
| --- | --- |
| Truncation / rolling window | yes, declared omission |
| Summary / compaction | no — needs a summary role or flag |
| Edit user message, regenerate | yes; regenerate re-keeps the user message |
| Active-branch selection | no — non-call event, or "latest leaf" rule |
| Branch into a new conversation | no — cross-conversation parent, or full copy |
| Edit assistant reply | only as a full-keep rewrite |
| System prompt change mid-way | yes, one full-kept turn |
| Delete a conversation | no — scoped delete + purge policy |
| Temporary chat (nothing kept) | yes, send unretained |
| Temporary chat, save later | no — backfill write |
| Cancel without streaming | partly — abandoned call looks like a branch |
| Attachments with bytes | no — blob store |

## 4 Open questions for the owner

1. **Cancel** (§3.6): should an abandoned call be marked, and by whom —
   a cancel that reaches the service, or the app writing an event?
2. **Temporary chat in v1** (F21): worth a toggle, given it costs one
   subject choice and gives the only "nothing kept" option a user has?
3. **System prompt** (F14, §3.4): none in v1, or a field fixed before the
   first send?
4. **Enter vs Ctrl+Enter** (N4): keep Ctrl+Enter, matching play, against
   the hosted-product habit?
5. **Token display** (F13): add a running context count in v1, since the
   data is on the call row and F12's only v1 behaviour is failure?
6. **Delete** (F20, §3.5): is the absence of any delete acceptable while
   kept text has no TTL, or should the purge decision (ADR-0264 §SD7)
   precede the chat's first release?
7. **Branch into a new conversation** (§3.2): allow a parent from another
   conversation, before resume makes the choice for us?

## References

Fetched 2026-09-27. OpenAI help pages returned HTTP 403 to direct
fetches and were read through a public reader proxy
(`r.jina.ai/<url>`); the content is OpenAI's public help page.

- [OA1] Temporary chat in ChatGPT — <https://help.openai.com/en/articles/8914046-temporary-chat-in-chatgpt>
- [OA2] Deleting and archiving chats in ChatGPT — <https://help.openai.com/en/articles/8809935-how-to-delete-and-archive-chats-in-chatgpt>
- [OA3] Chat and file retention in ChatGPT — <https://help.openai.com/en/articles/8983778-chat-and-file-retention-policies-in-chatgpt>
- [OA4] Exporting your ChatGPT history and data — <https://help.openai.com/en/articles/7260999-how-do-i-export-my-chatgpt-history-and-data>
- [OA5] ChatGPT shared links FAQ — <https://help.openai.com/en/articles/7925741-chatgpt-shared-links-faq>
- [OA6] ChatGPT release notes (branch in new chat; stop generating; shortcuts; paste-to-attachment; editing messages with attachments) — <https://help.openai.com/en/articles/6825453-chatgpt-release-notes>
- [CL1] Claude Help Center — deleting and renaming conversations — <https://support.claude.com/en/articles/8230524-how-can-i-search-my-chat-history>
- [CL2] Use incognito chats — <https://support.claude.com/en/articles/12260368-use-incognito-chats>
- [CL3] How do usage and length limits work? — <https://support.claude.com/en/articles/11647753-how-do-usage-and-length-limits-work>
- [CL4] Export your Claude data — <https://privacy.claude.com/en/articles/9450526-how-can-i-export-my-claude-data>
- [CL5] Share and unshare chats — <https://support.claude.com/en/articles/10593882-share-and-unshare-chats>
- [GE1] Use Gemini Apps — <https://support.google.com/gemini/answer/13275745?hl=en>
- [GE2] Manage & delete your activity in Gemini Apps — <https://support.google.com/gemini/answer/13278892?hl=en&co=GENIE.Platform%3DDesktop>
- [GE3] Find and manage your recent chats in Gemini Apps — <https://support.google.com/gemini/answer/13666746?hl=en>
- [LM1] LM Studio — Manage chats — <https://lmstudio.ai/docs/app/basics/chat>
- [LM2] LM Studio — `LLMPredictionConfigInput` — <https://lmstudio.ai/docs/typescript/api-reference/llm-prediction-config-input>
- [LM3] LM Studio 0.3.0 release post — <https://lmstudio.ai/blog/lmstudio-v0.3.0>
- [LM4] LM Studio — Chat with documents — <https://lmstudio.ai/docs/app/basics/rag>
- [LM5] LM Studio — Offline operation — <https://lmstudio.ai/docs/app/offline>
- [LM6] LM Studio — Stateful chats (REST) — <https://lmstudio.ai/docs/developer/rest/stateful-chats>
- [OW1] Open WebUI — Features — <https://docs.openwebui.com/features/>
- [OW2] Open WebUI — Chat features — <https://docs.openwebui.com/features/chat-conversations/chat-features/>
- [OW3] Open WebUI — Import & export — <https://docs.openwebui.com/features/chat-conversations/data-controls/import-export/>
- [OW4] Open WebUI — Keyboard shortcuts — <https://docs.openwebui.com/features/chat-conversations/chat-features/keyboard-shortcuts/>
- [OW5] Open WebUI — History & search — <https://docs.openwebui.com/features/chat-conversations/chat-features/history-search/>
- [OW6] Open WebUI — Context window / prompt too long — <https://docs.openwebui.com/troubleshooting/context-window/>
- [OW7] Open WebUI — Chat parameters — <https://docs.openwebui.com/features/chat-conversations/chat-features/chat-params/>
- [OW8] Open WebUI — URL parameters (temporary chat) — <https://docs.openwebui.com/features/chat-conversations/chat-features/url-params/>
- [OW9] Open WebUI — Chat & conversations — <https://docs.openwebui.com/features/chat-conversations/>
- [LC1] LibreChat — Features — <https://www.librechat.ai/docs/features>
- [LC2] LibreChat — Fork — <https://www.librechat.ai/docs/features/fork>
- [LC3] LibreChat — Temporary chat — <https://www.librechat.ai/docs/features/temporary_chat>
- [LC4] LibreChat — Resumable streams — <https://www.librechat.ai/docs/features/resumable_streams>
- [LC5] LibreChat — Message actions — <https://www.librechat.ai/docs/features/message_actions>
- [LC6] LibreChat — Shareable links — <https://www.librechat.ai/docs/features/shareable_links>
- [JN1] Jan v0.8.3 changelog (message branching) — <https://www.jan.ai/changelog/2026-06-24-jan-v0.8.3>
- [JN2] Jan data folder — <https://www.jan.ai/docs/desktop/data-folder>
- [JN3] Jan v0.8.1 changelog (per-message errors) — <https://www.jan.ai/changelog/2026-05-29-jan-v0.8.1>
