-- Swiss LV95 / LV03 ⇄ WGS84 coordinate transforms as ClickHouse SQL UDFs
-- (ADR-0244).
--
-- The server-side twin of lv95.go and lv95_rigorous.go: same formulas, same
-- constants, same two accuracy tiers. Install once per server (UDFs are
-- server-global, not per-database); every statement is CREATE OR REPLACE, so
-- re-running is safe. Go callers get the script from ScriptSQL() /
-- StatementsSQL(); by hand it is `clickhouse-client < lv95_udfs.sql`.
--
-- Naming: UPPER_SNAKE under the owned namespace `SWISSTOPO_`, so the family is
-- enumerable with `WHERE name LIKE 'SWISSTOPO\_%'` in system.functions. The
-- `_C_` segment is a folded constant, `_GAUSS_`/`_PROJ_`/`_ELL_`/`_DATUM_` are
-- the stages, and the unsegmented names are the surface a query calls.
--
-- The plain name is the cheap tier and `_EXACT` is the closed form, after
-- ClickHouse's own quantile / quantileExact and uniq / uniqExact. It is also
-- the split the Go side already makes, where LV95ToWGS84 is the series and
-- LV95ToWGS84Rigorous the closed form: `_EXACT` here is `Rigorous` there.
--
-- Angles are decimal degrees at the surface and radians between the stages.
-- Planimetric results are metres. The public functions return named tuples, so
-- a call reads as SWISSTOPO_LV95_TO_WGS84(e, n).lat. Naming the elements is
-- worth what it costs, and what it costs is NULL: ClickHouse has no
-- Nullable(Tuple), so a NULL argument reaches the closing CAST as a value the
-- result type cannot hold and the query fails. Feed a nullable column through
-- `ifNull(col, nan)` — NaN travels the whole chain and comes back as
-- `(nan, nan)` — or filter the NULLs out before the call.
--
-- Two things this file does NOT do, both of which need data a UDF has no way
-- to carry: the LN02/LHN95 height transformation, and FINELTRA — swisstopo's
-- triangulated CH1903 ⇄ CH1903+ transformation. The nominal ±2'000'000 /
-- ±1'000'000 offset between LV03 and LV95 is what SWISSTOPO_LV03_TO_LV95
-- applies, and the two realizations differ by up to ~1.6 m beyond it.
--
-- Two idioms recur below, both forced by how ClickHouse evaluates a SQL UDF:
--
--   * `arrayMap((x) -> …, [v])[1]` is a let-binding. A UDF parameter is
--     substituted textually at every mention, so an intermediate used twice
--     duplicates its whole subtree; the `_EXACT` chain compounds that until it
--     overflows max_expanded_ast_elements outright. Wrapping the value in a
--     one-element array and binding it as a lambda parameter mentions it once.
--     A UDF that takes the lambda itself cannot stand in for this — ClickHouse
--     refuses to nest one inside another as a recursive lambda.
--   * ln(tan(π/4 + φ/2)) and its inverse 2·atan(exp(s)) − π/2, which is how
--     the projection is published, are spelled here through atanh and sinh.
--     Same two functions of φ, but ClickHouse's exp() and log() are vectorised
--     approximations carrying ~1e-9 relative error (its atanh, sinh, asin,
--     atan, atan2 and hypot are not), and 1e-9 in isometric latitude is ~5 mm
--     on the ground.

-- ── Surface: the default tier ───────────────────────────────────────────────
-- swisstopo's truncated series, good to ~1 m within Switzerland's extent and
-- degrading quickly outside it. It is the default because it is what most
-- callers want: no iteration, no datum stage, roughly an order of magnitude
-- cheaper per row than `_EXACT`, and already sub-pixel on a 2 m raster. Reach
-- for `_EXACT` where the result is stored, published, or compared against
-- another implementation.
--
-- The auxiliary variables are the published ones: y', x' in fractional
-- megametres off the false origin, φ', λ' in units of 10'000 arc-seconds off
-- Bern. Nothing above this line is needed to read them.

