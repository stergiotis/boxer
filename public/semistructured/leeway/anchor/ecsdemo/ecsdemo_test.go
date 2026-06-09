package ecsdemo_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/ecsdemo"
)

// Example_entityComposition builds a World by scattering entities into the
// component columns, then gathers one back as a composed Entity and serializes
// it. The entity is nothing but its id plus the components attached to it.
func Example_entityComposition() {
	w := ecsdemo.NewWorld()
	// Entity 1: a grounded drone — Identity + Battery only.
	w.Scatter(ecsdemo.Entity{
		ID:       1,
		Identity: &ecsdemo.Identity{Status: "IDLE"},
		Battery:  &ecsdemo.Battery{Charge: 9000},
	})
	// Entity 7: an operating drone — all four components.
	w.Scatter(ecsdemo.Entity{
		ID:       7,
		Identity: &ecsdemo.Identity{Status: "IN_TRANSIT"},
		Battery:  &ecsdemo.Battery{Charge: 8000},
		Located:  &ecsdemo.Located{At: ecsdemo.GeoPoint{Lat: 47.5, Lng: 8.5, Cell: 12345}},
		Tasked:   &ecsdemo.Tasked{Window: ecsdemo.TimeRange{BeginIncl: 100, EndExcl: 200}, Tags: []string{"survey"}},
	})

	fmt.Println("entity 1 archetype:", w.Gather(1).Components())
	fmt.Println("entity 7 archetype:", w.Gather(7).Components())

	doc, err := ecsdemo.MarshalEntity(w.Gather(7))
	if err != nil {
		panic(err)
	}
	fmt.Println(string(doc))

	// Output:
	// entity 1 archetype: [identity battery]
	// entity 7 archetype: [identity battery located tasked]
	// {"id":7,"identity":{"status":"IN_TRANSIT"},"battery":{"charge":8000},"located":{"at":{"lat":47.5,"lng":8.5,"cell":12345}},"tasked":{"window":{"beginIncl":100,"endExcl":200},"tags":["survey"]}}
}

// Example_subsetAtTwoLevels shows the subset relation at both granularities: the
// field level (Schedule ⊆ Tasked) and the archetype / component-set level
// (Grounded ⊆ Operating). Both are directional.
func Example_subsetAtTwoLevels() {
	fmt.Println("Schedule ⊆ Tasked:", ecsdemo.Subset[ecsdemo.Schedule, ecsdemo.Tasked]())
	fmt.Println("Tasked ⊆ Schedule:", ecsdemo.Subset[ecsdemo.Tasked, ecsdemo.Schedule]())
	fmt.Println("Grounded ⊆ Operating:", ecsdemo.Grounded.SubsetOf(ecsdemo.Operating))
	fmt.Println("Operating ⊆ Grounded:", ecsdemo.Operating.SubsetOf(ecsdemo.Grounded))

	// Output:
	// Schedule ⊆ Tasked: true
	// Tasked ⊆ Schedule: false
	// Grounded ⊆ Operating: true
	// Operating ⊆ Grounded: false
}

// Example_archetypeApproximateVsExact shows the approximate/exact gap at the
// component-set level, driven by archetype-subset. An operating-entity document
// is "approximately" a Grounded entity (it has the required components) but not
// "exactly" one (it carries extra components); a grounded-entity document is not
// even approximately an Operating entity (missing components).
func Example_archetypeApproximateVsExact() {
	operating := []byte(`{"id":7,"identity":{"status":"IN_TRANSIT"},"battery":{"charge":8000},"located":{"at":{"lat":47.5,"lng":8.5,"cell":12345}},"tasked":{"window":{"beginIncl":100,"endExcl":200}}}`)
	grounded := []byte(`{"id":1,"identity":{"status":"IDLE"},"battery":{"charge":9000}}`)

	fmt.Println("operating vs Grounded approx:", ecsdemo.ArchetypePresence(operating, ecsdemo.Grounded))
	fmt.Println("operating vs Grounded exact:", ecsdemo.ArchetypeValidate(operating, ecsdemo.Grounded) == nil)
	fmt.Println("grounded vs Operating approx:", ecsdemo.ArchetypePresence(grounded, ecsdemo.Operating))
	fmt.Println("grounded vs Operating exact:", ecsdemo.ArchetypeValidate(grounded, ecsdemo.Operating) == nil)
	fmt.Println("operating vs Operating exact:", ecsdemo.ArchetypeValidate(operating, ecsdemo.Operating) == nil)
	fmt.Println("grounded vs Grounded exact:", ecsdemo.ArchetypeValidate(grounded, ecsdemo.Grounded) == nil)

	// Output:
	// operating vs Grounded approx: true
	// operating vs Grounded exact: false
	// grounded vs Operating approx: false
	// grounded vs Operating exact: false
	// operating vs Operating exact: true
	// grounded vs Grounded exact: true
}

