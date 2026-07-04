package example

// Located is the position device component, bound to the multi-sub-column
// geoPoint section (one membership carrying three aligned value columns);
// see identity_dto.go for the component-file layout rationale.
type Located struct {
	_    struct{} `kind:"located"`
	ID   uint64   `lw:",id"`
	Lat  float32  `lw:"deviceLoc,geoPoint:pointLat"`
	Lng  float32  `lw:"deviceLoc,geoPoint:pointLng"`
	Cell uint64   `lw:"deviceLoc,geoPoint:h3"`
}
