package attention

import (
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ScaleE is one of the two scales of ADR-0323 §SD1.
type ScaleE uint8

const (
	// ScaleAttention holds events nothing waits on.
	ScaleAttention ScaleE = iota + 1
	// ScaleWaiting holds events a turn or a decision is blocked on until
	// the person acts.
	ScaleWaiting
)

func (inst ScaleE) String() (s string) {
	switch inst {
	case ScaleAttention:
		s = "attention"
	case ScaleWaiting:
		s = "waiting"
	default:
		s = "unspecified"
	}
	return
}

// LevelE is how loud an event is, low to high; the first three are the
// attention scale, the last two the waiting scale (ADR-0323 §SD1).
type LevelE uint8

const (
	LevelUnspecified LevelE = iota
	LevelData
	LevelMessage
	LevelHighlight
	LevelYourTurn
	LevelCritical
)

var AllLevels = []LevelE{LevelData, LevelMessage, LevelHighlight, LevelYourTurn, LevelCritical}

func (inst LevelE) String() (s string) {
	switch inst {
	case LevelData:
		s = "data"
	case LevelMessage:
		s = "message"
	case LevelHighlight:
		s = "highlight"
	case LevelYourTurn:
		s = "your turn"
	case LevelCritical:
		s = "critical"
	default:
		s = "unspecified"
	}
	return
}

// Scale is the scale the level belongs to.
func (inst LevelE) Scale() (s ScaleE) {
	switch inst {
	case LevelData, LevelMessage, LevelHighlight:
		s = ScaleAttention
	case LevelYourTurn, LevelCritical:
		s = ScaleWaiting
	}
	return
}

// The host's own kinds (ADR-0323 §SD3): what the host asks the person. Every
// one is on the waiting scale.
const (
	KindGrantRequest      = "grant.request"
	KindWidening          = "grant.widening"
	KindConfirmation      = "confirmation"
	KindModeratorQuestion = "moderator.question"
	KindQuestion          = "question"
	KindExitApproval      = "exit.approval"
)

var hostKinds = []string{KindGrantRequest, KindWidening, KindConfirmation, KindModeratorQuestion, KindQuestion, KindExitApproval}

// ScaleOf is the scale of kind, and whether kind belongs to the closed
// vocabulary: the host's kinds and the run journal's entry kinds (ADR-0308
// §SD2). Which scale a kind is on is code, never configuration (ADR-0323
// §SD1).
func ScaleOf(kind string) (s ScaleE, known bool) {
	for _, k := range hostKinds {
		if k == kind {
			return ScaleWaiting, true
		}
	}
	if k, ok := agentrun.ParseKind(kind); ok {
		if k == agentrun.KindAsked {
			return ScaleWaiting, true
		}
		return ScaleAttention, true
	}
	return
}

// Event is what is classified: its kind, the effect of the operation it is
// about where it has one, the app it comes from and its conversation.
// Refused marks an outcome that did not succeed.
type Event struct {
	Kind         string
	Effect       app.OperationEffectE
	App          app.AppIdT
	Conversation string
	Refused      bool
}

// Key is an override's row (ADR-0323 §SD3): a kind, optionally narrowed by
// effect and app. The zero effect and the empty app match any.
type Key struct {
	Kind   string
	Effect app.OperationEffectE
	App    app.AppIdT
}

// AuthorE is who sets a level; it decides which scale they may set
// (ADR-0323 §SD5).
type AuthorE uint8

const (
	// AuthorPerson is the person, in host chrome.
	AuthorPerson AuthorE = iota + 1
	// AuthorApp is the app that owns a conversation, on the person's action
	// in its window.
	AuthorApp
)

func (inst AuthorE) String() (s string) {
	switch inst {
	case AuthorPerson:
		s = "person"
	case AuthorApp:
		s = "app"
	default:
		s = "unspecified"
	}
	return
}

// Change is one override set or cleared; Level is LevelUnspecified when
// cleared.
type Change struct {
	Conversation string
	Key          Key
	Level        LevelE
	Author       AuthorE
	App          app.AppIdT
}

// Levels is the table of ADR-0323: defaults, the person's overrides and
// per-conversation overrides, the most specific winning (§SD4). The zero
// value classifies by the defaults alone.
type Levels struct {
	mu     sync.Mutex
	person map[Key]LevelE
	conv   map[string]map[Key]LevelE
	// onWaiting is told every change on the waiting scale, for the trail
	// (§SD5).
	onWaiting func(Change)
	version   uint64
}

// SetOnWaitingChange installs the hook told of every change on the waiting
// scale.
func (inst *Levels) SetOnWaitingChange(f func(Change)) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.onWaiting = f
}

// Version counts the changes; a reader compares it to know its copy is
// stale (ADR-0323 §SD7).
func (inst *Levels) Version() (v uint64) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.version
}

