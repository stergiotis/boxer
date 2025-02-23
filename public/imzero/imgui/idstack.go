//go:build !bootstrap

package imgui

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"hash/crc32"
	"math/rand"
	"strconv"
)

type IdStack struct {
	instanceId ImGuiID
	seed0      ImGuiID
	stack      *containers.Stack[ImGuiID]
	DebugStack *containers.Stack[string]
}

func NewIdStack(enableDebug bool) *IdStack {
	var debugStack *containers.Stack[string]
	if enableDebug {
		debugStack = containers.NewStackSize[string](32)
	}
	return &IdStack{
		instanceId: ImGuiID(rand.Uint32()), //nolint:gosec
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
func (inst *IdStack) AddIDString(str string) *IdStack {
	ds := inst.DebugStack
	if ds != nil {
		ds.Push(str)
	}
	inst.stack.Push(ImGuiID(crc32.Update(uint32(inst.GetCurrent()), crc32.IEEETable, unsafeperf.UnsafeStringToByte(str))))
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
