---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-19 as background to
> [ADR-0249](../adr/0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md)
> (accepted the same day). Nothing here is a decision; §9 lists what the ADR
> took from it.
>
> **Provenance — clean-room.** The page rests on product and API
> documentation, format specifications, published blog posts, papers,
> changelogs and release notes, READMEs as rendered on a project's landing
> page, and the *prose* of public issue threads. **No source file, repository
> code view, diff, or package content of any surveyed project was read**, and
> where a permitted page carried code the behaviour is described and the code
> is not. The reading was done on the compile date by four delegated research
> passes, each held to that rule and each required to tag every claim; §10
> lists the pages and the incidents they reported. A claim appears below as
> fact only where a pass read it on a fetched page; what a pass could only
> recall is marked *(recalled)* or left out, and what is inferred here rather
> than sourced is marked *(inference)*. Two passes found the page-fetch tool's
> summaries inventing values and read raw page text instead; six load-bearing
> quotations from the pass that did not say it had done so were re-checked
> against raw page text by the compiler and held. Quotations are otherwise as
> the passes reported them. Numeric defaults are as documented on the compile
> date.

# Drawing a large vector field on a map — what the state of the art does, and what it had to fix

## 1 Question and scope

ADR-0249 decides on particles advected in Go through a windowed sample of a
gridded two-component field, drawn as stored-position trails over a slippy
map. Every piece of that has been built before, mostly in browsers, and the
builders have left a public trail of parameters, changelogs and bug reports.
The question here is narrow: **which combinations of algorithms are known to
work, and which edge cases has somebody already paid for**, so that the ADR
decides them instead of rediscovering them.

Four areas: the particle renderers themselves (§2–§3), the flow-visualisation
literature (§4), how large fields are stored and served (§5), and what
real-world wind and current data does to a contract that asks for a regular,
earth-relative lat/lon grid (§6–§7). §8 is the consolidated bug list; §9 sets
the findings against the ADR.

## 2 The renderers

Three trail techniques exist, and the choice between them decides most of the
rest.

| Technique | Who | What it costs |
| --- | --- | --- |
| **Screen accumulation buffer** — draw heads, fade the previous frame, repeat | Agafonkin's WebGL wind map, Mapbox `raster-particle`, Xweather MapsGL, the canvas lineage (leaflet-velocity, wind-layer) through `globalAlpha` | Long tails for nothing; the buffer is in screen space, so every pan or zoom invalidates it |
| **Stored position history** — keep a particle's last positions, draw segments between them | WeatherLayers GL (trails through a line layer), mapbox-exif-layer (`trailLength` 3), and the fix the windgl maintainer proposed for the row above | Survives pan and zoom; cost grows with history length |
| **Precomputed streamlines** — integrate once per view, animate a lit window along each line in the shader | Esri `FlowRenderer`, cesium-wind-layer's line mode | No per-frame advection; recomputed whenever the view settles; paths loop rather than evolve |

**Agafonkin, "How I built a wind map with WebGL" (2017, read from an archived
copy).** It states the CPU baseline plainly — random positions, move each by
the wind under it, reset a small portion to random positions each frame so
that areas the wind blows away from never empty, fade the screen and draw on
top — and its limits: "Earth uses ~5k" particles and takes "~2 seconds" on
every data or view update. Three details of his GPU version carry over to any
implementation. A reset decided from the particle's position alone makes
particles always vanish in the same places, and from its index alone always
kills the same particles. Areas of fast wind "look much more dense" than calm
ones, which he corrects by raising the reset probability with speed (the
*drop-rate bump*). And hardware-linear sampling of a one-degree grid was
still blocky, so he interpolates by hand.

**cambecc/earth (README).** A one-degree grid interpolated bilinearly in the
browser, "quite costly"; the projection's distortion of the vector is found
per screen point by finite differences; a new layer triggers a new grid, a
re-interpolation and a new animator, which "sometimes caus[es] visual
artifacts that (usually) quickly disappear". An issue reports that any UI
state change triggers the full re-interpolation. A newsletter post makes a
point §5 returns to: a grid cell is an area average, and the 0.25° model
showed 148 km/h for a hurricane measured at 220.

**Mapbox `raster-particle` (style specification).** `count` 512 *per tile*,
`max-speed` 1 (clamp and top of the colour ramp), `speed-factor` 0.2
(zoom-interpolable), `fade-opacity-factor` 0.98, `reset-rate-factor` 0.8 "to
avoid degeneration (empty areas without particles)", colour "evaluated at 256
uniformly spaced steps". The official example overrides nearly all of them
(4000, 40, 0.4, 0.9, 0.4), which says what the defaults are worth. Release
notes record a globe-view flicker fix, an Android rendering fix, "Fix raster
particle data decoding and improve rendering quality", and a later change of
colour-ramp interpolation to non-premultiplied colours.

**Esri `FlowRenderer` (API reference; precursor described in a 2021 Esri blog
post, read from an archive).** Defaults `density` 0.8, `flowSpeed` 10,
`trailLength` 100, `maxPathLength` 200 pt, `trailWidth` 1.5 pt,
`flowRepresentation` "flow-from". At high density "new streamlines may be
discarded". With low-magnitude data the animation "may appear slow and the
trail short (almost static)"; a flood model under 5 m/s needs `trailLength`
near 1500. The precursor's choices are instructive where they differ from the
particle lineage:

- the field for the current extent is **downsampled to one cell per 5 px**
  before integration — to save time, and because "full-resolution wind data
  may have high-frequency components that are likely to destabilize the
  simulator" — with an optional Gaussian blur;
- integration advances by a **fixed segment length, not a fixed time step**,
  so that "line lengths vary less" and vertices do not bunch, and stops where
  the field is too slow;
- each line gets a **random phase**, so lines of equal length do not pulse in
  step;