CREATE OR REPLACE FUNCTION SWISSTOPO_LV95_TO_WGS84 AS (east, north) ->
    arrayMap((yp) -> arrayMap((xp) -> CAST(
        ((16.9023892
          + 3.238272 * xp
          - 0.270978 * (yp * yp)
          - 0.002528 * (xp * xp)
          - 0.0447 * (yp * yp) * xp
          - 0.0140 * xp * (xp * xp)) * 10000 / 3600,
         (2.6779094
          + 4.728982 * yp
          + 0.791484 * yp * xp
          + 0.1306 * yp * (xp * xp)
          - 0.0436 * yp * (yp * yp)) * 10000 / 3600),
        'Tuple(lat Float64, lon Float64)'),
        [(north - 1200000) / 1000000])[1],
        [(east - 2600000) / 1000000])[1];

CREATE OR REPLACE FUNCTION SWISSTOPO_WGS84_TO_LV95 AS (lat, lon) ->
    arrayMap((pp) -> arrayMap((lp) -> CAST(
        (2600000 + 72.37
         + 211455.93 * lp
         - 10938.51 * lp * pp
         - 0.36 * lp * (pp * pp)
         - 44.54 * lp * (lp * lp),
         1200000 + 147.07
         + 308807.95 * pp
         + 3745.25 * (lp * lp)
         + 76.63 * (pp * pp)
         - 194.56 * (lp * lp) * pp
         + 119.79 * pp * (pp * pp)),
        'Tuple(e Float64, n Float64)'),
        [(lon * 3600 - 26782.5) / 10000])[1],
        [(lat * 3600 - 169028.66) / 10000])[1];

-- ── Constants of the exact tier ─────────────────────────────────────────────
-- Zero-argument UDFs: ClickHouse folds a constant expression once per query,
-- so naming them costs nothing and keeps the stage bodies readable.

-- Bessel 1841, the ellipsoid CH1903+ is referred to: 1/f = 299.1528128,
-- a = 6'377'397.155 m (written out at each use).
CREATE OR REPLACE FUNCTION SWISSTOPO_C_BESSEL_E2 AS () ->
    2 * (1 / 299.1528128) - (1 / 299.1528128) * (1 / 299.1528128);
CREATE OR REPLACE FUNCTION SWISSTOPO_C_BESSEL_E AS () ->
    sqrt(SWISSTOPO_C_BESSEL_E2());

-- WGS84: 1/f = 298.257223563, a = 6'378'137 m.
CREATE OR REPLACE FUNCTION SWISSTOPO_C_WGS84_E2 AS () ->
    2 * (1 / 298.257223563) - (1 / 298.257223563) * (1 / 298.257223563);

-- The fundamental point — the old observatory in Bern — in radians on Bessel.
CREATE OR REPLACE FUNCTION SWISSTOPO_C_PHI0 AS () -> 46.95240555555556 * pi() / 180;
CREATE OR REPLACE FUNCTION SWISSTOPO_C_LAMBDA0 AS () -> 7.439583333333333 * pi() / 180;

-- Radius of the projection sphere: the one that preserves curvature at the
-- fundamental point.
CREATE OR REPLACE FUNCTION SWISSTOPO_C_R AS () ->
    6377397.155 * sqrt(1 - SWISSTOPO_C_BESSEL_E2())
        / (1 - SWISSTOPO_C_BESSEL_E2() * sin(SWISSTOPO_C_PHI0()) * sin(SWISSTOPO_C_PHI0()));

-- Ratio between sphere and ellipsoid longitudes, the fundamental point's
-- latitude on the sphere, and the integration constant of the Gauss projection.
CREATE OR REPLACE FUNCTION SWISSTOPO_C_ALPHA AS () ->
    sqrt(1 + (SWISSTOPO_C_BESSEL_E2() / (1 - SWISSTOPO_C_BESSEL_E2()))
        * (cos(SWISSTOPO_C_PHI0()) * cos(SWISSTOPO_C_PHI0())
           * cos(SWISSTOPO_C_PHI0()) * cos(SWISSTOPO_C_PHI0())));
