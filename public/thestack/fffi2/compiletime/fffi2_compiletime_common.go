package compiletime

import (
	"errors"
	"fmt"
	"iter"
	"math/bits"
	"slices"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/compiletimeflags"
	"golang.org/x/exp/constraints"
)

var ErrInvalidState = eh.Errorf("builder is in wrong state")

type StateAndErrTracker[T constraints.Unsigned] struct {
	errs               []error
	initial            T
	state              T
	transitionActions  map[string]func()
	ErrorMessagePrefix string
}

func NewStateAndErrTracker[T constraints.Unsigned](initial T, errorMessagePrefix string) *StateAndErrTracker[T] {
	if compiletimeflags.ExtraChecks && initial == 0 {
		log.Panic().Msg("initial state is 0 (states are bit flags, 0 is not a meaningful state)")
	}
	return &StateAndErrTracker[T]{
		errs:               nil,
		initial:            initial,
		state:              initial,
		transitionActions:  make(map[string]func(), 2),
		ErrorMessagePrefix: errorMessagePrefix,
	}
}
func (inst *StateAndErrTracker[T]) ResetStateAndError() {
	inst.state = inst.initial
	if len(inst.errs) > 0 {
		clear(inst.errs)
		inst.errs = inst.errs[:0]
	}
}
func (inst *StateAndErrTracker[T]) SetTransitionActionPost(src T, dest T, action func()) {
	key := fmt.Sprintf("%d->%d P", src, dest)
	inst.transitionActions[key] = action
}
func (inst *StateAndErrTracker[T]) SetReachActionPost(dest T, action func()) {
	key := fmt.Sprintf("%d P", dest)
	inst.transitionActions[key] = action
}
func (inst *StateAndErrTracker[T]) GetState() T {
	return inst.state
}
func (inst *StateAndErrTracker[T]) MergeError(err error) {
	if err != nil {
		inst.errs = append(inst.errs, err)
	}
}
func (inst *StateAndErrTracker[T]) CheckAndTransitionState(destState T, allowed T) (srcState T) {
	srcState = inst.state
	inst.CheckState(allowed)
	inst.state = destState
	if len(inst.transitionActions) > 0 {
		key1 := fmt.Sprintf("%d->%d P", srcState, destState)
		a1 := inst.transitionActions[key1]
		if a1 != nil {
			a1()
		}
		key2 := fmt.Sprintf("%d P", destState)
		a2 := inst.transitionActions[key2]
		if a2 != nil {
			a2()
		}
	}
	return
}
func iterateHighBits(v uint64, offset uint8) iter.Seq2[uint8, uint64] {
	return func(yield func(uint8, uint64) bool) {
		if v == 0 {
			return
		}
		n := bits.OnesCount64(v)
		for i := uint8(0); i < 64; i++ {
			m := uint64(1) << i
			if (m & v) != 0 {
				n--
				if !yield(i+offset, m) || n == 0 {
					break
				}
			}
		}
	}
}

func (inst *StateAndErrTracker[T]) CheckState(allowed T) {
	if inst.state&allowed == 0 {
		inst.MergeError(eb.Build().
			Uint64("currentState", uint64(inst.state)).
			Uints64("allowedStates", slices.Collect(functional.IterRightOnly(iterateHighBits(uint64(allowed), 1)))).
			Errorf("%s: %w", inst.ErrorMessagePrefix, ErrInvalidState))
	}
}
func (inst *StateAndErrTracker[T]) Check(destSate T, allowedStates T) (err error) {
	inst.CheckAndTransitionState(destSate, allowedStates)
	if len(inst.errs) > 0 {
		err = errors.Join(inst.errs...)
	}
	return
}