// Classify is ev's level.
func (inst *Levels) Classify(ev Event) (l LevelE) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	keys := [4]Key{{ev.Kind, ev.Effect, ev.App}, {ev.Kind, ev.Effect, ""}, {ev.Kind, 0, ev.App}, {ev.Kind, 0, ""}}
	if ev.Conversation != "" {
		if m := inst.conv[ev.Conversation]; m != nil {
			for _, k := range keys {
				if l = m[k]; l != LevelUnspecified {
					return
				}
			}
		}
	}
	for _, k := range keys {
		if l = inst.person[k]; l != LevelUnspecified {
			return
		}
	}
	return DefaultLevel(ev)
}

// DefaultLevel is ev's level by the defaults of ADR-0323 §SD6. A kind
// outside the vocabulary is data.
func DefaultLevel(ev Event) (l LevelE) {
	switch ev.Kind {
	case KindConfirmation, KindModeratorQuestion:
		return LevelCritical
	case KindGrantRequest, KindWidening, KindQuestion, KindExitApproval:
		return LevelYourTurn
	}
	k, ok := agentrun.ParseKind(ev.Kind)
	if !ok {
		return LevelData
	}
	switch k {
	case agentrun.KindAsked:
		if ev.Effect == app.OperationEffectConsequential {
			return LevelCritical
		}
		return LevelYourTurn
	case agentrun.KindIntent:
		if ev.Effect == app.OperationEffectDocument || ev.Effect == app.OperationEffectRun || ev.Effect == app.OperationEffectConsequential {
			return LevelMessage
		}
	case agentrun.KindOutcome:
		if ev.Refused {
			return LevelMessage
		}
	case agentrun.KindNote, agentrun.KindTurn:
		return LevelMessage
	}
	return LevelData
}

// Set stores an override; conversation empty is the person's table. It
// refuses an unknown kind, a level across the kind's scale (the floor,
// ADR-0323 §SD2) and an app's change on the waiting scale (§SD5).
func (inst *Levels) Set(author AuthorE, by app.AppIdT, conversation string, key Key, level LevelE) (err error) {
	scale, known := ScaleOf(key.Kind)
	if !known {
		err = eb.Build().Str("kind", key.Kind).Errorf("no such kind")
		return
	}
	if level.Scale() != scale {
		err = eb.Build().Str("kind", key.Kind).Stringer("level", level).Stringer("scale", scale).Errorf("the level is not on the kind's scale")
		return
	}
	if scale == ScaleWaiting && author != AuthorPerson {
		err = eb.Build().Str("kind", key.Kind).Stringer("author", author).Errorf("only the person sets a level on the waiting scale")
		return
	}
	if author == AuthorApp && conversation == "" {
		err = eb.Build().Str("kind", key.Kind).Errorf("an app sets levels for a conversation only")
		return
	}
	inst.store(Change{Conversation: conversation, Key: key, Level: level, Author: author, App: by}, scale)
	return
}

// Clear removes an override; the same authors may clear what they may set.
func (inst *Levels) Clear(author AuthorE, by app.AppIdT, conversation string, key Key) (err error) {
	scale, known := ScaleOf(key.Kind)
	if !known {
		err = eb.Build().Str("kind", key.Kind).Errorf("no such kind")
		return
	}
	if scale == ScaleWaiting && author != AuthorPerson {
		err = eb.Build().Str("kind", key.Kind).Stringer("author", author).Errorf("only the person sets a level on the waiting scale")
		return
	}
	if author == AuthorApp && conversation == "" {
		err = eb.Build().Str("kind", key.Kind).Errorf("an app sets levels for a conversation only")
		return
	}
	inst.store(Change{Conversation: conversation, Key: key, Author: author, App: by}, scale)
	return
}

// ClearConversation drops a conversation's overrides, when it is deleted.
func (inst *Levels) ClearConversation(conversation string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if _, ok := inst.conv[conversation]; ok {
		delete(inst.conv, conversation)
		inst.version++
	}
}

func (inst *Levels) store(ch Change, scale ScaleE) {
	inst.mu.Lock()
	var m map[Key]LevelE
	if ch.Conversation == "" {
		if inst.person == nil {
			inst.person = make(map[Key]LevelE, 8)
		}
		m = inst.person
	} else {
		if inst.conv == nil {
			inst.conv = make(map[string]map[Key]LevelE, 8)
		}
		if m = inst.conv[ch.Conversation]; m == nil {
			m = make(map[Key]LevelE, 4)
			inst.conv[ch.Conversation] = m
		}
	}
	if ch.Level == LevelUnspecified {
		delete(m, ch.Key)
	} else {
		m[ch.Key] = ch.Level
	}
	inst.version++
	hook := inst.onWaiting
	inst.mu.Unlock()
	if scale == ScaleWaiting && hook != nil {
		hook(ch)
	}
}

// Overrides lists the overrides of one scope: conversation empty is the
// person's table.
func (inst *Levels) Overrides(conversation string) (out map[Key]LevelE) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	src := inst.person
	if conversation != "" {
		src = inst.conv[conversation]
	}
	out = make(map[Key]LevelE, len(src))
	for k, v := range src {
		out[k] = v
	}
	return
}
