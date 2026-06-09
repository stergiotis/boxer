// Package ecsdemo is a two-stage, end-to-end example built on anchor's example
// schema. It models a small Entity-Component-System (ECS) and asks one question
// two different ways: "can this JSON document be unserialized into this shape?"
//
// Stage 1 (this file plus ecsdemo_json.go) answers with encoding/json/v2 and
// reflection only — no leeway runtime. Stage 2 (added later as ecsdemo_leeway.go)
// answers the same question through leeway's mappingplan -> marshallingen ->
// dml/ra pipeline, binding the same components to anchor's TableDesc sections.
// Both stages share the types declared here, so they stay in agreement.
//
// # The entity is the join key
//
// A component (Identity, Battery, Located, Tasked) is pure, id-free data — one
// aspect of an entity. An entity is *only* an id; what it "is" emerges from the
// components attached to that id. That gives two views of the same thing, joined
// on the id:
//
//   - World (storage / structure-of-arrays): one column per component type,
//     each keyed by entity id. Systems iterate these columns.
//   - Entity (gathered / array-of-structs): an id plus the components present
//     for it. This bundle is the "entity composed of components".
//
// Gather builds the Entity view from the World; Scatter writes it back. Those
// are the stage-1 analogues of leeway RA (FillFromArrow) and DML
// (BuildEntities), and an Entity is the analogue of a leeway DTO row: an id
// plain column plus populated sections.
//
// # Tree via nesting
//
// GeoPoint and TimeRange are nested value structs; that nesting is what becomes
// a leeway multi-sub-column section (geoPoint, timeRange) in stage 2. The ECS
// storage itself is flat.
//
// # Subset at two granularities
//
// The subset relation appears twice, as the same width-subtyping idea at two
// levels:
//
//   - field-subset (Subset[A,B]): within one component, A's fields ⊆ B's fields
//     (e.g. Schedule ⊆ Tasked).
//   - archetype-subset (Archetype.SubsetOf): across an entity's component set
//     (e.g. Grounded ⊆ Flying ⊆ Operating).
//
// # Two strengths of "can this be unserialized?"
//
// Mirroring leeway's ADR-0066 read-back artefacts (Presence / Validator /
// Projection), the check comes approximate-and-exact, at both the per-component
// level (Presence/Validate, see ecsdemo_json.go) and the archetype level
// (ArchetypePresence/ArchetypeValidate). The approximate check is always a
// necessary, not sufficient, sub-computation of the exact one.
package ecsdemo

import (
	json "encoding/json/v2"
	"slices"
)

// EntityID identifies an entity. An entity owns no data of its own; it is just
// this id, and the components attached to it across the World's columns.
type EntityID uint64

// GeoPoint is a nested value type. Its fields map onto anchor's "geoPoint"
// multi-sub-column section (pointLat, pointLng, h3) in stage 2.
type GeoPoint struct {
	Lat  float32 `json:"lat"`
	Lng  float32 `json:"lng"`
	Cell uint64  `json:"cell"`
}

// TimeRange is a nested value type mapping onto anchor's "timeRange" section
// (beginIncl, endExcl) in stage 2.
type TimeRange struct {
	BeginIncl int64 `json:"beginIncl"`
	EndExcl   int64 `json:"endExcl"`
}

// Identity is the component every active entity carries. ↔ section "symbol".
type Identity struct {
	Status string `json:"status"`
}

// Battery is an entity's charge level. ↔ section "u64Array" (unit).
type Battery struct {
	Charge uint64 `json:"charge"`
}

// Located places an entity in space. ↔ section "geoPoint".
type Located struct {
	At GeoPoint `json:"at"`
}

// Tasked records an entity's assignment. ↔ sections "timeRange" + "symbolArray".
type Tasked struct {
	Window TimeRange `json:"window"`
	Tags   []string  `json:"tags,omitzero"`
}

// Schedule is a narrower view of Tasked — a window with no tags — so
// Schedule ⊆ Tasked at the field level (Subset[Schedule, Tasked] is true). It
// illustrates that the subset relation lives one granularity below
// archetype-subset; it is not itself stored as a World column.
type Schedule struct {
	Window TimeRange `json:"window"`
}

// World is the storage view (structure-of-arrays): one column per component
// type, each keyed by entity id. It is the stage-2 analogue of leeway's Arrow
// columns.
type World struct {
	Identity map[EntityID]Identity `json:"identity,omitzero"`
	Battery  map[EntityID]Battery  `json:"battery,omitzero"`
	Located  map[EntityID]Located  `json:"located,omitzero"`
	Tasked   map[EntityID]Tasked   `json:"tasked,omitzero"`
}

