---
type: adr
status: proposed
date: 2026-09-27
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0265: A chat app over retained model calls — session-only, and a coordinator of apps

## Context

[ADR-0264](./0264-retained-model-conversations-on-facts.md) lets an app
ask the host to keep its model conversations on `boxer.facts`, and gives
it no verb to read them back: the kept text is a record, read with SQL,
and removable by one statement to check that no app relies on it
(§SD6). Nothing sends retained requests yet.

What chat products offer, and which of it touches the storage, is
surveyed in
[llm-chat-app-requirements-survey](../adr-background-work/llm-chat-app-requirements-survey.md);
the verdicts below follow its v1/defer split.

The parts a chat needs exist separately: `llm.complete` takes a
multi-turn history and returns one answer
([ADR-0254](./0254-model-inference-as-a-keelson-capability.md));
`widgets/chatview`
([ADR-0239](./0239-play-chat-panel-and-chatview-widget.md)) draws a
transcript from a model rebuilt every frame and lets the host draw a
message body itself; `widgets/markdown` renders one; `bgjob` and
`jobprogress` run a request off the render goroutine with a cancel.
What does not exist is a composer, a turn loop, and an app that holds
the two grants.

## Decision

We will build a standalone app, named chat, that holds a conversation
in memory for the length of its window session, sends its turns on
`llm.retain.complete` unless the user turned keeping off for that
conversation, and reads nothing back. With Apps on, a turn is a tool loop
over the host's `runtime.agent` services, and the app is the conversation's
coordinator under ADR-0269 (SD6). With Questions on, the model may ask the
person structured questions through a tool of the app's own (SD7).

### SD1 — A standalone app, not a play pane

The app declares `llm.ClientCaps` and `llm.RetainCaps` and nothing else.
Play stays what it is for conversations: the SQL viewer of what was
kept, through its Chat pane over a query on the `llmMessage` kind.
Putting the chat in play would give play the retain grant and a second
reason to exist beside running SQL; the manifest would no longer say
which of play's gestures keeps text.

### SD2 — The conversation is the app's, for one session

The history the app resends each turn is its state; a new window starts
an empty conversation with a fresh id. The app never reads the
`llmMessage` rows, so it passes the ADR-0264 ditch check by
construction — ditch every row and it behaves the same. Past
conversations are read in play. Resuming one in the app is the
deferred step, and would make the reliance deliberate (SD5).

### SD3 — The turn loop