CREATE OR REPLACE FUNCTION SWISSTOPO_C_B0 AS () ->
    asin(sin(SWISSTOPO_C_PHI0()) / SWISSTOPO_C_ALPHA());
CREATE OR REPLACE FUNCTION SWISSTOPO_C_K AS () ->
    atanh(sin(SWISSTOPO_C_B0()))
        - SWISSTOPO_C_ALPHA() * atanh(sin(SWISSTOPO_C_PHI0()))
        + SWISSTOPO_C_ALPHA() * SWISSTOPO_C_BESSEL_E()
            * atanh(SWISSTOPO_C_BESSEL_E() * sin(SWISSTOPO_C_PHI0()));

-- ── Gauss projection: Bessel ellipsoid ⇄ projection sphere ──────────────────

-- Ellipsoid latitude -> sphere latitude.
CREATE OR REPLACE FUNCTION SWISSTOPO_GAUSS_FWD AS (phi) ->
    atan(sinh(SWISSTOPO_C_ALPHA() * atanh(sin(phi))
        - SWISSTOPO_C_ALPHA() * SWISSTOPO_C_BESSEL_E() * atanh(SWISSTOPO_C_BESSEL_E() * sin(phi))
        + SWISSTOPO_C_K()));

-- One fixed-point step of the inverse: the latitude appears on both sides
-- through the isometric term, and the eccentricity is small enough that the
-- map contracts by ~3e-3 per step.
CREATE OR REPLACE FUNCTION SWISSTOPO_GAUSS_INV_STEP AS (s, phi) ->
    atan(sinh(s + SWISSTOPO_C_BESSEL_E() * atanh(SWISSTOPO_C_BESSEL_E() * sin(phi))));

-- Sphere isometric latitude -> ellipsoid latitude. Six steps off the
-- eccentricity-free seed, one past the last step that still moves a double.
CREATE OR REPLACE FUNCTION SWISSTOPO_GAUSS_INV AS (s) ->
    arrayMap((iso) ->
        SWISSTOPO_GAUSS_INV_STEP(iso,
            SWISSTOPO_GAUSS_INV_STEP(iso,
                SWISSTOPO_GAUSS_INV_STEP(iso,
                    SWISSTOPO_GAUSS_INV_STEP(iso,
                        SWISSTOPO_GAUSS_INV_STEP(iso,
                            SWISSTOPO_GAUSS_INV_STEP(iso, atan(sinh(iso)))))))),
        [s])[1];

-- ── Swiss Oblique Mercator: Bessel geodetic (radians) ⇄ LV95 (metres) ───────
-- The sphere is rotated so the fundamental point lies on the pseudo-equator,
-- then Mercator-projected. Both directions fold atanh(sin(asin(u))) back to
-- atanh(u), which is the same value spelled without the round trip.

-- The rotated sphere's longitude and latitude, recovered from a grid position.
CREATE OR REPLACE FUNCTION SWISSTOPO_PROJ_BARL AS (east) -> (east - 2600000) / SWISSTOPO_C_R();
CREATE OR REPLACE FUNCTION SWISSTOPO_PROJ_BARB AS (north) ->
    atan(sinh((north - 1200000) / SWISSTOPO_C_R()));

CREATE OR REPLACE FUNCTION SWISSTOPO_PROJ_FWD AS (phi, lam) ->
    arrayMap((b) -> arrayMap((l) ->
        (SWISSTOPO_C_R() * atan2(sin(l),
             sin(SWISSTOPO_C_B0()) * tan(b) + cos(SWISSTOPO_C_B0()) * cos(l))
             + 2600000,
         SWISSTOPO_C_R() * atanh(
             cos(SWISSTOPO_C_B0()) * sin(b) - sin(SWISSTOPO_C_B0()) * cos(b) * cos(l))
             + 1200000),
        [SWISSTOPO_C_ALPHA() * (lam - SWISSTOPO_C_LAMBDA0())])[1],
        [SWISSTOPO_GAUSS_FWD(phi)])[1];

