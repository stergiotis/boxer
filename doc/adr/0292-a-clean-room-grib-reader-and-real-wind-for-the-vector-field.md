---
type: adr
status: proposed
date: 2026-10-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0292: a clean-room GRIB reader in boxer, and real wind for the vector field

## Context

[ADR-0249](./0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md)
drew vector fields on the map and deferred their file format to a decision of
its own: GRIB2 is what numerical weather models publish in, artifacts build
with `CGO_ENABLED=0` ([ADR-0215](./0215-retire-mimalloc-reproducible-builds.md)),
which rules out the reference C decoders, and which of GRIB's packing templates
a pure-Go reader must cover depends on whose files it opens. Until now a field
has been filled from whatever a caller could already read into two `float32`
planes, and the tour's storm is synthetic.

[ADR-0291](./0291-a-precomputed-vector-field-family-of-keelson-tables.md) makes
a field readable through SQL in a browser tab with no ClickHouse, which is what
a demo a reader can open needs. What it lacks is real data.

A reader exists. It was written by this project's owner in another
repository, under an accepted decision, and is now to be maintained here. Its
properties, measured before this decision was proposed:

- It reads GRIB editions 1 and 2 without cgo and without any third-party
  codec: the complex packings, the CCSDS adaptive entropy coder and the
  JPEG 2000 profile producers emit are written from the standards (the WMO
  Manual on Codes, CCSDS 121.0-B-3, ITU-T T.800). It descends from no existing
  GRIB library; ecCodes served as an oracle for expected values, run outside
  the code.
- About 6,600 lines of Go, 1,700 of tests, and WMO code tables of about 10,000
  lines of TSV. Its dependencies outside the standard library are boxer itself
  and `urfave/cli`, already in boxer's module graph.
- It compiles for `wasip1` and `js`, and its 99 tests pass under `wasip1` in
  Node's WASI runtime as natively — so it runs in a tab.

## Decision

We will import the reader into boxer as `public/science/geo/grib`, with the
decisions it was built under restated here, carry only the test fixtures whose
licences are clear, add a small adapter from GRIB wind to the vector field's
planes, and commit one GFS wind forecast as the demo's data.

### Subsidiary design decisions

- **SD1 — The reader and its rules.** `grib` and its sub-packages `aec`,
  `jpeg2000` and `tables` move under `public/science/geo/grib`. They keep the
  rules they were built under, now recorded here:
  - *A named subset.* The grid, product and packing templates real producers
    write are implemented; every other template, flag value or codec feature
    is refused with `ErrUnsupported` naming it. Adding one is a decision
    recorded in this ADR's Updates, with its fixture.
  - *Stored order.* Values come back in the order the message stores them,
    missing points as NaN, with the scan flags exposed; a raster view applies
    them once, on request. Nothing is reordered silently.
  - *Refuse, never guess, never panic.* Malformed bytes fail with
    `ErrMalformed`, self-contradicting sections with `ErrInconsistent`; a
    property test holds that no input panics.
  - *No encoder, no regridding, no names beyond the WMO tables.*

  Package documentation cites this ADR for those rules.

- **SD2 — Fixtures whose licences are clear.** The producer messages that come
  along are NOAA's (US Government works) and ecCodes' public test files
  (Apache-2.0, with the notice the licence asks for); the JPEG 2000 codestreams
  were synthesised for the reader with OpenJPEG and come along too. Messages
  from DWD and MeteoSwiss (CC BY 4.0) and ECCC (Open Government Licence –
  Canada) stay out, and the tests that read them are removed. Their loss is
  one of coverage: they were the only producer files using CCSDS packing
  (template 5.42), which the `aec` package's unit tests still cover on
  synthesised data, but no longer end to end. Expected values were produced
  with ecCodes by a script kept beside the fixtures as text; the oracle stays
  out of the test lane.

- **SD3 — Tables and the command line.** The WMO code tables (MIT licence, from
  `wmo-im/GRIB2` and `wmo-im/CCT`) are generated into `tables` and embedded,
  with their origin and notice recorded beside them; the generator and the
  reader's command (listing a file's fields as an index, dumping a field)
  join boxer's CLI.

- **SD4 — From GRIB to a field.** A small package beside the vector field turns
  pairs of eastward and northward wind messages into `vectorfield.Grid` planes
  and valid times, the input `keelsonfield` takes
  ([ADR-0291](./0291-a-precomputed-vector-field-family-of-keelson-tables.md)
  §SD3). It pairs u and v by reference time, forecast offset and level,
  requires a regular latitude/longitude grid whose components are
  earth-relative, applies the scan flags through the reader's raster view,
  and refuses anything else by name: grid-relative winds would need a rotation
  this decision does not build.

