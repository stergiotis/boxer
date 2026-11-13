---
type: adr
status: accepted
date: 2026-11-04
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-12
---

# ADR-0356: geodesy — WGS84, ECEF and local tangent frames as a science package

## Context

Three consumers of boxer carry their own copy of the same small body of
geodesy. shadow-boxer's aircraft Doppler geolocation (its ADR-0022) converts
ADS-B positions to Earth-centred, Earth-fixed metres, lays a lattice in a
local east-north-up frame and reads ground velocities in the frame at the
aircraft; its router and gazetteer each have a haversine; boxer's own
`science/geo/swisstopo` holds the WGS84 constants privately for the datum
shift, and the portolan widget holds the spherical radius for the web
Mercator projection. None of these is wrong, and each was the right size
when written. Together they are the same ellipsoid written four times, with
nothing that pins them to one another: a constant typed with one digit off
in one copy is a bias no test in another package can see.

What is shared is not a projection — that stays with the frame that owns
it, as swisstopo's Swiss Oblique Mercator does — but the datum and the two
coordinate systems every metric computation on the Earth passes through:
geodetic latitude, longitude and ellipsoidal height; the ECEF Cartesian
frame; and the local tangent frame at a point, in which "east", "north" and
"up" mean something. A distance, a range rate, a bearing of a velocity
report, the vertical through a lattice cell: all are a few lines once those
three exist and pages of special cases without them.

The height convention is the trap every consumer meets. Terrain models are
referenced to a geoid and barometric altitude to a standard atmosphere;
neither is a height above the ellipsoid, and a conversion that takes one as
the other is off by the geoid undulation, up to a hundred metres, without
erroring. A shared package can state the convention once, in the type.

## Decision

We will add `public/science/geo/geodesy`: the WGS84 datum, Cartesian
vectors, geodetic positions with ellipsoidal heights, the conversions
between geodetic and ECEF, and local east-north-up tangent frames as rigid
motions of ECEF. shadow-boxer's foghorn moves onto it in the same change;
the other copies are candidates, not obligations.

### Subsidiary design decisions

- **SD1 — One datum, and heights are ellipsoidal.** The package knows
  WGS84 and nothing else; a second datum is a second package or a
  parameter added when it is needed, not a field reserved for it.
  `Geodetic.HeightM` is above the ellipsoid, and the doc comment says what
  is not: geoid-referenced terrain and barometric altitude. The geoid
  itself (an EGM96 or EGM2008 grid) is out of scope: a caller passes the
  undulation it knows.

- **SD2 — ECEF is the working frame; a tangent frame is a rigid motion.**
  Every metric quantity — a distance, a closing speed, a radial velocity —
  is computed on ECEF vectors and is therefore identical in any
  east-north-up frame, because `Frame` is a translation and a rotation and
  nothing else. The frame exists to give directions names and to lay flat
  things, never to approximate the Earth as flat; a caller who wants a flat
  approximation makes it knowingly, with the curvature bounds the package
  exports.

- **SD3 — Two rotations, at the origin and at a point.** A frame carries
  its rotation once, for the many points referred to one origin. A velocity
  report is east-north-up *where it was made*, so the package also rotates
  a vector in the frame at any geodetic point without building a frame:
  `EnuToEcefAt` and `EcefToEnuAt`.

- **SD4 — Values, float64, no allocation.** `Vec3`, `Geodetic` and `Frame`
  are plain value types with no pointers and no errors: a NaN input gives
  a NaN output, as `math` does, and a caller that needs a refusal checks
  its inputs where it knows their meaning. Nothing in the package
  allocates, so it can sit inside a per-sample loop.

