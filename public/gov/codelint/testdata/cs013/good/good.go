package good

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func wrapOnly() (err error) {
	err = eh.Errorf("open config: %w", errors.New("x"))
	return
}

func contextOnBuilder(path string, n int) (err error) {
	err = eb.Build().
		Str("path", path).
		Int("len", n).
		Errorf("unable to capture context: %w", errors.New("x"))
	return
}

// A bare message carries no directive, so it is not this rule's concern —
// eb has no New, and eh.New vs eh.Errorf is a separate claim.
func bareMessage() (err error) {
	err = eb.Build().Str("k", "v").Errorf("nothing to do")
	return
}

// Joining preserves both errors; multiple %w is what the rule protects.
func joined(a error, b error) (err error) {
	err = eh.Errorf("%w; %w", a, b)
	return
}

// A doubled %% is an escape, not a directive.
func escapedPercent() (err error) {
	err = eh.Errorf("100%% full: %w", errors.New("x"))
	return
}

// A non-constant format string is undecidable here; go vet's printf analyzer
// is the one that reports on it.
func dynamicFormat(f string, err0 error) (err error) {
	err = eh.Errorf(f, err0)
	return
}

// Errorf on an unrelated type is not an eh / eb constructor.
type reporter struct{}

func (inst *reporter) Errorf(format string, a ...any) {}

func unrelatedErrorf() {
	r := &reporter{}
	r.Errorf("count %d", 3)
}
