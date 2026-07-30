package bindings

import (
	"fmt"
	"iter"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/compiletimeflags"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/hashing/splitmix64"
	"github.com/zeebo/xxh3"
)

var seenIds = containers.NewHashSet[uint64](10000)

func checkId(id uint64) uint64 {
	if seenIds.Has(id) {
		log.Warn().Uint64("id", id).Caller(2).Msg("id has already been used")
	} else {
		seenIds.Add(id)
	}
	if compiletimeflags.ExtraChecks && id == 0 {
		log.Panic().Msg("id value 0 is disallowed by egui...")
	}
	return id
}

// zeroIdReplacement stands in for a derived id of exactly zero, the one
// value egui forbids (`egui::Id` wraps a NonZeroU64; the client's
// `Id::from_high_entropy_bits` panics on zero). An arbitrary high-entropy
// odd constant — the only requirements are non-zero and "unlikely to be
// produced by a real derivation", since an id that genuinely derives to
// this value would alias the stand-in.
const zeroIdReplacement uint64 = 0xd6e8feb86659fd93

// ensureNotZeroId maps zero onto [zeroIdReplacement] and leaves every
// other value untouched.
//
// The mapping is INJECTIVE, and that is the whole point. Its predecessor
// was `id |= 1`, which is not: it merges each even id with its odd
// successor. Losing one bit of a 64-bit hash is unobservable, so the
// merge was invisible for ids derived from a label — but these creators
// also take an id the caller supplies directly, and there the collapse
// bites. MakeAbsoluteIdHighEntropy(n) and MakeAbsoluteIdHighEntropy(n+1)
// for even n derived to the SAME id, so two widgets shared one r7
// response slot and only the later one's flags survived Sync's
// newest-wins compaction: the earlier widget rendered, took its click
// Rust-side (egui hit-tests on its own auto-ids, not on ours — see
// apply_widget), and then read back nothing through SendResp. Numbering a
// handful of buttons with consecutive integers is exactly what a caller
// reaches for.
//
// Applied at the derive boundary rather than at construction, so a
// relative id whose XOR chain cancels to zero (nesting the same key twice:
// PrepareStr("a") inside IdScope(PrepareStr("a"))) is caught too — that
// case used to put a zero id on the wire and panic the client.
func ensureNotZeroId(id uint64) uint64 {
	if id == 0 {
		return zeroIdReplacement
	}
	return id
}

var zeroSplitMix64PlusOne = splitmix64.Reverse(0) + uint64(1)

func makeHighEntropy(idx uint64) uint64 {
	return splitmix64.Forward(idx + zeroSplitMix64PlusOne)
}

type WidgetIdCreatorI interface {
	// Derive side effect free
	Derive() uint64
	// DeriveStacked side effect: stack manipulation
	DeriveStacked() uint64
	// PopIdFromStack side effect: stack manipulation
	PopIdFromStack()
	// PopIdFromStackChecked side effect: stack manipulation
	PopIdFromStackChecked(expectedId uint64)
}

func hashLabelToId(label string) uint64 {
	return xxh3.HashString(label)
}

// AbsoluteWidgetId is an id that replaces the stack value instead of
// composing with it — for top-level windows, modals and global overlays.
//
// The constructors normalise, so the numeric value of an AbsoluteWidgetId
// IS the id that goes on the wire: `uint64(id) == id.Derive()` for every
// id built through them. Code that keys its own side tables by an
// absolute id (probe seqs, retained per-widget state) can therefore use
// either spelling without the two drifting apart. That did not hold
// before: `Derive` OR-ed in bit 0, so `uint64(MakeAbsoluteIdStr(s))`
// disagreed with the id on the wire for roughly half of all labels.
type AbsoluteWidgetId uint64

var _ WidgetIdCreatorI = AbsoluteWidgetId(0)

