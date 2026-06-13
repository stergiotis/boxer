// Package timerangepicker holds the value types and library code for
// the imzero2 time range picker (ADR-0016). The package is the umbrella
// for the picker's pure-Go layer: TimeRange (user-input expressions
// + tz), EvaluatedRange (resolved epoch-millisecond bounds for FFFI2
// consumers), and the sub-packages evaluator (clickhouse-local
// driver), validator (in-process syntax check via boxer's dsl ANTLR
// parser), and presets (Grafana 7.5-derived quick-range registry).
//
// The Phase 3 widget UI is not in this package — this layer is
// consumed by it.
package timerangepicker

import (
	"strings"
	"time"
)

// rangeDelimiter (\x1e ASCII record separator) packs the tz / from /
// to expression strings into the picker's single r9_s wire payload.
// See PackRange / UnpackRange and time_range_picker.rs for the
// rationale. Phase 3 of ADR-0016 used a 2-segment from\x1eto shape;
// Phase 4b extended it to tz\x1efrom\x1eto so the in-widget tz
// dropdown can return the user's selection without a second
// databinding.
const rangeDelimiter = "\x1e"

// PoolName is the chlocalbroker pool name the Phase-4 evaluator
// routes through (ch.local.exec.<PoolName>). Hosts that wire the
// picker declare a SubjectFilter against this exact subject in
// their AppI Manifest, then construct the evaluator with the same
// string. Lives here, in the widget's canonical package, so
// consumers don't drift on naming.
const PoolName = "timerangepicker"

// Expression is the source-of-truth string for a from or to bound.
// It must parse as a ClickHouse SQL expression that evaluates to a
// DateTime / DateTime64. The picker's evaluator injects a per-Apply
// `anchor_now` DateTime64(3, 'UTC') via a WITH clause; user
// expressions can reference it freely.
type Expression string

// TimeRange is the picker's user-input state — two ClickHouse SQL
// expressions plus the IANA timezone (interned index) in which to
// interpret them. TzID == 0 is reserved for "System" (time.Local);
// TzID == 1 is reserved for "UTC". Phase 4 lands the catalogue.
type TimeRange struct {
	From Expression
	To   Expression
	TzID uint16
}

// EvaluatedRange is the resolved (concrete) form of a TimeRange after
// evaluation against an anchor instant. It is what FFFI2 consumers
// see on the wire (Phase 3 onwards).
type EvaluatedRange struct {
	FromEpochMS int64
	ToEpochMS   int64
	TzID        uint16
}

// AsFromTime returns the from bound as time.Time in UTC.
func (inst EvaluatedRange) AsFromTime() (t time.Time) {
	t = time.UnixMilli(inst.FromEpochMS).UTC()
	return
}

// AsToTime returns the to bound as time.Time in UTC.
func (inst EvaluatedRange) AsToTime() (t time.Time) {
	t = time.UnixMilli(inst.ToEpochMS).UTC()
	return
}

// Duration returns ToEpochMS - FromEpochMS as a time.Duration. May be
// negative if the user set To earlier than From; the picker UI is
// expected to reject that, but the value type itself is permissive.
func (inst EvaluatedRange) Duration() (d time.Duration) {
	d = time.Duration(inst.ToEpochMS-inst.FromEpochMS) * time.Millisecond
	return
}

// PackRange packs (tz, from, to) into the canonical wire payload
// string used by the TimeRangePicker FFFI2 widget. tz is the IANA
// zone name the user selected from the in-widget dropdown — empty
// means "use whatever the picker was configured with via the Tz()
// builder." Inverse of UnpackRange.
//
// Expressions containing the ASCII record separator (\x1e) are
// unsupported and may produce an incorrect unpack — \x1e has no
// meaning in ClickHouse SQL or IANA zone names, so this is a
// non-issue in practice.
func PackRange(tz, from, to string) (packed string) {
	packed = tz + rangeDelimiter + from + rangeDelimiter + to
	return
}

// UnpackRange splits the wire payload into (tz, from, to). On an
// empty payload (uninitialised binding pre-Apply) all three returns
// are empty. A 2-segment legacy payload (no tz prefix) is treated as
// (from, to) with empty tz so callers can interoperate with the
// Phase 3 wire shape. A payload with no delimiter at all returns the
// whole payload in `from` so a downstream evaluator surfaces a
// sensible error.
func UnpackRange(packed string) (tz, from, to string) {
	if packed == "" {
		return
	}
	parts := strings.SplitN(packed, rangeDelimiter, 3)
	switch len(parts) {
	case 3:
		tz, from, to = parts[0], parts[1], parts[2]
	case 2:
		from, to = parts[0], parts[1]
	case 1:
		from = parts[0]
	}
	return
}
