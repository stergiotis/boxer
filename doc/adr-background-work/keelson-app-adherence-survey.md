---
type: explanation
audience: whoever decides which apps to bring up to the keelson-app standard first, and what the standard itself has to change
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-07
---

> Compiled 2026-11-07 by six agent-assisted read passes over the working
> tree, one per group of apps, against the §10 checklist of
> [the keelson-app skill](../skills/keelson-app/SKILL.md) as it stood that
> day. Every claim names the path and symbol it was checked at; "not
> determined" is kept where a pass could not verify. The tree surveyed
> included uncommitted work (`apps/riskviews`, edits in `apps/chat`), so
> rows for those two may already be stale. Nothing here is a decision;
> the skill was amended the same day for the findings in §5, and this page
> records what was seen.

# Keelson app adherence survey

The question: how far do the apps under `apps/` already follow the idiom
the keelson-app skill writes down, where do they all fall short in the
same way, and where does the code show the standard to be wrong? The
answer decides two things: the order in which apps are brought up, and
the sentences in the skill that had to change before anyone is reviewed
against it.

## 1. Scope and method

Twenty-six packages sit under `apps/`. Twenty-three implement `AppI`,
one is a headless service (`servicedemo`), and two are `package main`
commands that the checklist does not cover (`jsonbench`, a trial's
solution artifact; `svgserver`, an HTTP prototype). `sqlapplet` is a
minter that registers one manifest per embedded document, so its row
reads partly for the minter and partly for each minted window.

Each pass read the skill in full, then the app, with grep over the
symbols the rules name (`RegisterFactory`, `launchcfg`, `fsmview`,
`fsmops`, `task.ForApp`, `bgjob`, `jobprogress`, `PausableAppI`,
`recordtable`, `adhocdata`, `clipboardbroker`, `search.ParseQuery`,
`regexedit`, `styletokens`, `ButtonKind`, `lazypane`, `attention`,
`help/`, `scenes/`, `registry.Demo`) and targeted reads to confirm what
grep suggested. A mechanical presence matrix built independently with
grep agreed with the passes on every presence claim it could check.

## 2. The matrix

One row per app, one column per checklist line, in checklist order.
**y** meets the line, **p** partly, **n** does not, **–** not applicable
by the app's shape (and the skill now says why, §5). The two commands
are listed for completeness and not scored.

| App | Manifest | Launch | Data | Persist | Phases | Work | Catalog | Help | Tips | Scene | Attn | Filter | Method | Look | Perf | Seam | Declare |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| play | y | y | p | p | y | p | y | y | p | p | y | y | p | p | p | y | p |
| chat | p | p | p | n | y | p | y | n | y | p | y | – | y | y | p | y | p |
| mdedit | y | p | p | y | n | p | p | p | y | p | p | n | – | p | p | – | y |
| tally | y | y | p | y | n | p | n | p | n | p | y | y | – | p | y | – | y |
| jackstay | p | p | n | p | y | y | p | n | y | p | y | y | – | p | y | – | p |
| imztop | p | – | p | y | n | p | p | n | p | p | y | n | p | p | y | – | p |
| imzrt | p | – | p | y | – | p | n | n | n | p | y | – | p | p | p | – | y |
| capinspector | y | n | – | y | – | – | y | p | p | p | y | – | – | y | p | y | y |
| watchbill | p | y | p | p | p | p | p | n | y | p | y | n | – | p | y | – | y |
| appstate | p | – | n | y | – | p | y | n | y | p | y | p | – | p | y | – | p |
| sqlapplet | p | p | y | p | – | p | p | p | – | p | y | – | – | – | – | y | p |
| sqlappletcreator | p | y | – | p | – | p | n | n | y | n | y | – | – | p | y | – | y |
| writingstylescope | p | n | p | p | n | n | n | n | y | p | y | – | y | p | p | – | y |
| terrainscope | p | n | n | y | p | p | n | n | p | p | y | – | p | n | p | – | y |
| fibscope | p | n | n | p | – | – | n | n | y | p | y | – | y | n | p | – | y |
| riskviews | p | – | n | y | – | – | n | n | y | p | y | – | p | p | y | – | y |
| splashscreen | p | – | – | y | – | p | n | n | – | p | y | – | – | p | y | – | y |
| agentconsole | p | p | – | y | – | p | – | n | p | p | y | – | – | p | y | – | y |
| opsdemo | p | – | – | y | – | p | y | n | y | p | y | – | – | p | y | – | y |
| taskdemo | p | – | – | y | – | p | n | n | y | n | y | – | – | p | y | – | y |
| watchbilldemo | p | – | n | y | – | p | n | n | – | n | y | – | – | p | y | – | y |
| adhocdemo | p | – | p | p | – | p | p | n | – | n | y | – | – | p | y | – | p |
| capdemo | p | – | – | y | n | n | n | n | – | n | y | – | – | p | y | – | y |
| servicedemo | p | – | – | y | – | y | – | – | – | – | – | – | – | – | – | – | y |
| jsonbench | command, not scored | | | | | | | | | | | | | | | | |
| svgserver | command, not scored | | | | | | | | | | | | | | | | |

