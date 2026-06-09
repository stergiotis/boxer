// Package ecsdemo is a small, didactic Entity-Component-System and the first
// stage of a two-stage example under anchor/. Stage 1 (here) serializes the
// model with encoding/json/v2; stage 2 (the stage2/ subpackage) mirrors it
// through a bespoke leeway TableDesc + marshallgen codec, with a real
// clickhouse-local roundtrip.
//
// An entity is just an id; components (Identity, Battery, Located, Tasked) are
// pure id-free data. A World stores them column-per-type (structure of arrays);
// an Entity is the gathered bundle for one id. Gather/Scatter join the two
// views, mirroring leeway RA/DML.
//
// "Can this document be unserialized into this shape?" is answered two ways at
// two levels, mirroring leeway ADR-0066's Presence/Validator/Projection: an
// approximate jsontext shape-scan (Presence, ArchetypePresence) that is
// necessary but not sufficient, and an exact strict decode (Validate,
// ArchetypeValidate). See EXPLANATION.md for the ECS background.
package ecsdemo

import (
	json "encoding/json/v2"
	"iter"
	"maps"
	"slices"
)

// EntityID identifies an entity. The entity owns no data; it is just this id and
// the components attached to it across the World's columns.
type EntityID uint64

// GeoPoint and TimeRange are nested value types; the nesting becomes a leeway
// multi-sub-column section (geoPoint, timeRange) in stage 2.
type (
	GeoPoint struct {
		Lat  float32 `json:"lat"`
		Lng  float32 `json:"lng"`
		Cell uint64  `json:"cell"`
	}
	TimeRange struct {
		BeginIncl int64 `json:"beginIncl"`
		EndExcl   int64 `json:"endExcl"`
	}
)

// Identity, Battery, Located and Tasked are the components: plain id-free data,
// each mapping onto an anchor section (symbol, u64Array, geoPoint,
// timeRange+symbolArray) in stage 2.
type (
	Identity struct {
		Status string `json:"status"`
	}
	Battery struct {
		Charge uint64 `json:"charge"`
	}
	Located struct {
		At GeoPoint `json:"at"`
	}
	Tasked struct {
		Window TimeRange `json:"window"`
		Tags   []string  `json:"tags,omitzero"`
	}
)

// Schedule is a narrower view of Tasked (a window, no tags), so Schedule ⊆ Tasked
// at the field level — see Subset.
type Schedule struct {
	Window TimeRange `json:"window"`
}

// World stores components column-per-type, keyed by entity id (structure of
// arrays). ≈ leeway's Arrow columns in stage 2.
type World struct {
	Identity map[EntityID]Identity `json:"identity,omitzero"`
	Battery  map[EntityID]Battery  `json:"battery,omitzero"`
	Located  map[EntityID]Located  `json:"located,omitzero"`
	Tasked   map[EntityID]Tasked   `json:"tasked,omitzero"`
}

// NewWorld returns a World with every column initialized; the zero World also
// works, since Scatter allocates columns lazily.
func NewWorld() *World {
	return &World{
		Identity: make(map[EntityID]Identity),
		Battery:  make(map[EntityID]Battery),
		Located:  make(map[EntityID]Located),
		Tasked:   make(map[EntityID]Tasked),
	}
}

// Entity is the gathered (array-of-structs) view of one id: the id plus the
// components present for it (a nil pointer means "not attached"). ≈ a leeway DTO
// row (id plain column + populated sections) in stage 2.
type Entity struct {
	ID       EntityID  `json:"id"`
	Identity *Identity `json:"identity,omitzero"`
	Battery  *Battery  `json:"battery,omitzero"`
	Located  *Located  `json:"located,omitzero"`
	Tasked   *Tasked   `json:"tasked,omitzero"`
}

// Gather joins the columns on id into the Entity view. ≈ leeway RA.
func (inst *World) Gather(id EntityID) Entity {
	return Entity{
		ID:       id,
		Identity: getComp(inst.Identity, id),
		Battery:  getComp(inst.Battery, id),
		Located:  getComp(inst.Located, id),
		Tasked:   getComp(inst.Tasked, id),
	}
}

