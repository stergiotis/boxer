---
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-27
---

# Boxer Documentation Standards

This repository contains deeply nested, technical Go packages. Documentation must be precise and placed exactly where developers need it.

To prevent "Markdown sprawl" (a proliferation of overlapping `DESIGN.md`, `SPEC.md`, and `ARCH.md` files) and to keep documentation close to the code, we adopt two complementary conventions:

1. **[Diátaxis](https://diataxis.fr/)** for descriptive documentation — every doc is Reference, How-To, Explanation, or Tutorial. Mixing quadrants causes cognitive overload.
2. **Architecture Decision Records (ADRs)** for decisions — an orthogonal artifact that captures *choices*, keeping Explanation focused on timeless theory.

Every Markdown doc must declare its type in a front-matter stanza (see §4). This makes intent explicit for readers, tooling, and AI agents.

**Workflow assumption.** This standard is tuned for small, AI-assisted data-engineering teams committing directly to `main` — no PR branches, consistent with the Motivation section of [`CODINGSTANDARDS.md`](../CODINGSTANDARDS.md). Document status is carried in the front-matter stanza and surfaced by a mandatory draft banner; drafts and stable docs coexist on `main` and are distinguished by those two signals alone. No branch metadata, filename convention, or external review tooling carries state.

---

## 1. Artifact Types & Where They Belong

### Reference (Information-Oriented)
> *"Reference guides are technical descriptions of the machinery and how to operate it. [...] The only purpose of a reference guide is to describe, as succinctly as possible, and in an orderly way."* — [Diátaxis: Reference](https://diataxis.fr/reference/)

- **Goal:** Factual, succinct description of APIs, structs, and functions. Assumes the reader knows what they are looking for.
- **Format:** Go doc comments (`//`) plus `doc.go`.
- **Rules:**
    - **No Markdown files for API references.** `pkgsite` is canonical.
    - A doc comment on an exported symbol begins with the identifier's name and ends with a period. Write one wherever there is something to say that the signature does not already say (§4 *Go doc comments*); a comment that only respells the signature adds a second thing to keep true.
    - Follow [go.dev/doc/comment](https://go.dev/doc/comment). Use Go 1.19+ doc links (`[pkg.Symbol]`, `[Type.Method]`) for cross-references instead of bare URLs.
    - Deprecate with a `Deprecated:` paragraph naming the replacement. Flag known defects with `BUG(who):`.
    - Any package with more than ~3 exported symbols, or any package that warrants package-level discussion, carries a `doc.go`.

### How-To Guides (Problem-Oriented)
> *"How-to guides are directions that guide the reader through a problem or towards a result. [...] A how-to guide helps the user get something done, correctly and safely."* — [Diátaxis: How-to guides](https://diataxis.fr/how-to-guides/)

- **Goal:** Recipes for a developer who needs to solve a specific problem (e.g., "How to encode a packet with `golay24`").
- **Format:** `example_test.go` (testable Go examples).
- **Rules:**
    - Name examples by Go convention: `ExampleFoo`, `ExampleFoo_bar`, `ExampleFoo_Method_variant`.
    - Include an `// Output:` or `// Unordered output:` block so the example is both compiled and asserted by `go test`. Examples without an output block are not enforced and rot silently.
    - Each example is minimal and self-contained — no shared fixtures across examples.
    - *Exception:* if a How-To requires external environment setup (e.g., compiling C++ for `imzero`), use `HOWTO.md` in that package.

### Explanation (Understanding-Oriented)
> *"Explanation is discussion that clarifies and illuminates a particular topic. [...] Explanation clarifies, deepens and broadens the reader's understanding of a subject."* — [Diátaxis: Explanation](https://diataxis.fr/explanation/)

- **Goal:** The "why" that does not decay — theory, mathematics, memory layout, invariants, trade-offs that follow from physics or the problem domain.
- **Format:** `EXPLANATION.md`, alongside the `.go` files.
- **Rules:**
    - Explain *properties that would still hold if someone rewrote the code* (e.g., Hamming distance of a code, why a structure is lock-free, why an encoding is self-synchronising).
    - Mutable choices ("we picked Zstd over LZ4 because…") belong in an ADR, not here. The same test separates durable prose from perishable prose everywhere else in this standard — see §4 *Claims that decay*.
    - Split into an `EXPLANATION/` directory if the file exceeds ~400 lines or covers distinct concerns.

### Tutorials (Learning-Oriented)
> *"Tutorials are lessons that take the reader by the hand through a series of steps to complete a project [...] wholly learning-oriented, and specifically [...] oriented towards learning how rather than learning what."* — [Diátaxis: Tutorials](https://diataxis.fr/tutorials/)

- **Goal:** A lesson that takes a newcomer end-to-end.
- **Format:** `TUTORIAL.md`.
- **Rules:**
    - Tutorials typically cross package boundaries. **Do not bury them in deep sub-packages.**
    - Place them at a top-level module root (`<module>/TUTORIAL.md`) or at the repository root.

### Architecture Decision Records (Why-It-Is-This-Way)
- **Goal:** Capture a single decision with its context, alternatives considered, and consequences. Replaces ad-hoc `DESIGN.md` and internal wiki pages.
- **Format:** `doc/adr/NNNN-kebab-title.md`, monotonic numbering. The decision history lives in git plus an explicit `Updates` section (below); the body text is not byte-sacred.
- **Rules:**
    - One decision per file, one file per decision.
    - Status lifecycle: `proposed → accepted → (deferred | deprecated | superseded by ADR-XXXX)`, or `proposed → withdrawn` when a proposal is retracted before acceptance. Supersession is the documented escape hatch when the decision itself changes (see "Editing ADRs" below).
    - If a decision affects a specific package, link to it from that package's `README.md` and, where relevant, its `EXPLANATION.md`.
    - **Cite an ADR by number, not by filename.** `ADR-0058 §SD3` survives a retitle and resolves mechanically. A hardcoded `doc/adr/0058-<slug>.md` breaks the moment the title changes, and a citation carrying the wrong number sends the reader to a different decision entirely — the failure is silent in both directions. This applies to citations from code as much as from prose; a `.go` comment naming an ADR is subject to the same rule.
    - **A citation of a non-`accepted` ADR names its state** — "ADR-0008 (superseded by ADR-0113)", "ADR-0112 (proposed)". Without it a reader cannot tell a decision in force from a record of one that is not, and code that cites a `proposed` ADR reads as though the decision had been taken. Where code cites a `proposed` ADR because the work has in fact shipped, that is the signal to flip the ADR (see *When to flip* below), not to annotate the citation.
    - Minimum sections: *Context*, *Decision*, *Alternatives*, *Consequences*, *Status*.
    - **Design-space analysis (QOC):** when a decision has ≥3 viable options evaluated against ≥3 explicit criteria, use the optional `Design space (QOC)` section from the ADR template — Questions, Options, Criteria (MacLean, Bellotti, Young, Moran, 1991). The prose `Alternatives` section may then cross-reference the QOC matrix instead of duplicating the rationale. Below the threshold, a prose `Alternatives` list is sufficient.
    - **Tier-1 sections (`Surfaces`, `Migration`, `Verification plan`):** a decision that touches a core surface — the enumerated list in [CODINGSTANDARDS § What triggers an ADR](../CODINGSTANDARDS.md#what-triggers-an-adr) — carries three further sections from the template. `Surfaces` inventories which named contracts change shape and what must move with them, which `Consequences` does not do: that section records what a change costs, not what it reaches. `Migration` states what breaks and what a reader on the old shape does about it. `Verification plan` names the lane that goes red if the decision is silently regressed, and "none, because …" is a valid entry. A leaf decision may use them and usually should not. These are template obligations, not lint obligations: doclint DL010 still enforces presence of the five minimum sections only, so ADRs written before this rule stay green and are not worth retrofitting.
    - **Sub-items and their done-ness:** when a decision decomposes into parts — subsidiary design decisions (`SD`), milestones (`M`), phases, steps, cuts — declare each as a **marker, an em-dash, and a title**, in either shape. The em-dash is what makes it a declaration rather than prose that mentions a marker; reserve the en-dash for ranges (`Phase 0–1`). Mark one done with a `✓` immediately after its title text — the end of the heading, or just past the closing `**`. Done-ness is binary and is the one thing the reader cannot derive: a sub-item's *existence* is surveyed from the body, and code citing its `§marker` is surveyed too, but many subsidiary decisions (an IP boundary, a performance posture) will never have code to cite — and milestones are never `§`-pinned at all — so nothing but the author can say a sub-item is finished. `boxer adr` reads these into the `subtask` table — as does `keelson('subtask')`, the same rows served in-process — and play's Kanban pane shows each ADR's sub-items as declared-done / cited-but-undeclared / neither ([ADR-0092](./adr/0092-adr-overview-tool.md#updates), [ADR-0122](./adr/0122-play-kanban-panel.md)); the middle bucket is the worklist of sub-items worth a `✓`. Note that a `✓` in a heading changes that heading's anchor slug.

      ```markdown
      ### SD3 — Subject taxonomy ✓

      - **SD1 — Provider registry + interface.** ✓ A `TableProvider` declares…
      ```

#### Editing ADRs: three tiers

ADRs evolve. Three tiers of change keep the body load-bearing and the audit trail where readers look.

**Tier 1 — Edit in place.** No dated entry, no new section. `git log` is the trail.

- Value tweaks inside an existing table or constant (`0.20 → 0.24`, `MinIdle = 2 → 3`).
- File-path / import-path / symbol-rename sweeps when the decision itself is unchanged.
- Typos, clarifying re-phrasings, broken-link repairs.
- Filling in a `TODO` or `Empty initially; results land here` placeholder when no design pivot accompanies the fill.

The commit message carries the rationale; the reader sees the corrected value first.

**Tier 2 — Append a dated entry to `## Updates`.** A single `## Updates` H2 (penultimate, before `## References`), with dated H3 entries inside it.

- Implementation revealed a constraint the design missed; the design changed to accommodate it.
- A new alternative surfaced after the original `Alternatives` section was written, with the reason it was (re-)rejected.
- An aspirational claim in the original body turned out partially false, and the entry corrects it.
- A milestone landed and the entry records what shipped vs. what was scoped, including in-flight contract refinements.

If a `## Updates` H2 already exists, add an H3 inside it — never a second `## Updates` H2.

**Tier 3 — Issue a new ADR that supersedes this one.** Flip the original's `status: superseded`, add a one-line pointer at the top of the body to the superseding ADR, and write the current state fresh in the new ADR.

- The chosen option changed (you picked O1, now you're picking O2).
- Scope changed materially (the ADR covered X; the new state covers X + Y, or X minus a major sub-decision).
- A new reader can no longer reach the current truth by reading the body alone — body and `Updates` chain disagree on substance, not just numbers.

Supersession is cheap. Prefer it over an `Updates` chain that has started to describe a different decision than the body.

#### When to flip `proposed → accepted`

`proposed` is for a decision the author wants reviewed before it is treated as in force. `accepted` is the steady state. A single code owner reading the ADR once and filling in `reviewed-by` + `reviewed-date` is the bar — review covers the body as it stands at the flip, not every future Tier 1 / Tier 2 change. Subsequent edits do not re-open the question and do not reset the status. A Tier 3 change later is a new ADR, not a re-review.

If ADRs accumulate in `proposed` indefinitely, the bar is being misread. Flip them.

`scripts/dev/adr-accept.sh <number|path>` does the mechanical part of the flip — front-matter status, `reviewed-by` / `reviewed-date`, the banner, and the leading sentence of `## Status` — then runs doclint. It leaves the rest of the `## Status` prose alone and prints it, because text written against a pending decision ("awaiting review by …") usually needs a human edit afterwards.

---

## 2. Directory Layout

Most packages need only code plus a `doc.go`. The Markdown artifacts are added as the package accumulates companion docs. A minimal leaf package:

```text
public/fec/ea/golay24/
├── golay24.go           # Code + Reference (doc comments)
└── doc.go               # Reference (package overview)
```

A fully documented, complex package with all quadrants represented:

```text
public/fec/ea/golay24/
├── golay24.go           # Code + Reference (doc comments)
├── doc.go               # Reference (package overview, cross-links to EXPLANATION and ADRs)
├── example_test.go      # How-To (executable recipes)
├── EXPLANATION.md       # Explanation (theory, math, memory layout)
└── README.md            # Optional package overview — see §3
```

Repository-wide artifacts:

```text
doc/
├── DOCUMENTATION_STANDARD.md   # this file
├── ARCHITECTURE.md             # system-level Explanation, cross-package
├── adr/                        # all decision records
│   ├── 0001-adopt-diataxis.md
│   └── ....md
├── templates/                  # canonical skeletons (see §9)
└── (tutorials may live here, e.g. GETTING_STARTED.md)
```

---

## 3. Package README.md (optional)

A package-level `README.md` is **optional**. When present, it is a normal Diátaxis artifact — pick the quadrant (almost always `reference`) and follow the rules in §4. There is no dedicated "router" type: a README of nothing but links duplicates `doc.go` and `pkgsite` without adding understanding, and it rots whenever any linked artifact moves.

**When to add one.** GitHub renders `README.md` by default when a reader navigates into a package directory, so packages with enough surface to warrant a landing page (top-level modules, subsystems worth onboarding contributors into) benefit from a substantive overview. Leaf packages already served by `doc.go` do not need a README.

**What goes in it.** Prose that is genuinely package-scoped: what the package is, the moving parts, how pieces fit together, and pointers into the companion artifacts (`EXPLANATION.md`, `example_test.go`, ADRs, `pkgsite`). Treat it like `doc.go` with Markdown affordances — tables, trees, and fenced code — not as a list of links.

**The repository-root `README.md` is exempt from the front-matter requirement.** It is the GitHub landing page for the whole project, not a Diátaxis artifact, and the badges in its first heading would render poorly below a YAML stanza.

All in-repo links inside a README must use fully qualified repo paths so they render correctly on both GitHub and local clones (see §7).

---

## 4. Writing Rules

### Voice and tone

Prose that *frames* the project — the repository `README`, package `README` overviews, the `Context`/motivation of an ADR, `doc/changelog/` entries, and commit messages — defaults to a descriptive, subtractive register. Describe what something is and how to use it; leave the reader to judge it.

- **Describe, don't assert.** State what a thing does, not how the reader should feel about it. "A ring buffer that windows the last N samples" carries more than "an elegant, high-performance buffer."
- **Cut self-praise adjectives.** Words like *focused*, *loosely-coupled*, *production-grade*, *battle-tested*, *polished*, *elegant*, *modern* name a reaction, not the artifact. If a word can be removed without changing the meaning, remove it.
- **Drop performance and quality claims** unless a reader needs the number to make a decision *and* it is independently verifiable. "Zero-allocation hot path" is a benchmark assertion — cite the benchmark or omit it.
- **Disclose provenance; don't claim equivalence.** For process notes — AI-assisted codegen, vendored ports — state the fact and stop. "Generated by X, gated behind build tag Y" is a fact; "held to the same standard as hand-written code" is a claim the reader cannot check, and it reads as defensive.
- **Lead with caveats.** Put *Maturity* / *Stability* notes above *Installation* / *Quickstart*, so a reader learns what is unfinished before they learn how to adopt it.
- **No taglines or manifestos.** Omit slogans and `Goals`/mission sections until the project is ready to state and defend its larger claims publicly. A smaller proxy claim made in their place misdirects readers about why the work exists — which costs more trust than saying nothing.
- **Prefer omission to overstatement.** A doc that under-tells is recoverable by adding to it later; one that overstates has to be walked back. When unsure whether a claim is earned, leave it out.
- **Don't overcorrect into modesty.** "Nothing special here" is also a claim, and a distracting one. The target is descriptive neutrality, not self-deprecation.

**Where this does not apply.** This governs prose that *frames* the project, not prose that *documents* it. An ADR's `Decision` section is meant to be definite — state the decision plainly. Reference documentation of factual behavior (Go doc comments, API descriptions) describes what the code does and needs no hedging. Internal design notes and scratch files are exempt. Voice and tone is a judgment call, not a mechanically enforced rule (§8).

### Claims that decay

Documents here carry two kinds of statement in one voice. A **durable** claim — the decision, the constraint, the reason an option was rejected — stays true as the code moves. A **perishable** claim — the current shape of a package, a count, a measurement — does not. Both are worth writing, and the detail that makes a document navigable is mostly of the second kind.

The failure is not that perishable claims exist. It is that a reader cannot tell which is which, so a perishable claim that has gone false still reads with the authority of the durable claim beside it. These rules make the difference legible. None of them asks for fewer *durable* claims — a decision stripped of its reasons is not more accurate, only less useful. How many perishable claims to keep at all is the question of *What to leave out*, below.

- **Reference code in a form that can be contradicted.** A file path is a Markdown link (§7), not a bare backticked path — a link is checked, and a reader can follow it. Name a symbol by its exact identifier, so a search either resolves it or shows that it is gone. A reference nothing can check is one nothing will correct.
- **A quantitative claim is dated or it is live.** A number recording evidence, a measurement, or a past state carries its date and, where a reader would need it to repeat the work, its method — "(2026-08-10, same method, 812 columns)". A number with no date is a claim about the tree as it stands and must be true there. Thresholds and estimates ("~25–30 files", "roughly 200 LOC") are neither; the tilde already says so.
- **Never refresh a frozen number.** Evidence gathered when a decision was taken — the size of a dependency that was surveyed, the cost of the shape being replaced — is the record of why the choice was defensible. Updating it rewrites that record. A newer measurement is a new dated entry beside the old one, never an edit to it. This holds with particular force for figures attached to *rejected* alternatives, which describe a world the project deliberately did not enter.
- **Don't transcribe what a mechanism can serve.** Field lists, signatures, enum members, and call-site inventories reproduce something that already has a single source of truth, with nothing binding the copy to the original. Link the source or the generated artifact instead. Where the shape *is* the decision — a wire format, a schema contract — reproduce it, put it under `Surfaces`, and give it a `Verification plan` entry so a silent divergence turns a lane red.
- **An exclusivity claim names its guard or its date.** "X is the sole client of Y", "nothing else writes this table" — claims of this shape carry decisions, and they go false the moment someone adds the second case, without touching the document. Name the lint, test, or registry that keeps the claim true, or date it as an observation and let it read as one.
- **Prefer the phrasing something can check.** Where a sentence could describe a behaviour or point at the mechanism that enforces it, point at the mechanism. Prose that no tool, test, or lane can contradict is precisely the prose that decays unobserved.

### What to leave out

A document carries a decision and what justifies it; the tree carries everything else, and the tree is what a reader trusts when the two disagree. Most of the length in a long ADR or how-to describes the tree as it stood while the author was looking at it — accurate that day, unverifiable a month later, written in the same voice as the decision beside it. Two tests, per sentence:

- **Could a reader regenerate it from the tree?** Link the source and cut the copy (§5 *What a model cannot supply*).
- **Would it go false after a refactor that does not revisit the decision?** Then it describes the environment, not the decision. Anchor it to what a refactor preserves — a symbol name, an `ADR-NNNN` marker, a registry key, a test name — or date it as an observation, or drop it. Line numbers, counts, orderings, and slugged paths are not preserved.

The sentences that fail both come in a few recurring shapes:

- **Source locations by line number.** `statemanagement.go:244` is stale on the next edit to that file and nothing flags it. Name the symbol and link the file. (2026-08-27: 291 such references under `doc/`.)
- **Inventories of the current state** — call-site lists, "the twelve packages that import X", file-by-file walkthroughs. Where the inventory *is* the decision it belongs under `Surfaces` with a `Verification plan` entry; elsewhere it is a copy nothing binds to the original.
- **The route to the decision.** "We first tried A, then noticed B, so…" — the record wants A's kill-reason, not the itinerary. What was asked and what a session discussed are exploration; compact it away, keep the reasons.
- **Implementation scaffolding.** The order of edits, the files each step touches, pasted test output. A milestone is a declared sub-item (§1) — marker, title, `✓` — and what shipped under it is a Tier 2 `Updates` entry; the *how* lives in the commits, and the `Verification plan` names the lane rather than quoting its output.
- **Undated time words.** *Today*, *currently*, *for now*, *not yet*, *will be* — true at the keyboard, silently false later. Rewrite as a durable statement, or date it.
- **Versions that are not the decision.** A toolchain version pinned in passing ages with the toolchain. Name the feature relied upon; pin a version only where the version *is* the decision (ADR-0199 is one).
- **Code that copies the implementation.** A fence shows a *shape* — an interface, a wire layout, an API before and after — and stays true while the shape does. A fence that must change when the body is refactored is a copy of the body.

What remains is shorter than a diligent author, or a model, produces by default: the Decision, the Alternatives with their kill-reasons, the Consequences, and the Context a reader a year out needs to see why the question arose. When a section wants to grow past that, what is growing is almost always the description of the tree.

### Go doc comments
- Follow [go.dev/doc/comment](https://go.dev/doc/comment) in full. Short summary first, begins with the identifier's name, ends with a period.
- Use doc links (`[pkg.Symbol]`, `[Type.Method]`) for cross-references so `pkgsite` can resolve them. Avoid bare URLs when a doc link will do.
- `BUG(who):` and `Deprecated:` paragraphs follow their established conventions — treat them as the canonical spelling.
- **A comment earns its place by carrying what the code cannot** — a constraint on callers, the reason for this shape rather than another, a hazard, a cost, a cross-reference, or what was tried before and did not work. Where there is none of that, the signature is the documentation, and a comment restating it adds a second thing to keep true without adding a second thing to know. The bar is content, not coverage.
- **A comment that references outside its own package is documentation** and follows *Claims that decay* above. Inside a package, proximity keeps a comment honest: whoever edits the code has the comment in front of them. That stops at the package boundary. A comment naming another package's symbols, a file elsewhere in the tree, or an ADR is prose about something its reader is not editing, and it decays like any other prose.

### Cross-linking between Go and Markdown
- **From a doc comment to a nearby Markdown file:** reference it by name, e.g., `See EXPLANATION.md for the derivation.` `pkgsite` will not render the link, but humans reading the source find it, and it survives file moves within the package.
- **From Markdown to a Go symbol:** prefer a `pkg.go.dev` URL for the *documentation* and a repo path for the *source*. Never link a bare directory name.

### Front-matter and document state (Markdown only)

Every Markdown doc begins with a YAML front-matter stanza. Go files are exempt — their role is obvious from their extension and location. The repository-root `README.md` is also exempt (see §3).

```yaml
---
type: explanation            # reference | how-to | explanation | tutorial | adr
audience: package maintainer # who should get value from this
status: stable               # see state machine below
reviewed-by: "@alice"        # required when status is stable or accepted
reviewed-date: 2026-04-16    # required when status is stable or accepted
---
```

**State machine — descriptive docs** (README, EXPLANATION, HOWTO, TUTORIAL):

| State | Meaning | Requirements |
|---|---|---|
| `draft` | Pre-human-review. Not authoritative. | Must display the draft banner (below). Default for new docs. |
| `stable` | Reviewed and approved by a code owner. Authoritative. | `reviewed-by` + `reviewed-date` required. Banner removed. |
| `deprecated` | Still accurate, but the subject is going away. | Must name a successor. |
| `superseded` | Replaced by another doc. | Must link to the replacement. |

**State machine — ADRs:**

| State | Meaning | Requirements |
|---|---|---|
| `proposed` | Pre-human-review; decision not yet in force. | Must display the draft banner. |
| `accepted` | Decision is active. Subsequent Tier 1/2 edits do not reset the review. | `reviewed-by` + `reviewed-date` required. Banner removed. |
| `deferred` | Decision shape is settled, but implementation is intentionally postponed pending a future trigger. | Must name the trigger in the `## Status` section (the condition that, when met, moves the ADR to `accepted` or motivates a successor). |
| `deprecated` | Decision no longer in force; no successor. | |
| `superseded` | Replaced by a later ADR. | Must link to the superseding ADR. |
| `withdrawn` | Proposal retracted before it was ever accepted or implemented. Distinct from `deprecated`, which implies prior adoption. | Kept under the append-only convention as a record of the option; the `## Status` section states why it was withdrawn. May carry a `withdrawn-date` in front-matter. |

**Draft banner.** Docs in `draft` or `proposed` state must display a banner immediately after the front-matter so readers browsing on GitHub or in IDE previews do not mistake them for authoritative content. Use exactly one of these forms so CI can detect it:

```markdown
> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
```

```markdown
> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.
```

Remove the banner when status flips to `stable` / `accepted`.

**Why state lives in front-matter, not the filename.** Filenames like `README.draft.md` were considered and rejected: they break tooling that expects canonical names (GitHub's `README.md` rendering, Go's `doc.go`, `pkgsite` conventions), they force a rename on every status change (fragmenting `git log`, breaking inbound links, and noisifying diffs), and they are incompatible with ADRs, which transition through multiple states while needing a stable URL. Front-matter plus the draft banner is the single mechanism; there is no parallel filename convention. Drafts — AI-drafted or in-progress — coexist with stable docs on `main` and are distinguished by `status` and the banner alone.

### When in doubt, pick the quadrant
- "I'm describing what the thing *is*." → **Reference**.
- "I'm showing how to do *one task*." → **How-To**.
- "I'm explaining *why* it is shaped this way, in terms that will still hold in five years." → **Explanation**.
- "I'm teaching a newcomer from zero to working code." → **Tutorial**.
- "We had to pick among N options and picked one." → **ADR**.

---

## 5. AI-Assisted Documentation

LLMs (Claude, GPT, Copilot) are well-suited to the administrative parts of documentation — summarizing git histories, reformatting notes, synthesizing a first draft from a discussion thread. We encourage their use, subject to the rules below. They are **not** a substitute for understanding the code.

### Where AI helps

- **ADR drafts** from an issue, PR thread, or design-chat transcript. Feed the transcript plus [`doc/templates/adr/0000-template.md`](./templates/adr/0000-template.md).
- **How-To drafts** whose ground truth is a working `example_test.go`.
- **Tutorial prose** once the code path runs end to end.
- **Activity summaries** for historical or retrospective artifacts.

### Where AI is risky

- **Explanation.** LLMs invent invariants, cite non-existent papers, and confidently describe complexity bounds that do not hold. Every claim in an `EXPLANATION.md` draft must be verified against the code or an authoritative source.
- **Reference.** LLMs hallucinate exported symbols, parameter orders, and doc-link targets. After any AI edit to Go doc comments, run `go build ./...` and preview with `pkgsite` or `go doc` to confirm every `[pkg.Symbol]` link resolves.
- **Decisions the model did not witness.** An ADR drafted from thin context will invent alternatives that were never considered and consequences that do not apply. Only draft ADRs when the model has access to the actual source material.

### What a model cannot supply

A model reading the code can produce an accurate description of what the code does. That description is also the passage most likely to be wrong in six months, and the one a reader can recover at any time — by reading the code, or by asking a model to read it.

What no model can supply is what was never in the code: the alternatives that were rejected, the constraint a caller must honour that is invisible at the definition, the failure that motivated a workaround, a measurement taken on a day that has passed. The code contains the surviving option and nothing about the others.

Spend the documentation budget there. **When a passage could be regenerated from the code on demand, that is a reason to cut it, not a reason to keep it current.** The corollary is uncomfortable and intended: the documentation worth writing is the documentation a model cannot draft for you, and cannot check afterwards.

A model that has just made a change also knows every file it touched, every command it ran, and every option it weighed on the way, and its default is to write all of it down. The files are the commit, the commands are the `Verification plan`'s lane, and the options that lost are one kill-reason each. §4 *What to leave out* lists the shapes this takes and what to anchor each to.

### Prompt hygiene

Include at minimum:

1. The relevant template from [`doc/templates/`](./templates/).
2. This standard (or the section relevant to the task).
3. The ground-truth source: code, transcript, or commit range.

Do not ask an LLM to write documentation "about package X" from memory. It will make things up.

### Human-in-the-loop

AI-generated documents are drafts. The author committing the doc assumes full responsibility for its factual accuracy — the same accountability whether the draft came from an LLM or was hand-written. New AI-drafted docs enter the state machine at `draft` (or `proposed` for ADRs) and must carry the draft banner; flip to `stable` / `accepted` and fill in `reviewed-by` / `reviewed-date` only after verifying:

- No hallucinated symbols, flags, file paths, or imports.
- No invented invariants, complexity bounds, or trade-offs.
- No missed breaking changes or deprecations.
- Every doc link and URL resolves.

The same standard applies whether the draft came from an LLM, a colleague, or a previous version of yourself.

---

## 6. Banned Files

The following are not permitted in package directories. If you find one, migrate its contents according to this standard:

- `SPEC.md`, `DESIGN.md`, `ARCH.md` — split into `EXPLANATION.md` (timeless theory) and/or a new ADR (the choices).
- `TODO.md`, `IDEA.md` — move to the issue tracker. Static files are not for task tracking.
- `NOTES.md`, `MISC.md` — choose a quadrant or delete.

---

## 7. Explicit Linking

Markdown docs must link with fully qualified Go import paths, not bare directory names. Navigability — for `pkgsite`, for readers on GitHub, for contributors jumping across a deeply nested tree, and for any tool that walks the graph — depends on stable, unambiguous references.

- **Bad:** "This relies on leeway."
- **Good:** "This relies on [`github.com/stergiotis/boxer/public/semistructured/leeway`](../public/semistructured/leeway)."

Prefer `pkg.go.dev` URLs when referring to a symbol's *documentation*, and repo paths when referring to the *source*.

---

## 8. Enforcement

A standard without checks erodes. Enforcement has three parts: how checks are invoked, which invariants are mechanically checked and by what, and how state transitions are signed off.

### Orchestration

All checks are invoked through scripts under `./scripts/`. The scripts wrap Go-native tooling — `go test`, `go vet`, `go build`, and `boxer gov doclint` (the repo-local subcommand at [`public/gov/doclint`](../public/gov/doclint)) — so that direct tool invocation stays an implementation detail contributors do not depend on. No Node, Python, Rust, or other external binaries are introduced into the check toolchain.

Contributors run [`scripts/ci/lint.sh`](../scripts/ci/lint.sh) before committing to `main`; CI runs the same script.

### Invariants → enforcer

Every invariant stated in this standard maps to exactly one enforcer. The `Rule` column carries either:

- a `DLNNN` rule ID (implemented in `public/gov/doclint/`),
- a `DLNNN (pending)` ID (planned but not yet wired up),
- a stdlib invocation (`go test`, `go vet`),
- or *manual* (a judgment call that cannot be mechanically checked).

| Invariant | Defined in | Rule |
|---|---|---|
| Every exported symbol carries a doc comment that begins with its identifier name; the summary paragraph (up to the first blank line) ends with `.`, `!`, or `?`. | §1 Reference, §4 | `DL009` (existing comments with wrong form: warn; missing comments: info, baseline cleanup) |
| `Example*` functions carry an `// Output:` / `// Unordered output:` block and match it. | §1 How-To | `go test ./...` |
| Go doc-link targets `[Symbol]` resolve to an exported symbol in the same package. | §4, §5 | `DL008` (qualified `[pkg.Symbol]` and method `[Type.Method]` not yet checked) |
| ADRs contain `Context`, `Decision`, `Alternatives`, `Consequences`, `Status` sections. | §1 ADR | `DL010` |
| ADRs: QOC section is used when ≥3 options × ≥3 criteria. | §1 ADR | *manual* |
| Every `.md` under scoped paths begins with a compliant front-matter stanza. | §4 | `DL001` |
| `type` is in the allowed enum (reference / how-to / explanation / tutorial / adr). | §4 | `DL001` |
| `status` is in the allowed enum for the doc's `type`. | §4 | `DL001` |
| `reviewed-by` + `reviewed-date` present when `status` is `stable` / `accepted`; date parses as `YYYY-MM-DD`. | §4 | `DL003` |
| Draft banner present iff `status` is `draft` / `proposed`; banner state matches front-matter status. | §4 | `DL004` |
| Banned filenames (`SPEC.md`, `DESIGN.md`, `ARCH.md`, `NOTES.md`, `MISC.md`, `TODO.md`, `IDEA.md`, `IDEAS.md`) do not appear in package directories. | §6 | `DL005` |
| Cross-package Markdown references use fully qualified Go import paths, not bare directory names. | §7 | `DL006` |
| Every in-repo Markdown link resolves to an existing file that git tracks. A git-ignored target counts as missing: it resolves in a working checkout and in no clean one. | §7 | `DL007` (anchor existence not yet checked) |
| Open set of `status: draft` / `status: proposed` docs reported (informational, not a merge block). | §4 | `DL011` |
| Markdown references a source file by link, not by bare backticked path. | §4 *Claims that decay*, §7 | `DL012 (pending)` |
| Identifiers named in Markdown resolve to a symbol in the tree. Docs in `draft` / `proposed` are exempt: they describe symbols that do not exist yet, which is their purpose. | §4 *Claims that decay* | `DL013 (pending)` |
| ADR citations — in Markdown and in Go comments — resolve to an existing ADR, and cite by number rather than filename. | §1 ADR | `DL014 (pending)` |
| Quantitative claims are dated, or true against the current tree. | §4 *Claims that decay* | *manual* |
| Exclusivity claims name a guard or a date. | §4 *Claims that decay* | *manual* |
| Markdown does not pin a source location by line number (`path.go:NNN`); it names the symbol and links the file. | §4 *What to leave out* | `DL016 (pending)` |
| Prose records the decision; the state of the tree is linked, not transcribed — no call-site inventories, implementation walkthroughs, or pasted command output. | §4 *What to leave out* | *manual* |
| Time words (*today*, *currently*, *not yet*) carry a date or are rewritten as durable statements. | §4 *What to leave out* | *manual* |
| A doc comment carries something the signature does not. | §1 Reference, §4 | *manual* |
| Cross-package references inside Go comments follow the Markdown reference rules. | §4 | *manual* (doclint walks Markdown only) |

Rules not in the table are either process guidance (e.g., "use AI for drafts") or judgment calls (e.g., quadrant selection) and are not mechanically enforceable.

Several invariants above are marked *manual* because they turn on intent that no checker can read — whether a number is evidence or a live claim, whether a comment adds anything. That is a real limit, not a placeholder: the rules still hold, and a reviewer applies them. Where a check *is* possible, prefer wiring it, because the measurable difference between a checked reference and an unchecked one is not authorial care but whether anything ever contradicts the sentence.

---

## 9. Templates

Canonical skeletons live under `doc/templates/`:

- [`doc/templates/doc.go.tmpl`](./templates/doc.go.tmpl)
- [`doc/templates/EXPLANATION.md.tmpl`](./templates/EXPLANATION.md.tmpl)
- [`doc/templates/TUTORIAL.md.tmpl`](./templates/TUTORIAL.md.tmpl)
- [`doc/templates/HOWTO.md.tmpl`](./templates/HOWTO.md.tmpl)
- [`doc/templates/adr/0000-template.md`](./templates/adr/0000-template.md)
- [`doc/templates/trial/README.md.tmpl`](./templates/trial/README.md.tmpl) (trial
  protocol; see [doc/trials/](./trials/README.md))
- [`doc/templates/trial/logbook.md.tmpl`](./templates/trial/logbook.md.tmpl)

For reference, the ADR skeleton is:

```markdown
---
type: adr
status: proposed
date: YYYY-MM-DD
---

# ADR-NNNN: <short decision title>

## Context
What forces are at play? What constraints, incidents, or requirements prompted this decision?

## Decision
The choice we are making, stated in one or two sentences.

## Alternatives
Other options considered, with one sentence on why each was rejected.

## Consequences
What becomes easier, harder, or locked in by this decision. Include migration notes where relevant.
```

When starting a new doc, copy the matching template rather than writing from scratch.
