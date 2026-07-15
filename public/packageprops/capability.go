package packageprops

import "strings"

// Capability is one class of privileged operation a package can reach, as
// classified by capslock (ADR-0120).
//
// The numeric values are capslock's protobuf enum numbers, reproduced here so
// that packageprops stays a stdlib-only leaf (ADR-0080 SD2) and no capslock type
// crosses into it. Protobuf enum numbers are stable by the proto compatibility
// contract, which is what makes them safe to use as CapabilitySet bit positions:
// upstream can add a capability without renumbering the ones already recorded in
// committed package_props.go files.
type Capability uint8

const (
	CapabilityUnspecified        Capability = 0  // not a capability; capslock's zero value
	CapabilitySafe               Capability = 1  // surveyed; reaches nothing privileged
	CapabilityFiles              Capability = 2  // reads or writes the filesystem
	CapabilityNetwork            Capability = 3  // performs network I/O
	CapabilityRuntime            Capability = 4  // manipulates the Go runtime
	CapabilityReadSystemState    Capability = 5  // reads system state (env, hostname, /proc, …)
	CapabilityModifySystemState  Capability = 6  // modifies system state
	CapabilityOperatingSystem    Capability = 7  // other direct OS interaction
	CapabilitySystemCalls        Capability = 8  // issues syscalls directly
	CapabilityArbitraryExecution Capability = 9  // can execute arbitrary code in-process
	CapabilityCgo                Capability = 10 // calls into C
	CapabilityUnanalyzed         Capability = 11 // capslock could not analyse a reachable call
	CapabilityUnsafePointer      Capability = 12 // uses unsafe.Pointer
	CapabilityReflect            Capability = 13 // uses reflection
	CapabilityExec               Capability = 14 // executes an external process

	// capabilityMax is the highest capability this vocabulary knows. A survey
	// against a newer capslock that reports beyond it must extend this file
	// rather than silently drop the bit.
	capabilityMax = CapabilityExec
)

// capabilityTokens maps each capability to a stable lowercase token, indexed by
// capability number. Tokens are the wire/display form: they appear in harvest
// tables, in the keelson package_capabilities rows, and in diagnostics.
var capabilityTokens = [capabilityMax + 1]string{
	CapabilityUnspecified:        "unspecified",
	CapabilitySafe:               "safe",
	CapabilityFiles:              "files",
	CapabilityNetwork:            "network",
	CapabilityRuntime:            "runtime",
	CapabilityReadSystemState:    "read-system-state",
	CapabilityModifySystemState:  "modify-system-state",
	CapabilityOperatingSystem:    "operating-system",
	CapabilitySystemCalls:        "system-calls",
	CapabilityArbitraryExecution: "arbitrary-execution",
	CapabilityCgo:                "cgo",
	CapabilityUnanalyzed:         "unanalyzed",
	CapabilityUnsafePointer:      "unsafe-pointer",
	CapabilityReflect:            "reflect",
	CapabilityExec:               "exec",
}

// String renders the capability as a stable lowercase token. An unknown value
// renders "unspecified", matching the WASMState/Kind convention that an
// unrecognised value degrades to "asserts nothing" rather than erroring.
func (c Capability) String() (str string) {
	if c > capabilityMax {
		return capabilityTokens[CapabilityUnspecified]
	}
	return capabilityTokens[c]
}

// ParseCapability resolves a token produced by [Capability.String] back to its
// capability. Unknown tokens yield CapabilityUnspecified and ok false.
func ParseCapability(token string) (c Capability, ok bool) {
	for i, t := range capabilityTokens {
		if t == token && Capability(i) != CapabilityUnspecified {
			return Capability(i), true
		}
	}
	return CapabilityUnspecified, false
}

// AllCapabilities returns every capability this vocabulary knows, in ascending
// order, excluding CapabilityUnspecified (which is not a capability). Callers
// that must enumerate the taxonomy — code generators, table builders, tests —
// use it rather than hardcoding the range, so adding a capability reaches them
// without an edit.
func AllCapabilities() (cs []Capability) {
	cs = make([]Capability, 0, capabilityMax)
	for c := CapabilitySafe; c <= capabilityMax; c++ {
		cs = append(cs, c)
	}
	return
}