func MakeAbsoluteIdStr(str string) AbsoluteWidgetId {
	return AbsoluteWidgetId(ensureNotZeroId(hashLabelToId(str)))
}

// MakeAbsoluteIdHighEntropy takes the caller's value as the id verbatim.
// Distinct arguments yield distinct ids — including adjacent integers,
// which an earlier normalisation silently collapsed in pairs. The name
// still asks for high entropy because the value reaches egui's IdMap
// unhashed, so clustered ids cost lookup performance; they no longer cost
// correctness.
func MakeAbsoluteIdHighEntropy(id uint64) AbsoluteWidgetId {
	return AbsoluteWidgetId(ensureNotZeroId(id))
}
func MakeAbsoluteIdSeq(idx uint64) AbsoluteWidgetId {
	return AbsoluteWidgetId(ensureNotZeroId(makeHighEntropy(idx)))
}
func (inst AbsoluteWidgetId) Derive() uint64 {
	return ensureNotZeroId(uint64(inst))
}
func (inst AbsoluteWidgetId) DeriveStacked() uint64 {
	return inst.Derive()
}

func (inst AbsoluteWidgetId) PopIdFromStack() {
	// no-op
}

func (inst AbsoluteWidgetId) PopIdFromStackChecked(expectedId uint64) {
	// no-op
}

type WidgetIdStackStateE uint8

func (inst WidgetIdStackStateE) String() string {
	switch inst {
	case WidgetIdStackInitial:
		return "initial"
	case WidgetIdStackPrepared:
		return "prepared"
	}
	return "<invalid>"
}

const (
	WidgetIdStackInitial  WidgetIdStackStateE = 0
	WidgetIdStackPrepared WidgetIdStackStateE = 1
)

var _ fmt.Stringer = WidgetIdStackStateE(0)

type WidgetIdStack struct {
	idStack     *containers.Stack[uint64]
	currentName string
	currentId   uint64
	baseSalt    uint64
	state       WidgetIdStackStateE
}

func NewWidgetIdStack() *WidgetIdStack {
	return &WidgetIdStack{
		idStack:     containers.NewStack[uint64](),
		currentName: "",
		currentId:   0,
		state:       WidgetIdStackInitial,
	}
}

// SetBaseSalt installs a permanent instance salt: it acts as the empty
// stack's base id, so every derived id — and every scope built on the stack
// — XORs with it, and unlike a pushed scope it survives Reset. A multi-stack
// component (e.g. a PlayApp instance and its per-driver stacks) salts all
// its stacks with one per-instance value so two instances rendering in the
// same frame cannot collide in the global seenIds registry or share egui
// widget state. Zero (the default) reproduces the unsalted behaviour.
// Set it before the first Prepare/Derive; changing it mid-frame would
// unbalance PopIdFromStackChecked expectations.
func (inst *WidgetIdStack) SetBaseSalt(salt uint64) {
	inst.baseSalt = salt
}

var _ WidgetIdCreatorI = (*WidgetIdStack)(nil)

func (inst *WidgetIdStack) Derive() (id uint64) {
	inst.transitionState(WidgetIdStackInitial, WidgetIdStackPrepared)
	id = inst.currentId
	last := inst.peekIdFromStack()
	id = ensureNotZeroId(last ^ id)
	return
}
func (inst *WidgetIdStack) DeriveStacked() uint64 {
	id := inst.Derive()
	inst.pushIdToStack(id)
	return id
}

func (inst *WidgetIdStack) transitionState(to WidgetIdStackStateE, allowed WidgetIdStackStateE) {
	if inst.state != allowed {
		log.Panic().Stack().Stringer("state", inst.state).Stringer("allowed", allowed).Msg("invalid state transition")
	}
	inst.state = to
}
func (inst *WidgetIdStack) verifyState(allowed WidgetIdStackStateE) {
	if inst.state != allowed {
		log.Panic().Msg("builder is in wrong state")
	}
}

