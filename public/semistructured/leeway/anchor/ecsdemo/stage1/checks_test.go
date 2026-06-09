package stage1_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/ecsdemo/stage1"
)

// Example_subsetAtTwoLevels shows the subset relation at both granularities: the
// field level (Schedule ⊆ Tasked) and the archetype / component-set level
// (Grounded ⊆ Operating). Both are directional.
func Example_subsetAtTwoLevels() {
	fmt.Println("Schedule ⊆ Tasked:", stage1.Subset[stage1.Schedule, stage1.Tasked]())
	fmt.Println("Tasked ⊆ Schedule:", stage1.Subset[stage1.Tasked, stage1.Schedule]())
	fmt.Println("Grounded ⊆ Operating:", stage1.Grounded.SubsetOf(stage1.Operating))
	fmt.Println("Operating ⊆ Grounded:", stage1.Operating.SubsetOf(stage1.Grounded))

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

	fmt.Println("operating vs Grounded approx:", stage1.ArchetypePresence(operating, stage1.Grounded))
	fmt.Println("operating vs Grounded exact:", stage1.ArchetypeValidate(operating, stage1.Grounded) == nil)
	fmt.Println("grounded vs Operating approx:", stage1.ArchetypePresence(grounded, stage1.Operating))
	fmt.Println("grounded vs Operating exact:", stage1.ArchetypeValidate(grounded, stage1.Operating) == nil)
	fmt.Println("operating vs Operating exact:", stage1.ArchetypeValidate(operating, stage1.Operating) == nil)
	fmt.Println("grounded vs Grounded exact:", stage1.ArchetypeValidate(grounded, stage1.Grounded) == nil)

	// Output:
	// operating vs Grounded approx: true
	// operating vs Grounded exact: false
	// grounded vs Operating approx: false
	// grounded vs Operating exact: false
	// operating vs Operating exact: true
	// grounded vs Grounded exact: true
}

// TestComponentNecessaryNotSufficient pins the per-component property: the
// approximate check accepts a battery document the exact check rejects (the
// charge is a number token but overflows uint64 — invisible to a shape scan).
func TestComponentNecessaryNotSufficient(t *testing.T) {
	overflow := []byte(`{"charge":99999999999999999999}`)
	require.True(t, stage1.Presence[stage1.Battery](overflow), "approximate: charge is a number token")
	require.Error(t, stage1.Validate[stage1.Battery](overflow), "exact: charge overflows uint64")
}

// TestArchetypeNecessaryNotSufficient pins the same property one level up: an
// entity whose required components are present as objects passes the approximate
// archetype check, but fails the exact one because a component is internally
// invalid (identity is missing its mandatory status field).
func TestArchetypeNecessaryNotSufficient(t *testing.T) {
	doc := []byte(`{"id":1,"identity":{},"battery":{"charge":1}}`)
	require.True(t, stage1.ArchetypePresence(doc, stage1.Grounded), "approximate: identity and battery present as objects")
	require.Error(t, stage1.ArchetypeValidate(doc, stage1.Grounded), "exact: identity lacks its mandatory status")
}

// TestArchetypeRejectsExtraComponents pins the reject-unknown rule at the
// component-set level: a richer entity passes the approximate check of a smaller
// archetype but fails the exact one on the extra components.
func TestArchetypeRejectsExtraComponents(t *testing.T) {
	operating := []byte(`{"id":7,"identity":{"status":"X"},"battery":{"charge":1},"located":{"at":{"lat":1,"lng":2,"cell":3}},"tasked":{"window":{"beginIncl":0,"endExcl":1}}}`)
	require.True(t, stage1.ArchetypePresence(operating, stage1.Grounded))
	require.Error(t, stage1.ArchetypeValidate(operating, stage1.Grounded), "located/tasked are unexpected under Grounded")
}
