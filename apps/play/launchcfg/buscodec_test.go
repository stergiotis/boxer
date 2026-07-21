package launchcfg_test

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/apps/play/launchcfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func samplePlayLaunch() launchcfg.PlayLaunch {
	return launchcfg.PlayLaunch{
		FactId:   3,
		At:       time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Sql:      "SELECT count() FROM spinnaker.facts",
		AutoRun:  true,
		Live:     true,
		BandsSql: "SELECT 'a' AS band, now() - 3600 AS b, now() AS e",
		Tab:      "timeline",
	}
}

func TestBuscodecAutoRegistersPlayLaunch(t *testing.T) {
	got := buscodec.Lookup[launchcfg.PlayLaunch]()
	want := "playLaunch-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := samplePlayLaunch()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[launchcfg.PlayLaunch](wire)
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

func TestKindcheckIdentifiesPlayLaunch(t *testing.T) {
	// The window host gates argument-carrying opens on this check
	// (ADR-0135 §SD1); pin that encoded PlayLaunch bytes pass as their
	// own kind and are refused as a different one.
	wire, err := buscodec.Encode(samplePlayLaunch())
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
