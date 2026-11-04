---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# geodesy — Explanation

The formulas behind the package's three conversions — geodetic to ECEF, its
inverse, and the local east-north-up frame — and the conditioning and error
bounds a caller leans on. The decisions are in
[ADR-0356](../../../../doc/adr/0356-geodesy-wgs84-ecef-and-local-tangent-frames-as-a-science-package.md).

## Background

WGS84 models the Earth as an oblate ellipsoid of revolution with
equatorial radius $a = 6\,378\,137$ m and flattening
$1/f = 298.257\,223\,563$ (NIMA TR8350.2). Everything else follows:
the polar radius $b = a(1 - f)$ and the first eccentricity
$e^2 = f(2 - f)$.

A position is given either geodetically — latitude $\varphi$ (the angle
the ellipsoid's normal makes with the equatorial plane, *not* the angle
from the centre), longitude $\lambda$, and height $h$ along that normal —
or as Earth-centred, Earth-fixed Cartesian coordinates $(X, Y, Z)$ with
$X$ through the prime meridian on the equator and $Z$ through the north
pole. Lengths, angles and rates are computed on the Cartesian form; the
geodetic form is for people, maps and the inputs that arrive in it.

## How it works

### Geodetic to ECEF

With the prime vertical radius of curvature
$N(\varphi) = a / \sqrt{1 - e^2 \sin^2\varphi}$,

$$ X = (N + h)\cos\varphi\cos\lambda, \quad
Y = (N + h)\cos\varphi\sin\lambda, \quad
Z = \big(N(1 - e^2) + h\big)\sin\varphi . $$

The formula is closed-form and exact to rounding.

### ECEF to geodetic

Longitude is $\lambda = \operatorname{atan2}(Y, X)$ at once. Latitude and
height are coupled and the package iterates them to a fixed point: with
$\rho = \sqrt{X^2 + Y^2}$, start from the spherical guess
$\varphi_0 = \operatorname{atan2}\big(Z,\, \rho(1 - e^2)\big)$ and repeat

$$ h = \rho\cos\varphi + Z\sin\varphi - \frac{a^2}{N(\varphi)}, \qquad
\varphi \leftarrow \operatorname{atan2}\Big(Z,\; \rho\Big(1 - \frac{e^2 N}{N + h}\Big)\Big) $$

until $\varphi$ moves by less than $10^{-14}$ rad. The height form
$\rho\cos\varphi + Z\sin\varphi - a^2/N$ is the one to use: the textbook
$\rho/\cos\varphi - N$ divides by zero at the poles, where this one is
simply $|Z| - b$. Convergence is geometric with ratio about $e^2 h/(N+h)$,
so a handful of rounds reach sub-millimetre anywhere from the sea floor to
low Earth orbit; the property test covers $-11$ km to $2\,000$ km. Bowring's
one-step formula reaches the same accuracy for near-surface points, but
its error is a remainder term rather than a tolerance, and the iteration
costs about as much.

### The local tangent frame

At an origin $(\varphi_0, \lambda_0)$ the unit east, north and up vectors
in ECEF are the rows of

$$ R = \begin{pmatrix}
-\sin\lambda_0 & \cos\lambda_0 & 0 \\
-\sin\varphi_0\cos\lambda_0 & -\sin\varphi_0\sin\lambda_0 & \cos\varphi_0 \\
\cos\varphi_0\cos\lambda_0 & \cos\varphi_0\sin\lambda_0 & \sin\varphi_0
\end{pmatrix}, $$

and a point's east-north-up coordinates are
$R\,(\mathbf{P} - \mathbf{P}_0)$. $R$ is orthogonal and right-handed
($\mathbf{e} \times \mathbf{n} = \mathbf{u}$), so the conversion is a
rigid motion: every length, angle and rate is the same in ECEF and in the
frame. "Up" is the ellipsoid's normal at the origin, which is also the
direction geodetic height grows there, so a point straight above the
origin has frame coordinates $(0, 0, h - h_0)$.

A vector measured at a point other than the origin — a velocity report,
which is east-north-up where the receiver was — belongs in the frame
*there*. Rotating it with the origin's $R$ instead tilts it by the angle
between the two verticals, about $s/R_\oplus$ for a separation $s$:
$0.9°$ at 100 km, which on a 100 m/s velocity is a 1.6 m/s error in the
vertical rate. `EnuToEcefAt` applies the rotation at the point.

### Flat approximations and their bounds

The ellipsoid's radius of curvature lies between $b^2/a$ (the meridian at
the equator) and $a^2/b$ (at the poles) in every direction. A tangent
plane departs from the surface by $s^2/2R$ at distance $s$ — 78 m at
$s = 1$ km with the larger radius, so a lattice laid in a plane is a
three-dimensional object as soon as it spans kilometres, and the frame
gives its exact coordinates; the bounds exist so a caller can state how far
off a flat shortcut is, not so the package can take one.

A degree of latitude is shortest at the equator ($110\,574$ m), so
dividing a distance by `MinMetresPerDegreeLat` gives a degree span that
covers at least that distance; a degree of longitude is at least that
times $\cos\varphi$.

## Invariants

- `EcefToGeodetic(GeodeticToEcef(g))` returns to `g` within $10^{-3}$ m in
  position and $10^{-9}°$ in latitude for every height from $-11$ km to
  $2\,000$ km, the poles included.
- A frame's rotation is orthonormal to $10^{-14}$ and right-handed; its
  `Up()` equals `LocalUp(origin)`.
- `EnuToEcefAt(g, v)` equals `NewFrame(g).RotateEnuToEcef(v)` exactly.
- Nothing allocates, and every function is total: NaN in, NaN out.

## Trade-offs

- Heights are ellipsoidal by definition and the package has no geoid. A
  consumer with terrain or barometric heights adds the undulation it
  knows; the package cannot detect one that forgot.
- There is no geodesic distance on the ellipsoid. The ECEF chord
  underestimates the surface distance by $s^3/24R^2$ — a millimetre at
  10 km, 8 cm at 100 km — and the haversine on a sphere of radius $a$ is
  off by up to $0.5\,\%$; both are usually enough, and a caller who needs
  Karney's algorithm needs a decision of its own.

## Further reading

- NIMA TR8350.2, *Department of Defense World Geodetic System 1984*, 3rd
  edition, 2000 — the defining parameters.
- B. Hofmann-Wellenhof, H. Lichtenegger, J. Collins, *GPS: Theory and
  Practice*, §10.2 — the conversions and the iterative inverse.
- Decisions: [ADR-0356](../../../../doc/adr/0356-geodesy-wgs84-ecef-and-local-tangent-frames-as-a-science-package.md)
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/science/geo/geodesy