// PrepareStr, PrepareSeq and PrepareHighEntropy keep the caller's value
// verbatim; the non-zero guard runs once at [WidgetIdStack.Derive], after
// the XOR with the enclosing scope. Normalising here instead would be both
// too early (the XOR can still land on zero) and lossy, since it applied
// to distinct arguments before they were ever combined.
func (inst *WidgetIdStack) PrepareStr(str string) *WidgetIdStack {
	inst.transitionState(WidgetIdStackPrepared, WidgetIdStackInitial)
	inst.currentName = str
	inst.currentId = hashLabelToId(str) // high entropy
	return inst
}

// PrepareSeq maps index sequences 0,1,2,3,... to valid ids (high-entropy, non-zero).
func (inst *WidgetIdStack) PrepareSeq(idx uint64) *WidgetIdStack {
	inst.transitionState(WidgetIdStackPrepared, WidgetIdStackInitial)
	inst.currentName = ""
	inst.currentId = makeHighEntropy(idx)
	return inst
}

// PrepareHighEntropy takes the caller's value as the scope-relative id
// verbatim. Distinct arguments stay distinct — including adjacent
// integers, which the previous normalisation collapsed in pairs.
func (inst *WidgetIdStack) PrepareHighEntropy(id uint64) *WidgetIdStack {
	inst.transitionState(WidgetIdStackPrepared, WidgetIdStackInitial)
	inst.currentName = ""
	inst.currentId = id
	return inst
}

func (inst *WidgetIdStack) pushIdToStack(id uint64) {
	//log.Trace().Caller(3).Int("depth", inst.idStack.Depth()).Msg("pushIdToStack")
	inst.idStack.Push(id)
}
func (inst *WidgetIdStack) pushIdToStackLabeled(id uint64, label string) {
	//log.Trace().Caller(3).Int("depth", inst.idStack.Depth()).Str("label", label).Msg("pushIdToStack")
	inst.idStack.Push(id)
}
func (inst *WidgetIdStack) PopIdFromStack() {
	inst.verifyState(WidgetIdStackInitial)
	//log.Trace().Caller(3).Int("depth", inst.idStack.Depth()).Msg("popIdToStack")
	_, err := inst.idStack.Pop()
	if err != nil {
		log.Warn().Caller(3).Msg("nesting id stack is empty (incorrect nesting/id handling)")
	}
}
func (inst *WidgetIdStack) PopIdFromStackChecked(expectedId uint64) {
	inst.verifyState(WidgetIdStackInitial)
	//log.Trace().Caller(3).Int("depth", inst.idStack.Depth()).Msg("popIdToStack")
	id2, err := inst.idStack.Pop()
	if err != nil {
		log.Warn().Caller(3).Msg("nesting id stack is empty (incorrect nesting/id handling)")
	}
	if id2 != expectedId {
		log.Warn().Caller(3).Uint64("expectedId", expectedId).Uint64("poppedId", id2).Msg("popped id does not match expected id")
	}
}
func (inst *WidgetIdStack) peekIdFromStack() uint64 {
	return inst.idStack.PeekDefault(inst.baseSalt)
}
func (inst *WidgetIdStack) Depth() int {
	return inst.idStack.Depth()
}
func (inst *WidgetIdStack) Reset() {
	inst.idStack.Reset()
}

// IdScope pushes id onto the widget id stack for the duration of the
// for-range body (via DeriveStacked) and pops it on exit (via
// PopIdFromStackChecked). Accepts any WidgetIdCreatorI so callers can
// scope under either a *WidgetIdStack (actual stack manipulation) or an
// AbsoluteWidgetId (no-op push/pop), matching the polymorphic contract
// used by every block-iterator factory in this package.
func IdScope(i WidgetIdCreatorI) iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		id := i.DeriveStacked()
		defer i.PopIdFromStackChecked(id)
		yield(functional.NilIteratorValue)
	}
}