// Scatter writes an entity's present components back into the columns. ≈ leeway DML.
func (inst *World) Scatter(e Entity) {
	putComp(&inst.Identity, e.ID, e.Identity)
	putComp(&inst.Battery, e.ID, e.Battery)
	putComp(&inst.Located, e.ID, e.Located)
	putComp(&inst.Tasked, e.ID, e.Tasked)
}

// All iterates every entity (any id with at least one component) as a gathered
// view, ids ascending. It is the SoA→AoS traversal a "system" ranges over.
func (inst *World) All() iter.Seq2[EntityID, Entity] {
	return func(yield func(EntityID, Entity) bool) {
		for _, id := range inst.ids() {
			if !yield(id, inst.Gather(id)) {
				return
			}
		}
	}
}

func (inst *World) ids() []EntityID {
	set := make(map[EntityID]struct{}, len(inst.Identity))
	for id := range inst.Identity {
		set[id] = struct{}{}
	}
	for id := range inst.Battery {
		set[id] = struct{}{}
	}
	for id := range inst.Located {
		set[id] = struct{}{}
	}
	for id := range inst.Tasked {
		set[id] = struct{}{}
	}
	return slices.Sorted(maps.Keys(set))
}

// Components reports the entity's archetype: the kinds attached, in a fixed
// order. This set — computed at runtime, free to change — is what the entity
// "is" (composition over inheritance).
func (inst Entity) Components() Archetype {
	var a Archetype
	if inst.Identity != nil {
		a = append(a, KindIdentity)
	}
	if inst.Battery != nil {
		a = append(a, KindBattery)
	}
	if inst.Located != nil {
		a = append(a, KindLocated)
	}
	if inst.Tasked != nil {
		a = append(a, KindTasked)
	}
	return a
}

// ComponentKindE names a component by its json member name in an Entity.
//
//codelint:enum-prefix=Kind
type ComponentKindE string

const (
	KindIdentity ComponentKindE = "identity"
	KindBattery  ComponentKindE = "battery"
	KindLocated  ComponentKindE = "located"
	KindTasked   ComponentKindE = "tasked"
)

// Archetype is a set of component kinds — a composition contract. SubsetOf is
// the component-set lifting of the field-level Subset relation.
type Archetype []ComponentKindE

// Grounded ⊆ Flying ⊆ Operating.
var (
	Grounded  = Archetype{KindIdentity, KindBattery}
	Flying    = Archetype{KindIdentity, KindBattery, KindLocated}
	Operating = Archetype{KindIdentity, KindBattery, KindLocated, KindTasked}
)

// SubsetOf reports whether every kind in inst also appears in b (inst ⊆ b).
func (inst Archetype) SubsetOf(b Archetype) bool {
	for _, k := range inst {
		if !slices.Contains(b, k) {
			return false
		}
	}
	return true
}

// MarshalWorld serializes the store; Deterministic gives stable map-key order.
func MarshalWorld(inst *World) ([]byte, error) {
	return json.Marshal(inst, json.Deterministic(true))
}

// UnmarshalWorld is the inverse of MarshalWorld.
func UnmarshalWorld(data []byte) (inst *World, err error) {
	inst = &World{}
	err = json.Unmarshal(data, inst)
	return
}

// MarshalEntity serializes one gathered entity.
func MarshalEntity(e Entity) ([]byte, error) {
	return json.Marshal(e)
}

// UnmarshalEntity strictly decodes an entity document, rejecting unknown members.
func UnmarshalEntity(data []byte) (e Entity, err error) {
	err = json.Unmarshal(data, &e, json.RejectUnknownMembers(true))
	return
}

func getComp[C any](m map[EntityID]C, id EntityID) *C {
	if c, ok := m[id]; ok {
		return &c
	}
	return nil
}

func putComp[C any](m *map[EntityID]C, id EntityID, c *C) {
	if c == nil {
		return
	}
	if *m == nil {
		*m = make(map[EntityID]C)
	}
	(*m)[id] = *c
}
