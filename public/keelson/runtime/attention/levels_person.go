package attention

import (
	"encoding/json"
	"slices"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The person's table (ADR-0323 §SD4, §SD5): held by the host and kept
// across its restarts, edited in host chrome as a grid of kinds and levels.
// The conversations' overrides are not kept: a conversation's watch lasts
// as long as the host that holds it.

// PersonStoreI is where the host keeps the person's table: one value under
// one key.
type PersonStoreI interface {
	Get(key string) (value []byte, found bool, err error)
	Set(key string, value []byte) (err error)
}

// personStoreKey is the key the table is kept under.
const personStoreKey = "levels_person"

type wirePersonTable struct {
	V    uint8          `json:"v"`
	Rows []wireLevelRow `json:"rows,omitempty"`
}

// AttachPersonStore loads the person's table from store and keeps it there
// on every change of it. A kept row the table now refuses — a kind it no
// longer knows, a level across the kind's scale — is dropped and logged:
// the floor holds for what was kept too (§SD2). Load before AttachTrail:
// loading is not the person's change, and the trail is not told of it.
func AttachPersonStore(levels *Levels, store PersonStoreI, log zerolog.Logger) (err error) {
	if levels == nil || store == nil {
		return eh.Errorf("nil levels or store")
	}
	b, found, err := store.Get(personStoreKey)
	if err != nil {
		return eh.Errorf("read the person's table: %w", err)
	}
	if found {
		var w wirePersonTable
		if jerr := json.Unmarshal(b, &w); jerr != nil {
			log.Warn().Err(jerr).Msg("attention: the kept table of levels does not decode; the defaults stand")
		} else {
			for _, r := range w.Rows {
				if lerr := levels.load(keyOf(r), LevelE(r.Level)); lerr != nil {
					log.Warn().Err(lerr).Str("kind", r.Kind).Msg("attention: a kept level is refused and dropped")
				}
			}
		}
	}
	levels.SetOnPersonChange(func(table map[Key]LevelE) {
		rows := rowsOf(table)
		slices.SortFunc(rows, func(a, b wireLevelRow) int {
			switch {
			case a.Kind != b.Kind:
				if a.Kind < b.Kind {
					return -1
				}
				return 1
			case a.Effect != b.Effect:
				return int(a.Effect) - int(b.Effect)
			case a.App < b.App:
				return -1
			case a.App > b.App:
				return 1
			}
			return 0
		})
		v, jerr := json.Marshal(wirePersonTable{V: wireVersion, Rows: rows})
		if jerr == nil {
			jerr = store.Set(personStoreKey, v)
		}
		if jerr != nil {
			log.Warn().Err(jerr).Msg("attention: the person's table of levels was not kept; it holds until the host stops")
		}
	})
	return
}

// GridRow is a row of the person's grid: a key, and its default level.
type GridRow struct {
	Key     Key
	Default LevelE
	// Note says what the key does not separate, where the default does.
	Note string
}

// gridEffects are the effects a row is narrowed by where the defaults
// differ between them.
var gridEffects = []app.OperationEffectE{app.OperationEffectNone, app.OperationEffectView, app.OperationEffectDocument, app.OperationEffectRun, app.OperationEffectConsequential}

// GridRows are the rows of the person's grid (ADR-0323 §SD5): the kinds the
// defaults of §SD6 name, intent and asked by effect, then the host's own.
// The grid does not narrow by app; the table does, for whoever sets it.
func GridRows() (rows []GridRow) {
	add := func(k Key, note string) {
		rows = append(rows, GridRow{Key: k, Default: DefaultLevel(Event{Kind: k.Kind, Effect: k.Effect}), Note: note})
	}
	for _, kind := range []string{"user", "request", "reply"} {
		add(Key{Kind: kind}, "")
	}
	for _, e := range gridEffects {
		add(Key{Kind: "intent", Effect: e}, "")
	}
	add(Key{Kind: "outcome"}, "a refused outcome is message by default; one row sets both")
	for _, kind := range []string{"note", "turn", "answered"} {
		add(Key{Kind: kind}, "")
	}
	add(Key{Kind: "asked"}, "")
	add(Key{Kind: "asked", Effect: app.OperationEffectConsequential}, "")
	for _, kind := range hostKinds {
		add(Key{Kind: kind}, "")
	}
	return
}

// SetOnPersonChange installs the hook told the person's table after each
// change of it.
func (inst *Levels) SetOnPersonChange(f func(table map[Key]LevelE)) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.onPerson = f
}

// load puts a kept row in the person's table, under the floor, telling no
// hook.
func (inst *Levels) load(key Key, level LevelE) (err error) {
	scale, known := ScaleOf(key.Kind)
	switch {
	case !known:
		return eh.Errorf("no such kind")
	case level.Scale() != scale:
		return eh.Errorf("the level is not on the kind's scale")
	}
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.person == nil {
		inst.person = make(map[Key]LevelE, 8)
	}
	inst.person[key] = level
	inst.version++
	return
}