## 3. What every app gets wrong the same way

Ranked by how many apps it touches, with the rule it breaks. Each of
these is a sweep, not a redesign.

- **No help book (A4).** Five of twenty-three have `Manifest.Help`
  (play, mdedit, tally, capinspector, and sqlapplet's books). Of those,
  only play's names its operations and phases; mdedit's and
  capinspector's are written for the person and say nothing an agent
  needs. The skill names watchbill as the manifest to copy, and watchbill
  has no help book.
- **Goroutines outside the lifetime scope (W5).** Only servicedemo and
  chat's coordinator use `app.LifetimeOf` or `lifetime.New`. Play starts
  fifteen bare goroutines; chat seven; mdedit, tally, writingstylescope,
  terrainscope, imztop, imzrt, adhocdemo, agentconsole, taskdemo, opsdemo
  and capdemo one or more each; watchbill, watchbilldemo and appstate use
  a hand-built cancel-and-WaitGroup. None is visible to the host's drain.
  capdemo's `Unmount` is a no-op, so its watch subscription outlives the
  window. tally builds `task.NewBusApi` from the bus alone, so its lanes'
  tasks carry no instance key, mount cancel or lifetime.
- **No button kinds (L2).** `Kind(c.ButtonKind*)` appears four times in
  the tree: play's Run, chat's Send and Answer, appstate's Danger on
  confirm-forget. No other app names a primary or a danger button.
- **No operations catalog (A1).** Eleven apps with state declare none:
  tally, imzrt, jackstay (phase queries only), writingstylescope,
  terrainscope, fibscope, riskviews, sqlappletcreator, taskdemo,
  watchbilldemo, capdemo, splashscreen. A minted sqlapplet window hides
  the catalog of the play inside it although `BundleViewOps` already
  forwards it for embedders.
- **No scene ends on F1 (A7), and most scenes assert nothing.** Only
  capinspector's scene presses F1. Scenes with a `read` and an assertion:
  play (six of ninety-one), chat (five of thirty-four), agentconsole
  (three of six), jackstay, appstate, sqlapplet, capinspector (by a
  value-matched `wait`). Eleven apps have no scene at all.
- **Raw clipboard requests (D5).** mdedit, tally, play (six callers
  through one helper), capdemo and opsdemo call
  `bus.Request(clipboardbroker.SubjectWrite, …)` unlabelled; capdemo's
  rendered markdown teaches that shape. Only chat uses
  `clipboardbroker.Write` with a `Payload`. Codelint CS014 covers the
  egui2 opcode, not the bus subject, so all of these pass lint.
- **Substring filters (F1).** imztop's process filter, watchbill's kind
  box, mdedit's find and appstate's `list_entries.App` argument are
  lower-cased `strings.Contains`. tally (RE2 in SQL), jackstay (WHERE
  fragment) and play (regex battery) are the three that do it right.
- **Colour literals the lint cannot see (L1).** terrainscope (six),
  fibscope (four), svgserver (one) and imztop (one, `availabilityBandColor`)
  use `color.Hex(0x…)` or a bare `uint32`. The tier-1 L2 lint matches
  `color.RGB`/`color.RGBA` constructors only.
- **Handover that reports before the reply (D4, W8).** tally and imzrt
  open play with a fire-and-forget `go RequestOpen`; play's own *Open in
  Playground* blocks but reports only failure. writingstylescope, mdedit,
  adhocdemo, chat and agentconsole report after the reply.
- **Phases without a machine (T1–T4).** play, chat, jackstay and
  watchbill have one; terrainscope (`selectionStageE`), imztop
  (`ReplayStateE`), mdedit (open/save/send/follow), writingstylescope and
  capdemo carry an enum or flags with no machine, chip or mount.
  watchbill's chip is untethered, carries no provenance and sits in a
  detail header. play's second machine (the projector) is neither
  tethered nor mounted.
- **No tour entry (A8).** Eight apps register a `registry.Demo`
  (imzrt, imztop, writingstylescope, terrainscope, fibscope, mdedit,
  capinspector, splashscreen). play reaches the carousel only as a whole
  app.
- **Ids from string concatenation (P6).** play (twenty-three sites) and
  chat (twelve). Most join a prefix to a stable key, which the skill now
  allows; the rule's wording was the defect.
- **Repaint discipline (W7).** play requests a repaint unconditionally at
  the end of every frame; watchbill, watchbilldemo and appstate never
  request one when their poller lands, so under reactive cadence the
  snapshot waits for the next input.

## 4. Defects found on the way

These are bugs or contradictions rather than idiom, and are worth fixing
before any sweep above.

- `apps/chat/chat_roster.go:storeTitle` writes a conversation title
  through `ctx.Storage()` while the chat manifest declares no
  `PersistedKeys`, so the host injects no persist capability and the bus
  refuses every write; the failure is logged at debug and the tests use
  an in-memory storage that cannot see it.
- `apps/terrainscope/terrainscope.go:runSweepWorker` calls
  `finishWorker(nil)` on error and only logs, so the window shows
  "Computing line-of-sight sweep ensemble…" indefinitely and
  `maybeRecompute` never relaunches because the parameters did not
  change.
- `apps/capdemo/capdemo.go:runPersistGet` writes `inst.scratchpad` from
  a goroutine while the `TextEdit` binds it on the render goroutine.
- `apps/adhocdemo/adhocdemo.go:Mount` calls `play.PublishBundle` on the
  render goroutine, against that function's own doc comment.
- `apps/watchbill/watchbill_ops.go:JobRow` describes states (`leased`,
  `done`, `dead`) that `watchbillstore` does not define; `cancel_job` and
  `retry_job` reply `applied` before the store answers, so "did not
  apply" reaches the window badge and never the agent.
- `apps/watchbilldemo/app_register.go` sets `Display: "Watchbill"`, the
  same display name as the management window.
- `apps/imzrt/imzrt_panel_profiles.go:explore` sets `e.opening` and
  nothing reads it, so the "Opening…" state is never drawn.
- `apps/opsdemo/opsdemo_test.go:TestTheCatalogRegisters` omits
  `opCopyNote` from the lookup loop.
- `apps/splashscreen/app_register.go:assetsFS` says the artwork is
  `assets/patronus.png` and git-ignored; the embedded, tracked file is
  `assets/splash.png`.
- `apps/mdedit/mdedit.go:handOver` opens a plain window on an undecodable
  launch config and a test pins that; sqlappletcreator and play return
  the error from `Mount`, which S4 asks for. Decided 2026-11-07: failing
  the mount is the house shape, so mdedit and its test change.
- `apps/sqlapplet/sqlapplet_register.go:WindowSize` reads
  `BOXER_SQLAPPLET_WINDOW_SIZE` to size the window for screenshots, the
  `BOXER_PLAY_SCREENSHOT`-family pattern AGENTS.md says not to copy.

## 5. What the code said about the standard

Each pass was asked where the code showed a rule to be unrealistic,
contradicted by an ADR, or ambiguous. The findings, and what the skill
now says (amended 2026-11-07):

| Finding | Evidence | Skill now |
| --- | --- | --- |
| "No file export from an app" contradicted three apps' Powerbox saves | mdedit's Save (ADR-0178), sqlappletcreator's export (ADR-0132 O4), jackstay's plan export; ADR-0317 §SD6 defers export as a *subject*, it does not forbid a broker-dialog write | D4 forbids a write under the app's own name; a save through the fs broker's dialog is the Powerbox's exit |
| "Tier-1 lints enforce the palette" overstated | `tier1-mechanical.md` L2 matches `color.RGB`/`RGBA` only; every literal found was `color.Hex` or `uint32` | L1 names the gap; a lint is a §12 item |
| Play coupling "confined to the leaf" was false | sqlapplet and adhocdemo import `apps/play` for `PublishBundle`, `NewLivePlayApp`, `PlayApp`; capslock and the tab commands too | §0 lists the three shapes the playground abstraction must cover |
| "No gate of its own" contradicted by accepted ADRs | ADR-0270 §SD7 (play's env write gates), ADR-0185 M3 (armed forget), ADR-0259 (two-step DDL/sync confirm), ADR-0342 (export ask), ADR-0284 §SD4 (screenshot budget), ADR-0329 §SD4 (destination check) | X4 names three things that are not gates (harness ceilings per ADR-0318, runtime-helper checks, the person's inline confirm) and one tolerated gate (play's, retired by a runtime ADR) |
| "Env vars seed, never configure" contradicted by chat | ADR-0284 §SD4 and ADR-0280 declare ceilings as env vars | S5 makes the harness the exception |
| "Every `Reason` says what the app does" unachievable for `task.ProducerCaps()` | `public/keelson/runtime/task/caps.go` carries a fixed reason | S3 exempts fixed-reason helpers; a reason parameter is a §12 item |
| W3 pointed at an example that does not exist | `OnBehalfOf` does not appear in `apps/watchbilldemo`; only the how-to shows the copy | W3 says so, dated |
| W4's exemplars fail W5 | taskdemo and watchbilldemo run bare goroutines | W4 says copy the monitor, not the goroutine shape |
| D4's exemplar fails W5 | writingstylescope's `go inst.requestHandover()` | D4 says copy the reporting; the goroutine is a named debt |
| "Confirmed by the person every time" had no runtime path for gestures ADR-0345 §SD4 keeps off the catalog | appstate's forget, jackstay's DDL | A2 and A9 allow an inline, non-modal arm-and-confirm for the person's own click |
| `run` effect vs ADR-0329 §SD4 | `enter_replay` stays `view` while it dials | A2 cites the exception |
| P6 forbade a shape that is stable | `PrepareStr("verdict-" + verdict.String())` | P6 allows a prefix on a stable key, forbids positions and counters |
| W7 said nothing about pollers or the continuous default | play repaints every frame; three pollers never request one | W7 rewritten |
| `task.NewBusApi` is a trap W2 did not name | tally | W2 names it |
| Checklist had no scope | two `package main` commands, one headless service, one minter | §10 preamble; ADR-0352 cited |
| A1 had no floor | agentconsole asserts `Operations == nil`; splashscreen's only state is a tab | A1 names the two exceptions and the minter case |
| "A read and an expect" excluded capinspector's value-matched `wait` | `apps/capinspector/scenes` | A7 and the checklist say "assertion" |
| §11 cited exemplars that fail the rows beside them | riskviews (no catalog, help, tour), capinspector (one coordinate click, deferred), watchbill's chip | §11 rows say what to copy from each |
| "Pause" as a verb for a sampler freeze | imzrt's Pause stops every window's sampler; imztop says Freeze/Go live | W6 and L3 name the verbs |
| D6 silent on a pane split and on lading writes | watchbill persists its split; adhocdemo writes a lading tree through a client built from env | D6 says chrome stays out, a runtime-path publish is not a store, an env-built client is |
| CS014 does not catch the shape D5 names | `gov_codelint_rule_cs014.go` covers the opcode | D5 says so; a lint is a §12 item |

Two findings were put to the maintainer and decided 2026-11-07: a
full-window app owes a chrome-less `registry.Demo` like any other, so
play's carousel entry does not satisfy A8; and sqlapplet's applet store
service moves under the runtime, which needs an ADR-0132 update or a
small ADR of its own (recorded in §12 of the skill).

## 6. What was done well, and where

The passes were also asked what to cite. The skill's §11 was rewritten
from these; the list here is the longer one.

- **Manifests with reasons and a comment on the capability not
  declared:** mdedit, capdemo, play (every cap composed from a helper,
  reason naming the ADR).
- **Launch config that fails the mount on a bad decode, with the ADR
  cited in the comment:** sqlappletcreator, play (five-tier precedence
  comment), tally (the config doubles as the workingset), watchbill.
- **State machines:** jackstay (pure observe, graded mirror, tethered
  chip with provenance, three tests), play (query machine, idle the only
  unreachable state tested), chat (turn machine), watchbill (selected-row
  machine mounted without history).
- **Work:** jackstay (seven runners, `takeResults` first, repaint while
  running), imzrt's profile capture (`bgjob.Runner` on `task.ForApp`,
  `jobprogress` with cancel and ETA), tally's lanes (one helper turns
  every lane into a named task with the standard row and a
  cancelled-versus-failed empty state), servicedemo and chat's
  coordinator (lifetime scopes), appstate's pause test.