- the old mesh is re-transformed under the moving view until the new one is
  ready; line width is applied after the view scale and multiplied by the
  pixel ratio.

Release 4.23 "fixes a reprojection issue that was affecting the direction of
the flow" in both the product and the published sample.

**WeatherLayers GL (docs, changelog, and the maintainer in deck.gl-particle
issues).** `numParticles` 5000, `maxAge` 100 frames, `speedFactor` 1, default
`maxZoom` 15 because "rendering artifacts may occur in high zoom levels due
to a low precision". The maintainer on why state is geographic: "Storing
particles in viewport coordinates assumes that the viewport is static,
therefore any interaction forces that the particle state has to be reset. I'd
like to avoid such resetting." Data side: two images and a weight for time
(`image`, `image2`, `imageWeight`), sample-time interpolation NEAREST, LINEAR
or CUBIC with **CUBIC the default**, input in EPSG:4326 only. Its changelog is
the richest single source for §8.

**Xweather MapsGL (reference, changelog).** Began with a fixed `count`
(65 536) and migrated to a `density` enum that scales with the viewport;
`dropRate` 0.01, `dropRateBump` 0.01, `trailsFadeFactor` 0.98.

**The canvas lineage (wind-layer docs, leaflet-velocity README and issues).**
`maxAge` 90 frames, `paths` 800, `velocityScale` 1/25 (leaflet-velocity:
0.005, "arbitrarily"), a 20 ms frame interval, and field options `wrapX`,
`flipY` and `translateX` that exist because inputs disagree about longitude
origin and row order.

**mapbox-exif-layer (README)** is the closest published analogue to stored
trails: an age threshold after which reset probability rises (500 steps) and
a hard maximum (1000) that "prevents particles from degenerating into
circular/looped patterns"; an update interval (50 ms) decoupled from
rendering; a warning that too large a velocity factor "can make particles
jump across multiple grid cells and miss intermediate flow detail"; and on a
field swap only half the particles are re-seeded, a soft transition.

**windgl (README, issues)** shipped **with no trails at all**, and says why
(§8 row 1). **cesium-wind-layer (README, release notes)** clamps trail length
to a range by speed and needed six releases to make speed independent of the
display's refresh rate. **Windy** states a 30 fps target and nothing about
method; **Ventusky** and **Zoom Earth** have published nothing found.

## 3 Agreement and forks

**What every surveyed renderer does.**

- Bilinear interpolation of components or better; nearest is rejected
  everywhere it is mentioned.
- Some random re-seeding. None relies on advection alone.
- One forward step per update. No page read names a higher-order integrator.
- **A dimensionless speed factor, and no claim of physical speed.** Esri's
  knots-to-pixels constant was chosen because it "seemed to be a good value".
- Colour by speed through a ramp over a fixed, clamped domain.
- A default line about 1–2 px wide, white.
- The v-sign, row-order and from/to mistakes of §8 rows 12–13, more than
  once each.

**Where good implementations differ.**

| Fork | One side | The other |
| --- | --- | --- |
| Particle state | Screen space: uniform density for free; a reset and a 0.75–2 s hiccup on every interaction (earth, leaflet-velocity) | World or geographic space: survives interaction; needs 64-bit positions or a zoom cap, and view-aware re-seeding (WeatherLayers, olwind) |
| Speed against zoom | Constant in world units: faster, longer and denser on screen as one zooms in — wind-layer's maintainer calls it "expected" | Constant on screen: scaled by 2^−zoom (deck.gl-particle; one user preferred base 1.7), or a tuned ramp (windgl 0.5 → 0.8 over z0–10) |
| Count | Fixed N, or N per tile | Density per viewport area — Xweather moved to it, Esri caps by screen size |
| Reset | Probability per frame, plus a speed bump | Maximum age with a random phase; exif-layer uses both |
| Time base | Step per rendered frame, later patched to normalise to 60 fps | Fixed update interval decoupled from rendering (20 ms, 50 ms, Windy's 30 fps) |
| Field preparation | Sample the source grid per particle | Resample to a view-aligned grid first — earth pays ~2 s for it, Esri does it at one-fifth resolution on purpose |
| Where Mercator happens | At build time, into Mercator tiles (Mapbox MTS, the first carbonplan maps) | At sample time from lat/lon (WeatherLayers, carbonplan's later zarr-layer, xpublish-tiles) — the direction of travel |

## 4 The literature

### 4.1 What makes a flow display readable

Laidlaw et al. (2005) compared six static methods on three tasks and found
users did better with methods that "1) showed the sign of vectors within the
vector field, 2) visually represented integral curves, and 3) visually
represented the locations of critical points." Arrow grids — regular or
jittered — were the worst at locating critical points; the method without a
sign (LIC) was slow at advection because "LIC does not display any flow
direction information. This information is critical". Evenly spaced
streamlines with no speed cue were weaker at locating critical points, which
the authors put down to the missing speed information. No animated method was
tested.

**Direction in a still frame.** Fowler & Ware (1989): plain segments "are
ambiguous as to direction", and "The direction of perceived flow was away from
the end of the stroke which matched the background colour" — 10 of 234
observations deviated. On a neutral grey background the effect was weaker.
Mitchell, Ware & Kelley (2009), a preference study with few participants: "The
heads of streaklets were always made more visually distinct than the tails",
by opacity, width, or both; a blend from the background colour is suggested
over an arrowhead. So a trail whose alpha falls with age is *not* the
ambiguous case. The ambiguous cases are a trail dimmed uniformly (a whole-trail
birth fade), a trail too short to show a gradient, and a head with little
contrast against a busy basemap *(the last is inference from the grey-background
result)*.

