// Package contract states what a name mint accepts: which tag values a
// registry may be built on, and which names and machine-readable forms it may
// register. A registry holds one contract and consults it on every
// registration, so a rule stated here is enforced everywhere that flavour of
// registry is used.
//
// The distinction the contracts draw is who governs the assignment. A
// version-controlled vocabulary's ids are reviewed in a diff and carried by
// stored rows forever; an ephemeral one's live and die with a test.
package contract

import (
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/tagmint"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

type ContractI interface {
	ValidateTagValue(tv identifier.TagValue) error
	ValidateNaturalKeyHumanReadable(tv identifier.TagValue, name naming.StylableName) error
	ValidateNaturalKeyMachineReadable(tv identifier.TagValue, m []byte) error
	ValidateMembershipVerbatimMachineReadable(m []byte) error
	ValidateMembershipVerbatimHumanReadable(name naming.StylableName) error
	ValidateMembershipParamsMachineReadable(m []byte) error
	// ValidateImplicitOrdinal reports whether this contract admits ids minted
	// from registration order rather than declared beside the name.
	ValidateImplicitOrdinal() error
}

// VcsManagedContract governs a vocabulary whose assignments live in version
// control: reviewed in a diff, pinned by a committed golden, and carried by
// stored rows for as long as the rows exist.
type VcsManagedContract struct {
}

// NewVcsManagedContract returns the contract for a version-controlled
// vocabulary.
func NewVcsManagedContract() *VcsManagedContract {
	return &VcsManagedContract{}
}

// ValidateTagValue requires a value from the vocabulary width class, and
// refuses the tag reserved for ids minted outside version control.
//
// The width is the rule because it is the one that means something: every
// vocabulary gets the same 32/32 split of the id, so `identsql` views and tag
// mask compares speak of one width, and the scheme's short prefixes — its
// widest bodies — stay available to the high-cardinality runtime generators
// that actually need them (ADR-0183 D0).
//
// It replaces a parity check that predated the fibonacci scheme: "convention
// A" required an even tag value, which under prefix-free codes carries no
// structural meaning at all — distinct tags cannot collide whatever their
// parity. It was also checked against the declared value while the wire saw
// the value plus a registry offset, so the three vocabularies it governed read
// it three different ways.
func (inst *VcsManagedContract) ValidateTagValue(tv identifier.TagValue) error {
	if tv == tagmint.RuntimeMintTagValue {
		return eb.Build().Uint32("tv", tv.Value()).Errorf("this tag value is reserved for ids minted outside version control")
	}
	tag := tv.GetTag()
	if !tag.IsValid() {
		return eb.Build().Uint32("tv", tv.Value()).Errorf("tag value does not encode to a valid tag")
	}
	if w := tag.GetTagWidth(); w != tagmint.VocabularyTagWidth {
		return eb.Build().Uint32("tv", tv.Value()).Int("width", w).Int("want", tagmint.VocabularyTagWidth).Errorf("a vcs-managed vocabulary claims from a different width class than this value's code")
	}
	return nil
}

func (inst *VcsManagedContract) ValidateNaturalKeyHumanReadable(tv identifier.TagValue, name naming.StylableName) error {
	if !name.IsValid() {
		return eb.Build().Stringer("name", name).Errorf("name is not a valid stylable name")
	}
	return nil
}

func (inst *VcsManagedContract) ValidateNaturalKeyMachineReadable(tv identifier.TagValue, m []byte) error {
	return nil
}

func (inst *VcsManagedContract) ValidateMembershipVerbatimMachineReadable(m []byte) error {
	return nil
}

func (inst *VcsManagedContract) ValidateMembershipVerbatimHumanReadable(name naming.StylableName) error {
	if !name.IsValid() {
		return eb.Build().Stringer("name", name).Errorf("name is not a valid stylable name")
	}
	return nil
}

func (inst *VcsManagedContract) ValidateMembershipParamsMachineReadable(m []byte) error {
	return nil
}

// ValidateImplicitOrdinal refuses: a version-controlled vocabulary states each
// ordinal beside its name.
//
// Registration order is not a property of the vocabulary — it is a property of
// how the source happens to be arranged, and it changes under an insertion, a
// var-block reorder, a file rename, or a partial link set. Any of those
// re-points ids that stored rows already carry, while compiling, vetting and
// reading clean (ADR-0183 D0).
func (inst *VcsManagedContract) ValidateImplicitOrdinal() error {
	return eb.Build().Errorf("a vcs-managed vocabulary declares each ordinal in source — use Begin(name, ordinal), not BeginNext(name)")
}

var _ ContractI = (*VcsManagedContract)(nil)

// EphemeralContract governs a registry whose assignments outlive nothing: test
// fixtures, throwaway registries, anything with no stored rows to re-point. It
// admits ids minted from registration order, which is why it exists as a
// separate contract rather than a flag.
type EphemeralContract struct {
}

// NewEphemeralContract returns the contract for a registry whose ids are not
// version-controlled and carry no stored data.
func NewEphemeralContract() *EphemeralContract {
	return &EphemeralContract{}
}

// ValidateTagValue accepts any valid tag value: an ephemeral registry shares
// no table with anything, so there is nothing for its tag to collide with.
func (inst *EphemeralContract) ValidateTagValue(tv identifier.TagValue) error {
	if !tv.IsValid() {
		return eb.Build().Uint32("tv", tv.Value()).Errorf("tag value 0 is the invalid sentinel")
	}
	return nil
}

func (inst *EphemeralContract) ValidateNaturalKeyHumanReadable(tv identifier.TagValue, name naming.StylableName) error {
	if !name.IsValid() {
		return eb.Build().Stringer("name", name).Errorf("name is not a valid stylable name")
	}
	return nil
}

func (inst *EphemeralContract) ValidateNaturalKeyMachineReadable(tv identifier.TagValue, m []byte) error {
	return nil
}

func (inst *EphemeralContract) ValidateMembershipVerbatimMachineReadable(m []byte) error {
	return nil
}

func (inst *EphemeralContract) ValidateMembershipVerbatimHumanReadable(name naming.StylableName) error {
	if !name.IsValid() {
		return eb.Build().Stringer("name", name).Errorf("name is not a valid stylable name")
	}
	return nil
}

func (inst *EphemeralContract) ValidateMembershipParamsMachineReadable(m []byte) error {
	return nil
}

// ValidateImplicitOrdinal accepts: nothing stored depends on these ids.
func (inst *EphemeralContract) ValidateImplicitOrdinal() error {
	return nil
}

var _ ContractI = (*EphemeralContract)(nil)
