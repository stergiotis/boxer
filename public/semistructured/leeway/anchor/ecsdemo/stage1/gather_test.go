package stage1_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/ecsdemo/stage1"
)

// Example_entityComposition builds a World by scattering entities into the
// component columns, then gathers one back as a composed Entity and serializes
// it. The entity is nothing but its id plus the components attached to it.
func Example_entityComposition() {
	w := stage1.NewWorld()
	// Entity 1: a grounded drone — Identity + Battery only.
	w.Scatter(stage1.Entity{
		ID:       1,
		Identity: &stage1.Identity{Status: "IDLE"},
		Battery:  &stage1.Battery{Charge: 9000},
	})
	// Entity 7: an operating drone — all four components.
	w.Scatter(stage1.Entity{
		ID:       7,
		Identity: &stage1.Identity{Status: "IN_TRANSIT"},
		Battery:  &stage1.Battery{Charge: 8000},
		Located:  &stage1.Located{At: stage1.GeoPoint{Lat: 47.5, Lng: 8.5, Cell: 12345}},
		Tasked:   &stage1.Tasked{Window: stage1.TimeRange{BeginIncl: 100, EndExcl: 200}, Tags: []string{"survey"}},
	})

	fmt.Println("entity 1 archetype:", w.Gather(1).Components())
	fmt.Println("entity 7 archetype:", w.Gather(7).Components())

	doc, err := stage1.MarshalEntity(w.Gather(7))
	if err != nil {
		panic(err)
	}
	fmt.Println(string(doc))

	// Output:
	// entity 1 archetype: [identity battery]
	// entity 7 archetype: [identity battery located tasked]
	// {"id":7,"identity":{"status":"IN_TRANSIT"},"battery":{"charge":8000},"located":{"at":{"lat":47.5,"lng":8.5,"cell":12345}},"tasked":{"window":{"beginIncl":100,"endExcl":200},"tags":["survey"]}}
}

// TestGatherScatterRoundTrip pins the SoA↔AoS join: scattering an entity then
// gathering it (directly and through a json round-trip of the World) reproduces
// the same composition, and absent components stay absent.
func TestGatherScatterRoundTrip(t *testing.T) {
	w := stage1.NewWorld()
	w.Scatter(stage1.Entity{
		ID:       7,
		Identity: &stage1.Identity{Status: "X"},
		Battery:  &stage1.Battery{Charge: 5},
		Located:  &stage1.Located{At: stage1.GeoPoint{Lat: 1, Lng: 2, Cell: 3}},
	})

	data, err := stage1.MarshalWorld(w)
	require.NoError(t, err)
	w2, err := stage1.UnmarshalWorld(data)
	require.NoError(t, err)

	got := w2.Gather(7)
	require.Equal(t, stage1.EntityID(7), got.ID)
	require.Equal(t, stage1.Archetype{stage1.KindIdentity, stage1.KindBattery, stage1.KindLocated}, got.Components())
	require.Equal(t, "X", got.Identity.Status)
	require.Equal(t, uint64(3), got.Located.At.Cell)
	require.Nil(t, got.Tasked, "entity 7 never had a Tasked component")

	// The gathered entity round-trips through its own document too.
	doc, err := stage1.MarshalEntity(got)
	require.NoError(t, err)
	back, err := stage1.UnmarshalEntity(doc)
	require.NoError(t, err)
	require.Equal(t, got, back)
}

// TestSystemOverAll shows a "system": a query that ranges the World's entities
// (World.All yields them id-ascending) and selects those matching a predicate.
func TestSystemOverAll(t *testing.T) {
	w := stage1.NewWorld()
	w.Scatter(stage1.Entity{ID: 1, Battery: &stage1.Battery{Charge: 50}})
	w.Scatter(stage1.Entity{ID: 2, Battery: &stage1.Battery{Charge: 9000}})
	w.Scatter(stage1.Entity{ID: 3, Battery: &stage1.Battery{Charge: 10}})

	var low []stage1.EntityID
	for id, e := range w.All() {
		if e.Battery != nil && e.Battery.Charge < 1000 {
			low = append(low, id)
		}
	}
	require.Equal(t, []stage1.EntityID{1, 3}, low)
}
