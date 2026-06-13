package timerangepicker_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker"
)

func TestPackUnpackRoundTrip(t *testing.T) {
	cases := []struct {
		tz, from, to string
	}{
		{"UTC", "anchor_now - INTERVAL 5 MINUTE", "anchor_now"},
		{"Asia/Tokyo", "toStartOfDay(anchor_now)", "addDays(toStartOfDay(anchor_now), 1)"},
		{"", "", ""},
		{"", "x", "y"},
		{"Europe/Berlin", "", ""},
	}
	for _, c := range cases {
		packed := timerangepicker.PackRange(c.tz, c.from, c.to)
		tz, from, to := timerangepicker.UnpackRange(packed)
		if tz != c.tz || from != c.from || to != c.to {
			t.Errorf("roundtrip failed: tz=%q→%q, from=%q→%q, to=%q→%q", c.tz, tz, c.from, from, c.to, to)
		}
	}
}

func TestUnpackEmptyReturnsEmpty(t *testing.T) {
	tz, from, to := timerangepicker.UnpackRange("")
	if tz != "" || from != "" || to != "" {
		t.Errorf("expected empty triple, got %q/%q/%q", tz, from, to)
	}
}

func TestUnpackLegacyTwoSegmentPayload(t *testing.T) {
	// 2-segment payload (Phase 3 wire shape) decodes as (from, to)
	// with empty tz — keeps the picker interoperable with the older
	// format.
	tz, from, to := timerangepicker.UnpackRange("anchor_now - INTERVAL 1 HOUR\x1eanchor_now")
	if tz != "" {
		t.Errorf("expected empty tz on legacy 2-segment payload, got %q", tz)
	}
	if from != "anchor_now - INTERVAL 1 HOUR" || to != "anchor_now" {
		t.Errorf("expected legacy (from,to), got %q,%q", from, to)
	}
}

func TestUnpackMissingDelimiterTreatsAsFrom(t *testing.T) {
	// A payload without any delimiter is malformed; we choose to put
	// it all into `from` so the caller's downstream evaluator surfaces
	// a sensible error instead of silently splitting at unexpected
	// positions.
	tz, from, to := timerangepicker.UnpackRange("just from no delimiter")
	if tz != "" {
		t.Errorf("expected empty tz on no-delimiter payload, got %q", tz)
	}
	if from != "just from no delimiter" {
		t.Errorf("expected from to capture the whole payload, got %q", from)
	}
	if to != "" {
		t.Errorf("expected to to be empty, got %q", to)
	}
}
