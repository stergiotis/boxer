package introspecthost

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TestStart_DisabledGate: KEELSON_INTROSPECT_ENABLE=false returns a no-op
// stop, binds nothing, and publishes no endpoint.
func TestStart_DisabledGate(t *testing.T) {
	Enabled.SetForTest(t, "false")
	introspect.SetLocalQueryEndpoint("") // reset process global

	stop, err := Start(Deps{Log: zerolog.Nop()})
	if err != nil {
		t.Fatalf("Start (disabled) returned error: %v", err)
	}
	if stop == nil {
		t.Fatal("Start must always return a non-nil stop")
	}
	if got := introspect.LocalQueryEndpoint(); got != "" {
		t.Fatalf("disabled gate must not publish an endpoint, got %q", got)
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("no-op stop returned error: %v", err)
	}
}

// TestStart_NoRunnerDoesNotPublishEndpoint: when chlocal is unavailable the
// HTTP table source still binds (external url() consumers can reach /table),
// but /query is unbacked (503), so the co-resident-app discovery endpoint
// stays unpublished. A nil window host is tolerated (drops keelson.windows).
func TestStart_NoRunnerDoesNotPublishEndpoint(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("") // reset process global

	stop, err := Start(Deps{
		WindowHost:       nil,
		Bus:              nil,
		ChlocalAvailable: false,
		Log:              zerolog.Nop(),
	})
	if err != nil {
		t.Fatalf("Start (enabled, no chlocal) returned error: %v", err)
	}
	t.Cleanup(func() { _ = stop(context.Background()) })

	if got := introspect.LocalQueryEndpoint(); got != "" {
		t.Fatalf("an unbacked /query must not be published as a query target, got %q", got)
	}

	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop returned error: %v", err)
	}
	if got := introspect.LocalQueryEndpoint(); got != "" {
		t.Fatalf("stop must leave the endpoint cleared, got %q", got)
	}
}

// TestStart_RegistersWorkingsetsFromFacts pins the ADR-0148 §SD7 wiring: the
// facts store handed in as a dep is the one keelson('workingsets') reads, so a
// record saved by the window host is queryable through this endpoint.
func TestStart_RegistersWorkingsetsFromFacts(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("")

	facts := factsstore.NewInMemoryFactsStore()
	_, err := facts.WriteWorkingset(factsstore.WorkingsetRow{
		AppId: "play", Name: "default", Kind: "playLaunch", Config: []byte("SELECT 1"),
	})
	if err != nil {
		t.Fatalf("WriteWorkingset: %v", err)
	}
	reg := introspect.NewRegistry()
	stop, err := Start(Deps{Registry: reg, Facts: facts, Log: zerolog.Nop()})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = stop(context.Background()) })

	p, ok := reg.Lookup("workingsets")
	if !ok {
		t.Fatalf("workingsets not registered; tables = %v", reg.Names())
	}
	rec, err := p.Snapshot(introspect.AllColumns())
	if err != nil {
		t.Fatalf("workingsets Snapshot: %v", err)
	}
	defer rec.Release()
	if rec.NumRows() != 1 {
		t.Fatalf("expected the stored record, got %d rows", rec.NumRows())
	}
}

// TestStart_CoverageTablesPresentWithoutSampler: an uninstrumented build
// wires no sampler, and the three coverage tables must still exist, empty
// (ADR-0169 §SD5) — the table names must not depend on the build lane.
func TestStart_CoverageTablesPresentWithoutSampler(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("")

	reg := introspect.NewRegistry()
	stop, err := Start(Deps{Registry: reg, Log: zerolog.Nop()})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = stop(context.Background()) })

	for _, name := range []string{"coverage_status", "coverage_pkgs", "coverage_funcs"} {
		p, ok := reg.Lookup(name)
		if !ok {
			t.Fatalf("%s must register even without a sampler; tables = %v", name, reg.Names())
		}
		rec, err := p.Snapshot(introspect.AllColumns())
		if err != nil {
			t.Fatalf("%s Snapshot: %v", name, err)
		}
		if rec.NumRows() != 0 {
			t.Fatalf("%s: expected an empty table, got %d rows", name, rec.NumRows())
		}
		rec.Release()
	}
}

// TestStart_WorkingsetsPresentWithoutFacts: no store wired means an empty
// table, not a missing one — the table name must not depend on the wiring.
func TestStart_WorkingsetsPresentWithoutFacts(t *testing.T) {
	Enabled.SetForTest(t, "true")
	introspect.SetLocalQueryEndpoint("")

	reg := introspect.NewRegistry()
	stop, err := Start(Deps{Registry: reg, Log: zerolog.Nop()})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = stop(context.Background()) })

	p, ok := reg.Lookup("workingsets")
	if !ok {
		t.Fatalf("workingsets must register even without a facts store; tables = %v", reg.Names())
	}
	rec, err := p.Snapshot(introspect.AllColumns())
	if err != nil {
		t.Fatalf("workingsets Snapshot: %v", err)
	}
	defer rec.Release()
	if rec.NumRows() != 0 {
		t.Fatalf("expected an empty table, got %d rows", rec.NumRows())
	}
}
