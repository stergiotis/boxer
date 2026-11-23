---
name: keelson-ux
description: "Use when deciding what a window shows and how a person operates it — before the first widget of a new app, when adding a control, a tab, a pane or a message to an existing one, and when reviewing a screen. Do and don't pairs for three layers: who the app is for and what job it does, the order of work on the screen, and which control a thing should be instead of a push button; then the words on it, the use of space, and how to check the result with a cold read, a scene and the layout checks. Each rule names the imzero2 symbol to use. Applies to apps on the keelson runtime here and in repositories that consume it. For the app's shape (data, state, work, operations) see keelson-app; for how a widget is drawn see imzero2."
type: reference
audience: contributor or agent designing, writing or reviewing what a person sees in an app window
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# What the person sees and does — do and don't

This page is a set of rules for the surface of an app: its reader, its
order, its controls, its words and its space. It does not replace the
[keelson-app skill](../keelson-app/SKILL.md), which decides the app's shape
(data, state, work, operations, look), nor the
[imzero2 skill](../imzero2/SKILL.md), which says how a frame is built. Where
this page and keelson-app meet (§7 there: button kinds, verbs, empty states,
the fold, the status row), keelson-app is the rule and this page says how to
apply it. Where either disagrees with an ADR, the ADR wins; the decisions
this page rests on are in
[ADR-0404](../../adr/0404-what-a-person-reads-in-a-window-a-stated-reader-one-way-to-draw-a-decision-and-the-chat-window-as-the-first-case.md).

The rules come from general interaction practice, cut down to what a survey
of scene captures on 2026-11-22 showed this toolkit's apps get wrong and
right. That survey read captures, not people at work; no rule here has been
tested with a person of the role it names. Thresholds ("about seven") are
prompts to regroup, not limits.

Rules are numbered by section so a review can cite one (`U2`, `C4`). The
sections run in the order the decisions are made, and the order is the
first rule: **reader, then job, then screen, then controls.** A control
chosen before the job is known is usually a button.

## 0. The three layers

| Layer | The question | What goes wrong without an answer |
| --- | --- | --- |
| Reader (§1) | Who is this for, and what do they already know? | The builder becomes the reader by default: identifiers as content, diagnostics in the first row, remedies only the person who launched the process can carry out |
| Job (§2) | What did they come to do, and what is the first step? | The screen is organised by mechanism: a tab per view type, a pane per subsystem, a toolbar per feature, and the person supplies the order |
| Control (§3) | What kind of thing is this: a command, a choice, a setting, a selection, a place, a gesture? | Everything is a push button of the same weight |

A fault at a lower layer is often an unanswered question one layer up. A
toolbar of twelve commands is rarely fixed by restyling the twelve.

## 1. Reader

**U1 — Write the brief before the first widget.** One sentence: *for* a
role, *who needs to* do a job, *when* something triggers it, *knowing*
this, *not knowing* that. It goes in the first paragraph of the help
book's `overview.md`, and `Manifest.Summary` is its verb-first short form
(keelson-app S3).

- Do: "For someone moving tables between two ClickHouse servers, who needs
  to see what a sync would change before anything is written. Knows SQL
  and both servers; has not read the sync tool's source."
- Don't: "for developers, analysts and agents". A brief that excludes
  nobody decides nothing.
- Don't: a summary that names the mechanism in place of the job ("run
  DDL and INSERT SELECT over HTTP" where the job is "bring a second
  server in line with the first").

**U2 — Pick one primary reader; the builder is a legitimate one.** Three
kinds of reader recur: the builder of the toolkit or the app, the engineer
who is fluent in the data and its query language, and the person of a
domain (a steward, an auditor, a writer). They differ in vocabulary more
than in skill. Choose one as primary and say so; the others are served by
the fold (keelson-app L5), the tethered inspector, the playground and the
operations catalog.

- Do: mark an app whose reader is the builder as that (`Kind` demo, or the
  brief), and then write it in the builder's words without apology.
- Don't: ship a domain reader's app with the builder's words because the
  builder wrote it.
- Don't: answer two readers with a setting that swaps the window between
  a plain and a full rendering. That is two windows to keep working, and
  the person who has just arrived is the one asked to choose (ADR-0404
  §SD2).