// TestGatherScatterRoundTrip pins the SoA↔AoS join: scattering an entity then
// gathering it (directly and through a json round-trip of the World) reproduces
// the same composition, and absent components stay absent.
func TestGatherScatterRoundTrip(t *testing.T) {
	w := ecsdemo.NewWorld()
	w.Scatter(ecsdemo.Entity{
		ID:       7,
		Identity: &ecsdemo.Identity{Status: "X"},
		Battery:  &ecsdemo.Battery{Charge: 5},
		Located:  &ecsdemo.Located{At: ecsdemo.GeoPoint{Lat: 1, Lng: 2, Cell: 3}},
	})

	data, err := ecsdemo.MarshalWorld(w)
	require.NoError(t, err)
	w2, err := ecsdemo.UnmarshalWorld(data)
	require.NoError(t, err)

	got := w2.Gather(7)
	require.Equal(t, ecsdemo.EntityID(7), got.ID)
	require.Equal(t, ecsdemo.Archetype{ecsdemo.KindIdentity, ecsdemo.KindBattery, ecsdemo.KindLocated}, got.Components())
	require.Equal(t, "X", got.Identity.Status)
	require.Equal(t, uint64(3), got.Located.At.Cell)
	require.Nil(t, got.Tasked, "entity 7 never had a Tasked component")

	// The gathered entity round-trips through its own document too.
	doc, err := ecsdemo.MarshalEntity(got)
	require.NoError(t, err)
	back, err := ecsdemo.UnmarshalEntity(doc)
	require.NoError(t, err)
	require.Equal(t, got, back)
}

// TestComponentNecessaryNotSufficient pins the per-component property: the
// approximate check accepts a battery document the exact check rejects (the
// charge is a number token but overflows uint64 — invisible to a shape scan).
func TestComponentNecessaryNotSufficient(t *testing.T) {
	overflow := []byte(`{"charge":99999999999999999999}`)
	require.True(t, ecsdemo.Presence[ecsdemo.Battery](overflow), "approximate: charge is a number token")
	require.Error(t, ecsdemo.Validate[ecsdemo.Battery](overflow), "exact: charge overflows uint64")
}

// TestArchetypeNecessaryNotSufficient pins the same property one level up: an
// entity whose required components are present as objects passes the approximate
// archetype check, but fails the exact one because a component is internally
// invalid (identity is missing its mandatory status field).
func TestArchetypeNecessaryNotSufficient(t *testing.T) {
	doc := []byte(`{"id":1,"identity":{},"battery":{"charge":1}}`)
	require.True(t, ecsdemo.ArchetypePresence(doc, ecsdemo.Grounded), "approximate: identity and battery present as objects")
	require.Error(t, ecsdemo.ArchetypeValidate(doc, ecsdemo.Grounded), "exact: identity lacks its mandatory status")
}

// TestArchetypeRejectsExtraComponents pins the reject-unknown rule at the
// component-set level: a richer entity passes the approximate check of a smaller
// archetype but fails the exact one on the extra components.
func TestArchetypeRejectsExtraComponents(t *testing.T) {
	operating := []byte(`{"id":7,"identity":{"status":"X"},"battery":{"charge":1},"located":{"at":{"lat":1,"lng":2,"cell":3}},"tasked":{"window":{"beginIncl":0,"endExcl":1}}}`)
	require.True(t, ecsdemo.ArchetypePresence(operating, ecsdemo.Grounded))
	require.Error(t, ecsdemo.ArchetypeValidate(operating, ecsdemo.Grounded), "located/tasked are unexpected under Grounded")
}

// TestSystemOverAll shows a "system": a query that ranges the World's entities
// (World.All yields them id-ascending) and selects those matching a predicate.
func TestSystemOverAll(t *testing.T) {
	w := ecsdemo.NewWorld()
	w.Scatter(ecsdemo.Entity{ID: 1, Battery: &ecsdemo.Battery{Charge: 50}})
	w.Scatter(ecsdemo.Entity{ID: 2, Battery: &ecsdemo.Battery{Charge: 9000}})
	w.Scatter(ecsdemo.Entity{ID: 3, Battery: &ecsdemo.Battery{Charge: 10}})

	var low []ecsdemo.EntityID
	for id, e := range w.All() {
		if e.Battery != nil && e.Battery.Charge < 1000 {
			low = append(low, id)
		}
	}
	require.Equal(t, []ecsdemo.EntityID{1, 3}, low)
}
