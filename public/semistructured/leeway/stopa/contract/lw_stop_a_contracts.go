package contract

import (
	"github.com/stergiotis/boxer/public/identity/identifier"
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
}

type VcsManagedContract struct {
}

func NewVcsManagedContract() *VcsManagedContract {
	return &VcsManagedContract{}
}

func (inst *VcsManagedContract) ValidateTagValue(tv identifier.TagValue) error {
	if tv.Value()%2 != 0 {
		return eb.Build().Uint32("tv", tv.Value()).Errorf("convention A expects even tag values (tv %% 2 == 0) for vcs managed tag ids")
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
	if name.IsValid() {
		return eb.Build().Stringer("name", name).Errorf("name is not a valid stylable name")
	}
	return nil
}

func (inst *VcsManagedContract) ValidateMembershipParamsMachineReadable(m []byte) error {
	return nil
}

var _ ContractI = (*VcsManagedContract)(nil)