- **SD5 — The inverse is iterative, and conditioned at the poles.**
  `EcefToGeodetic` iterates latitude and height to a fixed point from the
  spherical first guess, evaluating the height as ρ cos φ + Z sin φ − a²/N,
  which stays finite at the poles where ρ/cos φ does not. It converges to
  sub-millimetre for every point from the Mariana depth to low Earth orbit;
  the property test says so over that range. Bowring's one-step formula was
  not chosen because the iteration is as fast at these heights and its
  error is a tolerance, not a formula's remainder.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API under `public/` | added: `public/science/geo/geodesy` | the package-props table (ADR-0080): the package compiles for every WASM target |
| shadow-boxer's foghorn package (its ADR-0022) | its geodesy file is deleted and its callers import this package | shadow-boxer's pin on boxer, bumped past this commit once it is pushed |

## Alternatives

- **Leave it in foghorn.** The package was correct there, but it was the
  third copy, and the next consumer would make a fourth.
- **Extend `science/geo/swisstopo`.** Its WGS84 constants are an input to
  a Swiss datum shift; geodesy with no Swiss frame in it does not belong
  under that name, and swisstopo may later consume this package instead.
- **Take a geodesy library.** A port of GeographicLib carries geodesics,
  projections and gravity models the consumers do not use, in a licence
  and a dependency surface that would need a decision of their own. The
  twenty functions here are textbook and a reader can check them against
  the EXPLANATION in an afternoon.

## Consequences

### Positive

- One ellipsoid. A consumer that disagrees with another about a distance
  is disagreeing about its own geometry, not about a constant.
- The height convention is stated in the type that carries it and in the
  doc of every function that reads it.
- The package compiles for `wasip1`, `js` and freestanding WASM, so a tab
  can convert coordinates without a server.

### Negative

- A second copy of the WGS84 constants remains in swisstopo and a
  spherical radius in portolan until someone moves them; this decision
  does not oblige anyone to.
- A caller who wants a geodesic distance on the ellipsoid (Vincenty or
  Karney) does not get one; chord length through ECEF and the haversine on
  a sphere are what exists, and the EXPLANATION says how far each is off.

### Neutral

- A consumer that needs another datum will reopen SD1; the package's
  shape — constants, two conversions, frames — does not change when it
  does.

## Migration — Tier 1

- **Breaks.** Nothing in boxer. In shadow-boxer, `foghorn.Vec3`,
  `foghorn.Geodetic`, `foghorn.Frame`, `foghorn.NewFrame`,
  `foghorn.GeodeticToEcef`, `foghorn.EcefToGeodetic`,
  `foghorn.GroundVelocityEnu` and `foghorn.MinMetresPerDegreeLat` are
  gone; the same names are here.
- **Path.** Replace the `foghorn.` prefix with `geodesy.` and import this
  package. Nothing changes numerically: the formulas moved as they were,
  and foghorn's tests, which pin a search result to tens of metres, pass
  unchanged.
- **Regeneration.** The package-props table.
- **Old shape.** Removed outright with the move.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the package: known points on the axes
  and poles against the ellipsoid's radii, a published point at 45°, and
  three property tests under `rapid` — the geodetic round trip to a
  millimetre and a nanodegree over heights from −11 km to 100 km, the
  frame's rotation orthonormal and right-handed, and the rotation at a
  point equal to a frame built there.
- **What would fail.** A constant typed wrong fails the known points; a
  sign error in the rotation fails handedness; a conditioning failure at
  the poles fails the round trip, whose latitude range includes them.
- **Gap.** Nothing checks the package against an external geodesy
  implementation; the published point at 45° is the only outside number.
  Acceptable because the formulas are closed-form and the round trip
  proves each conversion against the other.

## Status

Accepted 2026-11-12.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- NIMA TR8350.2, *Department of Defense World Geodetic System 1984*, third
  edition — the datum's defining parameters.
- [ADR-0080](./0080-packageprops-per-package-declarations.md) — the package-props table a new
  package under `public/` joins.
- [ADR-0244](./0244-swisstopo-coordinate-udfs.md) — the Swiss frames, which
  keep their own WGS84 constants for now.
- shadow-boxer ADR-0022 — the first consumer, whose geodesy this package is.
