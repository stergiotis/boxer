package bad

import (
	"errors"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func formattedWrap(path string, n int) (err error) {
	err = eh.Errorf("open %q after %d tries: %w", path, n, errors.New("x")) // want a CS013 finding here
	return
}

func formattedRoot(kind uint8) (err error) {
	err = eh.Errorf("unknown kind %d", kind) // want a CS013 finding here
	return
}

func builderStillFormats(path string) (err error) {
	err = eb.Build().Str("path", path).Errorf("open %s failed", path) // want a CS013 finding here
	return
}

// A builder held in a variable is reached through the receiver type, not the
// package qualifier.
func builderInVariable(path string) (err error) {
	b := eb.Build().Str("path", path)
	err = b.Errorf("open %v failed", path) // want a CS013 finding here
	return
}

// The format parameter of ErrorfWithData is the second one.
func withData(n int) (err error) {
	err = eh.ErrorfWithData(nil, "n=%d", n) // want a CS013 finding here
	return
}

// A format folded from string constants is judged like a literal.
const prefix = "stage %s: "

func constantFolded(err0 error) (err error) {
	err = eh.Errorf(prefix+"%w", "map", err0) // want a CS013 finding here
	return
}

func suppressed(n int) (err error) {
	err = eh.Errorf("n=%d", n) //boxer:lint disable=CS013 reason="testdata coverage of suppression"
	return
}
