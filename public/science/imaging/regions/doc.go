// Package regions is regions of an image as values (ADR-0399 §M1): a
// parametric shape in a stated frame, an anchor naming the positions of a
// stack it belongs to, and an identity apart from any style; one written
// rule for which pixels a shape covers, and a second, by covered area,
// under its own name; statistics and line profiles computed from the
// samples at full resolution.
//
// # Shapes and frames
//
// A [Shape] is a point, a line segment, a rectangle or an ellipse with a
// rotation, or a polygon of one ring (§SD1), kept by its parameters. Angles
// are in degrees and a positive angle turns +x toward +y — clockwise on
// screen in pixel coordinates, where rows grow downward. A [Region]'s
// coordinates are in exactly one [Frame] (§SD2): the continuous pixel
// coordinates of ADR-0373 §SD3, or a placement's frame named as the
// placement names it (ADR-0398 §SD4). There is no default. [ToPixels]
// brings a shape to an image's pixels through its placement, exactly where
// the placement is affine, and says what happened ([Mapping]); [FromPixels]
// is its inverse, for a viewer that states in a placement's frame a shape
// drawn in pixels. A frame with an unknown code matches nothing: the engine
// resolves no codes. [OutlinePolyline] is a shape's outline for drawing,
// an ellipse as a polygon within a stated tolerance; nothing measured reads
// it (ADR-0399 §M2).
//
// # Which pixels
//
// [CoverageOf] is the inclusion rule (§SD3), the rule for counts, ranges,
// percentiles and everything drawn. An area shape covers the pixels whose
// centre (col + ½, row + ½) lies inside it. A centre on the outline is
// decided on its row: the shape meets the row's line y = row + ½ in
// intervals, each holding its left end and not its right end; a polygon's
// edge meets the line only where one end lies at or above it and the other
// strictly below (min(y₀, y₁) ≤ y < max(y₀, y₁)), and a polygon's inside is
// decided by the even-odd rule. So a centre on a left or a top edge is
// inside and one on a right or a bottom edge is not; a vertex or a tangent
// point alone on the line covers nothing; two shapes sharing an edge cover
// each centre on it once; a rectangle on whole coordinates covers the
// pixels of the equal [image.Rectangle]. A point covers the pixel
// containing it, a line the pixels it passes through ([Profile]). Ties are
// decided by exact predicates on the float64 parameters, never by rounding;
// a rectangle's corners and an ellipse's rotation are computed from its
// parameters, exactly at multiples of 90°.
//
// [AreaFractions] is coverage by area (§SD4): the exact fraction of each
// pixel's area inside a rectangle, an ellipse or a polygon, in closed form.
//
// # Numbers
//
// [Measure] gives each plane's count of valid and invalid samples, range,
// mean, standard deviation with divisor n and nearest-rank percentiles over
// the covered samples (§SD7), from [stats.OfSpans], so the definitions —
// exact for a kind with codes, binned otherwise — are the engine's, in
// values through each plane's value transform; and, asked for, the
// area-weighted sum, mean and covered area of §SD4 beside them. [Profile]
// gives a line's pixels in order, each once, with the length of the line
// in each, the distance to its middle and every plane's stored sample
// (§SD8). Both read full-resolution samples from a [SourceI] — an image in
// memory or a tiled store — and every result carries [MethodId], level 0,
// the coverage rule each figure used and the [Mapping] (§SD9).
//
// # The document
//
// A region and a set of regions serialise to a versioned JSON document
// ([MarshalRegion], [MarshalRegions]) that holds everything a region holds
// and nothing else (§SD6); reading it is strict.
package regions
