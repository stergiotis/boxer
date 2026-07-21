package appletcreatecfg_test

import (
	"testing"
	"time"

	"github.com/stergiotis/boxer/apps/sqlappletcreator/appletcreatecfg"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/kindcheck"
)

func sampleAppletCreate() appletcreatecfg.AppletCreate {
	return appletcreatecfg.AppletCreate{
		FactId:   3,
		At:       time.Unix(0, 1_700_000_000_000_000_000).UTC(),
		Sql:      "SELECT * FROM keelson('runtime-apps')",
		Endpoint: appletcreatecfg.EndpointIntrospection,
	}
}

func TestBuscodecAutoRegistersAppletCreate(t *testing.T) {
	got := buscodec.Lookup[appletcreatecfg.AppletCreate]()
	want := "appletCreate-sparse-cbor"
	if got.Name() != want {
		t.Fatalf("Lookup.Name() = %q, want %q", got.Name(), want)
	}
}

func TestBuscodecRoundTrip(t *testing.T) {
	orig := sampleAppletCreate()
	wire, err := buscodec.Encode(orig)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := buscodec.Decode[appletcreatecfg.AppletCreate](wire)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.Sql != orig.Sql {
		t.Errorf("Sql: got %q, want %q", got.Sql, orig.Sql)
	}
	if got.Endpoint != orig.Endpoint {
		t.Errorf("Endpoint: got %q, want %q", got.Endpoint, orig.Endpoint)
	}
}

func TestKindcheckIdentifiesAppletCreate(t *testing.T) {
	// The window host gates argument-carrying opens on this check
	// (ADR-0135 §SD1); pin that encoded AppletCreate bytes pass as their own
	// kind and are refused as a different one.
	wire, err := buscodec.Encode(sampleAppletCreate())
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	kind, err := kindcheck.PeekKind(wire)
	if err != nil {
		t.Fatalf("PeekKind: %v", err)
	}
	if kind != "appletCreate" {
		t.Fatalf("PeekKind = %q, want appletCreate", kind)
	}
	if err := kindcheck.Check("appletCreate", wire); err != nil {
		t.Fatalf("Check(appletCreate): %v", err)
	}
	if err := kindcheck.Check("launchRequest", wire); err == nil {
		t.Fatal("Check(launchRequest, appletCreate bytes) accepted a mismatched payload")
	}
}
