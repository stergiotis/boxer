package registry

import (
	"cmp"
	"iter"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/contract"
	"github.com/stergiotis/boxer/public/semistructured/leeway/stopa/naturalkey"
)

func NewTagValueRegistry[C contract.ContractI](offset identifier.TagValue, estSize int, namingStyle naming.NamingStyleE, contr C) (inst *MembershipValueRegistry[C], err error) {
	lookupTg := containers.NewBinarySearchGrowingKV[identifier.IdTag, RegisteredTagValue](estSize, cmp.Compare)
	lookupNk := containers.NewBinarySearchGrowingKV[naming.StylableName, RegisteredTagValue](estSize, cmp.Compare)
	inst = &MembershipValueRegistry[C]{
		offset:      offset,
		lookupTg:    lookupTg,
		lookupNk:    lookupNk,
		namingStyle: namingStyle,
		contr:       contr,
		memEnc:      naturalkey.NewEncoder(),
	}
	return
}
func MustNewTagValueRegistry[C contract.ContractI](offset identifier.TagValue, namingStyle naming.NamingStyleE, estSize int, contr C) (inst *MembershipValueRegistry[C]) {
	var err error
	inst, err = NewTagValueRegistry[C](offset, estSize, namingStyle, contr)
	if err != nil {
		log.Panic().Err(err).Msg("unable to create tag value registry")
	}
	return
}

func (inst RegisteredTagValue) GetModuleInfo() string {
	return inst.moduleInfo
}
func (inst RegisteredTagValue) SetVirtual() RegisteredTagValue {
	inst.flags = inst.flags.SetVirtual()
	return inst.register(inst)
}
func (inst RegisteredTagValue) ClearVirtual() RegisteredTagValue {
	inst.flags = inst.flags.ClearVirtual()
	return inst.register(inst)
}
func (inst RegisteredTagValue) SetFinal() RegisteredTagValue {
	inst.flags = inst.flags.SetFinal()
	return inst.register(inst)
}
func (inst RegisteredTagValue) ClearFinal() RegisteredTagValue {
	inst.flags = inst.flags.ClearVirtual()
	return inst.register(inst)
}
func (inst RegisteredTagValue) SetDeprecated() RegisteredTagValue {
	inst.flags = inst.flags.SetDeprecated()
	return inst.register(inst)
}
func (inst RegisteredTagValue) ClearDeprecated() RegisteredTagValue {
	inst.flags = inst.flags.ClearDeprecated()
	return inst.register(inst)
}
func (inst RegisteredTagValue) GetNaturalKey() naming.StylableName {
	return inst.naturalKey
}
func (inst RegisteredTagValue) GetTagValue() identifier.TagValue {
	return inst.tv
}
func (inst RegisteredTagValue) GetOrigin() string {
	return inst.origin
}
func (inst *MembershipValueRegistry[C]) IterateAll() iter.Seq[RegisteredTagValue] {
	return inst.lookupTg.IterateValues()
}
func (inst *MembershipValueRegistry[C]) GetRecordByTagValue(tv identifier.TagValue) (r RegisteredTagValue, has bool) {
	return inst.lookupTg.Get(tv.GetTag())
}
func (inst *MembershipValueRegistry[C]) GetRecordByTag(tg identifier.IdTag) (r RegisteredTagValue, has bool) {
	return inst.lookupTg.Get(tg)
}
func (inst *MembershipValueRegistry[C]) HasRecordByTag(tg identifier.IdTag) (has bool) {
	return inst.lookupTg.Has(tg)
}
func (inst *MembershipValueRegistry[C]) HasRecordByTagValue(tv identifier.TagValue) (has bool) {
	return inst.lookupTg.Has(tv.GetTag())
}

func (inst *MembershipValueRegistry[C]) Length() int {
	return inst.lookupNk.Len()
}
func (inst *MembershipValueRegistry[C]) GetOffset() identifier.TagValue {
	return inst.offset
}
func (inst *MembershipValueRegistry[C]) MustRegister(naturalKey naming.StylableName, tv identifier.TagValue) (r RegisteredTagValue) {
	var err error
	r, err = inst.Register(naturalKey, tv)
	if err != nil {
		log.Panic().Err(err).Msg("unable to register natural key")
	}
	return
}
func (inst *MembershipValueRegistry[C]) Register(nk naming.StylableName, tv identifier.TagValue) (r RegisteredTagValue, err error) {
	if !nk.IsValid() {
		err = eb.Build().Stringer("nk", nk).Errorf("natural key is not a valid stylable name")
		return
	}
	nk = nk.Convert(inst.namingStyle)
	err = inst.contr.ValidateNaturalKeyHumanReadable(tv, nk)
	if err != nil {
		err = eb.Build().Uint32("tagValue", tv.Value()).Stringer("nk", nk).Errorf("unable to register invalid human readable natural key: %w", err)
		return
	}
	err = inst.contr.ValidateTagValue(tv)
	if err != nil {
		err = eh.Errorf("unable to register invalid tag value: %w", err)
		return
	}
	origin := getOrigin()
	var has bool
	tg := tv.GetTag()
	r, has = inst.lookupTg.Get(tg)
	if has {
		if r.origin != origin {
			err = eb.Build().Str("origin1", r.origin).Str("origin2", origin).Uint32("tv", tv.Value()).Errorf("two different code locations register the same tag value")
			return
		}
	}
	r = RegisteredTagValue{
		tv:         tv + inst.offset,
		origin:     origin,
		moduleInfo: getModuleInfo(2),
		naturalKey: nk,
		flags:      MembershipValueNone,
		register: func(r RegisteredTagValue) RegisteredTagValue {
			inst.lookupTg.UpsertSingle(tg, r)
			inst.lookupNk.UpsertSingle(nk, r)
			return r
		},
	}
	existed := inst.lookupTg.UpsertSingle(tg, r)
	if existed {
		err = eb.Build().Uint32("tv", tv.Value()).Errorf("tag value is not unique")
		return
	}
	existed = inst.lookupNk.UpsertSingle(nk, r)
	if existed {
		err = eb.Build().Stringer("naturalKey", nk).Errorf("unique key is not unique")
		return
	}
	return
}
func (inst RegisteredTagValue) GetFlags() RegisteredValueFlagsE {
	return inst.flags
}
