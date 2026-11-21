// Package registration collects the failures of registrations a process
// makes at package initialisation (ADR-0396 §2): an app whose manifest does
// not validate, a duplicate alias, a launch-kind probe or an egress
// destination registered twice. A registry records a failure here instead of
// panicking in init, where no caller can handle it, or dropping the entry
// with a log line nobody reads. A host refuses to start while any failure is
// recorded, and lists every one, so a mistake in a manifest is found at the
// first start rather than as an app missing from the launcher.
//
// The record is process-wide and append-only. A test that exercises a
// registry's failure path uses that registry's error-returning method form,
// which does not record here.
package registration

import (
	"errors"
	"sync"

	"github.com/stergiotis/boxer/public/observability/eh"
)

var (
	mu       sync.Mutex
	failures []error
)

// Fail records that a registration in registry failed with err. A nil err
// records nothing.
func Fail(registry string, err error) {
	if err == nil {
		return
	}
	mu.Lock()
	failures = append(failures, eh.Errorf("%s: %w", registry, err))
	mu.Unlock()
}

// Err is every recorded failure joined, nil when there is none.
func Err() (err error) {
	mu.Lock()
	defer mu.Unlock()
	if len(failures) == 0 {
		return
	}
	err = errors.Join(failures...)
	return
}

// Failures is the recorded failures in the order they were recorded.
func Failures() (errs []error) {
	mu.Lock()
	errs = make([]error, len(failures))
	copy(errs, failures)
	mu.Unlock()
	return
}
