package imzero2env

import "testing"

// TestRenderCadence_Values pins the string values. They are a cross-language
// contract: the Rust client (src/imzero2/app.rs) hardcodes "reactive" when
// reading IMZERO2_RENDER_CADENCE, so a rename here must be matched there.
// Pinning the values surfaces a drift as a test failure rather than a silent
// Go/Rust mismatch (continuous on one side, reactive on the other).
func TestRenderCadence_Values(t *testing.T) {
	if RenderCadenceContinuous != "continuous" {
		t.Errorf("RenderCadenceContinuous: got %q want continuous", RenderCadenceContinuous)
	}
	if RenderCadenceReactive != "reactive" {
		t.Errorf("RenderCadenceReactive: got %q want reactive", RenderCadenceReactive)
	}
}

func TestRenderCadence_DefaultsToContinuous(t *testing.T) {
	RenderCadence.SetForTest(t, "")
	if got := RenderCadence.Get(); got != RenderCadenceContinuous {
		t.Errorf("RenderCadence default: got %q want %q", got, RenderCadenceContinuous)
	}
}

func TestRenderCadence_AcceptsReactive(t *testing.T) {
	RenderCadence.SetForTest(t, RenderCadenceReactive)
	if got := RenderCadence.Get(); got != RenderCadenceReactive {
		t.Errorf("RenderCadence reactive: got %q want %q", got, RenderCadenceReactive)
	}
}

func TestRenderCadence_OutOfSetFallsBackToDefault(t *testing.T) {
	// An unrecognised value is user error; Get falls back to the default
	// (same convention as Bool/Int/Duration vars — see CategorialStringVar.Get).
	RenderCadence.SetForTest(t, "bogus")
	if got := RenderCadence.Get(); got != RenderCadenceContinuous {
		t.Errorf("RenderCadence out-of-set: got %q want default %q", got, RenderCadenceContinuous)
	}
}
