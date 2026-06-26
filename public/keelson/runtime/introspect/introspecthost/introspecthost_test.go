package introspecthost

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

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
