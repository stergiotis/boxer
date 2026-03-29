package marshalling

import (
	"fmt"
	"iter"
	"slices"
)

// Tuple represents a ClickHouse tuple value with optional named slots.
// It uses []any for Go interop and is used by MarshalGoValueToSQL.
// For typed tuple representation, use TypedLiteral with KindTuple.
type Tuple struct {
	slotNames  []string
	slotValues []any
}

func NewTuple(slotNames []string) (inst *Tuple) {
	inst = &Tuple{
		slotNames:  slotNames,
		slotValues: make([]any, len(slotNames)),
	}
	return
}

func NewUnnamedTuple(values ...any) (inst *Tuple) {
	names := make([]string, len(values))
	for i := range names {
		names[i] = fmt.Sprintf("_%d", i)
	}
	inst = &Tuple{
		slotNames:  names,
		slotValues: make([]any, len(values)),
	}
	copy(inst.slotValues, values)
	return
}

func (inst *Tuple) Len() int { return len(inst.slotValues) }
func (inst *Tuple) SetByName(slotName string, val any) (found bool) {
	idx := slices.Index(inst.slotNames, slotName)
	found = idx != -1
	if found {
		inst.slotValues[idx] = val
	}
	return
}
func (inst *Tuple) SetByIndex(i int, val any) (found bool) {
	found = i >= 0 && i < len(inst.slotValues)
	if found {
		inst.slotValues[i] = val
	}
	return
}
func (inst *Tuple) GetByIndex(i int) (val any, found bool) {
	found = i >= 0 && i < len(inst.slotValues)
	if found {
		val = inst.slotValues[i]
	}
	return
}
func (inst *Tuple) GetByName(slotName string) (val any, found bool) {
	idx := slices.Index(inst.slotNames, slotName)
	found = idx != -1
	if found {
		val = inst.slotValues[idx]
	}
	return
}
func (inst *Tuple) IterateAll() iter.Seq2[int, any] {
	return func(yield func(int, any) bool) {
		for i, val := range inst.slotValues {
			if !yield(i, val) {
				return
			}
		}
	}
}
func (inst *Tuple) IterateAllWithNames() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for i, val := range inst.slotValues {
			if !yield(inst.slotNames[i], val) {
				return
			}
		}
	}
}
