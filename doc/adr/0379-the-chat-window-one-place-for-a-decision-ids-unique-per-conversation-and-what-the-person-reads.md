---
type: adr
status: proposed
date: 2026-11-17
---

# ADR-0379: The chat window — one place for a decision, assertion ids unique per conversation, and what the person reads

## Context

A review of the chat window's use, made on 2026-11-17 from the maintained
scene tour against a scripted model and from the code, found the window
built mostly for the person who designed the harness. The findings that
need a decision are below. The rest of the review changed only what the
window draws. Those changes are listed under SD5 so that this record says
what was kept as well as what moved. Neither the review nor this record
rests on a drive against a real model.

- A question the model asks (`ask_user`, a held stop, an exit approval)
  was drawn twice: as a form in the transcript and as a card in the host's
  queue. Both could be answered. With both on screen the person had two
  forms to read for one answer
  ([ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md)'s
  row "also posted to the queue; answered from either surface").
- An assertion id was unique only within its turn
  ([ADR-0340](./0340-cited-replies-assertions-the-window-checks-and-verdicts-in-the-journal.md)
  §SD1). Models number each turn's claims `a1`, `a2`, …, so ids repeat
  across turns. The artefact gathers references across turns and resolves
  each against "the latest turn's standing where two turns reused an id"
  (§SD6). In practice turn 1's `[a1]` was shown, checked and guarded as
  turn 3's `[a1]`.
- A copy of a reply or of the artefact carried the raw text: references
  without the outcome they show in the window, and no citations under them.
  A held copy said why only on the status line above the composer, which
  is in a different pane from the artefact's Copy.
- A resume read back the artefact's last head only
  ([ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md),
  Update of 2026-10-27). The journal holds a snapshot of every move of the
  head between turns, and those were not read. Over an export whose
  revision was older than the journal's head, the restored head also lost
  the screenshots the export had brought back.

## Decision

### SD1 — A decision is drawn in one place, chosen by focus

While the chat window has the person's focus, a question the model or the
harness asks is the window's form alone. Once the window loses focus, the
question is posted to the host's queue as a card. A posted card stays until
it is answered or withdrawn, so it does not come and go with focus.
Answering either surface settles both, as before. An urgent conversation's
question still opens its card at once, but only once the person is in
another window
([ADR-0323](./0323-attention-levels-one-table-two-scales-configurable-within-a-floor.md)
§SD4). In the chat window the form is already in front of them. This narrows ADR-0324's
row for chat questions. The question is still answerable from any window,
because leaving the chat window is what posts it.

### SD2 — Assertion ids are unique in the conversation

`cite` refuses an id that an earlier turn's verdict used and names the next
free id. Each cite result's note also names the next free id. The tool's
schema and the citations prompt say so. This replaces ADR-0340 §SD1's
"unique within the turn". The transcript, the artefact, the copy guard and
the export then resolve a reference by its id alone. The SD6 rule for
reused ids remains only for runs written before this change.

### SD3 — A copy carries what the person reads

Copy on a reply copies its references marked with their outcomes, plus its
citations. Copy on the artefact copies the text with the citations as a
foot, as the editor hand-off already did. The guard of ADR-0340 §SD6 still
checks the raw text's references, which the marks would hide from it. A
held gesture is drawn beside the control that made it, with the reason and
an **Anyway** button, as well as on the status line.

### SD4 — A resumed artefact's revisions are the journal's snapshots

