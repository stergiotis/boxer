---
type: reference
audience: end-user
status: draft
title: Stops
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Stops

A stop is a condition you, or the chatlet you opened, put on a conversation:
a time box, a size the artefact may reach, a phrase that marks the work as
done, a tool call you want a look at first (ADR-0370, proposed). When it
holds, the stop holds the turn, ends it, ends the conversation or closes the
window. The **Stops** tab lists the stops in force, takes one of your own, and
shows what fired.

A stop is not a safeguard. The model is not shown the stops, but a model that
means to avoid a pattern can. What the model may do is decided in
[Settings](settings.md), by the ceiling and by what you grant.

## A stop is a query

Every stop is one `SELECT` over the conversation's own record. No rows means
it is quiet. Any row means it fired, and the rows are what you are shown as
the reason.

The tab shows the query each stop runs. For a stop of your own it is an SQL
editor: change the statement there and press **Apply**. Until then the
statement in force is the one before your edit, and a statement that is not
admitted cannot be applied; the tab says why while you type. **Revert** puts
the statement in force back. An edited stop keeps its name and what it does,
and is from then on its statement, whatever kind you declared it as. A
chatlet's statement is shown and cannot be edited. In the Simple view the
statement is folded text under **Statement**.

The named kinds are spellings of such a query:

| Kind | Declared as | Fires on |
| --- | --- | --- |
| Time | `after: 15m` | each such period of the turn's working time |
| Artefact size | `artefact_bytes: 200000` | each time the artefact has grown by that much |
| Artefact text | `artefact_match: '(?m)^## Conclusion'` | each match of the pattern in the artefact |
| Reply or result | `journal_match: 'DONE'` | each reply or tool result of the turn that matches; add `in: [user]` to read your own messages too |
| Tool call | `tool_match: '^query$'`, optionally `arguments_match: '(?i)\bdrop\b'` | each tool call of the turn that matches, before it runs |
| SQL | `sql: "SELECT …"` | whatever the statement returns |

Working time leaves out the time the turn waited on you. Patterns are RE2,
as the query engine reads them; in it `.` also matches a line break.

## The four relations

A statement reads these and nothing else:

| Relation | Columns |
| --- | --- |
| `journal` | the run's entries, in the columns of `keelson('agent_run_entries')`: `ordinal`, `at_ms`, `kind`, `turn`, `name`, `body`, `calls`, `arguments`, `effect`, `form` and the rest |
| `toolcalls` | one row per tool call a reply asked for, whether or not it ran: `ordinal` and `turn` of the reply, `position`, `name`, `arguments` |
| `artefact` | one row: `text`, `bytes`, `lines`, `revision` |
| `clock` | one row: `turn`, `round`, `working_ms`, `turn_ms`, `now_ms` |

It is one statement, it only reads, and it names no other table. Wall-clock
and random functions are refused: read `clock` for time. A statement that
returns more than 256 rows is an error.

```sql
-- the turn has taken twenty model replies, and each ten after that
SELECT turn, intDiv(count(), 10) AS tens
FROM journal
WHERE kind = 'reply' AND turn = (SELECT turn FROM clock)
GROUP BY turn
HAVING tens >= 2
```

The relation carries no token counts: a reply's `body` is its text. A budget
in tokens or cost is the host's ration, not a stop.

## What "fired" means

A stop fires when it returns a row you have not acknowledged. You
acknowledge a row by answering **Continue** on a held turn, or, after a stop
ended a turn, by sending your next message. So a time box that fired at 15
minutes fires again at 30, not at the next check, and a size stop that you
continued past is quiet until the next step.

Write your own statement the same way: return rows that change when there is
something new, and include `turn` among the columns when the rows belong to
one turn. A statement whose rows differ at every check fires at every check.

## What a stop does

| `then` | Effect |
| --- | --- |
| `pause` | holds the turn; the form in the transcript offers Continue and Stop the turn |
| `ask` | holds the turn and puts the same question in the host's queue |
| `stop` | ends the turn as stopped; the conversation stays open |
| `end` | ends the conversation; the window stays and shows the record |
| `close` | ends the conversation and closes the window; in a pane inside another app's window it acts as `end` |

When several fire together the strongest acts. A held turn keeps what it has
done, and Continue goes on from there. A stopped turn is not sent to the model
again, as any turn without an answer.

A stop is checked before each request to the model and after each reply,
before the reply's tool calls run. Nothing is checked while a call is in
flight, so a time box is overshot by at most one model call or one tool call.
With the turn's answer there is nothing left to hold or stop: only `end` and
`close` act there, and the others are noted in the tab.

A stop that cannot be checked stops the turn and says why. That includes a
host that runs no local SQL worker.

## Whose stops

A chatlet's stops are fixed: the tab shows them as "fixed by" the chatlet and
offers no Remove. Stops you add are yours, kept for the window across its
conversations, and in force from the next turn. A resumed conversation brings
back the stops it last ran under.

## Trying a stop on a past conversation

The `journal` relation has the columns of `keelson('agent_run_entries')`, so
a statement can be tried in play against a run that already happened: name
that run's rows `journal` in a `WITH` clause and put the statement after it.
`toolcalls` has no such twin; the calls that ran are the run's `intent`
entries.