**Animation.** Ware et al. (2016), on steady flow: both animated methods beat
both static ones at pattern detection; at tracing advection animated
streamlets were best and animated *orthogonal* segments worst — the
flow-aligned trail carries the benefit, not motion alone. Kettunen & Oksanen
(2019, abstract seen only in a search summary) found motion dominating
graphical variations for locating maxima, which argues for carrying speed
redundantly in colour when display speed is stylised.

**Static presentations, for the deferral.** Pilar & Ware (2013): barbs on a
grid convey speed and lose the pattern; streamlines convey the pattern and "do
not accurately convey the wind speed"; glyphs placed along equally spaced
streamlines beat the classic barb. Ware, Kelley & Pilar (2014): "for static
graphic presentations, equally spaced streamlines may be optimal". The barb
conventions themselves (half barb 5 kt, full 10, pennant 50, calm circle;
barbs clockwise from the shaft in the northern hemisphere and counter-clockwise
in the southern; the wind blows *from* the barbed end) were read from
secondary sources; the authority is WMO-No. 485, which was not reached.

### 4.2 Keeping density uniform

Jobard, Erlebacher & Hussaini (2002) state the problem of any pure particle
display: particles "accumulate into small clusters that follow almost
identical trajectories, leaving regions of flow divergence with a low density
of particles. To maintain dense coverage of the domain, the data structures
must support dynamic insertion and deletion of particles or track more
particles than needed". Their injection rate "should be modeled as… a
constant term… and a term that is a function of the velocity divergence"; in
practice a fixed 2–3 % per frame, and too much "deteriorates the quality of
the temporal correlation". This is the published counterpart of the web
lineage's per-frame reset.

Jobard, Ray & Sokolov (2012) name the cost of insertion and deletion —
"strong popping artifacts" — and the synchronisation artefact: "all arrows
born (resp. died) at time t have the same transparency coefficient, leading to
a visual artifact. This can be solved by adding a random delay". They fade
glyphs in and out, and replace them with discs in calm regions.

Stricter control — an occupancy grid that caps births in full cells and forces
them in empty ones, from the evenly-spaced-streamline work of Jobard & Lefer
and Mebarki et al. — exists, at the price of more popping. No paper found
evaluates lifetime distributions perceptually; the practice is folklore plus
the two statements above.

### 4.3 Integration

Weiskopf & Erlebacher (2005): "first order Euler integration might be
acceptable for streamlets but not for longer streamlines." Jobard et al.
(2002) found that for short streaks a second-order scheme gave "no noticeable
improvement", and for anything tracked longer that "the accumulation of error…
is very noticeable… Unlike the first order scheme, the second order scheme
produces closed circles"; "a second order Runge-Kutta midpoint algorithm
provides sufficient accuracy." Hauser's course slides: "RK-2: even with dt=1
(9 steps) better than Euler with dt=1/8 (72 steps)" and "RK-4: pays off only
with complex flows". Step size is bounded in cell widths — one to two at most
in the Lagrangian-Eulerian paper.

The failure is specific: on a closed circulation each Euler step lengthens the
radius, so particles spiral outward and **a cyclone reads as a weak source** —
the display misstates the type of the critical point *(the mechanism is
recalled; the effect is in the sources)*. The surveyed renderers all take one
forward step; the literature says that is safe only while trails stay short.

Interpolating components is the standard throughout. It shortens the vector
between differing directions — to zero between opposite ones — but keeps the
field continuous. Interpolating speed and direction separately preserves speed
and breaks continuity, needs shortest-arc handling at 0/360°, and is undefined
at calm *(derivation; no single citable source found)*. NCEP's `bilingrb` page
on doing it to direction: "it will mess up your data seriously near the branch
cut".

### 4.4 Averaging wind: the names

Two different means, and meteorology has names for both.

- **Vector mean**, also *resultant* wind: average the components. EPA's
  monitoring guidance has "resultant mean wind speed" and "resultant mean wind
  direction" computed from mean components; NDBC calls it "a true vector
  average… All u and v components are then averaged separately". WMO-No. 8:
  "Vector averaging is to be preferred to scalar averaging."
- **Scalar mean wind speed**: average the speeds. NDBC: the scalar method
  "will produce greater wind speeds than if a true vector average was used."
  Copernicus Marine: the vector average "is equal to or smaller than the scalar
  average" — opposing 5 m/s winds have scalar mean 5 and vector mean 0 — with
  the guidance **scalar for magnitude and energy, vector for transport**.
- Their ratio, |vector mean| / scalar mean, is the wind's **constancy** (Singer
  1967, seen as a search summary): 1 for a unidirectional field, 0 for a
  symmetric one.

Linear filtering of components commutes with bilinear interpolation and with
linear blending in time; speed commutes with none of them *(inference)*. That
is the argument for a separate scalar-mean plane — and **no surveyed system
and no paper found builds one**. The vector-field simplification literature is
cluster-, feature- or topology-driven (Telea & van Wijk, Heckel et al.,
Theisel et al.); nothing found addresses a box-filtered pyramid of components
for interactive display. Scale-space work (Bauer & Peikert) does say what to
expect of one: under smoothing, critical points move, merge and vanish in
pairs, so a coarse level legitimately has fewer vortices.

### 4.5 Time

In steady flow pathlines, streamlines and streaklines coincide; in unsteady
flow they do not (Weiskopf & Erlebacher). Jobard et al.: "a single frame
represents the instantaneous structure of the flow (streamlines)". Where
display time is decoupled from data time — particles cross the screen while
the forecast clock stands still — **each trail is a streamlet of the
instantaneous, time-blended field and not a trajectory**, however much it
looks like one *(inference from the two)*.