// CapabilitySet is a set of capabilities held as a bitmask, where bit n is the
// capability with capslock proto enum number n (see [Capability]).
//
// It is a scalar by design: [Props] is copied by value through [Entry], [Table]
// and the [All] snapshot, so a slice- or map-typed field would turn those copies
// into shared mutable state.
//
// The zero value is the empty set, which means *not surveyed* — it asserts
// nothing, per the ADR-0080 zero-value rule. A completed survey that finds
// nothing privileged records CapabilitySafe instead, so "surveyed and clean" and
// "never surveyed" stay distinguishable (ADR-0120 SD4).
type CapabilitySet uint32

// Caps builds a CapabilitySet from capabilities. It is the constructor emitted
// into generated package_props.go files, chosen over a bare numeric literal so
// the declarations stay readable, IDE-navigable and greppable — `grep -r
// CapabilityExec` answers "which packages execute external processes?".
func Caps(cs ...Capability) (s CapabilitySet) {
	for _, c := range cs {
		s = s.With(c)
	}
	return
}

// With returns the set with c added. A capability beyond capabilityMax is
// ignored rather than silently aliasing onto another bit.
func (s CapabilitySet) With(c Capability) (out CapabilitySet) {
	if c > capabilityMax {
		return s
	}
	return s | 1<<c
}

// Has reports whether the set contains c.
func (s CapabilitySet) Has(c Capability) (b bool) {
	if c > capabilityMax {
		return false
	}
	return s&(1<<c) != 0
}

// Surveyed reports whether a capability survey has recorded a verdict for this
// set. It is false only for the zero value, which asserts nothing.
func (s CapabilitySet) Surveyed() (b bool) { return s != 0 }

// Safe reports whether the set records a completed survey that found nothing
// privileged. It is distinct from an unsurveyed set: Safe implies Surveyed.
func (s CapabilitySet) Safe() (b bool) { return s.Has(CapabilitySafe) }

// Privileged returns the set without the CapabilitySafe marker — the actual
// privileged capabilities.
//
// CapabilitySafe is a marker, not a member: it records that a survey ran and
// found nothing, so it is set exactly when no other bit is. That makes it the
// one bit that must be masked off before comparing two sets, because it is
// present precisely when a set is otherwise empty. Comparing raw sets instead
// produces nonsense — a package whose own code is clean but which reaches files
// through a dependency has Safe in its direct set and not in its reachable one,
// which naively reads as the direct set "not being a subset" of everything the
// package can reach.
//
// Use it for any set algebra across sets; use the raw set for display and
// storage.
func (s CapabilitySet) Privileged() (out CapabilitySet) { return s &^ (1 << CapabilitySafe) }

// Subset reports whether every privileged capability in s is also in other. The
// CapabilitySafe marker is ignored on both sides (see [CapabilitySet.Privileged]).
func (s CapabilitySet) Subset(other CapabilitySet) (b bool) {
	return s.Privileged()&^other.Privileged() == 0
}

// Capabilities returns the set's members in ascending capability order.
func (s CapabilitySet) Capabilities() (cs []Capability) {
	for c := CapabilitySafe; c <= capabilityMax; c++ {
		if s.Has(c) {
			cs = append(cs, c)
		}
	}
	return
}

// Names returns the set's members as stable lowercase tokens, in ascending
// capability order. It returns nil for an unsurveyed (zero) set.
func (s CapabilitySet) Names() (names []string) {
	cs := s.Capabilities()
	if len(cs) == 0 {
		return nil
	}
	names = make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.String())
	}
	return
}

// String renders the set as its comma-separated tokens ("files,exec"), or
// "unsurveyed" for the zero value.
func (s CapabilitySet) String() (str string) {
	names := s.Names()
	if len(names) == 0 {
		return "unsurveyed"
	}
	return strings.Join(names, ",")
}
