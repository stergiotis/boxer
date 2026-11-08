package playground_test

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
	"github.com/stergiotis/boxer/public/keelson/runtime/playground"
)

func sampleLaunch() playground.Launch {
	return playground.Launch{
		FactId:   3,
		At:       time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Sql:      "SELECT count() FROM boxer.facts",
		AutoRun:  true,
		Live:     true,
		BandsSql: "SELECT 'a' AS band, now() - 3600 AS b, now() AS e",
		Tab:      "timeline",
	}
}

func TestBuscodecAutoRegistersLaunch(t *testing.T) {
	got := buscodec.Lookup[playground.Launch]()
	want := "playLaunch-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleLaunch()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[playground.Launch](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.Sql != orig.Sql {
		t.Errorf("Sql: got %q, want %q", got.Sql, orig.Sql)
	}
	if got.AutoRun != orig.AutoRun {
		t.Errorf("AutoRun: got %v, want %v", got.AutoRun, orig.AutoRun)
	}
	if got.Live != orig.Live {
		t.Errorf("Live: got %v, want %v", got.Live, orig.Live)
	}
	if got.BandsSql != orig.BandsSql {
		t.Errorf("BandsSql: got %q, want %q", got.BandsSql, orig.BandsSql)
	}
	if got.Tab != orig.Tab {
		t.Errorf("Tab: got %q, want %q", got.Tab, orig.Tab)
	}
}

func TestKindcheckIdentifiesLaunch(t *testing.T) {
	// The window host gates argument-carrying opens on this check
	// (ADR-0135 §SD1); pin that encoded Launch bytes pass as their own
	// kind and are refused as a different one.
	wire, err := buscodec.Encode(sampleLaunch())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	kind, err := kindcheck.PeekKind(wire)
	if err != nil {
		t.Fatalf("PeekKind: %v", err)
	}
	if kind != "playLaunch" {
		t.Fatalf("PeekKind = %q, want playLaunch", kind)
	}
	if err := kindcheck.Check("playLaunch", wire); err != nil {
		t.Fatalf("Check(playLaunch): %v", err)
	}
	if err := kindcheck.Check("launchRequest", wire); err == nil {
		t.Fatal("Check(launchRequest, playLaunch bytes) accepted a mismatched payload")
	}
}
