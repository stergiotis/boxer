---
type: explanation
audience: end-user
status: draft
title: The chat window
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The chat window

For someone who asks a model to work on their data and in their windows, and
stays in charge of what it may do: they come here to ask a question and read
the answer, to let the model work in a window and decide what it asks, to get
a document out, or to pick up an earlier conversation. They know their data.
They have not read the harness, and the window does not expect them to. This
reading of who the window is for is ADR-0404 §SD7 (proposed); the window has
not been tried with such a person.

A chat window is one conversation with the host's model (ADR-0265). The
window is the presentation and the audit key; the conversation's **run** —
its journal — is the record; a **task**, when the model works in other
windows, is the authority. Open as many windows as you like: each holds
one conversation and runs at most one turn at a time.

The chat is a regular app, not a system one. It holds the model grant
because the person registered it as a coordinator; every other app
declares operations and never calls the model itself (ADR-0318). This book
is for the person at the window and for an agent that shares it: the
[turn machine](turn.md) is what such an agent can read; the
[coordinator](coordinator.md) is how this window's model works in others;
[settings and ceilings](settings.md) say who enforces what; the
[artefact](artefact.md) is the conversation's document; [stops](stops.md)
are the conditions that hold or end a conversation.

## The bar

Above the bar is the window's menu bar, with two menus. **View** opens and
closes the views beside the transcript — Conversations, Settings, Document
when the conversation has one, Analytics (Definition in a chatlet) and
Stops — each checked while it is open. **Conversation** holds **Watch**,
**Urgent** and **Open in mdedit** once the conversation has started.

