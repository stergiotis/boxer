// Package vectorfield is the data contract through which a renderer reads a
// gridded two-component vector field — a 10 m wind, an ocean current, the
// gradient of a scalar — and the first implementation of it, an in-memory
// pyramid (ADR-0249 SD1, SD2).
//
// The caveats first. The contract is deliberately narrow: a [Window] is
// regular in latitude and longitude and its components are earth-relative
// east and north. A model on a Lambert, polar-stereographic, rotated or
// curvilinear grid does not satisfy that as it comes — its source has to
// collocate staggered components, rotate them, resample them as a vector and
// only then decimate, in that order. Skipping the rotation leaves speed intact
// and turns direction smoothly, so nothing downstream can detect it; the
// conformance suite in the vectorfieldtest package checks the form of what a
// source returns, not its meteorology. Decoding GRIB or NetCDF is not here.
//
// A source is one field. Level and variable select a source; they are not
// axes of the contract, and an app offering ten pressure levels holds ten
// sources. The renderer asks for a window — bounds, a step, the most columns
// and rows worth returning — and the window's size is bounded by that
// request, never by the data, which is what keeps a renderer's cost
// independent of the field's size.
//
// The [Pyramid] halves a step's grid level by level with a box mean of the
// components: the vector mean, meteorology's resultant wind. The vector mean
// is never longer than the mean of the lengths and is much shorter where
// directions disagree, so each level also carries the scalar mean of the
// magnitude. Advect with U and V; colour and read speed from Speed. No
// system or paper surveyed for ADR-0249 builds that second mean, so it rests
// on the terminology and not on precedent. Near a pole the east/north basis
// turns with longitude and a box mean across longitudes cancels a real
// cross-polar flow; the pyramid does not correct for it (ADR-0249, deferred).
package vectorfield
