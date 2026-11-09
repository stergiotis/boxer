package chat

import (
	"context"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
)

// The Simple view of the plain chat: the bar folded under Details, the
// artefact brought forward, and a running turn described in plain words. The
// person's choice is kept in the app's storage, so the next window opens the
// way the last one was left.
const simpleViewKey = "simple-view"

const (
	simpleViewOn  = "on"
	simpleViewOff = "off"
)

// sceneMarkPrefix begins the component mark of a host a scene launched.
const sceneMarkPrefix = "scene-"

// simpleStoreFor is where a window keeps the choice, nil for one that does
// not. A preset's document says what its view is. A scene shares the store of
// the machine it runs on, so it would otherwise draw what someone last ticked
// there, and its own ticks would reach that person's next window.
func simpleStoreFor(storage app.StorageI, preset bool, mark string) app.StorageI {
	if preset || strings.HasPrefix(mark, sceneMarkPrefix) {
		return nil
	}
	return storage
}

// guided is whether the window describes its work for someone who does not
// know the harness: always for a preset, by the setting in the plain chat.
func (inst *App) guided() bool {
	return inst.preset != nil || inst.simple
}

// loadSimpleView reads the kept choice off the frame: a Get is a bus round
// trip, which the host's persist service answers.
func (inst *App) loadSimpleView() {
	if inst.simpleStore == nil {
		return
	}
	storage := inst.simpleStore
	// Not a task: nothing the person would want listed or stopped.
	inst.simpleJob.Start(nil, bgjob.Spec{Kind: "chat-simple-view", Title: "the view setting"}, func(context.Context) (on *bool, err error) {
		v, found, err := storage.Get(simpleViewKey)
		if err != nil {
			return
		}
		kept := found && string(v) == simpleViewOn
		return &kept, nil
	})
}

// syncSimpleView takes the kept choice once it is read and keeps a changed
// one. The render goroutine calls it once per frame; a write happens only on
// a change, one at a time so the last choice is the one that stays.
func (inst *App) syncSimpleView() {
	if inst.simpleStore == nil {
		return
	}
	if !inst.simpleRead {
		if on, _, ok := inst.simpleJob.TakeResult(); ok {
			inst.simpleRead = true
			if inst.simple == inst.simpleKept {
				// Unchanged since the window opened: a tick made while the
				// read was on its way is the newer choice, and stands.
				inst.simple = *on
			}
			inst.simpleKept = *on
		} else if inst.simpleJob.Snapshot().State == bgjob.StateFailed {
			// The window opens with the default, and a later change is
			// still kept if the store has come back.
			inst.simpleRead = true
		}
	}
	if !inst.simpleRead || inst.simple == inst.simpleKept || inst.simpleWriting.Load() {
		return
	}
	inst.simpleKept = inst.simple
	inst.simpleWriting.Store(true)
	storage, value := inst.simpleStore, simpleViewOff
	if inst.simple {
		value = simpleViewOn
	}
	started := inst.offFrame(func(context.Context) {
		defer inst.simpleWriting.Store(false)
		if err := storage.Set(simpleViewKey, []byte(value)); err != nil {
			inst.log.Debug().Err(err).Msg("chat: keep the simple view setting")
		}
	})
	if !started {
		inst.simpleWriting.Store(false)
	}
}