**U3 — Every visible string is a word the primary reader uses.** Run ids,
record keys, digests, type codes, package paths, environment-variable
names and ADR numbers are the builder's vocabulary. For any other reader
they belong one step away: a tooltip (`c.HoverText`), a tethered
inspector, a *Details* fold, or a copy affordance beside a human name.

- Do: show a conversation by its title and time; keep its id behind a copy
  icon.
- Do: keep typed column headers (`n u64`) where the reader is the
  engineer; they are that reader's words.
- Don't: print a placeholder identifier (`<not-a-tagged-id>`) as the first
  line of a pane.
- Don't: cite an ADR in window text outside a builder's app. The help book
  is where a decision is cited.

**U4 — A remedy is something this reader can do from here.** A refusal
names its remedy (keelson-app A2); the remedy has to be within the
primary reader's reach.

- Do: "No server answers at this address. Check the address above, or ask
  whoever runs the server whether it is up."
- Do, for a builder's or engineer's app: name the environment variable.
- Don't: give a domain reader a variable name or a shell command as the
  only way forward. Say what is missing in their words and put the
  variable in the help book.

**U5 — The window's text is for the person; the agent has its own
surface.** An agent reads the catalog, the phase and the help book
(keelson-app §5). Text drawn in the window explains the task to the
person, not the implementation to a maintainer or the contract to a model.

- Don't: describe goroutines, gates, estimators' internals or frame
  timing in a pane's body. Method caveats that change how a figure is
  read go in a tooltip (keelson-app A5, F3); the rest is help.

**U6 — Builder instruments stay out of the reader's first row.** The host
draws a capture row above every window's body and a telemetry status bar
on the desktop; both are the builder's. An app adds no diagnostics of its
own to its first row. Where a deployment's readers are not builders, the
per-window buttons can be switched off (`KEELSON_WINDOW_SVG_BUTTON`,
`KEELSON_WINDOW_PAUSE_BUTTON`); their default and placement are the
host's decision, listed in §8.

## 2. Job

