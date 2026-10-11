---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0317: Clipboard and export — a labelled broker payload, one host choke point, copy by reference inside boxer, approval tiers for plaintext out, and the screen attributed by watermark

## Context

[ADR-0026](./0026-app-runtime-and-capability-subjects.md)'s 2026-05-30
entry made copy-to-clipboard a brokered subject, `clipboard.write`, write-only
UTF-8 text with an empty ack, and said the broker is the only builder of the
egui copy opcode. A read of the hosts and the IDL on 2026-10-10
([agent-harness §4.8](../adr-background-work/agent-harness.md)) found: a
broker copy and a native selection copy both reach every host as one
`egui::OutputCommand::CopyText(String)`; the headless host forwards it to the
active session, the browser host sends a one-field `ClipboardData{text}`
frame, and the desktop host hands it to egui-winit, which writes the OS
clipboard outside this tree; the broker's audit record carries app id, bytes
and result and no label or source widget; per-widget selection can be turned
off for `label`, `labelAtoms` and `codeView` and via
`textEdit.interactive(false)` under egui 0.35, but not for `labelWidgetText`,
markdown bodies, Rust-side text edits or Ctrl+C in an editable field, and no
global switch exists in the IDL; egui 0.35's `Plugin::output_hook` runs after
label selection and before any host sees `FullOutput`, would see every
`CopyText` once on all three hosts, and is unused; two callers build the
opcode directly and no lint keeps the broker the only builder.

The threat model ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) §SD6) puts the clipboard, file export and printing in one row:
prevented above the tier policy on org-managed hosts, with copy by reference
inside boxer; detected and recorded on hosts the person administers; the
screen and camera attributed by watermark, never prevented. A spike of the
`watermark` crate ([ADR-0241](./0241-cooperative-image-identification-watermark.md))
over real captures recovered the payload from full frames, tile-sized crops at
any offset and a JPEG re-save, and not at all after an 80 % or 50 % rescale or
at thumbnail size.

## Design space (QOC)

**Question.** Where is a copy of labelled text intercepted?

**Options.**

- **O1 — Per-widget selection off** for widgets that can, nothing else.
- **O2 — A labelled broker payload and a host plugin** on `output_hook` that
  drops copies the broker did not make, under a policy.
- **O3 — OS-level data-loss prevention** outside boxer.
- **O4 — Record only**: audit every copy, prevent none.

**Criteria.**

- **C1 — Covers the three hosts**, the desktop included.
- **C2 — Covers the widgets** that cannot turn selection off.
- **C3 — Daily cost** to the person.
- **C4 — Within the tree**: no dependency on the host operating system.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | ++ | +  | ++ |
| C2 | −− | ++ | ++ | ++ |
| C3 | ++ | +  | −  | ++ |
| C4 | ++ | +  | −− | ++ |

O4 is what hosts the person administers get under the threat model; O2 is
taken for org-managed hosts and uses O1 where a widget allows it.

## Decision

We will route every copy of labelled text through the broker with a
**label and a source** on the payload, intercept every `CopyText` at one
**host plugin** on egui's `output_hook` so that a copy the broker did not
make is dropped when a clipboard policy says so, copy model-touched content
**by reference** inside boxer, apply the deployment's **approval tiers** to
plaintext leaving boxer, record rather than prevent on hosts the person
administers, and mark every frame leaving the host with the cooperative
watermark so that the screen is attributed. The payload, the lint, the
handles and the policy are built; the host plugin and the watermark are
the parts still ahead (SD2, SD5).

### SD1 — The broker payload carries a label and a source

`clipboard.write` grows the label of
[ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)
and the source widget: a labelled payload (text, label, source) beside the
plain text the subject took before, which the broker still accepts as a
raw copy of unknown provenance. The broker answers a labelled write with a
copy record — digest, level, tier, source, whether it went plaintext or as
a handle, under which path, or was refused and why — which the audit keeps
in a bounded ring and the caller journals. A `copy` entry in the journal
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)
§SD2) records each copy of labelled text with its digest, written before
the broker is asked, so a refused journal write refuses the copy.

### SD2 — One choke point, and the broker the only builder

A plugin registered beside the SVG export plugin is to implement
`output_hook` and see every `CopyText` once, on the desktop, headless and
browser hosts alike, dropping under a clipboard policy opcode — the one
Rust change this record needs, with the browser host digest refreshed as
[ADR-0278](./0278-tab-mode-for-downstream-apps.md) §SD5 requires — a
`CopyText` that did not come through the broker; the plugin and the opcode
are deferred (SD6). The lint, CS014, keeps the broker and the host's drain
the only Go users of the opcode's binding, as a call, a function value or
through a dot import; the two direct callers are routed through the broker
(ADR-0310 §SD11 step 10;
[ADR-0026](./0026-app-runtime-and-capability-subjects.md) and
[ADR-0011](./0011-codelint.md), Updates of 2026-10-11). Rendering
label-bearing text non-selectable where the widget allows it, with a
broker-routed copy action beside it, is deferred with the plugin.

### SD3 — Copy by reference inside boxer

Model-touched content copies as an opaque handle, `boxer:clip/<id>` with
a random id that says nothing about the content, that resolves only inside
boxer with its labels (`clipboard.resolve`) and that an outside program
sees as an inert string; the broker holds a bounded number of handles,
oldest first. A paste of a handle restores the labels; an unrecognised
paste enters at integrity level 2 and `CLEAR`. Content at `CLEAR` with an
integrity label only (level 3 text) copies plaintext with a `copy` entry
and no gesture: integrity labels mark what comes back, not what goes out;
text another run's model touched (level 4) copies by reference whatever
the mode.

### SD4 — Plaintext out follows the approval tiers

