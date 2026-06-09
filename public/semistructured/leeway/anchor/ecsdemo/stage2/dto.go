package stage2

import "time"

// DroneEntity is the leeway DTO: the flattened, lw:-tagged form of an ecsdemo
// entity — the stage-2 analogue of stage-1's gathered Entity. marshallgen parses
// this file to emit dto.out.go (the schema-agnostic BuildEntities / FillFromArrow
// codec). Each field binds to a section of the bespoke drone schema (schema.go):
//
//   - Status              -> symbol         (scalar)
//   - Battery             -> u64Array,unit   (single-valued homogenous-array attribute)
//   - Tags                -> symbolArray
//   - Lat / Lng / Cell    -> geoPoint        (multi-sub-column scalar)
//   - WindowBegin / End   -> timeRange       (multi-sub-column scalar)
//
// The membership names (droneStatus / droneBattery / droneTags / droneLoc /
// droneWindow) resolve to uint64 ids via a lookup at marshal time. Fields of a
// multi-sub-column section share one membership and must be declared in the
// section's sub-column order (geoPoint: pointLat, pointLng, h3; timeRange:
// beginIncl, endExcl), so BuildEntities passes BeginAttribute(...) args in order.
type DroneEntity struct {
	_ struct{} `kind:"droneEntity"`

	ID      uint64   `lw:",id"`
	Status  string   `lw:"droneStatus,symbol"`
	Battery uint64   `lw:"droneBattery,u64Array,unit"`
	Tags    []string `lw:"droneTags,symbolArray"`

	Lat  float32 `lw:"droneLoc,geoPoint:pointLat"`
	Lng  float32 `lw:"droneLoc,geoPoint:pointLng"`
	Cell uint64  `lw:"droneLoc,geoPoint:h3"`

	WindowBegin time.Time `lw:"droneWindow,timeRange:beginIncl"`
	WindowEnd   time.Time `lw:"droneWindow,timeRange:endExcl"`
}