**J1 — Name the top tasks, and open on the first step of the first.**
Write three to five tasks as verb, object, outcome ("compare two
documents and find the closest sections"). The window's first-open state
shows where task one starts, and the first control in reading order is
that step. Whoever opens the window cold should be able to say within a
few seconds what it is for and what to do first (§6, E3).

- Do: a heading that asks the step's question ("Which two servers?").
- Don't: open on a toolbar and four empty panes.

**J2 — The screen runs in the order of the work.** Left to right and top
to bottom is input, then action, then result, then what follows from the
result (keelson-app F6). The action sits after the inputs it uses and
beside the result it produces.

- Do: put the handover, the export and the save next to the result they
  act on.
- Don't: collect every command in one bar at the top because that is
  where bars go.

**J3 — A job with ordered, dependent steps is drawn as steps.** When a
later step is invalid before an earlier one, show the sequence:
`breadcrumbs.Render` with a state per step, one page per step, one
primary action per page, *Back* and *Next* in a fixed place, and a running
"so far". `apps/jackstay` is the worked example (observed 2026-11-22;
there is no stepper widget, §8).

- Do: keep a blocked step visible and say what unblocks it.
- Don't: force steps on exploratory work. A playground is not a wizard.

**J4 — Views answer questions; tabs are for peers.** A tab names a
question or an object the reader asks about. One strip holds what fits at
the window's surface size without clipping; past about seven, group them
or move the rest to a menu. A view that cannot show the current result is
not offered as a peer: list it in a menu with the reason it does not
apply, as play's *Panes* menu does. Two tab levels at most, and one tab
style per level.

- Don't: mark inapplicable tabs with a private sign (a trailing " -") and
  explain the sign somewhere else.
- Don't: let a strip overflow into half-words (`Sche`, `Pr`).

**J5 — The first-open empty state is the introduction.** A pane with
nothing in it says what will be there and names the one action that fills
it; where an example exists, offer it (an *Example* button that loads
one). Which of the empty conditions holds is keelson-app L4 and the
[empty-states pattern](../../design-system/patterns/empty-states.md).

- Do: "Select a job in the list." in an empty detail pane.
- Don't: a selected tab with a blank body, no sentence and no spinner.

**J6 — Ask only what cannot be defaulted, and show what is still
missing.** Start from the common case. Where input is required, count it
and locate it ("1 of 3 required set · name and 1 more not set").

**J7 — One job, one window.** If a task needs three windows arranged by
hand, it is one window with panes, or a workstatelet that opens them
placed ([ADR-0369](../../adr/0369-workstatelets-a-desktops-windows-and-layout-as-a-markdown-document.md)).
Choose the surface size that fits the job
([ADR-0065](../../adr/0065-imzero2-design-system-surface-sizes.md),
`styletokens.Surface*` through `app.SurfaceHints`), and make the whole
first step visible at that size.

**J8 — Say what happened, where it went and what comes next.** After a
command: the outcome in the status row (keelson-app W8, L7), in a
sentence, with the place the result now lives and the next step if there
is one. A dry run before a write, with the write disabled until the dry
run is clean and the reason beside it, is the pattern for anything
consequential.

## 3. Control

**C1 — A push button is for a command.** A command happens once when
pressed and has a name the reader would say aloud. Before `c.Button`, ask
what the thing is:

| If it is | Use | Not |
| --- | --- | --- |
| a choice among two to five exclusive options | `selector.Segmented` (`StyleSegmented`, or `StyleRadio` in a settings list) | a row of buttons with one highlighted |
| a choice among many | `c.ComboBox` | — |
| an on/off setting | `c.Checkbox` | a button whose label flips |
| a filter facet | a selectable chip, `badge.New(…).Selected(on)`; text filters per keelson-app F1 | a button per facet |
| a switch between panes or views | the dock's tabs (`c.DockArea`), at every level of the window; a view the person opens and closes is in the declared layout always, passed to `Tab` only while open, closable from its tab (`Closable`, `CloseRequested`) and a checked item in a *View* menu; a pane alone in its leaf draws no strip (`HideTabBarWhenAlone`) | buttons, or a segmented bar, that swap a body; a pane header of your own |
| picking an item to look at | selection: `recordtable` (`Result.Clicked`), `tree` and `fsbrowser` (`Activated` for Enter and double-click), with the detail beside it | an *Open* button on every row |
| a command on one item among many | select the item, then act from the detail pane or one bar that says what is selected; or `c.ContextMenu` on the row; at most one `ButtonKindGhost` or `ButtonKindDangerGhost` icon per row for the row's dominant command | the same framed button on every row |
| a command used rarely | the window's menu bar: `c.MenuBar` with a `c.MenuButton` per menu, each named for what it holds. A menu lists every command of its subject, the ones that also have a button included | a place on the toolbar; a lone *More* button; a menu of the leftovers |
| a command used constantly | a button and a key, with the key shown (`ButtonFluid.ShortcutText`; capture per [ADR-0177](../../adr/0177-imzero2-focus-scoped-keyboard-capture.md)) | a button only |
| a parameter | the input itself, applied as it changes (`c.Slider*`, `c.DragValue*`, `c.TimeRangePicker`) | an input plus *Apply*, unless the computation is costly; then one primary |
| moving, zooming, ranging or drilling a picture | the gesture on the picture (implot's hover, legend and `DragSelects`; treemap and icicle drill; the shared `camera`), with one hint line and a *fit* or *reset* | zoom, pan and range buttons |
| going somewhere | `breadcrumbs.Render` inside the app; the handover verb to the playground (keelson-app D4, L3); `c.Hyperlink` for a URL | a button that reads like a command |
| a state | a `badge` with a tone, never framed like a button (keelson-app L2, [ADR-0273](../../adr/0273-imzero2-ids-button-kinds.md) SD5) | a coloured button |
| an explanation | `c.HoverText`, or a tethered `inspector` | a *?* or *Info* button that opens a paragraph |
| a question the person settles with a button | `decision.Render` (C7) | a label and two buttons laid out by hand |

Whatever the control, the change goes through the same handler an agent's
call would (`appops.Gesture`, keelson-app A3); choosing a check box over a
button does not change the catalog.

**C2 — Kinds carry the hierarchy.** One `ButtonKindPrimary` per view, on
the action the view exists for (keelson-app L2). Toolbar commands are
`Ghost`. `Tertiary` is for one action worth pointing at beside the
primary. A call site that states no kind is `Secondary`; a view where
every button is `Secondary` has no hierarchy.

- Don't: put the fill on *Refresh*, *Copy* or the handover unless that is
  what the view is for.
- Don't: draw `Danger` on every row. The filled danger kind belongs to the
  confirm step, one at a time; a row gets `DangerGhost`.

**C3 — Count the commands at first open.** More than about seven push
buttons visible, or more than about five of one weight in a row, is a
prompt to apply C1 again: some are choices, some are per-item, some are
rare. Group what remains by step, with a separator and a caption per
group, inputs before the command that uses them. A group opens with
`sectionhead.New(caption).Send()` — space above, the caption, a rule
under it — the same in a pane, an inspector or a settings page; a pane's
title is not a group's caption and stays a heading of its own.

**C4 — A disabled control says why, next to itself.** In a sentence
beside or above it, not only in a tooltip.

- Do: "3 tables cannot be compared before the DDL is applied on the
  Structure step."
- Don't: remove a control the reader expects; an absent command reads as
  a missing feature.
- A menu holds the same items in every state. One that does not apply is
  greyed, with the reason in its hover and, where several are greyed for
  one reason, in a short line under them. A menu that opens on a sentence
  and no items reads as a tooltip.

**C5 — Destructive commands are armed, then confirmed, and say what is
kept.** Inline arm-and-confirm for the person's own click; the host
confirms consequential operations (keelson-app A2, A9; no modal of the
app's own). Prefer a command that can be undone to one that must be
confirmed, and say so in the label.

- Do: "A clear appends a tombstone: the app starts from its defaults,
  and the trail keeps the old rows."

**C6 — Captions lead, units follow, everything pointable has a name.** A
field's caption stands before or above its control, the unit after the
value. An icon-only control carries a tooltip and an accessible name
(keelson-app A5, A6); a slider or an unlabelled check box does too.

- Don't: `[auto ▼] value`.

**C7 — A decision is drawn by the decision widget, which assigns its
emphasis.** A question the person settles with a button
(approve, accept, allow, confirm) goes through `decision.Render`: what is
asked, the thing it is about, what follows from each answer as wrapped
body text, then the options
([decisions pattern](../../design-system/patterns/decisions.md), ADR-0404
§SD3). The affirmative is primary, on `OriginConfirm` (the person
started the act) and on `OriginRequest` (a model, an agent or the harness
asks for something); a request that carries a note in a warning or an
error tone has no primary, so the surface does not favour yes over its
own caution (§SD4). The widget assigns the kinds; the caller states the
origin, the roles and the notes. A long `Detail` scrolls
inside a bound, so the pane's own controls under the decision stay where
they were. A decision about what one tab shows is drawn inside that tab
and brings it forward once (`ActivateTab`); above the tabs it pushes
their strip out of the pane. While it is open, the pane does not also
show the lines the question restates.

- Do: say what each answer does where the verb does not ("Accept writes
  these 18 lines into the document; Reject discards them and tells the
  model").
- Don't: set the consequence in small or weak type, or leave it to a
  tooltip.
- Don't: show the same question in two places with two sets of buttons.

**C9 — The toolkit's mechanism first.** Menus are the menu bar, panes
are the dock, a choice is the selector, a question is the decision
widget. Before composing a control from primitives, find the one the
toolkit and egui already have; a second mechanism for the same thing is
a defect even when each looks fine alone. Where the mechanism lacks the
one capability the screen needs, that is a change to the mechanism, said
aloud before any workaround (ADR-0404 §SD8).

- Don't: replace the dock with panels because one tab would not go where
  it was wanted.
- Don't: judge a composed control from a still capture in one theme;
  hover it, and look at it in the light theme too.
- In a row that wraps, give a button its tooltip with `.HoverText()` and
  keep its label whole with `.Extend()`; a `c.HoverText` scope around it
  never moves to the next line.
- A button that shows its frame only on hover or when selected takes
  `.ReserveFrame()`, or its neighbours move when the frame appears.

**C8 — One command, one place.** While a question about a command is
open, the button that raised it is not offered again beside it; the
answer is in the question.

## 4. Words

**N1 — Commands are verb and object; places are nouns; a step may be a
question.** "Discover the servers", "Compare content", "Save record".

- Don't: a bare verb or an internal noun where the object is not obvious
  (`Turn`, `Call`, `Panel`, `settle`).

**N2 — One word per act across the desktop.** The shared verbs are
keelson-app L3's list. Before coining a label, look for the act in
another app and use its word; the same holds for gestures (*click*,
*double-click*, *drag*, *ctrl+scroll*).

**N3 — Say the consequence when the verb does not.** "Remove (and the 4
that build on it)". An ellipsis marks a command that asks for more before it
acts ("Apply the DDL…", "Save as…").

**N4 — At most one line of prose per pane, and it tells the reader what
to do or how to read.** "Every A section against every B section. Bright
is close." A count line above data is the best place for it: what is
shown, of how many, what was left out, and the gesture ("228 nodes · 225
edges · labels on hover past 48 nodes").

**N5 — An error is one plain sentence, then the verbatim text, once.**
What failed, why if known, what to do; under it the original message
unparaphrased (the empty-states pattern requires it), folded or drawn with
`errorview.Render` when it is a chain. It appears where it happened, and
the status pill agrees with it.

- Don't: show the same error in the field, the status line and the pane.
- Don't: a pill that says `idle` while a fault is on screen.

**N6 — Numbers are for reading.** A unit, a precision the reader can use,
a humanized duration and time.

- Don't: `16.899612ms`, sixteen-digit floats, timestamps with nine zeros.

## 5. Space

**V1 — The figure gets the room, and the share is stated.** The data,
the document or the picture is the largest thing in the window
(keelson-app L6). Measure it on the first-open capture at the app's
surface size: the figure's area over the window's. Under half, with
nothing else the reader asked for on screen, is a finding. Two rows of
controls above the figure is a prompt to move some into a fold or beside
it; a side pane with nothing to say is closed until it has something
(V2).

**V2 — Content fills its pane, or the pane gives the room back.** A pane
that has one sentence to say is collapsed or narrow until it has more. A
side pane opens when it has something the reader needs (a result, a
decision, a view they asked for), once for each such occasion, and stays
closed when the reader closes it. A
form or a table that needs a third of the width does not sit alone in a
window three times as wide: choose a smaller surface size (J7).

**V3 — Nothing is clipped at the surface size, and nothing at the
smallest size the app claims to support.** Labels, tab strips and the
status line fit; data that must be cut carries the full text in a
tooltip. Run the layout matrix (§6, E4) before calling a screen done.

**V4 — Selection on one side, its detail on the other, and the detail
follows.** The empty detail says what to select (J5).

**V5 — A picture that cannot be read says so and offers a way in.** A
graph past the size where labels fit states its count and offers a
filter, a focus or a table, in place of an unlabelled mass. A hint line
names the gestures (N2).

## 6. Checking a screen

**E1 — Brief and tasks exist before the screen.** U1 and J1, in the help
overview. A review that cannot find them stops there.

**E2 — Each top task is a scene.** The scene format is a task script: a
start state, steps by widget name, an assertion on the outcome
(keelson-app A7; the [imzero2-drive skill](../imzero2-drive/SKILL.md) has
the vocabulary). Written before the screen it is a testable description
of the task; its step count is what the task costs; a step that needs a
coordinate or an internal id marks a control with no name.

**E3 — Read it cold.** Give the first-open capture, and nothing else, to
a reader who has not seen the brief or the code: a colleague, or a fresh
agent session with no repository context. Ask three things: who is this
for, what does it do, what would you do first. Compare the answers with
the brief. Disagreement is a finding against U or J, whatever the
controls look like. A cold read by an agent is a pre-test, not a
substitute for E5: an agent shares the builder's vocabulary far more than
a domain reader does.

**E4 — Run the mechanical checks.** The scene's layout invariants
(`outside`, `overlap`, `hit-size`, `clipped`, `cut`; package `scene/layoutcheck`,
[ADR-0363](../../adr/0363-porting-from-an-end-user-egui-application.md))
across the size, theme and density matrix; then a count of the commands
on the first-open capture against C3, and the figure's share against V1.

**E5 — Put it in front of a person of the role.** Three to five people,
the top tasks, thinking aloud; note the first click and where they stop.
Where no such person was available, the help overview says the screen is
untested with its reader, in the same register the repository uses for an
unverified number.

**E6 — Let use decide the toolbar.** A click routed through
`appops.Gesture` is logged with the person as writer
([ADR-0269](../../adr/0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
§SD8). Commands that are never used leave the toolbar for a menu (C1).

## 7. Review checklist

- [ ] The help overview opens with the brief: role, job, trigger, what
      they know and do not (U1); one primary reader and no view mode per
      reader (U2)
- [ ] No identifier, variable name, package path or ADR number in window
      text that the primary reader would not use (U3); every remedy is in
      that reader's reach (U4)
- [ ] Pane text explains the task, not the implementation (U5); no
      diagnostics of the app's own in the first row (U6)
- [ ] Top tasks written; the window opens on step one; a cold reader can
      say what it is for and what to do first (J1, E3)
- [ ] Reading order is the order of work; follow-up actions sit by the
      result (J2); dependent steps are drawn as steps (J3)
- [ ] Tabs are peers that fit the strip; inapplicable views are in a menu
      with their reason; at most two levels (J4)
- [ ] Every empty pane says what will be there and how to fill it (J5);
      required input is counted and located (J6)
- [ ] The job fits one window at its surface size (J7); outcomes are
      reported with where the result went (J8)
- [ ] Every `c.Button` is a command; choices, settings, facets, view
      switches, selections, rare commands, parameters and picture
      gestures use the control in C1's table
- [ ] One primary; toolbar commands are ghost; no column of filled danger
      (C2); about seven commands or fewer at first open (C3)
- [ ] Menus are the menu bar, panes and view switches are the dock; no
      second mechanism for either; composed controls checked on hover and
      in both themes (C9)
- [ ] Disabled controls say why beside themselves (C4); destructive
      commands arm, confirm and say what is kept (C5)
- [ ] Every question the person settles goes through `decision.Render`,
      with its origin stated and each answer's effect in words (C7); the
      command that raised it is not offered twice (C8)
- [ ] Captions before, units after, names on everything pointable (C6)
- [ ] Commands are verb and object; the desktop's word for the act is
      used (N1, N2); consequences are in the label where the verb hides
      them (N3)
- [ ] One line of prose per pane at most (N4); errors once, plain then
      verbatim, and the pill agrees (N5); numbers readable (N6)
- [ ] The figure is the largest thing and its share is stated; no pane
      holds a sentence in a sixth of the window (V1, V2); nothing clipped
      or cut across the layout matrix (V3, E4)
- [ ] A scene per top task, by widget name, with an assertion (E2)
- [ ] Tested with a person of the role, or the overview says it was not
      (E5)

## 8. What this skill does not decide

Gaps observed 2026-11-22 that an app author cannot close from inside an
app. Each is the toolkit's or the host's, and its own ADR when taken up.

- Where the per-window capture row and the desktop's telemetry belong for
  a reader who is not the builder (U6).
- A place on the manifest, or a checked place in the help book, for the
  audience and the job, and a launcher that shows it (U1).
- Affordances C1 points at that are thin or missing: actions for the
  selected row and multi-select on `recordtable`; an overflow menu; a
  stepper widget (J3); a confirm helper (C5); an empty-state helper (J5);
  a shortcut registry that fills `ShortcutText`; undo outside text
  editing.
- A searchable list of a window's commands for the person. The operations
  catalog already holds each command with its description; only agents
  read it.
- Roles in the accessibility tree: tabs, rows, tree nodes and chips are
  reported as `button`, so a count of commands cannot be taken from a
  tree sidecar (C3, E4) and an agent cannot tell a tab from a command by
  role.
- The hierarchy and empty-state review of
  [ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md)'s
  second tier (rubrics V1 and V3 in
  [tier2-llm-review](../../design-system/policy/tier2-llm-review.md)),
  which E3 does by hand.
- What the validation on the chat window (ADR-0404, verification plan)
  needed and did not have: a dock tab strip that wraps or overflows into
  a menu, without which a pane too narrow for its tabs has no way to
  switch views that C1 allows; dock tabs that cannot be dragged; a stroke
  on a button without borrowing a kind; a scene step that asserts
  something is absent; a scene that launches a widget demo; a tooltip
  without a wrapping scope on widgets other than the button; an
  accessible name on a button apart from its text, for icon-only
  controls; a check box's checked state and a panel's bounds in
  the tree sidecar, without which C1's check boxes are hard to assert in
  a scene and V1's share is an estimate; an accessibility click that
  closes a menu as a pointer press does; an operation's display title
  reaching the window that reports a call to it.
- One word for the rendered view of a document: *Read* in the editor,
  the notebook and the chat, *Preview* in the playground and the store
  browser (N2).
- Label casing: keelson-app L3 says sentence case on labels; the
  [tier-1 table](../../design-system/policy/tier1-mechanical.md) says
  Title Case on buttons. This page takes no side.
- The number of empty states: keelson-app L4 names three, the pattern
  document seven.