Plaintext leaving boxer — the clipboard to another program, file export,
printing — is an exit of [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md):
free below the channel's clearance (`BOXER_CELL_CLIPBOARD_CLEARANCE`), a
recorded justification or an organisation approver above it, never at the
top, as the deployment's approval mapping declares. On an org-managed host
(`BOXER_CELL_MANAGED`) the broker enforces: above the clearance, text on
the person's path leaves as a handle the person may resolve inside boxer,
and every other path is refused; a raw, unlabelled write is treated as
content of unknown provenance; the plugin, when it lands, extends the same
policy to copies the broker did not make. On a host the person administers
the same path records and does not prevent, which the threat model calls
hygiene. Export and print as broker subjects follow their first consumer
(SD6). Law **L20**: every clipboard write of labelled text above the
clearance is a handle, a refusal or a recorded exit; integrity-only labels
went plaintext with a record; every `CopyText` the plugin forwarded has a
broker record behind it, a raw copy's included.

### SD5 — The screen is attributed

Every frame or image leaving the host is to carry the `watermark` crate's
payload as a session handle plus a display epoch, issuance journaled as an
`issued` entry; deferred (SD6), the `issued` kind reserved. Law **L21**:
every capture or image the trail shows leaving the host has an earlier
`issued` entry whose payload the file decodes to. The
epoch is what an endorsement names (ADR-0311 §SD3). The spike's result
qualifies the promise: a crop of at least a tile is recovered, a rescale is
not, and a phone photograph is outside the qualified path; ADR-0241's live
path is unqualified and the threat model now relies on it for the live screen,
which is a dated Update on ADR-0241 recording both the scaling result and the
payload layout. Downscaling before sending
([ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md)
§SD7) must happen before the mark is applied, or the mark applied at the sent
size.

### SD6 — Deferred, recorded

- Clipboard read (paste from outside boxer carries no label and enters at
  level 2; a labelled paste channel is not designed).
- Printing and file export as broker subjects of their own; this record
  states the policy, the subjects follow the first consumer.
- A watermark that survives rescaling.
- The `output_hook` plugin and the clipboard policy opcode (SD2); the
  watermark issuance, the `issued` entry and L21 (SD5); non-selectable
  rendering of label-bearing text.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `clipboard.write`, `clipboard.resolve` (ADR-0026) | payload gains label and source; the reply is a copy record; a handle resolves to its payload; the Update of 2026-10-11 | the broker's tests; the widgets' copy buttons |
| the egui2 IDL and the Rust hosts | a clipboard policy opcode; an `output_hook` plugin; deferred | `app egui2gen generate`; the browser host digest (ADR-0278 §SD5) |
| `copy`, `issued` entries (ADR-0308 §SD2) | `copy` written by the caller before the broker write; `issued` reserved | the kind enum's golden |
| the capture PEP ([ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md) §SD6) | the watermark obligation carries handle and epoch; a dated Update, with SD5 | the obligation's version |
| ADR-0241 | a dated Update: the scaling result, the payload layout, the live path relied on; with SD5 | the crate's README |
| `codelint` | CS014, the broker-only rule for the copy opcode (ADR-0011, Update of 2026-10-11) | the lint gate |

## Alternatives

O1, O3 and O4 are weighed in the QOC. Two narrower choices:

- **Reference-only for every labelled copy**, integrity labels included.
  Rejected: copying agent-written SQL into an outside editor would cost one
  approval per copy, and a person doing so is not the adversary; integrity
  labels mark what comes back.
- **A Go-side clipboard** instead of the egui path. Rejected by ADR-0026's
  entry already: it targets the wrong clipboard and is meaningless under the
  browser host.

## Consequences

### Positive

- One choke point covers the three hosts, the desktop included, for the first
  time.
- Copies carry labels and sources; a paste inside boxer restores them.
- The screen is attributed where the geometry survives, with the epoch tied to
  endorsements.

### Negative

- A Rust change and a browser host digest refresh.
- Plaintext out above the free tier costs a gesture; on administered hosts
  the control is a record only.
- The watermark does not survive the relays people use most.

### Neutral

- Hosts the person administers see the same audit and no refusal.

## Migration — Tier 1

- **Breaks.** Nothing until `BOXER_CELL_MANAGED` is set; without it the
  broker records every copy and forwards it, and raw writes keep working.
- **Path.** The two direct callers are routed through the broker and the
  lint is in; next the opcode and the plugin, then the policy per host kind.
- **Old shape.** Raw, unlabelled writes, accepted as unknown provenance.

## Verification plan — Tier 1

- **Lane.** Default: the lint with an empty allowlist; the broker's policy
  and handle tests; L20 as a Go check over a run of copy records. The
  scene per widget kind, L21 and the per-host scene for the plugin wait
  for SD2 and SD5.
- **What would fail.** A `CopyText` forwarded with no broker request under
  policy; a labelled copy with neither handle nor approval; an image leaving
  the host with no `issued` entry.
- **Gap.** A photograph of the screen; a rescaled screenshot.

## Status

Accepted 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the `clipboard.write` capability.
- [ADR-0241](./0241-cooperative-image-identification-watermark.md) — the watermark.
- [ADR-0278](./0278-tab-mode-for-downstream-apps.md) §SD5 — the browser host digest.
- [ADR-0281](./0281-window-captures-through-one-policy-enforcement-point.md), [ADR-0287](./0287-chat-pixels-a-setting-for-showing-screenshots-to-the-model.md) — captures and pixels.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md), [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md), [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md), [ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md) — the threat model, the journal, labels, exits.
- [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) §SD11 step 10 — the lint.
- [agent-harness](../adr-background-work/agent-harness.md) §4.8 and Appendix A (d), (e) — the clipboard reading and the watermark spike.
