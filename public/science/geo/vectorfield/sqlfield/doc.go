// Package sqlfield is a [vectorfield.SourceI] over a field held in ClickHouse
// (ADR-0250): every window request is one reduction query, so what crosses the
// wire is bounded by the request and the field may be any size.
//
// A field relation yields the columns lat, lon, u, v and optionally t —
// degrees, earth-relative east and north components, one row per grid node
// per step, on a grid regular in latitude and longitude. Everything the
// vectorfield contract asks of a loader is the relation's job: rotate
// grid-relative components, filter to one level and one run, turn sentinels
// into NULL or NaN. The package refuses what it can detect — more than one
// row per node, nodes off a regular grid — and cannot detect a component that
// was never rotated.
//
//	src, err := sqlfield.NewSourceE(ctx, queryer, sqlfield.Relation{From: "gfs.wind10m"},
//		sqlfield.Options{Meta: vectorfield.Meta{Name: "10 m wind", Unit: "m/s"}})
//
// Values never reach the statement text: bounds, factors and the step ride
// parameters named with the ff_ prefix, which a relation must not use. The
// relation itself is the caller's SQL and is trusted as such.
//
// The source keeps no windows. A renderer holds the ones it shows, and a
// second copy here would need an invalidation rule the server already has.
package sqlfield
