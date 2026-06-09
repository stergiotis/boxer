// Package stage1 is the json/v2 stage of the ecsdemo Entity-Component-System
// example. It serializes the model with encoding/json/v2 and reflection only (no
// leeway runtime); the sibling stage2 package mirrors it through a bespoke leeway
// TableDesc + marshallgen codec with a real clickhouse-local roundtrip.
//
// An entity is just an id; components (Identity, Battery, Located, Tasked) are
// pure id-free data. A World stores them column-per-type (structure of arrays);
// an Entity is the gathered bundle for one id. Gather/Scatter join the two views,
// mirroring leeway RA/DML (see gather.go).
//
// "Can this document be unserialized into this shape?" is answered two ways at
// two levels, mirroring leeway ADR-0066's Presence/Validator/Projection: an
// approximate jsontext shape-scan (Presence, ArchetypePresence) that is necessary
// but not sufficient, and an exact strict decode (Validate, ArchetypeValidate) —
// see checks.go. See ../EXPLANATION.md for the ECS background.
package stage1

import "slices"

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

// Archetype is a set of component kinds — a composition contract. SubsetOf is the
// component-set lifting of the field-level Subset relation.
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