A turn appends the user's message to the history and sends the whole
history with the conversation id and the previous reply's call id as
parent, as a background job. Its reply is appended as the assistant
message the next turn echoes, so the service's prefix check (ADR-0264
§SD3) holds and each turn keeps only what is new. One turn runs at a
time; the composer stays editable while it does, so the next message can
be written, and sends once the answer is in. Cancel — or Escape in the
composer — ends the wait and
stops the provider call (`llm.cancel`, ADR-0254's 2026-09-27 update); a
kept turn keeps its messages with the call's error, as a refused one
does (ADR-0264 §SD4). A failed, refused or cancelled turn stays in the
transcript as a failed bubble with the reason, and the next turn
continues from the last successful reply.

**The last turn can be taken back.** Retry sends a failed last turn
again; a failed turn never reached the history, so only the transcript
changes. Regenerate and Edit take back the last *answered* turn: the
history and the parent return to what they were before it, so the next
request is the one that turn sent, naming the same parent — a branch,
which the service keeps beside the first (ADR-0264 §SD3). Regenerate
sends the message again at once; Edit puts it in the composer and marks
the resend edited, and Cancel edit puts the turn back while nothing was
sent since. Only the last turn: an earlier one is in the history every
later turn resent, and taking it back would drop those turns from the
transcript. The transcript shows the branch it is on and no other; a
kept conversation holds both in its rows.

The whole answer arrives at once. Streaming is deferred with ADR-0254
§SD7; until it exists a pending turn is a placeholder bubble with a
spinner and the elapsed time.

The app never shortens the history it sends. A conversation that
outgrows the model's context fails with the provider's error, shown on
the failed bubble with New conversation as the way on. The capability
already accepts a declared window (ADR-0264 §SD3), so a later version
can send one without changing what is stored; choosing the window —
how much to drop, whether to keep the system message — is a decision of
its own (SD5).

### SD4 — What the app shows

- **The transcript** is chatview over the in-memory history, every
  message body — the user's as well as the model's — rendered as
  markdown, with the message's actions under it: Copy on every message,
  Retry and Edit on a failed last turn, Edit on an answered one, and
  Regenerate on the last answer (SD3). A reply's code blocks carry Copy,
  and an `sql` block Open in play, which opens a play window with the
  SQL in its editor, not run: the SQL is the model's, and the person runs
  it. A turn that lands is shown from its start — the question at the
  top of the view, then the answer — rather than at the end of a long
  answer. Before the first send the transcript says what the window talks
  to, whether it keeps, and how to send.
- **A failure is inspectable.** Under the reason, Details shows the
  failure's class, the call's id in the host's call record, when, how
  long the call took, and the error's whole text, selectable; Copy
  details puts them on the clipboard, and Open call in play opens the
  call's row of `keelson('llm_calls')` (ADR-0254, 2026-10-03 update). A
  failure that never reached the service has no call id and no row.
- **What an action did.** Copy and Open run off the render thread; the
  line above the composer says what each did, and a failed one says why
  and stays until dismissed.
- **The composer** is a multi-line text input highlighted as markdown
  with mdedit's lexer (which colours the text's own bytes, where the
  canonicalising highlighter would colour a rewrite), and a Send button
  beside it;
  Ctrl+Enter sends, gated the way play's SQL editor gates Run: the chord
  is read for the whole process and acted on only by the instance whose
  window is focused (`app.WindowFocusI`), so two open chats do not both
  send. A focused text input does not compete for the chord.
- **The model and the host** from `llm.describe`, in the bar, as
  ADR-0254 §SD1 asks; with no model configured the transcript gives the
  reason and there is no composer.
- **Whether a turn was kept.** A reply whose verdict is not kept shows
  the reason until dismissed, once per conversation, so a deployment
  below `durable` is visible rather than silent; the Keep badge's hover
  keeps it after.
- **Keep this conversation**, a toggle beside New conversation, on by
  default. Off, the conversation's turns go on `llm.complete` and only
  the counts are recorded — the one "nothing kept" choice a user has,
  the temporary chat of the products surveyed. It is fixed at the first
  send, so a conversation is kept whole or not at all.
- **The context used**: the last call's input and output tokens against
  the model's context size, which `llm.describe` reports when the host
  knows it (ADR-0254, 2026-10-03 update) — a meter, and from 80 % a line
  above the composer that says the next answer may not fit and New
  conversation is the way on (SD3). Without a size it is a count.
- **The title.** A conversation is called by the first line of its first
  message until the first answer lands; the app then asks the model once
  for a title of a few words — on `llm.complete`, never kept, under the
  sensitivity the conversation holds — and a person's rename wins over
  both. The title lives with the window, like the conversation; keeping
  it is decided with resume (SD5). Its hover says where it came from, and
  why the model's did not land when it did not.
- **Open in mdedit** hands the conversation to a new mdedit window as a
  markdown document (ADR-0178, 2026-10-03 update): the title, the model,
  when it started, whether it was kept, its id, then every message under a
  heading of its own, the tool calls as a list and a failure as a callout
  with its details. It is the branch the window shows. mdedit holds it
  unsaved, apart from the document it keeps.
- **New conversation** clears the transcript and mints a new id.
- **Where past conversations are.** The app says, once, that a closed
  conversation is read in play, since a user expects history to survive
  the window and this one does not — only for a conversation that is
  being kept.

### SD5 — Deferred, recorded

- **Resume and a conversation list**: a host table over the
  `llmMessage` kind, read under a `keelson.query` grant
  ([ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md))
  and scoped to the caller — the `app_logs` shape.
- **Tools beyond the coordinator's** (SD6): a capture the model views as
  an image, the operations of apps that serve no catalog.
- **Streaming** (ADR-0264 §SD7), and **keeping a title** — decided with
  resume, which is what would read it.
