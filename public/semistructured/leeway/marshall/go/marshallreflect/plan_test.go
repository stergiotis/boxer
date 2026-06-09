//go:build llm_generated_opus47

package marshallreflect_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// reflectDrone is the synthetic DTO used by the parse-side tests in
// this package. End-to-end round-trip tests against a real DML / RA
// live in consumer repos (e.g. pebble2impl's
// boxerstaging/leeway/marshallreflect_test/) so boxer's tests stay
// schema-free.
type reflectDrone struct {
	_ struct{} `kind:"droneMission"`

	ID       uint64 `lw:",id"`
	Tracking []byte `lw:",naturalKey"`
	Status   string `lw:"droneStatus,symbol"`
	Battery  uint64 `lw:"battery,u64Array,unit"`
}

// TestPlanFor_ReflectDrone confirms a representative DTO parses
// cleanly via PlanFor — catches regressions in the reflect-side
// shape classifier and plan builder.
func TestPlanFor_ReflectDrone(t *testing.T) {
	plan, err := marshallreflect.PlanFor[reflectDrone]()
	require.NoError(t, err)
	require.Equal(t, "droneMission", plan.KindName)
	require.Len(t, plan.PlainCols, 2)
	require.Len(t, plan.Fields, 2)
}