- **SD5 — One real forecast, committed.** The demo's data is NOAA GFS at 1.0°:
  10 m wind (`UGRD` and `VGRD` at 10 m above ground) from one run, 17 steps
  at 0 to 48 hours every 3 hours. The messages are cut by byte range from the
  open-data bucket using NOAA's `.idx` sidecar, so only the 34 needed are
  fetched; at this resolution they total a few megabytes, which is committed,
  with a sources file naming the run, the URLs, the fetch date and the
  licence. A script repeats the fetch for another run. A US Government work
  carries no attribution terms, so the demo shows it without them. The file is
  committed rather than fetched at build time, so a build stays offline
  ([ADR-0095](./0095-airgapped-build-bundle.md)).

### Deferred and open

- **Q1 — Producer fixtures with attribution terms.** DWD's and MeteoSwiss's
  files would restore end-to-end CCSDS coverage; carrying them means meeting
  CC BY 4.0's attribution in the tree. Left until someone wants it.
- **Q2 — Grid-relative components.** Rotating winds on a rotated or projected
  grid to earth-relative ones, which regional models need.
- **Q3 — Finer data.** GFS at 0.25° is sixteen times the bytes per step; a
  demo of it would fetch rather than commit, which reopens the offline build.

### Milestones

- **M1 — The reader.** SD1–SD3: the packages under `public/science/geo/grib`,
  the fixture subset and its notices, the tables and the command; its tests
  pass natively and under `wasip1`.
- **M2 — Real wind.** SD4 and SD5: the adapter, the committed GFS forecast and
  its fetch script, and the forecast served as a keelson field family.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `public/science/geo/grib` and sub-packages | added: the reader, from another repository of the same owner | package documentation cites this ADR |
| Fixtures | added: NOAA and ecCodes messages, synthesised JPEG 2000 codestreams | notices for Apache-2.0 and MIT material |
| boxer CLI | added: the reader's index and dump commands, the table generator | — |
| GRIB-to-planes adapter | added | the GFS demo data and its fetch script |

## Alternatives

- **A third-party pure-Go GRIB library.** Rejected: the survey behind the
  reader found the existing Go readers misread the cases that matter — complex
  packing with missing values, signed octets, scan flags — silently, and a
  silent misread of wind is worse than a refusal.
- **ecCodes through cgo.** Rejected: artifacts build without cgo (ADR-0215),
  and a tab cannot load a C library.
- **Keep the reader outside and depend on it.** Rejected: that repository
  depends on boxer, so a dependency back would be a cycle; and a module boxer
  cannot see would hold code its demo relies on.
- **All fixtures, with their attributions.** Not taken for now (Q1): the
  coverage gained is CCSDS end to end, and the cost is attribution text for
  data that serves only tests.

## Consequences

### Positive

- Boxer reads the format weather data comes in, without cgo, in every build
  target including a browser tab.
- The vector field demo shows a real forecast.

### Negative

- About 6,600 lines of decoder and 10,000 lines of tables to maintain.
- CCSDS packing is covered by unit tests only, not by a producer's file, until
  Q1 is taken up.
- The committed forecast is a snapshot; it ages as a picture, not as code.

### Neutral

- The reader serves no runtime path of boxer other than the ones that choose
  to read GRIB.

## Migration — Tier 1

None. Nothing in boxer reads GRIB today.

## Verification plan — Tier 1

- M1: the imported tests pass natively and under `wasip1`; the property test
  holds that no mutated fixture panics.
- M2: the adapter reads the committed forecast into 17 steps on a 360 × 181
  periodic grid, with spot values equal to ecCodes'; the family built from it
  passes ADR-0291's parity test.

## Status

Proposed 2026-10-07.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## References

- [ADR-0095](./0095-airgapped-build-bundle.md) — the offline build.
- [ADR-0215](./0215-retire-mimalloc-reproducible-builds.md) — builds without cgo.
- [ADR-0249](./0249-vector-fields-on-the-map-particles-over-a-batched-segment-opcode.md) — vector fields; GRIB deferred.
- [ADR-0291](./0291-a-precomputed-vector-field-family-of-keelson-tables.md) — a field as keelson tables.
- WMO Manual on Codes, Volume I.2 (GRIB); CCSDS 121.0-B-3; ITU-T T.800.