- **Edit and regenerate of an earlier turn**, and **the branches side by
  side** — a regenerated answer's earlier versions, an edited message's
  earlier text. SD3 takes back the last turn only, and shows one branch.
- **A context window** (SD3): what to drop when the conversation
  outgrows the model, sent as a declared omission.
- **A system prompt** of the person's. The only one is the coordinator's
  (SD6), fixed before the first send of a conversation with Apps on, since
  changing it later rewrites ordinal 0 and costs a fully kept turn
  (ADR-0264 §SD3).
- **Deleting a conversation.** Every hosted product surveyed offers it;
  here kept text stays until removed by hand (ADR-0264 §SD6/§SD7). The
  delete is its own decision, taken with ADR-0264's purge policy.
- **Branching into a new conversation**: a parent from another
  conversation. Decided with resume.
- **Sensitivity**: typed text is `ordinary`. A chat that pastes query
  results would have to carry their label, and this one has no way to
  paste them.

### SD6 — The coordinator (ADR-0269)

With Apps on, the model may work in windows the person shares, and only
through the host:

- **Registration.** The person registers the app as a coordinator
  (`BOXER_AGENT_COORDINATORS`); the manifest's `runtime.agent` grant alone
  lets it ask for nothing. A window of a registered chat starts with Apps
  on, since the registration is the intent Apps serves; the person can
  untick it before the first send. A conversation started without Apps is
  marked "no apps" in the bar — its model has no tools and says so when
  asked to open a window — and the empty window says the same before the
  first send.
- **Fixed tools.** `request_access`, `list_windows`, `describe_app`,
  `call_operation`, `open_window` and `stop_task`. Operation schemas load on demand
  through `describe_app`; a call's key is the model's tool-call id.
- **A turn.** Before the first model call the app asks the host for the
  changes others made since the previous turn (`runtime.agent.turn`) and
  puts them before the person's message; then it alternates model calls
  and tool calls, at most 24 rounds, and the transcript shows each tool
  call as a system line — also when the turn stops without an answer, which,
  unanswered, is not resent.
- **What the model reads.** Content an app marks untrusted, window titles,
  and every capture arrive between `<<untrusted …>>` delimiters with their
  source, and the fixed system prompt says such content is data. A
  confined result the host would not hand a remote model arrives as a data
  handle. Every model call declares the highest label the conversation
  holds.
- **One task per conversation.** The grant names the conversation; a new
  conversation stops the task, and the app's row shows the task, whether it
  read untrusted content and whether it holds confined content, with Stop.

### SD7 — Questions: the model asks the person with a form

With Questions on, the model is offered `ask_user`: one to four questions,
each with an optional short header, two to six options with an optional
one-line description, and whether several options may be chosen. The app
draws them as a form in the pending bubble — radio buttons for a single
choice, check boxes for several — with a note field under each chosen
option, a line per question for an answer of the person's own, and Answer
and Skip. The shape follows the question tool of Claude Code.

- **No grant.** The tool reaches nothing outside the chat window: what it
  shows is the model's text, and what it returns is what the person
  typed. It is not a `runtime.agent` call and needs no registration.
- **Its own toggle**, beside Apps, fixed at the first send with Keep and
  Apps for the same reason: the system prompt is ordinal 0 (SD5). Either
  toggle makes the turn the tool loop of SD6; with Questions alone,
  `ask_user` is the only tool and the window tools answer that there is
  no such tool. A separate toggle keeps a conversation with neither a plain
  completion, which an endpoint without tool calls still serves.
- **The call waits inside the turn.** The tool call blocks until the person
  answers or skips; the answer, as JSON, is the call's result, and the same
  turn continues. Cancel and New conversation withdraw the question with
  the turn. Answer is refused until every question has a chosen option or
  an answer of the person's own; Skip tells the model to go on and say what
  it assumed.
- **What stays.** The transcript's tool line for the call names each
  question by its header and what was chosen, with the notes. The
  questions and answers are tool messages in the history like any other.

## Alternatives

