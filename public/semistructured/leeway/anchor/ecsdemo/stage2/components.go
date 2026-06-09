package stage2

import "time"

// Identity, Battery, Located and Tasked are the leeway-side components: the split
// of DroneEntity into the same four aspects stage 1 models. Each is a flat
// lw:-tagged DTO carrying the entity id plus its own section(s), and reuses
// DroneEntity's membership names, so marshallreflect can read any one of them out
// of a shared fat row (see FatRow / Extract) — the columnar analogue of
// stage-1's World.Gather. They are flat (not nested) because marshallreflect has
// no nested-struct support.
type (
	Identity struct {
		_      struct{} `kind:"identity"`
		ID     uint64   `lw:",id"`
		Status string   `lw:"droneStatus,symbol"`
	}
	Battery struct {
		_      struct{} `kind:"battery"`
		ID     uint64   `lw:",id"`
		Charge uint64   `lw:"droneBattery,u64Array,unit"`
	}
	Located struct {
		_    struct{} `kind:"located"`
		ID   uint64   `lw:",id"`
		Lat  float32  `lw:"droneLoc,geoPoint:pointLat"`
		Lng  float32  `lw:"droneLoc,geoPoint:pointLng"`
		Cell uint64   `lw:"droneLoc,geoPoint:h3"`
	}
	Tasked struct {
		_           struct{}  `kind:"tasked"`
		ID          uint64    `lw:",id"`
		WindowBegin time.Time `lw:"droneWindow,timeRange:beginIncl"`
		WindowEnd   time.Time `lw:"droneWindow,timeRange:endExcl"`
		Tags        []string  `lw:"droneTags,symbolArray"`
	}
)
