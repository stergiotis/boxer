package common

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

func (inst *TableDescDto) GetPlainItemCount(itemType PlainItemTypeE) (n int) {
	switch itemType {
	case PlainItemTypeEntityId:
		return len(inst.EntityIdNames)
	case PlainItemTypeEntityTimestamp:
		return len(inst.EntityTimestampNames)
	case PlainItemTypeEntityRouting:
		return len(inst.EntityRoutingNames)
	case PlainItemTypeEntityLifecycle:
		return len(inst.EntityLifecycleNames)
	case PlainItemTypeTransaction:
		return len(inst.TransactionNames)
	case PlainItemTypeOpaque:
		return len(inst.OpaqueColumnNames)
	}
	return 0
}
func (inst *TableDescDto) AddPlainItemNames(itemType PlainItemTypeE, names []StylableName) {
	switch itemType {
	case PlainItemTypeEntityId:
		inst.EntityIdNames = append(inst.EntityIdNames, names...)
	case PlainItemTypeEntityTimestamp:
		inst.EntityTimestampNames = append(inst.EntityTimestampNames, names...)
	case PlainItemTypeEntityRouting:
		inst.EntityRoutingNames = append(inst.EntityRoutingNames, names...)
	case PlainItemTypeEntityLifecycle:
		inst.EntityLifecycleNames = append(inst.EntityLifecycleNames, names...)
	case PlainItemTypeTransaction:
		inst.TransactionNames = append(inst.TransactionNames, names...)
	case PlainItemTypeOpaque:
		inst.OpaqueColumnNames = append(inst.OpaqueColumnNames, names...)
	}
}
func (inst *TableDescDto) AddPlainItemTypes(itemType PlainItemTypeE, types []string) {
	switch itemType {
	case PlainItemTypeEntityId:
		inst.EntityIdTypes = append(inst.EntityIdTypes, types...)
	case PlainItemTypeEntityTimestamp:
		inst.EntityTimestampTypes = append(inst.EntityTimestampTypes, types...)
	case PlainItemTypeEntityRouting:
		inst.EntityRoutingTypes = append(inst.EntityRoutingTypes, types...)
	case PlainItemTypeEntityLifecycle:
		inst.EntityLifecycleTypes = append(inst.EntityLifecycleTypes, types...)
	case PlainItemTypeTransaction:
		inst.TransactionTypes = append(inst.TransactionTypes, types...)
	case PlainItemTypeOpaque:
		inst.OpaqueColumnTypes = append(inst.OpaqueColumnTypes, types...)
	}
}
func (inst *TableDescDto) AddPlainItemEncodingHints(itemType PlainItemTypeE, hints []encodingaspects.AspectSet) {
	switch itemType {
	case PlainItemTypeEntityId:
		inst.EntityIdEncodingHints = append(inst.EntityIdEncodingHints, hints...)
	case PlainItemTypeEntityTimestamp:
		inst.EntityTimestampEncodingHints = append(inst.EntityTimestampEncodingHints, hints...)
	case PlainItemTypeEntityRouting:
		inst.EntityRoutingEncodingHints = append(inst.EntityRoutingEncodingHints, hints...)
	case PlainItemTypeEntityLifecycle:
		inst.EntityLifecycleEncodingHints = append(inst.EntityLifecycleEncodingHints, hints...)
	case PlainItemTypeTransaction:
		inst.TransactionEncodingHints = append(inst.TransactionEncodingHints, hints...)
	case PlainItemTypeOpaque:
		inst.OpaqueColumnEncodingHints = append(inst.OpaqueColumnEncodingHints, hints...)
	}
}
func (inst *TableDescDto) AddPlainItemValueSemantics(itemType PlainItemTypeE, valueSemantics []valueaspects.AspectSet) {
	switch itemType {
	case PlainItemTypeEntityId:
		inst.EntityIdValueSemantics = append(inst.EntityIdValueSemantics, valueSemantics...)
	case PlainItemTypeEntityTimestamp:
		inst.EntityTimestampValueSemantics = append(inst.EntityTimestampValueSemantics, valueSemantics...)
	case PlainItemTypeEntityRouting:
		inst.EntityRoutingValueSemantics = append(inst.EntityRoutingValueSemantics, valueSemantics...)
	case PlainItemTypeEntityLifecycle:
		inst.EntityLifecycleValueSemantics = append(inst.EntityLifecycleValueSemantics, valueSemantics...)
	case PlainItemTypeTransaction:
		inst.TransactionValueSemantics = append(inst.TransactionValueSemantics, valueSemantics...)
	case PlainItemTypeOpaque:
		inst.OpaqueColumnValueSemantics = append(inst.OpaqueColumnValueSemantics, valueSemantics...)
	}
}
func (inst *TableDescDto) GetPlainItemNames(itemType PlainItemTypeE) []StylableName {
	switch itemType {
	case PlainItemTypeEntityId:
		return inst.EntityIdNames
	case PlainItemTypeEntityTimestamp:
		return inst.EntityTimestampNames
	case PlainItemTypeEntityRouting:
		return inst.EntityRoutingNames
	case PlainItemTypeEntityLifecycle:
		return inst.EntityLifecycleNames
	case PlainItemTypeTransaction:
		return inst.TransactionNames
	case PlainItemTypeOpaque:
		return inst.OpaqueColumnNames
	}
	return nil
}
func (inst *TableDescDto) GetPlainItemTypes(itemType PlainItemTypeE) []string {
	switch itemType {
	case PlainItemTypeEntityId:
		return inst.EntityIdTypes
	case PlainItemTypeEntityTimestamp:
		return inst.EntityTimestampTypes
	case PlainItemTypeEntityRouting:
		return inst.EntityRoutingTypes
	case PlainItemTypeEntityLifecycle:
		return inst.EntityLifecycleTypes
	case PlainItemTypeTransaction:
		return inst.TransactionTypes
	case PlainItemTypeOpaque:
		return inst.OpaqueColumnTypes
	}
	return nil
}
func (inst *TableDescDto) GetPlainItemEncodingHints(itemType PlainItemTypeE) []encodingaspects.AspectSet {
	switch itemType {
	case PlainItemTypeEntityId:
		return inst.EntityIdEncodingHints
	case PlainItemTypeEntityTimestamp:
		return inst.EntityTimestampEncodingHints
	case PlainItemTypeEntityRouting:
		return inst.EntityRoutingEncodingHints
	case PlainItemTypeEntityLifecycle:
		return inst.EntityLifecycleEncodingHints
	case PlainItemTypeTransaction:
		return inst.TransactionEncodingHints
	case PlainItemTypeOpaque:
		return inst.OpaqueColumnEncodingHints
	}
	return nil
}
func (inst *TableDescDto) GetPlainItemValueSemantics(itemType PlainItemTypeE) []valueaspects.AspectSet {
	switch itemType {
	case PlainItemTypeEntityId:
		return inst.EntityIdValueSemantics
	case PlainItemTypeEntityTimestamp:
		return inst.EntityTimestampValueSemantics
	case PlainItemTypeEntityRouting:
		return inst.EntityRoutingValueSemantics
	case PlainItemTypeEntityLifecycle:
		return inst.EntityLifecycleValueSemantics
	case PlainItemTypeTransaction:
		return inst.TransactionValueSemantics
	case PlainItemTypeOpaque:
		return inst.OpaqueColumnValueSemantics
	}
	return nil
}