- **A question that ends the turn** (SD7), its answer starting the next.
  Rejected: the history would hold a turn that begins with a tool message
  rather than the person's, and text typed in the composer while a question
  is open would need its own meaning. A blocking call is what
  `request_access` already does with the host's dialog.
- **`ask_user` always offered, or only with Apps** (SD7). Rejected: always
  would send `tools` on every turn and fail every turn on an endpoint
  without tool calls; only with Apps would tie a tool that needs no grant
  to the one that does.
- **A play pane** (SD1). Rejected for the grant it would add to play.
- **Resume in v1** (SD2). Rejected for now: it needs a read surface
  ADR-0264 chose not to add to the capability, and a session-only chat
  shows the storage working without depending on it.
- **Tools in v1.** Rejected for now: the loop is play's, and lifting it
  is its own change.

## Consequences

### Positive

- The first consumer of ADR-0264 exercises the whole write path, and
  play already reads what it writes.
- The app holds two grants and no read access, which is easy to review.

### Negative

- Closing the window ends the conversation in the app; it survives only
  as rows to read in play.
- Without streaming, a long answer from a local model is a long wait
  behind a spinner.
- A long conversation ends in an error rather than degrading, until a
  window exists.
- A cancelled turn is kept like a failed one and has no successor. The
  call row's error says it was cancelled, which is what tells it from a
  regenerated answer the user did not follow; a reader that looks only
  at the message rows cannot.

### Neutral

- Nothing in the host changes; the app is a consumer.

## Verification plan — Tier 1

- **Lane: default `go test`.** The turn loop against the llm service
  over a fake provider and an in-process bus: a second turn names the
  first reply's call id as parent and resends it verbatim; a failed
  turn is not a parent; New conversation mints a new id; a conversation
  with Keep off sends nothing on `llm.retain.*`. A taken-back turn's
  next request is the one it sent, from the same parent; Cancel edit puts
  it back; a failure carries its kind and call id.
- **Lane: scene.** The scenes under `apps/chat/scenes` answer from the
  scripted model (`BOXER_LLM_SCRIPT`, ADR-0269 M6): a turn sent, its SQL
  opened in play, the answer regenerated, the turn edited and put back;
  and, against an endpoint that refuses the connection, a failure's
  details copied and its call record opened in play; the model's title
  replacing the first line, a stated context of two tokens filled, and
  the conversation opened in mdedit.
- **What would fail.** A reply appended differently from how it came
  back makes every turn keep its whole history; the fake-provider test
  pins it by checking `RetainedFrom` on the second call.

## Status

Proposed — awaiting review by the code owner. Revised in place on
2026-10-01 for ADR-0269's M5: the coordinator of SD6, built and tested
against a scripted model over the host's services. Revised in place on 2026-10-03: the last turn
taken back (SD3), message actions, inspectable failures and the layout of
SD4, built and scene-run; and the title, the context meter and Open in
mdedit of SD4.

Built 2026-09-27, in the working tree, as the chat app beside the other
apps. The default lane passes: the turn loop against the llm service over
an in-process bus, and — over clickhouse-local with the ceiling durable —
a second turn keeping only its new messages. A headless run against a
stub OpenAI-compatible endpoint on loopback sent a turn with Ctrl+Enter,
showed the pending bubble and Cancel, drew the markdown reply, the
context count and the not-kept reason under `ring`; it is the first run
of the ADR-0254 capability against an endpoint. It is not a maintained
scene, since the scene runner has no model stub to start. Two additions
to the text above:

- `BOXER_CHAT_DRAFT` seeds the composer, because the driver cannot focus
  an empty unnamed text input — play's `BOXER_PLAY_SQL` shape.
- The Keep badge shows what the host did: a keep the host declined reads
  "not kept", beside the reason, and one with no verdict yet "keep asked".
- While a turn runs, Cancel takes Send's place — a button cannot be
  disabled here.

Revised in place on 2026-10-04 for SD7, built and tested against a scripted
model: the form's validation and reply, and a turn that waits for the form,
is skipped, or is cancelled while it waits.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-02 — refusals the model can act on, and typed operation tools