CREATE OR REPLACE FUNCTION SWISSTOPO_PROJ_INV AS (east, north) ->
    arrayMap((bb) -> arrayMap((bl) ->
        (SWISSTOPO_GAUSS_INV(
             (atanh(cos(SWISSTOPO_C_B0()) * sin(bb)
                    + sin(SWISSTOPO_C_B0()) * cos(bb) * cos(bl))
              - SWISSTOPO_C_K()) / SWISSTOPO_C_ALPHA()),
         SWISSTOPO_C_LAMBDA0() + atan2(sin(bl),
             cos(SWISSTOPO_C_B0()) * cos(bl) - sin(SWISSTOPO_C_B0()) * tan(bb))
             / SWISSTOPO_C_ALPHA()),
        [SWISSTOPO_PROJ_BARL(east)])[1],
        [SWISSTOPO_PROJ_BARB(north)])[1];

-- ── Ellipsoid ⇄ geocentric Cartesian ────────────────────────────────────────
-- Both at ellipsoidal height zero, which is what a two-dimensional datum
-- transformation means: a point's true height perturbs the result far below
-- the level the datum shift itself is specified to.

-- Semi-minor axis and second eccentricity squared of the ellipsoid (a, e²).
CREATE OR REPLACE FUNCTION SWISSTOPO_ELL_B AS (a, e2) -> a * sqrt(1 - e2);
CREATE OR REPLACE FUNCTION SWISSTOPO_ELL_EP2 AS (a, e2) ->
    (a * a - SWISSTOPO_ELL_B(a, e2) * SWISSTOPO_ELL_B(a, e2))
        / (SWISSTOPO_ELL_B(a, e2) * SWISSTOPO_ELL_B(a, e2));

CREATE OR REPLACE FUNCTION SWISSTOPO_ELL_GEOD_TO_CART AS (phi, lam, a, e2) ->
    arrayMap((nu) ->
        (nu * cos(phi) * cos(lam),
         nu * cos(phi) * sin(lam),
         nu * (1 - e2) * sin(phi)),
        [a / sqrt(1 - e2 * sin(phi) * sin(phi))])[1];

-- Bowring's inverse, whose error is below a micrometre for points near the
-- ellipsoid surface; `th` is his auxiliary angle.
CREATE OR REPLACE FUNCTION SWISSTOPO_ELL_CART_TO_GEOD AS (x, y, z, a, e2) ->
    arrayMap((p) -> arrayMap((th) ->
        (atan2(z + SWISSTOPO_ELL_EP2(a, e2) * SWISSTOPO_ELL_B(a, e2) * sin(th) * sin(th) * sin(th),
               p - e2 * a * cos(th) * cos(th) * cos(th)),
         atan2(y, x)),
        [atan2(z * a, p * SWISSTOPO_ELL_B(a, e2))])[1],
        [hypot(x, y)])[1];

-- ── Datum: CH1903+ ⇄ WGS84 (radians in, radians out) ────────────────────────
-- The three-parameter geocentric translation EPSG:1676, "CH1903+ to WGS 84
-- (1)". It is a fit: swisstopo's own REFRAME service uses a triangulated
-- transformation instead, and against its published reference points this
-- lands within about 1.5 cm. That residual is the Helmert parameters'
-- departure from REFRAME, not an error in the formulas.

CREATE OR REPLACE FUNCTION SWISSTOPO_DATUM_CH_TO_WGS84 AS (phi, lam) ->
    arrayMap((c) -> SWISSTOPO_ELL_CART_TO_GEOD(
            tupleElement(c, 1) + 674.374,
            tupleElement(c, 2) + 15.056,
            tupleElement(c, 3) + 405.346,
            6378137.0, SWISSTOPO_C_WGS84_E2()),
        [SWISSTOPO_ELL_GEOD_TO_CART(phi, lam, 6377397.155, SWISSTOPO_C_BESSEL_E2())])[1];

