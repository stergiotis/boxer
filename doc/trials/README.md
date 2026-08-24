---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-24
---

# Trials

Reproducible measurement protocols, in the sea-trials sense: standardized
runs performed against a build and **repeated on later builds**. A trial
measures two things at once: the system's numbers under the workload, and —
usually foremost — the toolbelt's fitness while *recreating* that workload
with native idioms: which gaps surface, which frictions get filed, and how
short the solution turns out to be. Which of the two leads varies per
trial; the protocol must say. A page here describes *how to run the trial
and what to record* — dataset, workload, arms, environment, metrics,
acceptance gates — precisely enough that a later run is comparable with an
earlier one.

What separates this directory from its neighbours:

- [`doc/adr/`](../adr/) holds decisions. A trial is not a decision; its
  numbers may feed one.
- [`doc/adr-background-work/`](../adr-background-work/) holds dated one-off
  analyses that age out once the decision they fed is taken. A trial
  protocol is the opposite: it is **maintained against the tree** and stays
  live for as long as re-running it is worth the effort.
- [`doc/howto/`](../howto/) explains tasks for someone operating the
  system. A trial is executed *on* the system, typically by an agent that
  did not build it, and its product is measurements and findings.

Each trial is a directory — a self-contained dossier:

```
<trial>/
  README.md            the protocol, and the trial's one citable claim (§0)
  logbook.md           one record per run: what it was, not what it argued
  runs/<YYYY-MM-DD-slug>/   one per run: the evidence backing its
                       logbook entry — environment capture, pinned
                       configs, EXPLAIN output, timing tables
  <vendored material as needed, e.g. upstream/ for pinned workloads>
```

A run directory holds small, textual, provenance-grade evidence. Bulk
results belong in the facts store; datasets are never committed. New
trials start from the skeletons in `doc/templates/trial/`.

**One page carries the prose.** A trial is a dossier of *evidence* plus a
single page of *claims*. Per-run results documents, verdicts and
side-experiment write-ups are not a separate tier: they are merged into the
README when the run that produced them closes, and the pre-merge state stays
in git history, cited by commit from the surviving page. The reason is
[*Citing a trial*](#citing-a-trial) below — every additional page of
measurement prose is another surface from which a figure can be quoted
without the condition that makes it true.

Consequences of that framing:

- **Every run appends a logbook entry.** An entry records the
  date, the build under test (repo commit, engine versions), the hardware
  and environment (CPU, memory, storage class — never hostnames or personal
  paths), what was attempted, the findings (classified per *Finding
  classification* below), a one-line outcome, and pointers to result data.
  The logbook is what makes two runs comparable and is the finding ledger's
  carrier until findings-as-facts land. Entries accumulate rather than being
  rewritten; a **compaction** — dropping the reasoning narrative once its
  conclusions have landed in the README — is the one exception, and it names
  the pre-compaction commit so the narrative stays recoverable.
- **Numbers are data, not prose.** Domain results land as facts and are
  read back through applets; a small result table inside a logbook entry is
  the exception and needs a reason stated in the protocol.
- **The solution is part of the result.** Artifacts the run produces
  (mappings, queries, applets) are committed, so their size is countable
  and their shape judgeable by a later reader.
- **Friction is filed, not patched around.** A trial run that hits a gap in
  the toolbelt records a finding; the protocol does not grow workarounds.
- **Protocols are versioned by ordinary edits.** A change that breaks
  comparability with earlier runs (new dataset tier, changed metric) must
  say so in the logbook.
- **Retirement is explicit.** A protocol no longer worth re-running is
  deleted, or moved to
  [`doc/adr-background-work/`](../adr-background-work/) as a snapshot if
  its last numbers still feed a pending decision.
- **Superseded numbers leave the tree.** A figure a later run has
  invalidated is not annotated in place — a caveat one paragraph away from a
  table does not survive being read in fragments, and restating a retracted
  figure in order to retract it is the most copyable shape there is. Delete
  it, say in the README what it was wrong about and why, and leave the
  figure in history.

## Citing a trial

A trial page has one job its evidence cannot do: state what the run
established and under which conditions. Two failure modes made this a rule
rather than a preference — a reader, human or machine, arriving at a
measurement table through a search rather than through the page, and
quoting a ratio whose meaning lives elsewhere; and superseded figures
sitting in-tree looking exactly like live ones.

- **§0 is the claim.** Every trial README opens with a *citable claim*
  section, before the protocol: the verdict with its condition inside the
  sentence, the conditions under which it changes, and an explicit list of
  the misreadings the trial's own numbers invite. It comes first because the
  top of a page is what retrieval reliably returns, and because a quotable
  sentence has to exist or the tables become the quote.
- **No orphan ratio.** A figure carries what it compares, under what
  conditions, at which scale, in the same sentence, cell or column header —
  not in a caption above it or a paragraph before it. A number that reads
  correctly only in document order will eventually be read out of it.
- **Name the arm, not the system.** An arm that measures a mistake — a
  wrong read path, a missing declaration — is labelled as that, not as the
  system under test. Otherwise its figure gets quoted as the system's cost.
- **Numbers are data.** Prefer a page that holds raw measurements and
  derives ratios at read time (a book applet does this) over a Markdown
  table of pre-computed ratios: what does not exist as text cannot be
  extracted without its context.
- **Say what the evidence is worth.** One machine, one run per
  configuration, one corpus, never reviewed — whichever of these applies
  belongs in §0, not only in a threats-to-validity section at the end.

## Finding classification

A finding is a triple — **competence × relation × quality characteristic**
— plus a severity and its evidence. One finding per proximate obstacle: the
thing that stopped the run, not the cause suspected underneath it.

- **Competence**, by vault slug ([`doc/competences/`](../competences/)).
  A need no competence covers anchors at the **nearest existing ancestor**
  with a proposed slug; findings never mint corpus entries. A proposed slug
  recurring across entries is the editorial signal to author one.
- **Relation** — which vault field the evidence feeds:
  `missing` (nothing covers the need) and `broken` (claimed, but the
  attempt failed) are *maturity* evidence; `pain` (worked, with friction)
  is *pain* evidence. A completed run leaning on a competence is positive
  maturity evidence, worth a line of its own. The vault's 0..5 fields flip
  from not-assessed only editorially — a judgement citing findings — never
  by automation.
- **Quality characteristic** — ISO 25010 (2023 revision), classified
  **evidence-first**: by what the run directory actually holds, not the
  suspected root cause. Measured slowness → performance efficiency; wrong
  output → functional correctness; crash or hang → reliability; had to
  read source to proceed → self-descriptiveness; a workaround sequence →
  operability; valid intent rejected by a grammar → functional
  completeness. Top-level characteristic mandatory, sub-characteristic
  optional.
- **Severity** — S1 blocker / S2 major / S3 minor / S4 note, orthogonal to
  both axes.

Logbook line format, one finding per line, mechanical to migrate to facts:

```markdown
- **[<relation> <competence-slug>[ → proposed:<slug>] /
  <characteristic>[.<sub-characteristic>] / S<1-4>]** <one-sentence
  statement of the obstacle> (evidence: <file under the run dir>)
```
