---
type: adr
status: proposed
date: 2026-11-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0400: Image operations — declared operators that stream a new image into a tiled store and record what they did

## Context

[ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md)
§SD9 fixed three rules for an operation that computes new samples, "ahead of
the pipeline that will carry such operations": it makes a new plane, it does
not read a display recipe, and its parameters are declared once. It deferred
the pipeline itself — "operators with a declared input region, scale
behaviour, border rule and output kind, pulled by the view and by export" —
to an ADR of its own. This is that ADR, for the engine; the app and the
viewer follow it.

The systems that run image operations lazily are well documented, and so is
what lazy evaluation at a reduced scale costs.

- **Pull by region.** libvips evaluates only what a sink asks for, in
  strips, through operators that state the input region an output region
  needs ([how it works](https://www.libvips.org/API/current/how-it-works.html)).
- **Scale is not free.** An operator evaluated on a reduced level equals the
  reduction of its full-resolution result only when it commutes with the
  reduction. A threshold, a rank filter, a count of connected components and
  any non-linear function of the samples do not, and a tool that previews
  them at screen resolution shows something the export will not contain.
- **The operations are the method.** Guidelines for publishing image
  analyses ask for the workflow, its versions, its settings and its regions
  to be kept as files
  ([Schmied et al. 2024](https://www.nature.com/articles/s41592-023-01987-9)).
  A tool whose operations read ambient state — a current image, a global
  option, the display range — cannot write that file
  ([ImageJ User Guide, Image › Type](https://imagej.net/ij/docs/guide/146-28.html)).

Two properties of this engine narrow the choice. Every image is resident in
memory and bounded (ADR-0374 §SD9, ADR-0398 §SD3). And the engine already
has a way to produce an image progressively and show it while it is
produced: a decoder streams rows into a tiled store, which builds every
reduced level in the same pass and offers each tile when it is final
(ADR-0374 §SD7).

## Design space (QOC)

**Question.** When is an operation's result computed, and at what scale?

**Options.**

- **O1** — pulled per tile at the level in view: the result is a tile
  source, each operator evaluates at the level asked for, and a cache keeps
  tiles.
- **O2** — streamed once at full resolution into a tiled store: the result
  is an image like a decoded one, its levels reduced from its own samples.
- **O3** — computed whole, then cut: the operator returns planes and a store
  is built from them.

**Criteria.**

- **C1** — what is on screen at a reduced level equals the reduction of the
  result, for every operator.
- **C2** — time to the first pixels, and the cost of a view that shows a
  small part.
- **C3** — peak memory beyond the result itself.
- **C4** — what an operator has to declare, and how many ways it can be
  wrong.
- **C5** — what the engine already has.
- **C6** — a path to images larger than memory.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | ++ | ++ |
| C2 | ++ | +  | −− |
| C3 | ++ | +  | −  |
| C4 | −− | +  | ++ |
| C5 | −  | ++ | +  |
| C6 | ++ | −  | −− |

O1 is the general design and the one to grow into. It needs each operator
classified by how it behaves under reduction, a test per operator that the
classification is true, and a visible mark on every preview that is not the
result. With every image resident, O2 gives the same result as an export
would, by construction, at the cost of computing parts nobody looks at.

## Decision

We will take O2. An operation is a declared operator applied to named
inputs with stated parameters; it runs once, at full resolution, as a
cancellable task that streams bands of rows into a tiled store, and its
result is an image like any other — with levels, a digest, and a provenance
that is the record of the operation. Nothing is evaluated at a reduced
level.

### Subsidiary design decisions

- **SD1 — An operator is declared once.** An operator has an identifier, a
  version, and a list of parameters, each with a name, a type, a unit where
  it has one, a range and a default. A dialog, an app operation's arguments,
  the help text and the record are all read from that declaration. An
  operator reads its inputs and its parameters and nothing else: no display
  recipe, no selection, no option held elsewhere.
- **SD2 — Inputs are images on one grid.** An operation's inputs are images,
  or planes taken from a stack, of one size. Inputs on different grids are
  refused; bringing one grid to another is an operation of its own
  (Deferred). An input is named in the record by its digest and, for a stack
  selection, by the positions taken.
- **SD3 — The plan before the samples.** From the inputs' descriptions and
  the parameters an operator states, before reading a sample: the output's
  size, kind, planes and their labels, value transform and invalid value;
  the rows and columns of input it needs around an output band; or why it
  cannot run. A refusal names its reason.
- **SD4 — Values in, values out.** Arithmetic is on values — stored samples
  through their value transform — and its result is a float plane with the
  identity transform: 32-bit where every input code is exact in 32 bits,
  64-bit otherwise. No arithmetic wraps or saturates. An operator whose
  output can be invalid where its input was not — at a border, or beside an
  invalid sample — also yields float, because the input's kind may have no
  code to spare for it; only an operator that can produce no new invalid
  sample keeps the kind it was given.
- **SD5 — Invalid in, invalid out.** An output sample that depends on an
  invalid input sample is invalid, unless the operator's declaration says it
  leaves invalid samples out, as a mean over an axis does. An arithmetic
  result that is not finite is invalid; an infinite input sample is a valid
  sample, as the engine holds it to be, and what is computed from it is
  judged by the same rule.
- **SD6 — The border is a parameter.** An operator that reads neighbours has
  no hidden rule for the edge of the image: what stands in for samples
  outside it — nothing, so that the affected output is invalid, or the
  nearest sample — is a declared parameter with no default that changes
  numbers silently, and it is in the record.
- **SD7 — Global quantities come from passes of their own.** An operator
  that needs a quantity of the whole image — a mean, a threshold found from
  the data — computes it in one or more passes before any output, and the
  quantity it used is in the record. Run again on the same inputs it finds the same
  quantity: reductions have a fixed order.
- **SD8 — The record.** A result's provenance is the operation: operator
  identifier and version, parameters as given and as resolved to pixels,
  each input's digest together with its value transforms and its declared
  invalid value — which the digest leaves out and the values depend on — the
  first-pass quantities, and the result's digest. The
  record is a value that serialises to a versioned JSON document, and a
  result whose inputs are themselves results carries their records, so the
  chain back to files is in the result. A request's identity — operator,
  version, parameters, and the inputs so identified — is known before it runs.
- **SD9 — Streaming and cancelling.** An operation produces its output in
  bands of rows, top to bottom, into the store the viewer may already be
  showing; it honours cancellation between bands; a cancelled or failed
  operation leaves no result.
- **SD10 — The first operators.** An expression over named inputs —
  arithmetic, comparisons, a small set of functions, a conditional — which
  covers band ratios, indices, differences and thresholds without code that
  runs in the host; a mean, median, minimum, maximum, standard deviation or
  count of valid samples across the planes it is given, which covers a
  composite over time or a stack of exposures; a Gaussian smoothing and a
  median filter, which exercise the neighbourhood and the border; and a
  standardisation by the image's own mean and standard deviation, which
  exercises the first pass.

### Milestones

- **M1** ✓ — the operator declaration, the plan, the runner into a tiled
  store, the record and its JSON form, and the five operators of SD10, in
  [`imaging/ops`](../../public/science/imaging/ops/doc.go). All five yield
  float. The store is made after the first passes, so nothing shows while
  they run. Measured informally on one machine (2026-11-21), each operator
  over a 4096 × 4096 16-bit image took between 0.3 and 1 s with the store
  and its levels, of which building a store of that size alone was 0.3 s.
- **M2** ✓ — the app runs operations from its catalog and from a panel built
  from the declarations; a result opens as an image and names its record. A
  window holds the opened image and its results, one shown at a time; a
  result is kept until it is dropped, under a stated bound on the bytes of
  results, and a run that would pass the bound is refused naming the remedy.
  One run at a time per window, cancellable, leaving nothing when cancelled.
  Progress counts the first passes as well as the output, though the store
  still appears only after them. Results and records last as long as the
  window unless a record is saved.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `public/science/imaging/ops` | added | its tests; the package-properties table; the image engine how-to |
| Operation record (JSON) | added, versioned | a round-trip test and a golden per operator |
| Expression grammar | added | a grammar test; the app's help book when it is offered there |
| App operations of `apps/imagescope` | with M2 | its help book and scenes |

## Alternatives

- **O1, pulled per tile at the level in view.** Not taken now, on C1 and C4:
  it needs each operator's behaviour under reduction declared and tested,
  and previews marked where they differ from the result. It is the way to
  images larger than memory, and SD3's plan — the input an output region
  needs — is the part of it decided here.
- **O3, computed whole.** Rejected on C2 and C3: nothing shows until
  everything is done, and the planes exist once outside the store and once
  in it.
- **Operators that write into an image.** Rejected by ADR-0373 §SD2: a plane
  is not modified.
- **A scripting language for expressions.** Rejected: a general interpreter
  in the host is a trust boundary for what a per-pixel formula needs. The
  grammar is closed, and a program that wants more composes operations.
- **Output in the input's kind, with saturation.** Rejected as the default:
  a difference of two unsigned images that saturates at zero is the
  best-known way an operation loses half its answer.
- **Display limits as a parameter source** ("apply the stretch"). Rejected:
  it is the dependency ADR-0373's first lesson is about. A rescale to
  stated limits is an expression with the limits in the record.

## Consequences

### Positive

- What is shown at any zoom is the reduction of the result, and an export
  writes the same samples.
- A result can be traced to files and repeated from its record.
- An operator is small: a plan and a function over bands.
- The viewer needs nothing new to show a result as it is computed.

### Negative

- An operation computes the whole image even when a corner is in view, and
  a parameter dragged in a dialog recomputes it. A quick look at one
  setting on a large image is slower than in tools that preview at screen
  resolution.
- Every result is resident, with its levels, until it is dropped; a chain of
  operations holds each link unless the caller releases it.
- Float output doubles or quadruples the memory of an 8- or 16-bit input.

### Neutral

- An operator that could run on a reduced level exactly — a mean of means —
  does not; there is one path.
- The expression grammar is one more small language in the tree.

## Verification plan — Tier 1

- **Lane.** Default `go test`: each operator against a direct evaluation
  written in the test, over every input kind, with invalid samples and at
  the image's edges under each border rule; a result streamed in bands
  equal, sample for sample, to the same operation computed in one band; the
  record's round trip, and a run from a record reproducing the digest; a
  cancelled run leaving no store; the expression grammar's accepted and
  refused inputs.
- **What would fail.** An operator that reads a recipe or a global; an
  output that depends on the band size; an integer result that wrapped; an
  invalid input sample that became a number; a border treated by a rule the
  record does not state; two runs of one record with different digests.
- **Gap.** Run time is not gated. Determinism is asserted within one build:
  a compiler that fuses a multiplication with an addition gives other last
  bits, so the goldens pin a digest only for the operators that do no such
  arithmetic.

## Deferred

- **Evaluation pulled per tile and per level (O1)**, with operators
  classified by their behaviour under reduction. Trigger: images larger
  than memory, or a measured need to preview faster than a full run.
- **Resampling one grid to another**, and with it operations across images
  of different placements.
- **Operators that produce labels or regions** — thresholding to a mask,
  connected components, detection — which wait for label images.
- **A decorrelation stretch, principal components and other transforms
  across bands**, which fit SD7 and need a decision on their numerics.
- **Operators that call a model or run outside the process.** They are
  operators with a model's identity and version in the record.
- **A cache of results by request identity** beyond what a caller keeps.

## Status

Proposed — awaiting review by the repository owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md) §SD2, §SD8, §SD9 — planes are not modified, numbers from samples, the three rules.
- [ADR-0374](./0374-imageview-shows-a-tile-source-and-the-decoder-streams-into-one.md) §SD7, §SD9 — the tiled store filled in one pass, and the memory bound.
- [ADR-0398](./0398-images-beyond-a-colour-model-more-sample-kinds-invalid-samples-stacks-a-placement-and-a-seam-for-readers.md) §SD1–§SD3 — sample kinds, invalid samples, the stack.