The bar is one row: **New conversation**; the conversation's title once it
has one, which a click renames; the **state chip**, which says where the
turn stands — *ready*, *thinking*, *working*, *your turn*, *answered* and
how else a turn ended; and what the model may do, in words ("Model: may edit
and run in your windows"), which opens **Settings**.

The state chip opens an inspector with the conversation's facts, each with a
copy: its **id** — the key every record of this chat carries, in
`keelson('llm_calls')` and `keelson('agent_actions')` under `conversation`
and on the audit trail — with a button that opens its trail rows in play;
what its first message fixed (**Keep** and whether the host kept it,
**Apps**, **Questions**, **Document**); the model and the endpoint it is
reached at, and whether that is on this machine; how much of the model's
context the last call used; and, while the model holds one, the **task**'s
id and what it was granted. Under the facts are the turn's states and how
one leads to the next, as a table, a graph and a history.

While the model holds a task, a row under the bar has **Stop task** and the
marks the conversation carries: **read untrusted content**, **holds confined
content**. A conversation that read untrusted content without a task, such as
an image you sent, carries the mark too. Under that, at most one notice: why
the conversation is not kept, or that closing the window ends it here and it
is read in play.

When a request reaches the profile's share of the route's context, the
chat compacts the conversation before sending it (ADR-0316): older tool
results become stubs the model can `recall`, and an older span may be
summarised. Your messages are never summarised; inside a long turn the cut
falls between rounds, and your message stays as you wrote it. A step of
the turn says what was done, or **refused** when no cut could bring the
request under, in which case it went out as it was.

## The views beside the transcript

Until something earns room, the transcript and the composer have the
window. A column opens beside them when the model first writes the document
or a decision waits in it, and when you open a view from **View** or from
the bar's words; a view opened while another is open joins it as a tab.
A view closes from the **×** on its tab or by unchecking it in **View**, and
the column goes when its last view does. A decision that arrives later
brings the document forward again. The transcript, the composer and the
views are tabs of one dock: the dividers drag, a tab can be dragged beside
another or below it, and the window keeps that layout while it is open. The
transcript and the composer have no tab of their own while each fills its
pane alone, and cannot be closed; a view dragged in beside one of them
brings the tabs back. A view you close and open again comes back in the
column, or beside the view you dragged it to.

**Conversations** lists the conversations other chat windows hold and the
runs this host kept, which a click resumes (ADR-0308); a star and a count on
the bar point at a highlight in another conversation. **Analytics** is part
of Advanced (`BOXER_CHAT_ADVANCED`): the conversation's agent surface and its
token and answer statistics, with **Open in play** on them. Analytics and
**Stops** are for whoever builds the harness.

## Decisions

Everything the window asks you to settle with a button is drawn the same
way (ADR-0404 §SD3): what is asked, what it is about, one sentence per answer
saying what it does, any note that the question is out of date, then the
answers, and after them what supports the answer — a diff, a preview. When
the model or the harness asks — a proposed change to the document, a
screenshot it wants to see, a stop that fired, a question with a form — no
answer is filled: each has the same weight. When you started the act and
are asked to go through with it — an export — the answer that goes ahead is
filled. An answer that cannot be given stays, greyed, with the reason under
it.

## The composer

The composer is a text input with the hint "Message the model — Ctrl+Enter
sends" and **Send** beside it; while a turn runs **Cancel** takes Send's
place and Escape in the input does the same, telling the host to stop the
call. What you type is coloured as markdown, since the transcript draws
every message as markdown. A status line above the input shows an action's
outcome — a copy, an open — and a warning when the context is nearly full.
While you edit a message you took back, a strip says so, with **Cancel
edit**.

Where the host registers one editor app, such as a terminal, **Edit in**
`<editor>` above the input hands the draft to it (ADR-0365). Each save there
replaces the draft while it still holds what was handed over; if you typed
here meanwhile, the save waits until you take it or keep yours. Sending the
message ends the edit.

Before the first message the transcript says what the window talks to,
`Chat with <model>` and whether it runs on this machine, and offers the four
choices the first message fixes: **Apps**, **Questions**, **Document** and
**Keep**, the same options as in Settings. After **Edit** takes back a
conversation's only turn, it says what that conversation was started with
instead. A host without a model says "No model to talk to" and why.

## The transcript

Your messages and the model's are bubbles; a line beginning `⚙` is what the
model did with a tool — "set sql in window 2 · done", "wrote the document ·
revision 1" — with **Details** listing the turn's steps as they were
recorded, tool names and token counts included; the line's hover gives it as
recorded too. A refused or failed call's line is in the error tone, since it
usually names a setting to change. Under each message: **Copy**, which
copies the message as shown, its references marked with how their checks
came out and its citations under it (a copy that cites a claim that did not
hold is held once, with the reason and **Anyway** under the message); on
your last message **Edit**, which takes the turn back and puts its text in
the composer so that sending replaces the answer; on the last reply
**Regenerate**. A message whose turn got no answer shows why, with
**Retry**, **Edit**, **Copy details** and, when the call reached the host's
record, **Open call in play**. A code block has **Copy**, and SQL has
**Open in play**. While a turn runs, a bubble reads "working… N s" with the
step it is on and what the model is doing, the reasoning as the model writes
it unless Settings hides it, and the answer as it streams. Once the turn
calls a tool, its steps are listed under it; a conversation without tools has
no steps to count.

## A chatlet's window

A chatlet is this window specialised by a committed document, and draws the
same bar and panes. Before the first message it says what the model can and
cannot do, in words taken from its settings. A document can also offer
ready-made next messages under the latest answer, which fill the composer and
send nothing, and can start its conversations watched. **Definition**, from
**View**, shows the document itself. A document that still names `view` opens
as before; the key no longer changes anything.

## A chat inside another app

Another app can draw a chatlet inside its own window (ADR-0368). The pane
works as a chatlet's window does, and the access dialog names the app it sits
in. Its conversations are listed in its own Conversations view, not in the
chat's or the chatlet's. A narrow pane shows the bar, wrapped, then a row
naming the other views — Conversations, Settings, Definition, Document — and
the transcript and composer: a click on a name shows that view in their
place, and **Chat** brings the transcript back. The row ends with the
window's two menus, **View** and **Conversation**; View holds the views the
row does not name. In a window with two panes, Ctrl+Enter sends only from the composer
you are typing in.

## The turn, the journal, and Keep

A **turn** is one message you send and all that answering it takes: with
Apps or Questions on, several model calls and the tool calls between them.
Every model call and tool call of a turn carries the turn's id
(ADR-0277 §SD5), and the failed turn stays in the transcript but is never
resent: the next turn continues from the last reply.

The chat keeps no message list of its own. The conversation's **journal**
(ADR-0308) holds the prompt sections, the tool set, your words, the
model's replies, every intended call and its outcome, each written before
the act leaves; each request to the model is a projection of those entries.
That is what **Conversations** resumes after a crash or a restart,
and what the trail viewer and play read back.

**Keep** governs only whether the message bodies also reach `boxer.facts`
as `llmMessage` rows, to read later in play. It is fixed at the first send,
and the host keeps them only when its `BOXER_LLM_RETAIN` is durable: the
badge reads *keep asked* until the first answer says, then *kept* or *not
kept* with the host's reason, in the state chip's inspector. The journal is
written either way.

## A chatlet

A chatlet is this window made for one scenario from a committed document, with
an entry of its own in the launcher ([ADR-0364](../../../doc/adr/0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)).
It starts with the settings the document names, and those are also the most its
Settings offer: you can lower them and not raise them. Everything else about
what the model may do is as in any chat, so the model still asks and you still
decide. The empty transcript shows the document's greeting and starters; a
starter fills the composer and waits for you to send it. **Definition**, in the
place of Analytics in the View menu, shows the whole document, which is exactly
what the model is told, with the settings it runs under. A chatlet's
conversations are listed in its own Conversations view and not in the plain
chat's. Editing a chatlet's prompt leaves
its earlier conversations read-only.

## Seeding a window

For scenes and demos a new window takes its starting state from the
environment (ADR-0009): `BOXER_CHAT_DRAFT` puts text in the composer,
`BOXER_CHAT_APPS`, `BOXER_CHAT_QUESTIONS`, `BOXER_CHAT_ARTEFACT` and
`BOXER_CHAT_ADVANCED` turn the options on. A chat listed in
`BOXER_AGENT_COORDINATORS` starts with Apps on anyway. On a host a scene
launched, a scripted model answers from a file under `BOXER_LLM_SCRIPT`; the
maintained scenes are
under `apps/chat/scenes`.
