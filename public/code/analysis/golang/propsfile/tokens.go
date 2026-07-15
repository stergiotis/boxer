package propsfile

import (
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/packageprops"
)

// This file maps packageprops values to the Go identifiers that name them in
// generated source, and back. The identifiers live here rather than in
// packageprops because they are a code-generation concern: the vocabulary
// package should not have to know it is being rendered into source.

// StateToken is the packageprops identifier for a wasm state.
func StateToken(s packageprops.WASMState) (tok string) {
	switch s {
	case packageprops.WASMCompiles:
		return "WASMCompiles"
	case packageprops.WASMBlocked:
		return "WASMBlocked"
	default:
		return "WASMUnknown"
	}
}

// ParseStateToken is the inverse of StateToken. An unrecognised token yields
// WASMUnknown, so an unreadable declaration asserts nothing rather than erroring.
func ParseStateToken(tok string) (s packageprops.WASMState) {
	switch tok {
	case "WASMCompiles":
		return packageprops.WASMCompiles
	case "WASMBlocked":
		return packageprops.WASMBlocked
	default:
		return packageprops.WASMUnknown
	}
}

// KindToken is the packageprops identifier for a Kind.
func KindToken(k packageprops.Kind) (tok string) {
	switch k {
	case packageprops.KindDemo:
		return "KindDemo"
	case packageprops.KindExample:
		return "KindExample"
	case packageprops.KindIntegrationTest:
		return "KindIntegrationTest"
	default:
		return "KindUnspecified"
	}
}

// ParseKindToken is the inverse of KindToken.
func ParseKindToken(tok string) (k packageprops.Kind) {
	switch tok {
	case "KindDemo":
		return packageprops.KindDemo
	case "KindExample":
		return packageprops.KindExample
	case "KindIntegrationTest":
		return packageprops.KindIntegrationTest
	default:
		return packageprops.KindUnspecified
	}
}

// CapabilityToken is the packageprops identifier for a capability
// ("CapabilityReadSystemState").
//
// Unlike the state and kind tokens above it is derived from the capability's own
// String() rather than switched over by hand: the taxonomy has fifteen members
// and grows with upstream capslock, so a hand-written table would be one more
// place to forget. The derivation is total over the kebab-case tokens
// packageprops emits, and TestCapabilityTokenRoundTrip pins that.
func CapabilityToken(c packageprops.Capability) (tok string) {
	var b strings.Builder
	b.WriteString("Capability")
	for part := range strings.SplitSeq(c.String(), "-") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}

var capabilityTokenIndex = sync.OnceValue(func() (m map[string]packageprops.Capability) {
	cs := packageprops.AllCapabilities()
	m = make(map[string]packageprops.Capability, len(cs))
	for _, c := range cs {
		m[CapabilityToken(c)] = c
	}
	return
})

// ParseCapabilityToken is the inverse of CapabilityToken. An unrecognised token
// yields ok false, letting the caller drop the bit rather than alias it onto
// another capability.
func ParseCapabilityToken(tok string) (c packageprops.Capability, ok bool) {
	c, ok = capabilityTokenIndex()[tok]
	return
}
