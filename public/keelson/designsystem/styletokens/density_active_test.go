package styletokens_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// TestActiveDensitySeedsFromEnv checks the lazy seed: with IMZERO2_DENSITY
// unset in the test environment, the first read resolves to the same value
// DensityFromEnv reports rather than to the atomic's zero value (which would
// alias DensityTight — the offset-by-one encoding exists for exactly this).
func TestActiveDensitySeedsFromEnv(t *testing.T) {
	restore := styletokens.ActiveDensity()
	defer styletokens.SetActiveDensity(restore)
	if got, want := styletokens.ActiveDensity(), styletokens.DensityFromEnv(); got != want {
		t.Fatalf("ActiveDensity() = %v, want the env seed %v", got, want)
	}
}

// TestSetActiveDensityMovesTheSpacingLadder is the property the Layout ▸
// Density menu depends on: a switch has to move the values the purpose-named
// tokens hand out, not just the enum. Runs the three presets through one
// token and against the §SD2 ladder.
func TestSetActiveDensityMovesTheSpacingLadder(t *testing.T) {
	restore := styletokens.ActiveDensity()
	defer styletokens.SetActiveDensity(restore)
	for _, d := range []styletokens.DensityE{
		styletokens.DensityTight, styletokens.DensityStandard, styletokens.DensityRoomy,
	} {
		styletokens.SetActiveDensity(d)
		if got := styletokens.ActiveDensity(); got != d {
			t.Fatalf("ActiveDensity() = %v after SetActiveDensity(%v)", got, d)
		}
		if got, want := styletokens.GapItems(styletokens.ActiveDensity()), styletokens.PxTable[3][d]; got != want {
			t.Errorf("GapItems at %v = %v, want %v", d, got, want)
		}
	}
}

// TestSetActiveDensityClampsOutOfRange guards the uint8 enum against a bad
// value reaching Px, where it would index PxTable's 3-wide rows and panic.
func TestSetActiveDensityClampsOutOfRange(t *testing.T) {
	restore := styletokens.ActiveDensity()
	defer styletokens.SetActiveDensity(restore)
	styletokens.SetActiveDensity(styletokens.DensityE(200))
	if got := styletokens.ActiveDensity(); got != styletokens.DensityStandard {
		t.Fatalf("ActiveDensity() = %v after an out-of-range set, want Standard", got)
	}
}
