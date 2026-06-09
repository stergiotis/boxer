package stage2

// DroneEntity is the leeway DTO: the flattened, lw:-tagged form of an ecsdemo
// entity — the stage-2 analogue of stage-1's gathered Entity. marshallgen parses
// this file to emit dto.out.go (the schema-agnostic BuildEntities / FillFromArrow
// codec). Each field binds to a section of the bespoke drone schema (schema.go):
//
//   - Status  -> symbol        (scalar)
//   - Battery -> u64Array,unit  (single-valued homogenous-array attribute)
//   - Tags    -> symbolArray
//
// The membership names (droneStatus / droneBattery / droneTags) resolve to uint64
// ids via a lookup at marshal time, mirroring the readback roundtrip test. A nil
// Tags marshals to an absent symbolArray attribute — the columnar analogue of an
// omitted (optional) component in stage 1.
type DroneEntity struct {
	_ struct{} `kind:"droneEntity"`

	ID      uint64   `lw:",id"`
	Status  string   `lw:"droneStatus,symbol"`
	Battery uint64   `lw:"droneBattery,u64Array,unit"`
	Tags    []string `lw:"droneTags,symbolArray"`
}
