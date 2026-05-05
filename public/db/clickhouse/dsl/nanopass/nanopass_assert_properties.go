//go:build llm_generated_opus47

package nanopass

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
)

// AssertProperties verifies the declared PassProperties of p against a
// corpus of input bodies.
//
// The check enforces:
//
//   - Idempotent and NeedsFixedPoint are mutually exclusive.
//   - When Idempotent is true, p.Apply(p.Apply(b)) == p.Apply(b) for every b.
//   - When NeedsFixedPoint is true, at least one corpus entry exhibits
//     non-fixed-point behaviour on a single Apply (otherwise the flag is
//     unjustified).
//
// Pass authors call this from a corpus-backed unit test. AssertProperties
// only fails the test on contract violations; it does not check semantic
// correctness of the pass output.
func AssertProperties(t *testing.T, p Pass, corpus []string) {
	t.Helper()

	if p.Properties.Idempotent && p.Properties.NeedsFixedPoint {
		t.Fatalf("pass %q declares both Idempotent and NeedsFixedPoint, which are mutually exclusive", p.Name)
	}

	if p.Properties.Idempotent {
		for _, body := range corpus {
			e1 := env.NewEnvironment()
			out1, err := p.Apply(e1, body)
			if err != nil {
				continue
			}
			e2 := env.NewEnvironment()
			out2, err := p.Apply(e2, out1)
			if err != nil {
				t.Errorf("pass %q: second Apply failed for body %q: %v", p.Name, body, err)
				continue
			}
			if out1 != out2 {
				t.Errorf("pass %q declares Idempotent=true but is not idempotent on body %q\n first: %q\nsecond: %q", p.Name, body, out1, out2)
			}
		}
	}

	if p.Properties.NeedsFixedPoint {
		anyDiffers := false
		for _, body := range corpus {
			e1 := env.NewEnvironment()
			out1, err := p.Apply(e1, body)
			if err != nil {
				continue
			}
			e2 := env.NewEnvironment()
			out2, err := p.Apply(e2, out1)
			if err != nil {
				continue
			}
			if out1 != out2 {
				anyDiffers = true
				break
			}
		}
		if !anyDiffers && len(corpus) > 0 {
			t.Errorf("pass %q declares NeedsFixedPoint=true but converged in one Apply on every corpus entry — flag is unjustified or corpus is too narrow", p.Name)
		}
	}
}