- **Catalogs:** opsdemo (the template), appstate and imztop (bounded,
  argument-taking queries with tests that they read their own arguments
  and that commands never toggle), mdedit (revision conflicts), play
  (one gesture path with a direct fallback, agent limits that fail a run
  and never change it), capinspector (points at the `keelson()` table
  instead of duplicating it), sqlapplet's `BundleViewOps` (forwarding
  with per-view availability and `SettlesRaw`).
- **Handover:** writingstylescope (busy guard, blocking request, refusal
  shown, no-bus line, success note; play's leaf in one file), adhocdemo's
  tree handover (blocking request inside a cancellable task), mdedit's
  facts-upload variant, chat's stats.
- **Honest method:** writingstylescope ("there is no threshold here on
  purpose", Berk-Jones versus DKW band in the tooltip, refusals with
  remedies, a per-cell failure aborting the sweep), terrainscope
  (assumption at the control, envelope beside the profile, Batlow chosen
  for CVD with the ADR cited), riskviews (per-view evidence line,
  synthetic-data caption, quantile dotplot, stepped threshold, "never
  colour alone"), play's series panel (gaps break the line, an irregular
  grid refused with a scaffold), imzrt's profiles panel (says which
  capture perturbs the series shown).
- **Tooltips as named constants:** terrainscope's `tip*` and `metric()`,
  copied by fibscope and writingstylescope; mdedit's `tipFindQuery`.