Zavala-Hidalgo et al. (2003) on linear interpolation between wind fields: it
"is a good approximation for features that behave like standing waves… When
there are fast moving features, such as fronts, the interpolated fields behave
like duplicated standing fronts, changing their amplitude instead of moving."
Stohl et al. (1995, abstract as a search summary) rank temporal interpolation
as the largest interpolation error in trajectory work. Alternatives exist —
complex-EOF, FFT phase shift, optical-flow morphing — and none was found in
any display tool's documentation; WeatherLayers and Xweather blend linearly,
the open build of earth stops the animation.

### 4.6 Projection and the poles

On a conformal projection local angles are correct and scale is the same in
every direction (Snyder). Web Mercator has h = k = sec φ: an east/north vector
maps to screen direction (u, −v) with no angular correction, and ground scale
grows with sec φ. Plate carrée is not conformal, and the screen vector is
(u·sec φ, v) up to scale. The general method is the finite-difference Jacobian
earth uses. With stylised speed, whether screen speed includes sec φ is a
choice, not a consequence.

At the pole, east and north are undefined. NCEP's ON388 gives the WMO
convention — a frame at the North Pole with its y-axis toward the prime
meridian — and notes that some grids carry one pole datum which "the user must
expand". A full pole row holds one physical vector in a basis that turns a
degree per degree of longitude, so **averaging components along or near it
cancels a real cross-polar flow** *(derivation)*. ECMWF records that two of its
own interpolation packages disagree at the pole rows and excluded those rows
from its comparison. wgrib2: "Vector interpolation is needed for interpolating
zonal and meridional winds near the poles."

## 5 Storing and serving a large field

