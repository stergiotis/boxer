//go:build identifier_tag_fixed_4 || identifier_tag_fixed_8 || identifier_tag_fixed_12 || identifier_tag_fixed_16 || identifier_tag_fixed_20 || identifier_tag_fixed_24 || identifier_tag_fixed_28 || identifier_tag_fixed_32

package identifier

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/compiletimeflags"
)

var MaxTagValue TagValue = (1 << MaxTagWidth) - 1

func (inst TagValue) IsValid() bool {
	return inst <= MaxTagValue
}

func (inst TagValue) GetTag() (tag IdTag) {
	return IdTag(uint64(inst) << (TotalIdWidth - TagWidth))
}

func (inst UntaggedId) AddTag(t IdTag) TaggedId {
	if compiletimeflags.ExtraChecks {
		if uint64(t)&uint64(inst) != 0 {
			log.Panic().Uint64("t", uint64(t)).Uint64("untagged", uint64(inst)).Msg("tag and untagged id overlap")
		}
	}
	return TaggedId(uint64(t) | uint64(inst))
}

func (inst IdTag) GetMaxPossibleIdIncl() (maxId UntaggedId) {
	maxId = (1 << (TotalIdWidth - TagWidth)) - 1
	return
}

// GetTagWidth Includes the trailing fibonacci code comma 11.
func (inst IdTag) GetTagWidth() (nBits int) {
	nBits = TagWidth
	return
}

func (inst IdTag) GetValue() (v TagValue) {
	v = TagValue(inst >> (TotalIdWidth - TagWidth))
	return
}

func (inst IdTag) ComposeId(id UntaggedId) (taggedId TaggedId) {
	taggedId = id.AddTag(inst)
	return
}

func (inst IdTag) SameTag(id TaggedId) bool {
	return id.GetTag() == inst
}

func (inst TaggedId) GetTagWidth() (nBits int) {
	nBits = TagWidth
	return
}

func getIdMask(tw int) (mask TaggedId) {
	mask = 1<<(64-tw) - 1
	return
}

func getTagMask(tw int) (mask TaggedId) {
	mask = ^getIdMask(tw)
	return
}

func (inst TaggedId) GetTagMask() (mask TaggedId) {
	tw := inst.GetTagWidth()
	mask = getTagMask(tw)
	return
}

func (inst TaggedId) GetTag() (tag IdTag) {
	tag, _ = inst.Split()
	return
}

func (inst TaggedId) RemoveTag() (untaggedId UntaggedId) {
	untaggedId = UntaggedId((^inst.GetTagMask()) & inst)
	return
}

func (inst TaggedId) Split() (tag IdTag, untaggedId UntaggedId) {
	tm := inst.GetTagMask()
	tag = IdTag(inst & tm)
	untaggedId = UntaggedId(inst & ^tm)
	return
}
func (inst TaggedId) IsValid() bool {
	return inst != 0
}

func (inst UntaggedId) IsValid() bool {
	return inst != 0
}
func (inst TaggedId) Value() uint64 {
	return uint64(inst)
}
func (inst UntaggedId) Value() uint64 {
	return uint64(inst)
}
func (inst TagValue) Value() uint32 {
	return uint32(inst)
}
func (inst IdTag) Value() uint64 {
	return uint64(inst)
}