CREATE OR REPLACE FUNCTION SWISSTOPO_DATUM_WGS84_TO_CH AS (lat, lon) ->
    arrayMap((c) -> SWISSTOPO_ELL_CART_TO_GEOD(
            tupleElement(c, 1) - 674.374,
            tupleElement(c, 2) - 15.056,
            tupleElement(c, 3) - 405.346,
            6377397.155, SWISSTOPO_C_BESSEL_E2()),
        [SWISSTOPO_ELL_GEOD_TO_CART(lat, lon, 6378137.0, SWISSTOPO_C_WGS84_E2())])[1];

-- ── Surface: the exact tier ─────────────────────────────────────────────────
-- Closed-form projection plus the datum shift, three orders of magnitude
-- closer to the ground than the default tier. The projection stage is exact to
-- floating point; the datum stage carries the ~1.5 cm noted above, which is
-- the floor and no choice of formula lowers it. Round-tripping a point does
-- not return it to the bit: both directions set the ellipsoidal height to
-- zero, and the ~1 mm that costs is what a two-dimensional datum shift is.

CREATE OR REPLACE FUNCTION SWISSTOPO_LV95_TO_WGS84_EXACT AS (east, north) ->
    arrayMap((g) -> CAST((tupleElement(g, 1) * 180 / pi(), tupleElement(g, 2) * 180 / pi()),
                         'Tuple(lat Float64, lon Float64)'),
        [arrayMap((p) -> SWISSTOPO_DATUM_CH_TO_WGS84(tupleElement(p, 1), tupleElement(p, 2)),
            [SWISSTOPO_PROJ_INV(east, north)])[1]])[1];

CREATE OR REPLACE FUNCTION SWISSTOPO_WGS84_TO_LV95_EXACT AS (lat, lon) ->
    CAST(arrayMap((g) -> SWISSTOPO_PROJ_FWD(tupleElement(g, 1), tupleElement(g, 2)),
             [SWISSTOPO_DATUM_WGS84_TO_CH(lat * pi() / 180, lon * pi() / 180)])[1],
         'Tuple(e Float64, n Float64)');

-- ── LV03 / CH1903 ───────────────────────────────────────────────────────────
-- The nominal frame offset only — see the FINELTRA note in the header. LV03
-- names its axes y (east) and x (north), which is why the argument order below
-- reads backwards for a planimetric pair. The offset is exact either way, so
-- the tier a composition carries is the one its LV95 half carries.

CREATE OR REPLACE FUNCTION SWISSTOPO_LV03_TO_LV95 AS (y, x) ->
    CAST((y + 2000000, x + 1000000), 'Tuple(e Float64, n Float64)');
CREATE OR REPLACE FUNCTION SWISSTOPO_LV95_TO_LV03 AS (east, north) ->
    CAST((east - 2000000, north - 1000000), 'Tuple(y Float64, x Float64)');

CREATE OR REPLACE FUNCTION SWISSTOPO_LV03_TO_WGS84 AS (y, x) ->
    SWISSTOPO_LV95_TO_WGS84(y + 2000000, x + 1000000);
CREATE OR REPLACE FUNCTION SWISSTOPO_LV03_TO_WGS84_EXACT AS (y, x) ->
    SWISSTOPO_LV95_TO_WGS84_EXACT(y + 2000000, x + 1000000);

CREATE OR REPLACE FUNCTION SWISSTOPO_WGS84_TO_LV03 AS (lat, lon) ->
    arrayMap((g) -> CAST((tupleElement(g, 1) - 2000000, tupleElement(g, 2) - 1000000),
                         'Tuple(y Float64, x Float64)'),
        [SWISSTOPO_WGS84_TO_LV95(lat, lon)])[1];
CREATE OR REPLACE FUNCTION SWISSTOPO_WGS84_TO_LV03_EXACT AS (lat, lon) ->
    arrayMap((g) -> CAST((tupleElement(g, 1) - 2000000, tupleElement(g, 2) - 1000000),
                         'Tuple(y Float64, x Float64)'),
        [SWISSTOPO_WGS84_TO_LV95_EXACT(lat, lon)])[1];
