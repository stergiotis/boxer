package attention

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The levels on the bus (ADR-0323 §SD5, §SD7): an app reads the table its
// conversation sees, classifies with it locally, and reads it again when the
// host announces a change. An app sets levels on the attention scale for its
// own conversations; the waiting scale is the person's, in host chrome.

const (
	// SubjectLevels answers the table one conversation sees.
	SubjectLevels = SubjectPrefix + "levels"
	// SubjectLevelSet sets or clears one attention-scale override of a
	// conversation.
	SubjectLevelSet = SubjectPrefix + "level"
	// SubjectChanged announces the table's new version.
	SubjectChanged = SubjectPrefix + "changed"
)

type wireLevelRow struct {
	Kind   string `json:"kind"`
	Effect uint8  `json:"effect,omitempty"`
	App    string `json:"app,omitempty"`
	Level  uint8  `json:"level"`
}

type wireLevels struct {
	V            uint8          `json:"v"`
	Version      uint64         `json:"version"`
	Person       []wireLevelRow `json:"person,omitempty"`
	Conversation []wireLevelRow `json:"conversation,omitempty"`
}

type wireLevelsRequest struct {
	V            uint8  `json:"v"`
	Conversation string `json:"conversation,omitempty"`
}

type wireLevelSet struct {
	V            uint8        `json:"v"`
	Conversation string       `json:"conversation"`
	Row          wireLevelRow `json:"row"`
}

type wireAck struct {
	V      uint8  `json:"v"`
	Ok     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

type wireChanged struct {
	V       uint8  `json:"v"`
	Version uint64 `json:"version"`
}

func rowsOf(m map[Key]LevelE) (rows []wireLevelRow) {
	rows = make([]wireLevelRow, 0, len(m))
	for k, l := range m {
		rows = append(rows, wireLevelRow{Kind: k.Kind, Effect: uint8(k.Effect), App: string(k.App), Level: uint8(l)})
	}
	return
}

func tableOf(rows []wireLevelRow) (m map[Key]LevelE) {
	if len(rows) == 0 {
		return nil
	}
	m = make(map[Key]LevelE, len(rows))
	for _, r := range rows {
		m[keyOf(r)] = LevelE(r.Level)
	}
	return
}

func keyOf(r wireLevelRow) Key {
	return Key{Kind: r.Kind, Effect: app.OperationEffectE(r.Effect), App: app.AppIdT(r.App)}
}

func (inst *BusService) levelsReply(msg *app.Msg) (rep any) {
	req, err := buscodec.Decode[wireLevelsRequest](msg.Payload)
	if err != nil {
		return wireLevels{V: wireVersion}
	}
	s := inst.levels.Snapshot(req.Conversation)
	return wireLevels{V: wireVersion, Version: s.Version, Person: rowsOf(s.Person), Conversation: rowsOf(s.Conversation)}
}

func (inst *BusService) levelSet(msg *app.Msg) (rep wireAck) {
	rep.V = wireVersion
	req, err := buscodec.Decode[wireLevelSet](msg.Payload)
	if err != nil {
		rep.Reason = "the request does not decode"
		return
	}
	k := keyOf(req.Row)
	if req.Row.Level == 0 {
		err = inst.levels.Clear(AuthorApp, msg.Sender, req.Conversation, k)
	} else {
		err = inst.levels.Set(AuthorApp, msg.Sender, req.Conversation, k, LevelE(req.Row.Level))
	}
	if err != nil {
		rep.Reason = err.Error()
		return
	}
	rep.Ok = true
	return
}

func (inst *BusService) announce(version uint64) {
	b, err := buscodec.Encode(wireChanged{V: wireVersion, Version: version})
	if err != nil {
		return
	}
	if err = inst.client.Publish(SubjectChanged, b); err != nil {
		inst.log.Warn().Err(err).Msg("attention: announce a change of the levels")
	}
}

// The client side, for an app holding ClientCaps.

// FetchLevels reads the table conversation sees.
func FetchLevels(bus app.BusI, conversation string) (s Snapshot, err error) {
	payload, err := buscodec.Encode(wireLevelsRequest{V: wireVersion, Conversation: conversation})
	if err != nil {
		return
	}
	raw, err := bus.Request(SubjectLevels, payload)
	if err != nil {
		err = eh.Errorf("levels: %w", err)
		return
	}
	w, err := buscodec.Decode[wireLevels](raw)
	if err != nil {
		return
	}
	return Snapshot{Version: w.Version, Person: tableOf(w.Person), Conversation: tableOf(w.Conversation)}, nil
}

// SetLevel sets an attention-scale override of conversation, or clears it
// with LevelUnspecified.
func SetLevel(bus app.BusI, conversation string, key Key, level LevelE) (err error) {
	payload, err := buscodec.Encode(wireLevelSet{V: wireVersion, Conversation: conversation,
		Row: wireLevelRow{Kind: key.Kind, Effect: uint8(key.Effect), App: string(key.App), Level: uint8(level)}})
	if err != nil {
		return
	}
	raw, err := bus.Request(SubjectLevelSet, payload)
	if err != nil {
		err = eh.Errorf("set level: %w", err)
		return
	}
	rep, err := buscodec.Decode[wireAck](raw)
	if err != nil {
		return
	}
	if !rep.Ok {
		err = eb.Build().Str("kind", key.Kind).Str("reason", rep.Reason).Errorf("the host refused the level")
	}
	return
}

// SubscribeChanges calls f with each new version of the levels; unsubscribe
// stops it.
func SubscribeChanges(bus app.BusI, f func(version uint64)) (unsubscribe func(), err error) {
	return bus.Subscribe(SubjectChanged, func(msg *app.Msg) {
		if w, derr := buscodec.Decode[wireChanged](msg.Payload); derr == nil {
			f(w.Version)
		}
	})
}