The agent-operations-play trial (GLM-4.6 and GPT-6.1 Sol, two tasks in
play) found every examined failure at a refusal that said what was wrong
but not what to do: a `call` with no `args`, a run whose destination the
grant lacked, a grant asking for nothing. The coordinator changed:

- **`call` is `call_operation`,** and it refuses keys beside `window` and
  `operation` by name instead of dropping them; a refusal of a call with no
  `args` says where arguments go.
- **A refusal carries `next`:** the tool to call and its arguments —
  `request_access` with the destinations a run needs, or the operation's
  argument schema. A grant request naming nothing to open says to name the
  app.
- **A call identical to one refused since the last call that was not** is
  answered without being made again; GLM-4.6 had repeated one refused call
  eighteen times.
- **`describe_app` returns schemas as JSON objects,** not text holding
  JSON.
- **Operation tools** (`BOXER_CHAT_OPERATION_TOOLS`, off by default): each
  operation of a window in the task is also a typed tool,
  `w<window>_<operation>`, beside the fixed tools. It is a trial arm, not a
  default: the tool list then changes from call to call.
- **`read_help`** reads the apps' help over `runtime.agent.help`
  (ADR-0269, update of this date): a search across the apps, an app's
  documents, or one section; `describe_app` marks the apps that have help,
  and the prompt says so. It is the apps' documentation, so it is not
  delimited as untrusted and does not taint the conversation.

### 2026-10-02 — statistics, and their handover to play

A **Statistics** panel beside the transcript shows the window's token and
answer statistics across its conversations: the turns, how many were
answered, the model calls and their tokens, and empirical distributions —
the `ecdf` widget, with its confidence band once it is computed — of the
answer time per turn and the input and output tokens per call. A
distribution needs two values with some spread; until then the panel says
so. Records live as long as the window and are kept on the render thread.

**Open in play** publishes them on demand as two ad-hoc datasets
(ADR-0240), `chat_turns` (one row per turn: outcome, rounds, tool calls,
tokens, wall time) and `chat_calls` (one row per model call), and opens a
play window on the turns. Every chat window publishes under those two
aliases, and a play window follows the newest. `BOXER_CHAT_ADVANCED=false`
hides the panel and the button; the manifest still declares the publish
and open grants, which are then unused.

### 2026-10-02 — a turn ends with an answer; play's reference in the prompt

A turn that used every round ended with no answer and a failure line: in
one observed turn 24 rounds of tool calls, 98k input tokens and 268 seconds
left nothing to read. The last round now offers no tools (`tool_choice`
`none`) and the host asks for the answer with what was found and what is
open; the note goes to that call only and is not resent. The prompt names
the bound, and the waiting line shows the round, "round 7 of 24", so a long
turn reads as working.

The prompt also carries the reference an agent needs before it writes SQL
in play: `keelson('<table>')` reads the host's own tables, with
`keelson('tables')` and `keelson('columns')` to discover them, and which
destination a run needs; and that play's `list_snippets`, `read_snippet`
and `list_functions` (ADR-0270, update of this date) hold worked queries and
the functions a query may call.

### 2026-10-02 — titled calls, and a task that ended

Every tool takes an optional `title`, a few words the person reads while
the call runs ("round 3 of 24 · Reading play's buffer") and in the
transcript after it. The coordinator strips it before dispatching, so it
reaches no app and does not make two calls differ for the repeat check; an
operation with a `title` argument of its own keeps it and its calls go
untitled.

A conversation outlives its task: when a call answers that the task ended
or its handle is unknown (ADR-0269 `TaskGone`), the coordinator forgets the
grant and the next `request_access` asks for a new task; a late task is
extended by `request_access` instead (ADR-0269, update of this date).

## References

- [ADR-0264](./0264-retained-model-conversations-on-facts.md) — what the app sends and why it reads nothing back.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — the contract the coordinator of SD6 drives apps under.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — the capability, its timeout, and `llm.cancel`.
- [ADR-0239](./0239-play-chat-panel-and-chatview-widget.md) — the transcript widget and play's pane as the reader.
- [LLM chat app requirements survey](../adr-background-work/llm-chat-app-requirements-survey.md) — the requirements, and which touch the storage model.
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) — the grant a resume would read under.
