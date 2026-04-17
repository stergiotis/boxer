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
- **Format:** `doc/adr/NNNN-kebab-title.md`, monotonic numbering. ADRs are **append-only**; superseded records are marked, not deleted.
- **Rules:**
    - One decision per file, one file per decision.
    - Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
    - If a decision affects a specific package, link to it from that package's `README.md` and, where relevant, its `EXPLANATION.md`.
    - Minimum sections: *Context*, *Decision*, *Alternatives*, *Consequences*, *Status*.
    - **Design-space analysis (QOC):** when a decision has ≥3 viable options evaluated against ≥3 explicit criteria, use the optional `Design space (QOC)` section from the ADR template — Questions, Options, Criteria (MacLean, Bellotti, Young, Moran, 1991). The prose `Alternatives` section may then cross-reference the QOC matrix instead of duplicating the rationale. Below the threshold, a prose `Alternatives` list is sufficient.

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
└── README.md            # Optional router — see §3
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

## 3. README routers (optional)

A `README.md` that routes between the four Diátaxis artifacts is **optional**, not required. Use one only when a package has enough companion docs that routing adds value.

**When to add a router.** Rule of thumb: the package contains **two or more** of `EXPLANATION.md`, `example_test.go` (or `HOWTO.md`), and a `TUTORIAL.md` that points back into this package. For small leaf packages with just `doc.go` + code, the package overview already serves as the single entry point — a README would be noise.

**When you do add one, it has one job: routing.** No prose bodies; that is what the other artifacts are for. A router template:

```markdown
---
type: router
package: github.com/stergiotis/boxer/public/fec/ea/golay24
---

# golay24

One-sentence description of what the package does.

- **Reference:** https://pkg.go.dev/github.com/stergiotis/boxer/public/fec/ea/golay24
- **How-To:** see [`example_test.go`](./example_test.go)
- **Explanation:** [`EXPLANATION.md`](./EXPLANATION.md)
- **Tutorials:** [`public/imzero/TUTORIAL.md`](../../../imzero/TUTORIAL.md)  *(if applicable)*
- **Decisions:** [ADR-0007](../../../../doc/adr/0007-fec-code-selection.md)
```

Routers must use fully qualified repo paths for in-repo links and render correctly on both GitHub and local clones.

---

## 4. Writing Rules

### Go doc comments
- Follow [go.dev/doc/comment](https://go.dev/doc/comment) in full. Short summary first, begins with the identifier's name, ends with a period.
- Use doc links (`[pkg.Symbol]`, `[Type.Method]`) for cross-references so `pkgsite` can resolve them. Avoid bare URLs when a doc link will do.
- `BUG(who):` and `Deprecated:` paragraphs follow their established conventions — treat them as the canonical spelling.

### Cross-linking between Go and Markdown
- **From a doc comment to a nearby Markdown file:** reference it by name, e.g., `See EXPLANATION.md for the derivation.` `pkgsite` will not render the link, but humans reading the source find it, and it survives file moves within the package.
- **From Markdown to a Go symbol:** prefer a `pkg.go.dev` URL for the *documentation* and a repo path for the *source*. Never link a bare directory name.

### Front-matter and document state (Markdown only)

Every Markdown doc begins with a YAML front-matter stanza. Go files are exempt — their role is obvious from their extension and location.

```yaml
---
type: explanation            # reference | how-to | explanation | tutorial | adr | router
audience: package maintainer # who should get value from this
status: stable               # see state machine below
reviewed-by: "@alice"        # required when status is stable or accepted
reviewed-date: 2026-04-16    # required when status is stable or accepted
---
```

**State machine — descriptive docs** (router, EXPLANATION, HOWTO, TUTORIAL):

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
| `accepted` | Decision is active. | `reviewed-by` + `reviewed-date` required. Banner removed. |
| `deprecated` | Decision no longer in force; no successor. | |
| `superseded` | Replaced by a later ADR. | Must link to the superseding ADR. |

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
| Every exported symbol carries a doc comment that begins with its identifier name and ends with a period. | §1 Reference, §4 | `DL009` (pending) |
| `Example*` functions carry an `// Output:` / `// Unordered output:` block and match it. | §1 How-To | `go test ./...` |
| Go doc-link targets `[pkg.Symbol]` / `[Type.Method]` resolve to a real symbol. | §4, §5 | `DL008` (pending) |
| ADRs contain `Context`, `Decision`, `Alternatives`, `Consequences`, `Status` sections. | §1 ADR | `DL010` |
| ADRs: QOC section is used when ≥3 options × ≥3 criteria. | §1 ADR | *manual* |
| Every `.md` under scoped paths begins with a compliant front-matter stanza. | §4 | `DL001` |
| `type` is in the allowed enum (reference / how-to / explanation / tutorial / adr / router). | §4 | `DL001` |
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
- `doc/templates/README.router.md.tmpl`
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
