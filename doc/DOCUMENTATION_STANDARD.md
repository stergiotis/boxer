---
type: reference
audience: contributor
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-04-16
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
    - Every exported symbol carries a doc comment that begins with the identifier's name and ends with a period.
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
    - Mutable choices ("we picked Zstd over LZ4 because…") belong in an ADR, not here.
    - Split into an `EXPLANATION/` directory if the file exceeds ~400 lines or covers distinct concerns.

### Tutorials (Learning-Oriented)
> *"Tutorials are lessons that take the reader by the hand through a series of steps to complete a project [...] wholly learning-oriented, and specifically [...] oriented towards learning how rather than learning what."* — [Diátaxis: Tutorials](https://diataxis.fr/tutorials/)

- **Goal:** A lesson that takes a newcomer end-to-end.
- **Format:** `TUTORIAL.md`.
- **Rules:**
    - Tutorials typically cross package boundaries. **Do not bury them in deep sub-packages.**
    - Place them at a top-level module root (e.g., `public/imzero/TUTORIAL.md`) or at the repository root.

### Architecture Decision Records (Why-It-Is-This-Way)
- **Goal:** Capture a single decision with its context, alternatives considered, and consequences. Replaces ad-hoc `DESIGN.md` and internal wiki pages.
- **Format:** `doc/adr/NNNN-kebab-title.md`, monotonic numbering. The decision history lives in git plus an explicit `Updates` section (below); the body text is not byte-sacred.
- **Rules:**
    - One decision per file, one file per decision.
    - Status lifecycle: `proposed → accepted → (deferred | deprecated | superseded by ADR-XXXX)`, or `proposed → withdrawn` when a proposal is retracted before acceptance. Supersession is the documented escape hatch when the decision itself changes (see "Editing ADRs" below).
    - If a decision affects a specific package, link to it from that package's `README.md` and, where relevant, its `EXPLANATION.md`.
    - Minimum sections: *Context*, *Decision*, *Alternatives*, *Consequences*, *Status*.
    - **Design-space analysis (QOC):** when a decision has ≥3 viable options evaluated against ≥3 explicit criteria, use the optional `Design space (QOC)` section from the ADR template — Questions, Options, Criteria (MacLean, Bellotti, Young, Moran, 1991). The prose `Alternatives` section may then cross-reference the QOC matrix instead of duplicating the rationale. Below the threshold, a prose `Alternatives` list is sufficient.
    - **Sub-items and their done-ness:** when a decision decomposes into parts — subsidiary design decisions (`SD`), milestones (`M`), phases, steps, cuts — declare each as a **marker, an em-dash, and a title**, in either shape. The em-dash is what makes it a declaration rather than prose that mentions a marker; reserve the en-dash for ranges (`Phase 0–1`). Mark one done with a `✓` immediately after its title text — the end of the heading, or just past the closing `**`. Done-ness is binary and is the one thing the reader cannot derive: a sub-item's *existence* is surveyed from the body, and code citing its `§marker` is surveyed too, but many subsidiary decisions (an IP boundary, a performance posture) will never have code to cite — and milestones are never `§`-pinned at all — so nothing but the author can say a sub-item is finished. `boxer adr` reads these into the `subtask` table, and the `adrboard` app shows each ADR's sub-items as declared-done / cited-but-undeclared / neither ([ADR-0092](./adr/0092-adr-overview-tool.md#updates)); the middle bucket is the worklist of sub-items worth a `✓`. Note that a `✓` in a heading changes that heading's anchor slug.

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

### Go doc comments
- Follow [go.dev/doc/comment](https://go.dev/doc/comment) in full. Short summary first, begins with the identifier's name, ends with a period.
- Use doc links (`[pkg.Symbol]`, `[Type.Method]`) for cross-references so `pkgsite` can resolve them. Avoid bare URLs when a doc link will do.
- `BUG(who):` and `Deprecated:` paragraphs follow their established conventions — treat them as the canonical spelling.

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
| Every in-repo Markdown link resolves to an existing file. | §7 | `DL007` (anchor existence not yet checked) |
| Open set of `status: draft` / `status: proposed` docs reported (informational, not a merge block). | §4 | `DL011` |

Rules not in the table are either process guidance (e.g., "use AI for drafts") or judgment calls (e.g., quadrant selection) and are not mechanically enforceable.

---

## 9. Templates

Canonical skeletons live under `doc/templates/`:

- `doc/templates/doc.go.tmpl`
- `doc/templates/EXPLANATION.md.tmpl`
- `doc/templates/TUTORIAL.md.tmpl`
- `doc/templates/HOWTO.md.tmpl`
- `doc/templates/adr/0000-template.md`

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
