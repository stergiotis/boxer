package bad

import (
	"errors"
	"fmt"
)

func leaks() (err error) {
	err = fmt.Errorf("oops: %w", errors.New("x")) // want a CS001 finding here
	return
}

// A package-level fmt.Errorf whose argument is not a sentinel is not the
// wrapped-sentinel form.
var ErrNotASentinel = fmt.Errorf("wraps a fresh error: %w", errors.New("x")) // want a CS001 finding here

func suppressed() (err error) {
	err = fmt.Errorf("ok-for-now: %w", errors.New("x")) //boxer:lint disable=CS001 reason="testdata coverage of suppression"
	return
}
