package caching_test

// External test package: stashtest imports caching, so wiring the suite
// from an in-package test would form an import cycle.

import (
	"testing"

	"github.com/stergiotis/boxer/public/caching"
	"github.com/stergiotis/boxer/public/caching/stashtest"
)

func TestSliceStash_Conformance(t *testing.T) {
	stashtest.Run(t, func(t *testing.T, capacity int) caching.StashBackendI[string, int] {
		return caching.NewSliceStash[string, int](capacity)
	}, stashtest.Opts{})
}

func TestMapStash_Conformance(t *testing.T) {
	stashtest.Run(t, func(t *testing.T, capacity int) caching.StashBackendI[string, int] {
		return caching.NewMapStash[string, int](capacity)
	}, stashtest.Opts{})
}