**Agreement.** Components are the stored form everywhere (Open-Meteo, Mapbox,
WeatherLayers; Esri's magnitude-direction average passes through U and V).
Time steps are a dimension of one store. The renderer takes "the coarsest
overview whose resolution is still sufficient" (Earthmover); previous content
stays until new content arrives (deck.gl's best-available refinement); time is
a linear blend of two bracketing steps.

**The box mean of components is the established downsampling.** Esri's
resample function has "Vector Average — Calculates vector average of
magnitude-direction using all involved pixels", and its vector-field renderer
thins at draw time by converting to U and V, averaging, and converting back.
GDAL's `average` takes "all non-NODATA contributing pixels". A 2× pyramid
costs about 33 % on top of the base (Earthmover), and levels may be derived
from one another, with a caveat: "This only applies for composable resampling
methods (mean, min, max, etc.) Non-composable resampling methods (median,
mode, nearest neighbor) should always use the native data". A NaN-aware mean
is composable only if each level carries how many valid samples went into each
cell *(inference)*. GDAL offers `rms` and has no guidance for direction or
component data; applied per component it loses the sign.

**Gutters.** Wherever interpolation meets an edge, the systems duplicate a
border: Mapbox's `buffer` is "the number of pixels around the border of each
tile which are duplicated across tiles", to be set "to decrease tile boundary
artifacts when using a raster layer with linear resampling"; xpublish-tiles
subsets with at least one point outside the viewport; GDAL's warper reads one
extra source pixel by default. windgl's README records the alternative: per-tile
textures, and sampling particles across tiles "hasn't been implemented".

**Missing values in a pyramid.** xESMF: by default NaNs are treated "like
regular values hence potentially resulting in missing values bleeding into the
regridded field"; the remedy is a threshold on the valid fraction (`na_thres`)
with renormalised weights. GDAL has the same knob
(`NODATA_VALUES_PCT_THRESHOLD`). At "any valid" an ocean-current pyramid floods
the land; at "all valid" it erodes the coast. `bilingrb` documents the strict
rule and its one exception — a new point coinciding with an old one — "to
avoid unnecessary swelling of coastlines when subsampling".

**Quantisation.** The Agafonkin lineage packs u and v into 8-bit channels over
a global range; WeatherLayers describes that as "quantized data into 256
possible values" and offers Float32 "when exact values with no quantization
errors are needed". Mapbox's raster-array stores 32-bit integers with a scale
and offset per layer, the scale to be "as large as tolerable while balancing
posterization due to quantization". Open-Meteo uses scale-factor integers.
Over ±30 m/s, 8 bits is a step of about 0.24 m/s per component — slow wind
stalls *(arithmetic)*. Transport formats quantise; analysis-grade stores keep
floats.

**Caches.** A public Mapbox GL JS issue, open on the compile date: band
playback every ~900 ms grows the heap "~150 MB → 500 MB → 1 GB+", pausing
"does not cause memory to fall back", and evicting off-screen tiles does not
help because the growth is in the visible tiles' decoded bands. deck.gl's tile
layer has a byte-sized cache bound that needs each item to report its length,
and states that an aborted load must not return data, because a truthy return
is cached.

**Level changes and request traffic.** Map clients converge on the same small
set: keep the best available content until the new level loads (deck.gl;
OpenLayers sizes its cache "to render two zoom levels"), separate up and down
thresholds so a view hovering at a boundary does not flicker (general
level-of-detail practice), rate-limit refetch (Leaflet's `updateInterval`
200 ms, deck.gl's `debounceTime`), and cancel what a newer view supersedes
(MapLibre's `cancelPendingTileRequestsWhileZooming`, default true).
Open-Meteo documents what happens otherwise: a whole variable copied per tile
request "blocks the main thread noticeably while panning and zooming".

**Two layouts.** Open-Meteo keeps the same data twice — time-series chunks and
one map-shaped file per timestamp — and dynamical.org gives the rule: "Use the
product that will fetch the fewest chunks for your access patterns." It bears
on the deferred SQL-backed source, not on the first cut.

## 6 What real data does to the contract

ADR-0249 §SD1 asks a source for a grid regular in latitude and longitude, with
earth-relative east/north components. Each clause hides a class of bug.

**Frame.** GRIB2 flag table 3.3 has a bit for whether components are "relative
to easterly and northerly directions" or "relative to the defined grid". On
lat-lon, Mercator and Gaussian grids the two coincide (wgrib2); on Lambert
conformal, polar stereographic and rotated grids they do not — HRRR, NAM,
WRF output, HARMONIE, and rotated-pole regional models. NOAA's HRRR FAQ gives
the Lambert rotation: an angle of the cone constant times the longitude offset
from the reference meridian. For HRRR that is nothing on its central meridian
and about 17° at either coast; on a polar stereographic grid the error *is* the
longitude offset *(arithmetic from the FAQ's constants)*. Speed is untouched
and the error is smooth, so the map looks plausible. Documented casualties: a
MetPy issue on NAM/HRRR soundings; a METplus discussion where wind error
statistics were inflated because rotation ran only when both components were
in the file; CDO remapping a Lambert GRIB without rotating; GDAL's GRIB driver,
whose documentation does not mention winds at all. The opposite trap: ECCC's
global product is a plain lat-lon grid whose documentation says components are
grid-relative — so the flag set on a lat-lon grid is not an error and not a
reason to touch v.

**Order of operations.** Staggered grids (WRF, ROMS, NEMO) define u and v at
different points. The NEMO forum: "you have to interpolate the u and v
coordinates to a common grid … in order for rotation to make sense." NCL's
developers on rotated grids: "Apply the formulas first, and then perform the
interpolation." So: collocate, rotate, resample as a vector, decimate.

**Scan order and rows.** Flag table 3.4: −i, +j, column-major and
row-alternating scans all exist (NDFD uses the last). i is west-to-east and j
south-to-north whatever the scan bits say. wgrib2 normalises to one order
because "geolocation" depends on it; a downstream JSON converter's issue reads
"expects scan mode 0 but data is in mode 64".

**Longitude.** GRIB2 lat-lon files run 0–360; GDAL began shifting them to ±180
by default in one release, so two versions georeference the same file
differently. Some gridline-registered periodic files repeat the first column
as the last (GMT: "the values in the first and last columns are equal"); most
model grids do not. wind-layer's maintainer on wrapping a regional field: do
not — wrap is a property of the field.

**Registration.** GRIB's first and last points are node positions; a
pixel-registered raster of the same data has bounds half a cell wider. ECMWF on
its reanalysis: "think of the data as point values".

**Missing values.** ecCodes substitutes 9999 by default for bitmapped points,
and "the value of the 'missingValue' key used to encode the data is not itself
encoded in the GRIB"; wgrib2 uses 9.999e20; NDFD carries missing values inside
complex packing with no bitmap at all; packed NetCDF needs the fill test on the
raw integer before scale and offset. u and v can carry different masks.

**Identity.** A single global forecast file holds u and v at seven heights
above ground, about forty pressure levels, three altitudes, the boundary
layer, the tropopause and the max-wind level. One display tool's changelog
records confusing the 10 m wind with the 10 hPa wind. Storm motion, shear and
momentum flux are vector pairs that are not winds. Wind *direction* is where
the wind comes from (MetPy: "the direction from which the wind is blowing");
current direction in GRIB2 says only "degree true"; leaflet-velocity carries a
four-way `angleConvention` option for exactly this, and its maintainers name it
first when a user's map disagrees with another site.

**Time.** Valid time is reference time plus step, and steps are not uniform:
hourly then 3-hourly, 3-hourly then 6-hourly, and a horizon that depends on
which run of the day it is. Some wind-like fields are interval maxima or
averages, not instants, and for those GRIB's valid time is the *end* of the
interval. A "best" series stitched across runs changes run — and spacing —
partway along (Unidata's forecast-model-run collections make the two time axes
explicit). Ensemble members are separate messages on one grid.

## 7 For the deferred decoder decision

Recorded because the pass found it, not because ADR-0249 decides it.

| Product | Packing | How well established |
| --- | --- | --- |
| ECMWF open data | template 5.42, CCSDS/AEC | ECMWF's cycle notes: "all gridded GRIB2 data will be disseminated using a CCSDS defined compression method" |
| DWD ICON-EU lat-lon | 5.42, inside bzip2 | a third party's issue prose, not DWD. The same issue reports DWD retiring that lat-lon product for native icosahedral files, old URLs to go 2026-11-30 |
| Météo-France AROME / ARPEGE | 5.42 | a third-party developer's notes |
| HRRR | 5.3, complex with spatial differencing | one example field in a third-party tutorial |
| NDFD | 5.2 / 5.3 with in-band missing values | NWS design document |
| JMA GSM / MSM | 5.0 | a developer's article |
| GFS, NAM, RAP, MeteoSwiss, ECCC | — | **no provider statement found**; running `wgrib2 -packing` on one file each settles it in minutes |

MeteoSwiss ships ICON on the native icosahedral grid, without coordinates in
the files, keeps it 24 h, and needs consortium-specific definitions to decode.
The Met Office's open data, CMEMS and HYCOM are NetCDF.

Pure-Go GRIB2 decoders, by their own READMEs only: one (MIT) states a port of
the AEC decompressor and templates 5.0, 5.2, 5.3, 5.4, 5.41, 5.42, with
JPEG2000 only through cgo; one (MIT) states simple, complex, spatial
differencing, IEEE, JPEG2000, PNG and CCSDS with no cgo, cross-validated
against ecCodes; one (BSD-3) implements complex packing with spatial
differencing only and targets GFS; one (MIT) is a port of an old wgrib2 and
"does not support jpeg, png and aec". wgrib2's own guidance: complex packing
and AEC are "much faster" than JPEG2000 and PNG. A decoder must also loop over
repeated sections: NCEP once put U and V in one message.

## 8 Bugs others have already paid for

*Applies* is to a CPU-side layer with world-coordinate particles and
stored-position trails: **yes**, **no** (only accumulation-buffer or GPU-state
designs), or **source** (the data contract, not the layer).

| # | Symptom | Cause | Fix shipped | Applies |
| --- | --- | --- | --- | --- |
| 1 | All trails vanish on every pan or zoom | The accumulation buffer is screen-space: "completely wrong when panning/zooming, so you have to throw it out" (windgl) | windgl shipped without trails; proposed keeping n+1 position states and drawing segments between them, n bounded | no — it is the argument *for* stored trails |
| 2 | Holes in trails | Point drawing, particle moves more than a pixel per frame | Cap the speed, or draw segments | no |
| 3 | Freeze, jump and a ~1 s restart on every drag | Screen-space state and a per-view field rebuild; leaflet-velocity's maintainer: a "known rendering limitation" | none | no, provided nothing is rebuilt on the render thread per view |
| 4 | **Particles faster, longer and denser when zooming in** | Velocity constant in world units, count constant | Zoom-scaled speed; Xweather: "Improve particle density and speed consistency across zoom levels, especially fractional zoom levels", over several releases | **yes** |
| 5 | **Motion dies or trails go straight above zoom ~15** | "particle coordinates stored as fp32" (deck.gl-particle maintainer) | Workaround only: `maxZoom` 15 | **yes** unless positions are 64-bit |
| 6 | **Speed doubles on a 120 Hz display** | One step per rendered frame | Xweather: "Normalize particle properties to 60 FPS". cesium-wind-layer took six releases, ending by normalising *trail length* separately. Or a fixed update interval | **yes** |
| 7 | **Density differs by screen size; a higher setting shows nothing more** | Fixed count against a variable viewport | Count from viewport area — and four follow-up fixes (Xweather) | **yes** |
| 8 | Field degenerates to a few lines; divergent areas empty | Pure advection | Per-frame random reset | **yes** |
| 9 | Fast regions look denser | Fast particles cover more pixels | Reset probability rises with speed | **yes** |
| 10 | Particles circle an eddy forever | No upper age | Soft age threshold plus hard maximum (exif-layer) | **yes** |
| 11 | The whole field pulses | Cohorts born together die together | Random initial age (Jobard 2012); a random phase per line (Esri) | **yes** |
| 12 | **Flow direction wrong** | Vector not transformed with the raster; v not flipped for screen-down y; row order | ArcGIS 4.23; Xweather: "Fix flipped wind particle y-axis direction"; wind-layer's `flipY` | **yes** |
| 13 | Arrows reversed | Direction *from* read as direction *to* | Esri `flowRepresentation`; leaflet-velocity `angleConvention` | source |
| 14 | **Slow regions look frozen; trails are dots** | Displacement and trail length both proportional to magnitude | Esri: raise speed and trail length, stop integrating below a minimum; cesium-wind-layer: clamp length to a range, "base pixel size offset" | **yes** |
| 15 | Particles leak past a regional field's edge | Clamp-to-edge sampling extends edge values | WeatherLayers: "drop particles out of bounds, to support regional vector data" | **yes** |
| 16 | A regional field is duplicated, or drawn outside its extent | Wrap switched on for non-global data | Maintainer: never — wrap belongs to the field | source |
| 17 | Half a field, or a field over itself, at the antimeridian | 0–360 data, or a window crossing ±180 | Unresolved in wind-layer; WeatherLayers: repeat for global data, clamp for regional | source |
| 18 | Bunching and rushing near the poles | Lon/lat particle state: a degree of longitude shrinks with cos φ | Agafonkin divides by cos φ; WeatherLayers: "Slow down particles in higher latitudes… generate more particles in higher latitudes" | no in Mercator world units *(inference)* |
| 19 | Blocky flow | Nearest or hardware-linear sampling of a coarse grid | Hand-written bilinear; cubic by default (WeatherLayers) | **yes** |
| 20 | The integrator is "destabilised" | High-frequency content in a full-resolution field | Sample a field no finer than ~5 px; optional blur (Esri) | **yes** — the pyramid's level choice does this |
| 21 | Particles skip flow detail | Step longer than a cell | Step under a cell (exif-layer; Esri's fixed one-cell segment) | **yes** |
| 22 | Missing values drawn, or bleeding into neighbours | NaN not detected; sentinels interpolated as numbers | WeatherLayers: "Fix detecting NaN in Float data"; sources: everything in §6 | **yes** and source |
| 23 | The field jumps at each time step | No interpolation | Two images and a weight; re-seed only half on a swap (exif-layer) | **yes** |
| 24 | Transient artefacts after new data | Download, re-interpolation and new animator land separately (earth) | none documented | **yes** — swap atomically |
| 25 | Heap grows past a gigabyte during playback | Decoded steps cached per entry, not per byte | none (issue open) | **yes** |
| 26 | A slow reply for an old view replaces a newer one; an aborted load is cached | No request identity | Abort signal; never return partial data (deck.gl) | **yes** |
| 27 | Flicker between two levels; refetch on every frame of a zoom | One threshold; request per view change | Hysteresis; debounce; cancel superseded requests | **yes** |
| 28 | Crash when a layer is removed mid-update | Asynchronous work outlives the layer (Xweather) | Fixed in a release | **yes** — the sampler goroutine's lifetime |
| 29 | The basemap flickers while the layer animates | The animation loop's redraws fight the host map's | Fixed three times in WeatherLayers, once in Mapbox | **yes** — the layer's repaint requests beside the map's |
| 30 | Colour ramp changes after an upgrade | Ramp interpolated in premultiplied space | Mapbox moved to non-premultiplied | **yes**, when building the palette lookup |
| 31 | Direction smoothly wrong away from a central meridian | Grid-relative components never rotated | §6 | source |
| 32 | A cyclone reads as a source | Forward Euler on a closed orbit | Midpoint RK2 (literature; no renderer documents one) | **yes**, growing with trail lifetime |
| 33 | Cross-polar flow vanishes at coarse levels | Components averaged across a rotating basis | none documented | **yes**, above ~80° *(derivation)* |

Not found in public prose: pausing on tab or window visibility; saturation
where many trails overlap; how `raster-particle` treats tile boundaries.

## 9 Against ADR-0249

**Confirmed.** Stored trails in world coordinates are what the accumulation
lineage's own maintainers proposed as the fix (row 1) and what WeatherLayers
ships. A box mean of components is the documented practice (§5). Coarsest
sufficient level, last good window held, linear blend of two steps: all
common. Stylised speed is universal. Sampling lat/lon at render time, not
pre-warping to Mercator, is where the field is heading. Alpha falling with age
is a readable direction cue in a still frame (§4.1).

**Changed by this survey.** Integration from an unstated single step to
midpoint RK2 with the step bounded in cells (row 32, §4.3). A fixed simulation
tick, with trail history sampled on it, not a step per frame (row 6). A
stated speed-against-zoom rule (row 4) and a count from viewport area (row 7).
Lifetime as random initial age, a per-tick reset probability rising with speed,
and a hard maximum (rows 8–11). A floor on trail length and a kill below a
minimum displacement (row 14). 64-bit world positions, said out loud (row 5).

**Added to the contract.** A gutter. Node-registered bounds. Periodicity as a
property of the source. A valid-count plane and a valid-fraction threshold. The
vector-mean / scalar-mean names, and a plain statement that the scalar-mean
plane has no precedent found. Per-step time that can say instant or interval,
and a missing step distinct from an empty one. Request identity, a byte budget,
hysteresis and debounce. A list of what a source must normalise, and the tests
that pin it (§6, and the ADR's verification plan).

**Left open.** Pole rows in the pyramid (row 33) — recorded as a deferral.
Anything better than linear blending in time (§4.5). Per-segment width for a
wider head (§4.1), which the opcode's shape would have to allow. Cubic
sampling (row 19). An occupancy grid (§4.2). Everything in §7.

## 10 Sources

Pages the passes fetched and relied on, by area. Live URLs that refused the
fetch and were read from a web archive are marked *(archive)*.

**Renderers.** Agafonkin, "How I built a wind map with WebGL", blog.mapbox.com
*(archive)* · docs.mapbox.com/style-spec/reference/layers/ ·
docs.mapbox.com/mapbox-gl-js/example/raster-particle-layer/ · mapbox-gl-js
release notes · github.com/mapbox/webgl-wind README and issues 9, 12 ·
developers.arcgis.com/javascript/latest/references/core/renderers/FlowRenderer/ ·
esri.com/arcgis-blog, "Visualize and animate flow in MapView with a custom
WebGL layer" and "Create an animated flow visualization" *(archive)* ·
community.esri.com, "Streamlines and flow animation in the ArcGIS API" ·
ArcGIS JS API release notes 4.22–4.24 *(archive)* · earth.nullschool.net/about ·
github.com/cambecc/earth README and issues 5, 47, 68, 71 ·
news.nullschool.net, "Hurricane wind speeds: understanding the effect of model
grids" · hint.fm/wind, hint.fm/projects/wind ·
docs.weatherlayers.com/weatherlayers-gl (particle layer, data properties, data
sources, changelog) · github.com/weatherlayers/deck.gl-particle README and
issues 3, 5, 10 · xweather.com/docs/mapsgl (styles reference, changelog,
custom wind particles example) · github.com/onaci/leaflet-velocity README,
issues 1, 48, 51, 53, 117, discussion 80 · github.com/sakitam-fdd/wind-layer
README, blog.sakitam.com/wind-layer/guide/, issues 165, 237, 250, 255, 266,
267 (maintainer statements only) · github.com/astrosat/windgl README and
issues 1, 2, 4, 12, 20, 37, 58 · github.com/zwang-geog/mapbox-exif-layer
README · github.com/gberaudo/olwind README ·
github.com/maplibre/maplibre-gl-js discussion 5991 ·
github.com/openlayers/openlayers discussion 16196, issue 16432 ·
cesium.com/blog/2019/04/29/gpu-powered-wind/ ·
github.com/RaymanNg/3D-Wind-Field README ·
github.com/hongfaqiu/cesium-wind-layer README and release notes ·
community.windy.com topics 3954, 4033, 7759, 15057.

**Literature.** Laidlaw, Kirby, Jackson, Davidson, Miller, da Silva, Warren,
Tarr, "Comparing 2D Vector Field Visualization Methods: A User Study", IEEE
TVCG 11(1), 2005 (full text) · Fowler, Ware, "Strokes for Representing
Univariate Vector Field Maps", Graphics Interface '89 (full text) · Mitchell,
Ware, Kelley, "Designing Flow Visualizations for Oceanography and Meteorology
using Interactive Design Space Hill Climbing", IEEE SMC 2009 (full text) ·
Ware, "Toward a Perceptual Theory of Flow Visualization", IEEE CG&A 28(2),
2008 (abstract) · Pilar, Ware, "Representing Flow Patterns by Using
Streamlines with Glyphs", IEEE TVCG 19(8), 2013 (abstract) · Ware, Kelley,
Pilar, "Improving the Display of Wind Patterns and Ocean Currents", BAMS
95(10), 2014 (abstract) · Ware, Bolan, Miller, Rogers, Ahrens, "Animated
versus static views of steady flow patterns", ACM SAP '16 (abstract) · Jobard,
Erlebacher, Hussaini, "Lagrangian-Eulerian Advection of Noise and Dye Textures
for Unsteady Flow Visualization", IEEE TVCG 8(3), 2002 (full text) · Jobard,
Ray, Sokolov, "Visualizing 2D Flows with Animated Arrow Plots",
arXiv:1205.5204 (full text) · Mebarki, Alliez, Devillers, "Farthest Point
Seeding for Efficient Placement of Streamlines", IEEE Vis 2005 (full text) ·
van Wijk, "Image Based Flow Visualization", ACM TOG 21(3), 2002 (abstract) ·
Weiskopf, Erlebacher, "Overview of Flow Visualization", in *The Visualization
Handbook*, 2005 (full text) · Hauser, TU Wien visualisation course slides,
"Integration of Streamlines" · Zavala-Hidalgo, Bourassa, Morey, O'Brien, Yu,
"A new temporal interpolation method for high-frequency vector wind fields",
IEEE OCEANS 2003 (full text) · Snyder, *Map Projections — A Working Manual*,
USGS PP 1395 (excerpt) · EPA-454/R-99-005 §6.2 · ndbc.noaa.gov/faq/wndav.shtml ·
WMO-No. 8, 7th ed. · help.marine.copernicus.eu, "How to average winds?" ·
sodar.com/FYI/vector_vs_scalar.html · wind-barb conventions from a US Navy
training module and a University of Washington page. Seen only as search
summaries and cited as such: Kettunen & Oksanen 2019; Singer 1967; Stohl et
al. 1995; Gorman 2009; Turk & Banks 1996; Jobard & Lefer 1997, 2001; Teitzel
et al. 1997; Telea & van Wijk 1999; Heckel et al. 1999; Theisel et al. 2003;
Bauer & Peikert 2002.

**Storing and serving.** docs.mapbox.com/mapbox-tiling-service (raster-array
recipe specification, raster recipe details, wind example, supported formats) ·
docs.mapbox.com/api/maps/raster-arrays/ · docs.mapbox.com/style-spec/reference/sources/ ·
github.com/mapbox/mapbox-gl-js issue 13688 · carbonplan.org/blog
(maps-library-release, zarr-layer-maps) · github.com/carbonplan/ndpyramid
README, ndpyramid.readthedocs.io · nasa-impact.github.io/zarr-visualization-report ·
github.com/zarr-conventions/multiscales README · earthmover.io/blog
(multiscales-in-al, dynamic map tile rendering with xpublish-tiles) ·
gdal.org (gdaladdo, gdalwarp, warp options) ·
developmentseed.org/titiler/endpoints/cog/ · github.com/open-meteo (open-data,
om-file-format, weather-map-layer READMEs) · dynamical.org/research/virtual-data-products/ ·
xesmf.readthedocs.io, Masking notebook · Esri documentation: Resample
function, Vector Field function, drawing raster data using vector symbols,
managing multidimensional data · deck.gl TileLayer reference · leafletjs.com
reference · OpenLayers TileLayer apidoc · MapLibre `MapOptions` · a
level-of-detail hysteresis note in the DigitalRune engine documentation.

**Data.** NCEP GRIB2 documentation: flag tables 3.3 and 3.4, code tables
4.2-0-2, 4.2-10-1, 4.5, 4.10, template 4.8; ON388 table B ·
cpc.ncep.noaa.gov/products/wesley/wgrib2 (new_grid, new_grid_winds, grid,
wind_dir, wind_uv, set_grib_type, speed) and bilingrb ·
rapidrefresh.noaa.gov/faq/HRRR.faq.html · nco.ncep.noaa.gov product pages for
GFS, HRRR, RTOFS · graphical.weather.gov/docs/grib_design.html · ECMWF
Confluence: ERA5 spatial reference, the octahedral reduced Gaussian grid, IFS
cycle 48r1, open data, the GRIB bitmap FAQ, MARS interpolation with MIR,
differences for 10 m winds · forum.ecmwf.int, "wind rotation question" ·
dwd.de NWP forecast data page and the open-data CDO guideline (PDF) ·
opendatadocs.meteoswiss.ch, numerical weather forecasting model ·
eccc-msc.github.io/open-data (GDPS, HRDPS) · reference.metoffice.gov.uk UM
rotation code · hirlam.github.io HARMONIE post-processing ·
documentation.marine.copernicus.eu, global physics product manual ·
hycom.org FAQs · forum.mmm.ucar.edu, "are u, v wind components grid-relative" ·
wrf-python and xWRF documentation · nemo-ocean.discourse.group, "rotating
velocities in output" · ncl.ucar.edu talk archive 2008/1248 and
`wind_direction` · scitools-iris cartography API · MetPy `wind_components` and
issue 1047 · dtcenter/METplus discussion 3252 · code.mpimet.mpg.de CDO forum
topics 1283, 2323 · gdal.org GRIB driver · cfgrib README · pygrib API ·
herbie.readthedocs.io (GRIB2 background, wgrib2 wrapper, `with_wind`) ·
fsspec/kerchunk issue 407 · dynamical-org/reformatters issue 1007 ·
docs.unidata.ucar.edu, forecast model run collections · GMT file-formats
reference · PyGNOME NetCDF file formats · weacast/weacast-grib2json issue 12 ·
github.com/cambecc/grib2json README · a 2021 blog post on feeding
leaflet-velocity · meteolab.fr developer changelog · a 2005 NCEP slide deck on
GRIB2 conversion · zenn.dev article on JMA template 5.200 · pkg.go.dev pages
of four Go GRIB2 packages, two HDF5/NetCDF packages, and one Zarr README.

**Incidents the passes reported.** A blob-view URL of a prose changelog was
requested and returned no content. The *conversation* tab of one pull request
was fetched to find which change fixed the date-line issues and returned no
text. Search results surfaced a gist, several repository code views and C
files on an FTP server; none was opened. Issue-thread code blocks were
stripped by script before reading. One pass declined to report what it
remembered of two projects' internals because it judged that memory to come
from their source. Nothing from any of these is used above.
