---
type: explanation
audience: end-user
status: draft
title: The chat window
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The chat window

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
[artefact](artefact.md) is the conversation's document.

## The bar

The first row is **New conversation**, the scale of what the model may do
(two markers, *may* and *now*, ADR-0280) and the **Turn** chip, which
names the turn's state and opens its graph.

The second row is what the conversation is: its **id** — the key every
record of this chat carries, in `keelson('llm_calls')` and
`keelson('agent_actions')` under `conversation` and on the audit trail —
with a copy button and one that opens the conversation's trail rows in
play; then, once the first message is sent, badges for what the
conversation was started with: **keep asked** until the first answer says
**kept** or **not kept**, **apps** or **no apps**, **questions**,
**artefact**; the context meter, tokens the last answered call used; and
the model line, `→ <model> · <endpoint host>`, with `· trusted` when the
host trusts that endpoint with sealed data.

While the model holds a task, a third row names it (`task …`) with
**Stop task**, and badges when the conversation has **read untrusted
content** or **holds confined content**. Under the rows, at most one
notice: why the conversation is not kept, or that closing the window ends
it here and it is read in play.

## The dock

The body is a dock of tabs: **Transcript** and **Message** (the composer)
on the left, and a column of **Conversations**, **Settings**, **Analytics**
and **Artefact**. Every divider drags. A tab whose feature is off stays and
says how to turn it on. **Conversations** lists the conversations other
chat windows hold and the runs this host kept, which a click resumes
(ADR-0308). **Analytics** is part of Advanced (`BOXER_CHAT_ADVANCED`): the
conversation's agent surface and its token and answer statistics, with
**Open in play** on them.

## The composer

The composer is a text input with the hint "Message the model — Ctrl+Enter
sends" and **Send** beside it; while a turn runs **Cancel** takes Send's
place and Escape in the input does the same, telling the host to stop the
call. What you type is coloured as markdown, since the transcript draws
every message as markdown. A status line above the input shows an action's
outcome — a copy, an open — and a warning when the context is nearly full.
While you edit a message you took back, a strip says so, with **Cancel
edit**.

Before the first message the transcript says what the window talks to and
keeps: `Chat with <model>`, its endpoint, whether the conversation is kept
on `boxer.facts`, whether Apps and Questions are on, and the keys. A host
without a model says "No model to talk to" and why.

## The transcript

Your messages and the model's are bubbles; a line beginning `⚙` is the
coordinator's activity — a tool call and how it ended — with **Details**
listing the turn's steps. Under each message: **Copy**; on your last
message **Edit**, which takes the turn back and puts its text in the
composer so that sending replaces the answer; on the last reply
**Regenerate**. A message whose turn got no answer shows why, with
**Retry**, **Edit**, **Copy details** and, when the call reached the host's
record, **Open call in play**. A code block has **Copy**, and SQL has
**Open in play**. While a turn runs, a bubble reads "thinking… N s" with the
round and the step, the reasoning as the model writes it unless Settings
hides it, and the answer as it streams.

## A chatlet's window

A chatlet is this window specialised by a committed document. Before the
first message it says what the model can and cannot do, in words taken from
its settings. If its document asks for the simple view, the bar shows only
**New conversation**, the title and **Details**, which unfolds the rest; the
task, **Stop task** and the warnings stay in view. A document can also offer
ready-made next messages under the latest answer, which fill the composer and
send nothing, and can start its conversations watched. **Artefact** comes to
the front when the model first writes it. The running turn counts steps, with
no limit beside the count. **Definition** shows the document itself.

The plain chat can look the same way: **Simple view** in Settings folds its
bar, brings **Artefact** forward and counts steps, and is remembered for the
next window.

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
That is what the **Conversations** tab resumes after a crash or a restart,
and what the trail viewer and play read back.

**Keep** governs only whether the message bodies also reach `boxer.facts`
as `llmMessage` rows, to read later in play. It is fixed at the first send,
and the host keeps them only when its `BOXER_LLM_RETAIN` is durable: the
badge reads *keep asked* until the first answer says, then *kept* or *not
kept* with the host's reason. The journal is written either way.

## A chatlet

A chatlet is this window made for one scenario from a committed document, with
an entry of its own in the launcher ([ADR-0364](../../../doc/adr/0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)).
It starts with the settings the document names, and those are also the most its
Settings tab offers: you can lower them and not raise them. Everything else about
what the model may do is as in any chat, so the model still asks and you still
decide. The empty transcript shows the document's greeting and starters; a
starter fills the composer and waits for you to send it. The **Definition** tab,
in the place of Analytics, shows the whole document, which is exactly what the model is told, with the
settings it runs under. A chatlet's conversations are listed in its own
Conversations tab and not in the plain chat's. Editing a chatlet's prompt leaves
its earlier conversations read-only.

## Seeding a window

For scenes and demos a new window takes its starting state from the
environment (ADR-0009): `BOXER_CHAT_DRAFT` puts text in the composer,
`BOXER_CHAT_APPS`, `BOXER_CHAT_QUESTIONS`, `BOXER_CHAT_ARTEFACT` and
`BOXER_CHAT_ADVANCED` turn the options on. A chat listed in
`BOXER_AGENT_COORDINATORS` starts with Apps on anyway. A scripted model
answers from a file under `BOXER_LLM_SCRIPT`; the maintained scenes are
under `apps/chat/scenes`.
