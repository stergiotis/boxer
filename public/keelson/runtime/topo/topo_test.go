package topo_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/keelson/runtime/topo"
)

// TestRegistryInvariants asserts the declared inventory is well-formed:
// tokens unique (Declare enforces), Needs references resolve to declared
// components (Declare defers this to here so declaration order is free),
// and no component needs itself.
func TestRegistryInvariants(t *testing.T) {
	components := topo.Registry()
	if len(components) == 0 {
		t.Fatal("registry is empty")
	}
	for _, c := range components {
		for _, need := range c.Needs {
			if need == c.Token {
				t.Errorf("component %q needs itself", c.Token)
			}
			if topo.Lookup(need) == nil {
				t.Errorf("component %q needs undeclared component %q", c.Token, need)
			}
		}
	}
}

// TestRegistrySorted asserts Registry returns tokens in sorted order —
// the stable enumeration keelson.components will serve.
func TestRegistrySorted(t *testing.T) {
	components := topo.Registry()
	for i := 1; i < len(components); i++ {
		if components[i-1].Token >= components[i].Token {
			t.Fatalf("registry not strictly sorted: %q before %q",
				components[i-1].Token, components[i].Token)
		}
	}
}

// TestLookupUnknown asserts unknown tokens resolve to nil (marks are
// reported verbatim by consumers; the registry join must not invent
// entries).
func TestLookupUnknown(t *testing.T) {
	if c := topo.Lookup("no-such-component"); c != nil {
		t.Fatalf("Lookup of unknown token returned %+v", c)
	}
}

// TestDeclareRejectsMalformedToken asserts the token shape gate.
func TestDeclareRejectsMalformedToken(t *testing.T) {
	for _, bad := range []string{"", "Upper", "has_underscore", "-lead", "trail-", "dot.ted"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Declare accepted malformed token %q", bad)
				}
			}()
			topo.Declare(topo.Component{Token: bad})
		}()
	}
}

// TestDeclareRejectsDuplicate asserts a token clash fails loudly.
func TestDeclareRejectsDuplicate(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Declare accepted a duplicate token")
		}
	}()
	topo.Declare(topo.Component{Token: topo.ImZero2Demo.Token})
}