- **Hit labels on painted marks:** riskviews' canvas helpers; the
  deferral note in capinspector's `archgraph.go` is the right shape for
  not having them yet.
- **Look:** imztop's theme and play's tone vocabularies derived from
  tokens; splashscreen's LUT from the sequential ramps; three-way empty
  states in tally, watchbill, appstate, capinspector, mdedit's files
  pane, sqlapplet's dataset notice; chat's Analytics tab behind the
  Advanced switch.
- **Cost model:** one `lazypane` per dock tab in play, imztop and tally;
  keyed memos in imztop, play, terrainscope (`maybeRecompute` coalescing
  on the parameter key), fibscope; fibscope's tested stable-pointer
  discipline for a scrubber.

## 7. An order of work

What the matrix suggests, cheapest first; each line is a sweep across
the tree rather than one app. Confirmed by the maintainer 2026-11-07.

1. Fix §4's defects; chat's refused title writes and terrainscope's
   silent sweep failure are user-visible.
2. Replace the five raw clipboard requests with `clipboardbroker.Write`
   and move every bare goroutine onto `task.ForApp` or the lifetime
   scope; write the two lints §12 of the skill names so neither comes
   back.
3. Button kinds and status badges in every window; the hex literals in
   terrainscope, fibscope and imztop.
4. Help books, written for the agent, for every app with a catalog
   (play's needs its state queries named; mdedit's and capinspector's
   need their operations), then for the rest.
5. Catalogs for the eleven apps without one, starting with tally and
   imzrt, whose state is already table-shaped, and the minted applet
   windows, where the forwarding exists.
6. Machines for terrainscope, imztop's replay and mdedit's file
   lifecycle; tether watchbill's chip and play's projector.
7. Scenes: an assertion in every scene that has none, an F1 ending, and
   a first scene for the eleven apps without one.
8. The playground abstraction, once its three shapes are agreed, and
   the applet store's move under the runtime (skill §12).