// NewWorld returns a World with every component column initialized.
func NewWorld() *World {
	return &World{
		Identity: make(map[EntityID]Identity),
		Battery:  make(map[EntityID]Battery),
		Located:  make(map[EntityID]Located),
		Tasked:   make(map[EntityID]Tasked),
	}
}

// Entity is the gathered view (array-of-structs): an id plus the components
// present for it. A nil component pointer means the entity does not have that
// component. This is the "entity composed of components", and the stage-2
// analogue of a leeway DTO row (id plain column + populated sections).
type Entity struct {
	ID       EntityID  `json:"id"`
	Identity *Identity `json:"identity,omitzero"`
	Battery  *Battery  `json:"battery,omitzero"`
	Located  *Located  `json:"located,omitzero"`
	Tasked   *Tasked   `json:"tasked,omitzero"`
}

// Gather joins the World's columns on id into the Entity view. ≈ leeway RA.
func (w *World) Gather(id EntityID) Entity {
	e := Entity{ID: id}
	if c, ok := w.Identity[id]; ok {
		e.Identity = &c
	}
	if c, ok := w.Battery[id]; ok {
		e.Battery = &c
	}
	if c, ok := w.Located[id]; ok {
		e.Located = &c
	}
	if c, ok := w.Tasked[id]; ok {
		e.Tasked = &c
	}
	return e
}

// Scatter writes an Entity's present components into the World's columns, the
// inverse of Gather. ≈ leeway DML.
func (w *World) Scatter(e Entity) {
	if e.Identity != nil {
		if w.Identity == nil {
			w.Identity = make(map[EntityID]Identity)
		}
		w.Identity[e.ID] = *e.Identity
	}
	if e.Battery != nil {
		if w.Battery == nil {
			w.Battery = make(map[EntityID]Battery)
		}
		w.Battery[e.ID] = *e.Battery
	}
	if e.Located != nil {
		if w.Located == nil {
			w.Located = make(map[EntityID]Located)
		}
		w.Located[e.ID] = *e.Located
	}
	if e.Tasked != nil {
		if w.Tasked == nil {
			w.Tasked = make(map[EntityID]Tasked)
		}
		w.Tasked[e.ID] = *e.Tasked
	}
}

// Components returns the entity's archetype: the kinds it currently has, in a
// fixed (declaration) order. The archetype is what the entity "is".
func (e Entity) Components() Archetype {
	var a Archetype
	if e.Identity != nil {
		a = append(a, KindIdentity)
	}
	if e.Battery != nil {
		a = append(a, KindBattery)
	}
	if e.Located != nil {
		a = append(a, KindLocated)
	}
	if e.Tasked != nil {
		a = append(a, KindTasked)
	}
	return a
}

// LowBattery is a trivial system over the Battery column: the ids charged below
// threshold, sorted for determinism.
func (w *World) LowBattery(threshold uint64) []EntityID {
	var out []EntityID
	for id, b := range w.Battery {
		if b.Charge < threshold {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// ComponentKind names a component by its json member in Entity.
type ComponentKind string

const (
	KindIdentity ComponentKind = "identity"
	KindBattery  ComponentKind = "battery"
	KindLocated  ComponentKind = "located"
	KindTasked   ComponentKind = "tasked"
)

// Archetype is the set of component kinds an entity is required to have — the
// composition contract. Subset over archetypes is the ECS-level lifting of the
// field-level Subset relation.
type Archetype []ComponentKind

// Grounded ⊆ Flying ⊆ Operating.
var (
	Grounded  = Archetype{KindIdentity, KindBattery}
	Flying    = Archetype{KindIdentity, KindBattery, KindLocated}
	Operating = Archetype{KindIdentity, KindBattery, KindLocated, KindTasked}
)

// SubsetOf reports whether every kind in a also appears in b (a ⊆ b).
func (a Archetype) SubsetOf(b Archetype) bool {
	for _, k := range a {
		if !slices.Contains(b, k) {
			return false
		}
	}
	return true
}

// MarshalWorld serializes the whole store. Deterministic gives stable map key
// ordering so the bytes are reproducible.
func MarshalWorld(w *World) ([]byte, error) {
	return json.Marshal(w, json.Deterministic(true))
}

// UnmarshalWorld is the inverse of MarshalWorld.
func UnmarshalWorld(data []byte) (*World, error) {
	var w World
	if err := json.Unmarshal(data, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

// MarshalEntity serializes one gathered entity.
func MarshalEntity(e Entity) ([]byte, error) {
	return json.Marshal(e)
}

// UnmarshalEntity is the strict projection of an entity document into an Entity,
// rejecting unknown members (components or fields not in the model).
func UnmarshalEntity(data []byte) (Entity, error) {
	var e Entity
	if err := json.Unmarshal(data, &e, json.RejectUnknownMembers(true)); err != nil {
		return e, err
	}
	return e, nil
}
