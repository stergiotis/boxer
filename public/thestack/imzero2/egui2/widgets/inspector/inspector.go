//go:build llm_generated_opus47

// Package inspector is the shared infrastructure backing the project's
// "value inspector" widgets — small surfaces that bind to a domain
// value (FSM state, distribution digest, arbitrary record, error chain)
// and expose it for human exploration. Every inspector carries a
// [Provenance] back to its source so screenshots are self-documenting;
// inspector widgets render [ProvenanceChip] in their header to make the
// binding visible without operators having to remember which on-screen
// window watches which subject.
//
// Two consumption shapes are supported:
//
//   - Method-arg / receiver-owned data: callers construct a
//     [Provenance] alongside their value and either pass it directly to
//     the inspector's `.Provenance(p)` builder or wrap the value with
//     [NewStaticSource] when they want to thread both together. This is
//     today's only wired shape — fsmview, distsummary, fieldview and
//     errorview all hold their value internally and just need the
//     identity card on the way out.
//
//   - Bus-bound (deferred): a future `LiveSource[T]` will subscribe to
//     a NATS-style subject via the runtime BusI (ADR-0026), decode
//     incoming payloads through a typed [buscodec], hold the latest
//     snapshot under a lock, and expose it via [Source.Snapshot]. The
//     [Source] interface here is shaped so that drop-in is one
//     constructor change at the call site, not a widget-API rewrite.
package inspector

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Provenance is the visible identity card every value inspector carries
// in its header. Subject is the canonical name — typically a bus / cap
// subject like `app.<id>.event.<name>` (ADR-0026 §SD3) — and the only
// load-bearing field: the zero Provenance is treated as "no binding,
// suppress the chip" by [ProvenanceChip] and by every migrated
// inspector's builder. The remaining fields are optional metadata:
//
//   - Schema disambiguates polymorphic subjects ("which version /
//     variant of this payload am I reading"); the value lands as a
//     trailing chip when non-empty.
//   - SourceApp names the producing application when distinct from
//     what the subject implies; useful for debug views that watch
//     subjects from third-party apps.
//   - SampledAt is the wall-clock time the snapshot was taken; rendered
//     as a relative "Xs ago" trailer when non-zero so operators can
//     spot stale data at a glance.
//
// Construct directly or via [Source.Provenance] when threading through
// a [Source].
type Provenance struct {
	Subject   string
	Schema    string
	SourceApp string
	SampledAt time.Time
}

// IsZero reports whether p is the zero value — every migrated inspector
// treats this as "no binding declared, suppress the header chip".
func (p Provenance) IsZero() bool {
	return p.Subject == "" && p.Schema == "" && p.SourceApp == "" && p.SampledAt.IsZero()
}

// Source adapts a value (T) plus its [Provenance] for consumption by a
// value inspector. Two implementations:
//
//   - [NewStaticSource] wraps a value already held by the caller
//     (today's only shape).
//   - LiveSource (deferred — first bus-bound inspector will land it):
//     subscribes to a bus subject and decodes incoming payloads.
//
// Inspectors that accept a Source pull the snapshot once per frame via
// [Source.Snapshot] and render [Source.Provenance] in their header. The
// pull-based shape matches immediate-mode rendering: no callbacks, no
// per-frame allocation, no async update timing concerns.
type Source[T any] interface {
	// Provenance returns the identity card. Stable across frames for
	// the same Source instance.
	Provenance() Provenance
	// Snapshot returns the latest value. Implementations must be safe
	// to call once per frame from the render goroutine and return in
	// constant time — heavy work (decoding, deserialization) belongs
	// behind a lock in the implementation, not inside the call.
	Snapshot() T
}

// staticSource is the trivial Source impl returned by [NewStaticSource].
type staticSource[T any] struct {
	p Provenance
	v T
}

func (s staticSource[T]) Provenance() Provenance { return s.p }
func (s staticSource[T]) Snapshot() T            { return s.v }

var _ Source[int] = staticSource[int]{}

// NewStaticSource wraps a value and its provenance into a [Source].
// Use when the caller already holds the value (method-arg snapshots,
// test fixtures, screenshot demos) and bus subscription does not
// apply. The returned Source is immutable — Snapshot returns the same
// value every call.
func NewStaticSource[T any](p Provenance, v T) Source[T] {
	return staticSource[T]{p: p, v: v}
}

// ProvenanceChip renders the standard one-line header carried by every
// value inspector. Subject (monospace), optional Schema chip, optional
// SourceApp chip, and a relative "Xs ago" trailer when SampledAt is
// non-zero. Renders nothing when p is the zero value, so callers can
// invoke unconditionally without an outer IsZero guard.
//
// Layout is a single [c.Horizontal]; the chip leaves the caller's
// vertical layout untouched. The caller is responsible for the
// preceding spacing and any trailing [c.Separator] — the chip itself
// does not introduce vertical chrome so it composes cleanly inside an
// existing window-content body.
//
// No id parameter today: every primitive the chip emits is id-less
// (Horizontal / Label / LabelAtoms). When a future variant adds an
// interactive sub-element (click-to-copy subject, navigate-to-source-
// app), it will be a separate function or a Renderer-shaped builder
// rather than overload this one.
func ProvenanceChip(p Provenance) {
	if p.IsZero() {
		return
	}
	mutedFg := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	transparentBg := color.Transparent
	density := styletokens.DensityFromEnv()
	gap := styletokens.GapInline(density)
	for range c.Horizontal().KeepIter() {
		// "↳" is the visual binding cue — same glyph used in the
		// bezier-connector demo's overlay caption so the affordance
		// reads as one consistent vocabulary.
		c.Label("↳").Send()
		if p.Subject != "" {
			c.LabelAtoms(c.Atoms().RichText(p.Subject).Monospace().EndRichText().Keep()).Send()
		}
		if !p.SampledAt.IsZero() {
			c.AddSpace(gap)
			c.LabelAtoms(c.Atoms().RichTextColored("· "+formatAgo(time.Since(p.SampledAt)), mutedFg, transparentBg).EndRichText().Keep()).Send()
		}
		if p.Schema != "" {
			c.AddSpace(gap)
			c.LabelAtoms(c.Atoms().RichTextColored("· "+p.Schema, mutedFg, transparentBg).EndRichText().Keep()).Send()
		}
		if p.SourceApp != "" {
			c.AddSpace(gap)
			c.LabelAtoms(c.Atoms().RichTextColored("· from "+p.SourceApp, mutedFg, transparentBg).EndRichText().Keep()).Send()
		}
	}
}

// formatAgo renders a duration as a short "Xs ago" / "Xm ago" / "Xh
// ago" string. Sub-second deltas collapse to "just now". Mirrors the
// ad-hoc formatting in the v0/v1 bezier-connector demo so the chip is
// drop-in compatible.
func formatAgo(d time.Duration) string {
	switch {
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}