A resume makes each snapshot the journal holds a revision, under its
number. A snapshot under a number at or below an earlier one (a rewind, or
a cancelled edit's restore) takes back what was numbered from there on, as
the live artefact did. A revert's snapshot names the revision it made
current again when the history still holds that revision. An export's
revision stands for its own number. A revision after it takes the
screenshot set before it, as a text write does. A revision before it has
no screenshots (ADR-0284). The writes inside one turn are not snapshots and
do not come back.

### SD5 — What the window draws, decided with the above

These changes draw differently and enforce nothing new. They are recorded
here for the reader who finds the window changed:

- **Yes-or-no questions.** The harness's own yes-or-no questions — a held
  stop — are one button per option, and a click answers. An exit approval
  keeps its form because it needs a justification.
- **Readable stop rows.** A stop's row reads as `column value` pairs.
  Before, it read as the worker's JSON line with a tool call's arguments
  escaped a second time.
- **Hidden plumbing.** A step's result shows the untrusted content with
  where it came from, not the delimiters the model reads. The Document view
  and the diffs leave out the `chat_*` properties; the Source view and
  every copy keep them.
- **One progress line.** A turn that is still one model call gets one
  progress line, not a summary and a step list. A conversation that offers
  no tools shows no round count.
- **Refusals.** A tool call that was refused or failed reads at full
  strength in the error tone.
- **First-message choices.** The plain chat's empty transcript offers the
  four choices the first message fixes (Apps, Questions, Artefact, Keep),
  bound to the same fields as Settings. After Edit takes back the only
  turn, the transcript describes the conversation being edited, not the
  next one.
- **Harness tuning.** "A tool per operation" and the rounds slider are
  folded under Settings' **Harness** header.
- **Tab titles.** The tab titles carry a glyph or a count rather than a
  word, so the column's titles fit its default width.

Kept as they were, by the person's choice on 2026-11-17: the full bar is the
plain chat's default (the Simple view of ADR-0364 SD6 stays opt-in), and
the dock's split is unchanged. Taint is unchanged as well: reading the
desktop's window list still marks the conversation, since a window title
can carry text someone typed (ADR-0269 §SD7).

## Alternatives

- **Both surfaces, as ADR-0324 had it.** Not taken: one answer, two forms
  to read, and the card sits over the window that already shows the form.
- **The queue alone.** Not taken: the transcript would lose the form where
  the person is working, and a turn would wait on a card in another place.
- **Ids qualified by their turn, invisible to the model.** Not taken: an
  artefact edited over several turns mixes turns within one text, so a
  reference would need its turn written beside it.
- **Replaying each artefact tool call on resume.** Deferred: it would bring
  back the writes inside a turn too, but it re-runs every write tool's
  semantics and the proposals' accept or reject outside the turn loop.
  Trigger: a person who needs to revert to a revision inside a turn of a
  resumed conversation.

## Consequences

- The cite tool's schema and the citations prompt are in the run's
  fingerprint, so conversations opened before this change are stranded:
  they stay readable and do not continue (ADR-0308).
- The host's count of what waits on the person leaves out a question while
  the chat window has focus.
- A resumed artefact's revision numbers have gaps where writes inside a
  turn were.

## Verification plan — Tier 1

- **Lane.** Default `go test`:
  - `TestAQuestionGoesToTheQueueOnlyOnceTheWindowLosesFocus`
  - `TestAssertionIdsAreUniqueInTheConversation`
  - `TestTheArtefactsHistoryIsReadBackFromTheJournal`
  - `TestAResumeRestoresTheArtefact`
  - `TestTheJournalsHeadGoesOverAnEarlierExport`
  - `TestAStopRowReadsAsFields`
  - `TestAButtonAnswersAtOnce`

  The preservation goldens are re-recorded for the schema and prompt
  change only.
- **Scenes.** The chat tour under `apps/chat/scenes`. The queue scenes
  take focus away from the chat before they expect a card.
- **Gap.** That the copy carries the marks is read off the code. No test
  reads the clipboard.

## Status

Proposed 2026-11-17.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0324](./0324-the-persons-decisions-wait-in-a-host-queue-not-in-modals.md) — the host's queue; SD1 narrows its row for chat questions.
- [ADR-0340](./0340-cited-replies-assertions-the-window-checks-and-verdicts-in-the-journal.md) §SD1, §SD6 — assertions, consumers and the copy guard; SD2 and SD3 change them.
- [ADR-0282](./0282-chat-artefact-one-markdown-document-per-conversation.md), Update of 2026-10-27 — the artefact on resume; SD4 widens it.
- [ADR-0284](./0284-chat-artefact-screenshots-sealed-under-a-budget.md) — why screenshots do not survive the process.
- [ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md) §SD7 — an export read back on resume.
- [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md) SD6 — the Simple view, kept opt-in.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) §SD7 — untrusted content, unchanged.
