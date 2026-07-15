package packageprops

import (
	"testing"
)

// TestCapabilityBitPositionsAreProtoEnumNumbers pins the load-bearing claim of
// ADR-0120 SD3: bit n is capslock's proto enum number n. These constants are the
// wire format of every committed package_props.go — renumbering one silently
// reinterprets every declaration in the tree, so they are pinned literally here
// rather than derived from anything.
func TestCapabilityBitPositionsAreProtoEnumNumbers(t *testing.T) {
	for c, want := range map[Capability]uint8{
		CapabilityUnspecified:        0,
		CapabilitySafe:               1,
		CapabilityFiles:              2,
		CapabilityNetwork:            3,
		CapabilityRuntime:            4,
		CapabilityReadSystemState:    5,
		CapabilityModifySystemState:  6,
		CapabilityOperatingSystem:    7,
		CapabilitySystemCalls:        8,
		CapabilityArbitraryExecution: 9,
		CapabilityCgo:                10,
		CapabilityUnanalyzed:         11,
		CapabilityUnsafePointer:      12,
		CapabilityReflect:            13,
		CapabilityExec:               14,
	} {
		if uint8(c) != want {
			t.Errorf("%v = %d, want proto enum number %d", c, uint8(c), want)
		}
	}
}

func TestCapabilityString(t *testing.T) {
	for c, want := range map[Capability]string{
		CapabilitySafe:            "safe",
		CapabilityExec:            "exec",
		CapabilityReadSystemState: "read-system-state",
		CapabilityUnspecified:     "unspecified",
		Capability(200):           "unspecified", // out of range degrades, never panics
	} {
		if got := c.String(); got != want {
			t.Errorf("Capability(%d).String() = %q, want %q", uint8(c), got, want)
		}
	}
}

func TestParseCapability(t *testing.T) {
	for _, c := range AllCapabilities() {
		got, ok := ParseCapability(c.String())
		if !ok || got != c {
			t.Errorf("round trip %v via %q gave (%v, %v)", c, c.String(), got, ok)
		}
	}
	if _, ok := ParseCapability("nonsense"); ok {
		t.Error("unknown token parsed as known")
	}
	// "unspecified" is not a capability, so it must not parse back to one.
	if _, ok := ParseCapability("unspecified"); ok {
		t.Error("unspecified must not parse as a capability")
	}
}

func TestAllCapabilities(t *testing.T) {
	all := AllCapabilities()
	if len(all) != int(capabilityMax) {
		t.Errorf("AllCapabilities() has %d entries, want %d (every capability except unspecified)", len(all), capabilityMax)
	}
	for i, c := range all {
		if i > 0 && c <= all[i-1] {
			t.Errorf("AllCapabilities() must ascend: %v follows %v", c, all[i-1])
		}
		if c == CapabilityUnspecified {
			t.Error("AllCapabilities() must exclude CapabilityUnspecified")
		}
	}
}

func TestCapabilitySetBasics(t *testing.T) {
	s := Caps(CapabilityExec, CapabilityFiles)
	if !s.Has(CapabilityExec) || !s.Has(CapabilityFiles) {
		t.Errorf("%v missing a member it was built with", s)
	}
	if s.Has(CapabilityNetwork) {
		t.Errorf("%v claims a capability it was not built with", s)
	}
	if !s.Surveyed() {
		t.Errorf("%v must count as surveyed", s)
	}
	if s.Safe() {
		t.Errorf("%v must not be safe", s)
	}
	if got, want := s.String(), "files,exec"; got != want {
		t.Errorf("String() = %q, want %q (ascending capability order)", got, want)
	}
	if got := Caps(); got != 0 {
		t.Errorf("Caps() = %v, want the empty (unsurveyed) set", got)
	}
}

// TestCapabilitySetZeroIsUnsurveyed pins the ADR-0080 zero-value rule: an unset
// field asserts nothing. It must be distinguishable from a completed survey that
// found nothing, which is what the safe marker is for (ADR-0120 SD4).
func TestCapabilitySetZeroIsUnsurveyed(t *testing.T) {
	var zero CapabilitySet
	if zero.Surveyed() {
		t.Error("the zero set must not count as surveyed")
	}
	if zero.Safe() {
		t.Error("the zero set must not count as safe — it asserts nothing")
	}
	if zero.Names() != nil {
		t.Errorf("the zero set must have no names, got %v", zero.Names())
	}
	if got, want := zero.String(), "unsurveyed"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	clean := Caps(CapabilitySafe)
	if !clean.Surveyed() || !clean.Safe() {
		t.Errorf("%v must be both surveyed and safe", clean)
	}
	if clean == zero {
		t.Error("safe and unsurveyed must not share an encoding")
	}
}

// TestPrivilegedMasksTheMarker covers the sharp edge SD4 buys: the safe marker
// lives in the same mask as real capabilities, so set algebra must exclude it.
func TestPrivilegedMasksTheMarker(t *testing.T) {
	if got := Caps(CapabilitySafe).Privileged(); got != 0 {
		t.Errorf("a safe set has no privileged capabilities, got %v", got)
	}
	if got := Caps(CapabilityExec).Privileged(); got != Caps(CapabilityExec) {
		t.Errorf("Privileged() must not alter a set without the marker, got %v", got)
	}

	// The real-world shape that motivated this: a package whose own code is
	// clean but that reaches files through a dependency. Raw set algebra reports
	// a subset violation; Subset does not.
	direct, reachable := Caps(CapabilitySafe), Caps(CapabilityFiles)
	if direct&^reachable == 0 {
		t.Fatal("precondition: the raw masks are expected to disagree here")
	}
	if !direct.Subset(reachable) {
		t.Errorf("direct %v must count as a subset of reachable %v", direct, reachable)
	}
	if !Caps(CapabilityFiles).Subset(Caps(CapabilityFiles, CapabilityExec)) {
		t.Error("a genuine subset must be reported as one")
	}
	if Caps(CapabilityExec).Subset(Caps(CapabilityFiles)) {
		t.Error("a genuine non-subset must not be reported as a subset")
	}
}

// TestCapabilitySetIgnoresOutOfRange keeps a capability beyond the vocabulary
// from aliasing onto another bit, which a bare 1<<c shift would do by wrapping.
func TestCapabilitySetIgnoresOutOfRange(t *testing.T) {
	s := Caps(CapabilityExec).With(Capability(200))
	if s != Caps(CapabilityExec) {
		t.Errorf("an out-of-range capability must be dropped, got %v", s)
	}
	if s.Has(Capability(200)) {
		t.Error("an out-of-range capability must never read as present")
	}
	// Capability(33) would shift past the uint32 width.
	if Caps(Capability(33)) != 0 {
		t.Error("a capability past the mask width must not set a bit")
	}
}

func TestCapabilitiesAscend(t *testing.T) {
	s := Caps(CapabilityExec, CapabilitySafe, CapabilityFiles, CapabilityNetwork)
	got := s.Capabilities()
	want := []Capability{CapabilitySafe, CapabilityFiles, CapabilityNetwork, CapabilityExec}
	if len(got) != len(want) {
		t.Fatalf("Capabilities() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Capabilities()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
