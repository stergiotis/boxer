package packageprops_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/packageprops"

	// Blank-import a couple of packages that carry a generated package_props.go.
	// Their init must register them, so packageprops.All() — the "what is
	// compiled into this binary" view — observes them here (ADR-0080).
	_ "github.com/stergiotis/boxer/public/functional/option"
	_ "github.com/stergiotis/boxer/public/hashing/splitmix64"
)

func TestRegistryPopulatedByInit(t *testing.T) {
	have := make(map[string]bool)
	for _, e := range packageprops.All() {
		have[e.ImportPath] = true
	}
	for _, want := range []string{
		"github.com/stergiotis/boxer/public/functional/option",
		"github.com/stergiotis/boxer/public/hashing/splitmix64",
	} {
		if !have[want] {
			t.Errorf("All() missing %s — its generated init should have registered it", want)
		}
	}
}