const idColumnEst = 4
const tsColumnEst = 4
const roColumnEst = 4
const lcColumnEst = 4
const txColumnEst = 4
const oqColumnEst = 4
const taSectionEst = 4

func NewTableDescDto() *TableDescDto {
	inst := &TableDescDto{}
	inst.Reset()
	return inst
}
func initSlice[T any](in []T, n int) (out []T) {
	if in == nil {
		out = make([]T, 0, n)
	} else {
		out = in[:0]
	}
	return
}
func (inst *TableDescDto) Reset() {
	inst.DictionaryEntry.Name = ""
	inst.DictionaryEntry.Comment = ""
	inst.DictionaryEntry.Name = ""
	inst.DictionaryEntry.Comment = ""
	inst.EntityIdNames = initSlice(inst.EntityIdNames, idColumnEst)
	inst.EntityIdTypes = initSlice(inst.EntityIdTypes, idColumnEst)
	inst.EntityIdEncodingHints = initSlice(inst.EntityIdEncodingHints, idColumnEst)
	inst.EntityIdValueSemantics = initSlice(inst.EntityIdValueSemantics, idColumnEst)
	inst.EntityTimestampNames = initSlice(inst.EntityTimestampNames, tsColumnEst)
	inst.EntityTimestampTypes = initSlice(inst.EntityTimestampTypes, tsColumnEst)
	inst.EntityTimestampEncodingHints = initSlice(inst.EntityTimestampEncodingHints, tsColumnEst)
	inst.EntityTimestampValueSemantics = initSlice(inst.EntityTimestampValueSemantics, idColumnEst)
	inst.EntityRoutingNames = initSlice(inst.EntityRoutingNames, roColumnEst)
	inst.EntityRoutingTypes = initSlice(inst.EntityRoutingTypes, roColumnEst)
	inst.EntityRoutingEncodingHints = initSlice(inst.EntityRoutingEncodingHints, roColumnEst)
	inst.EntityRoutingValueSemantics = initSlice(inst.EntityRoutingValueSemantics, idColumnEst)
	inst.EntityLifecycleNames = initSlice(inst.EntityLifecycleNames, lcColumnEst)
	inst.EntityLifecycleTypes = initSlice(inst.EntityLifecycleTypes, lcColumnEst)
	inst.EntityLifecycleEncodingHints = initSlice(inst.EntityLifecycleEncodingHints, lcColumnEst)
	inst.EntityLifecycleValueSemantics = initSlice(inst.EntityLifecycleValueSemantics, idColumnEst)
	clear(inst.TaggedValuesSections)
	inst.TaggedValuesSections = initSlice(inst.TaggedValuesSections, taSectionEst)
	inst.TransactionNames = initSlice(inst.TransactionNames, txColumnEst)
	inst.TransactionTypes = initSlice(inst.TransactionTypes, txColumnEst)
	inst.TransactionEncodingHints = initSlice(inst.TransactionEncodingHints, txColumnEst)
	inst.TransactionValueSemantics = initSlice(inst.TransactionValueSemantics, idColumnEst)
	inst.OpaqueColumnNames = initSlice(inst.OpaqueColumnNames, oqColumnEst)
	inst.OpaqueColumnTypes = initSlice(inst.OpaqueColumnTypes, oqColumnEst)
	inst.OpaqueColumnEncodingHints = initSlice(inst.OpaqueColumnEncodingHints, oqColumnEst)
	inst.OpaqueColumnValueSemantics = initSlice(inst.OpaqueColumnValueSemantics, idColumnEst)
}

var ErrInvalidType = eh.Errorf("table contains an invalid canonical type")

type TechnologyDto struct {
	Id   string
	Name string
}
