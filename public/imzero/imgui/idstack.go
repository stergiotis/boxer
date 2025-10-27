//go:build !bootstrap

package imgui

import (
	"hash/crc32"
	"iter"
	"math/rand/v2"
	"strconv"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/unsafeperf"
)

type IdStack struct {
	stack      *containers.Stack[ImGuiID]
	DebugStack *containers.Stack[string]
	instanceId ImGuiID
	seed0      ImGuiID
}

func NewIdStack(enableDebug bool) *IdStack {
	var debugStack *containers.Stack[string]
	if enableDebug {
		debugStack = containers.NewStackSize[string](32)
	}
	return &IdStack{
		instanceId: ImGuiID(rand.Uint32()),
		seed0:      0,
		stack:      containers.NewStackSize[ImGuiID](32),
		DebugStack: debugStack,
	}
}
func (inst *IdStack) reset() {
	inst.stack.Reset()
	ds := inst.DebugStack
	if ds != nil {
		ds.Reset()
	}
}
func (inst *IdStack) SyncSeed() *IdStack {
	inst.seed0 = GetItemID()
	inst.reset()
	return inst
}
func (inst *IdStack) SetSeed(seed ImGuiID) *IdStack {
	inst.seed0 = seed
	return inst
}
func (inst *IdStack) ForceResetPush() *IdStack {
	inst.seed0 = inst.instanceId
	PushOverrideID(inst.seed0)
	inst.reset()
	return inst
}
func (inst *IdStack) ForceResetPop() *IdStack {
	if len(inst.stack.Items) > 0 {
		log.Warn().Strs("debugStack", inst.DebugStack.Items).Interface("stack", inst.stack.Items).Msg("non-empty id stack in ForceResetPop")
	}
	PopID()
	return inst
}
func (inst *IdStack) GetSeed() ImGuiID {
	return inst.seed0
}
func (inst *IdStack) GetCurrent() ImGuiID {
	return inst.stack.PeekDefault(inst.seed0)
}

var imguiCrc32Table = crc32.MakeTable(crc32.Castagnoli) // SSE4.2 compatible

func calculateImGuiSeededId(seed ImGuiID, strId string) (id ImGuiID) {
	id = ImGuiID(crc32.Update(uint32(seed), imguiCrc32Table, unsafeperf.UnsafeStringToByte(strId)))
	return
}
func (inst *IdStack) AddIDString(str string) *IdStack {
	ds := inst.DebugStack
	if ds != nil {
		ds.Push(str)
	}
	inst.stack.Push(calculateImGuiSeededId(inst.GetCurrent(), str))
	return inst
}
func (inst *IdStack) PushIDString(str string) *IdStack {
	PushID(str)
	inst.AddIDString(str)
	return inst
}
func (inst *IdStack) AddIDInt(id int) *IdStack {
	s := strconv.FormatInt(int64(id), 16)
	inst.AddIDString(s)
	return inst
}
func (inst *IdStack) PushIDInt(id int) *IdStack {
	// push as string id to prevent endianess and two's complement issues
	s := strconv.FormatInt(int64(id), 16)
	return inst.PushIDString(s)
}
func (inst *IdStack) RemoveID() *IdStack {
	ds := inst.DebugStack
	if ds != nil {
		_, _ = ds.Pop()
	}
	_, err := inst.stack.Pop()
	if err != nil {
		log.Warn().Msg("disbalanced id stack add/remove or push/pop")
	}
	return inst
}
func (inst *IdStack) PopID() *IdStack {
	PopID()
	return inst.RemoveID()
}
func (inst *IdStack) PushIDStringR(id string) iter.Seq[string] {
	return func(yield func(string) bool) {
		inst.PushIDString(id)
		defer inst.PopID()
		yield(id)
	}
}
func (inst *IdStack) PushIDIntR(id int) iter.Seq[int] {
	return func(yield func(int) bool) {
		inst.PushIDInt(id)
		defer inst.PopID()
		yield(id)
	}
}
func (inst *IdStack) AddIDStringR(id string) iter.Seq[string] {
	return func(yield func(string) bool) {
		inst.AddIDString(id)
		defer inst.RemoveID()
		yield(id)
	}
}
func (inst *IdStack) AddIDIntR(id int) iter.Seq[int] {
	return func(yield func(int) bool) {
		inst.AddIDInt(id)
		defer inst.RemoveID()
		yield(id)
	}
}
