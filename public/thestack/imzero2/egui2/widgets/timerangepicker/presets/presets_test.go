package presets_test

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timerangepicker/presets"
)

func TestDefaultGrafana75NotEmpty(t *testing.T) {
	r := presets.DefaultGrafana75()
	if r.Len() == 0 {
		t.Fatal("expected non-empty default registry")
	}
}

func TestDefaultGrafana75ContainsExpectedLabels(t *testing.T) {
	r := presets.DefaultGrafana75()
	want := []string{
		"Last 5 minutes",
		"Last 24 hours",
		"Today so far",
		"Yesterday",
		"This week",
	}
	have := make(map[string]bool)
	for _, p := range r.All() {
		have[p.Label()] = true
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing default preset: %q", w)
		}
	}
}

func TestDefaultGrafana75AllReferenceAnchorNow(t *testing.T) {
	r := presets.DefaultGrafana75()
	for _, p := range r.All() {
		if !strings.Contains(p.FromSQL(), "anchor_now") {
			t.Errorf("%q FromSQL %q does not reference anchor_now", p.Label(), p.FromSQL())
		}
		if !strings.Contains(p.ToSQL(), "anchor_now") {
			t.Errorf("%q ToSQL %q does not reference anchor_now", p.Label(), p.ToSQL())
		}
	}
}

func TestRegistryAddAll(t *testing.T) {
	r := presets.NewRegistry()
	if r.Len() != 0 {
		t.Fatal("expected empty registry")
	}
	r.Add(presets.NewPreset("Custom 5m", "anchor_now - INTERVAL 5 MINUTE", "anchor_now"))
	if r.Len() != 1 {
		t.Fatalf("expected len 1, got %d", r.Len())
	}
	p := r.All()[0]
	if p.Label() != "Custom 5m" {
		t.Fatalf("expected 'Custom 5m', got %q", p.Label())
	}
	if p.FromSQL() != "anchor_now - INTERVAL 5 MINUTE" {
		t.Errorf("unexpected FromSQL: %q", p.FromSQL())
	}
}

func TestRegistryInsertionOrder(t *testing.T) {
	r := presets.NewRegistry()
	r.Add(presets.NewPreset("a", "anchor_now", "anchor_now"))
	r.Add(presets.NewPreset("b", "anchor_now", "anchor_now"))
	r.Add(presets.NewPreset("c", "anchor_now", "anchor_now"))
	got := []string{}
	for _, p := range r.All() {
		got = append(got, p.Label())
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("position %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
