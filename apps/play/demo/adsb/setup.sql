-- ADS-B demo schema for the play Map panel (ADR-0096).
--
-- Adopted from ClickHouse's adsb.exposed demo (its setup.sql):
--   https://github.com/ClickHouse/adsb.exposed
-- Only the planes tables are kept; the upstream `saved_queries` and `stats`
-- tables serve that project's own web UI, not the in-DB raster this demo loads.
--
-- mercator_x / mercator_y are MATERIALIZED from lat/lon with the same formulas
-- the Map panel mirrors in Go (apps/play/play_map.go lonToMercX / latToMercY,
-- ADR-0096 §SD4), so they recompute locally on INSERT and line up exactly with
-- the raster binning. The morton ORDER BY + minmax indexes make a viewport a
-- near-contiguous key range, so the bbox filter in ingest.sql prunes cheaply.
--
-- UPGRADING an existing database: the statements below are CREATE TABLE IF NOT
-- EXISTS, so re-running this file against a database that already has these
-- tables leaves the OLD column expressions in place — the projection changed on
-- 2026-07-28 (see below) and a plain re-run will not pick it up. Drop the three
-- tables and their materialized views and re-run, or ALTER TABLE ... MODIFY
-- COLUMN and re-ingest. Rows already written keep whatever their part was
-- written with; MATERIALIZED expressions are evaluated at INSERT, not on read.
--
-- The world span is 2^32, ClickHouse's own full-UInt32 mercator space (the one
-- MVTEncodeGeom / MVTBoundingBoxMercator use), NOT upstream adsb.exposed's
-- 0xFFFFFFFF — see the ADR-0096 2026-07-28 Update. The two differ by at most one
-- mercator unit, so ingest.sql's bbox against the REMOTE's columns deliberately
-- keeps the upstream constant; only these local columns moved.
--
-- The greatest/least clamp is load-bearing, not decoration: the scale is the
-- world SPAN, so lon = +180 projects exactly onto 2^32 and toUInt32 wraps that
-- to 0 — an unclamped antimeridian point would land at the far left of the
-- world. It also pins the poles, where tan(lat) diverges.
--
-- Three spellings here are load-bearing for exact agreement with the Go side;
-- verified 0 differing points over a 245861-point grid. Do not "tidy" them:
--   * floor(x + 0.5), not the bare cast — the implicit UInt32 cast TRUNCATES
--     where Go's math.Round rounds, and ClickHouse's round() is banker's
--     rounding, which Go's is not.
--   * ... * (lon + 180) / 360, not ... * ((lon + 180) / 360) — Go multiplies
--     before dividing, and the association changes the last bit.
--   * asinh(tan(lat/180*pi())), not log(tan((lat+90)/360*pi())) — the same
--     isometric latitude, but ClickHouse's log() is a fast approximation
--     carrying ~1.4e-9 relative error, which 2^32 magnifies into a whole
--     mercator unit on ~36% of points. Its asinh() is correctly rounded.
--
-- Idempotent: safe to re-run (demo.sh TRUNCATEs before each load).

CREATE TABLE IF NOT EXISTS planes_mercator
(
    mercator_x UInt32 MATERIALIZED greatest(0., least(4294967295., floor(4294967296. * (lon + 180) / 360 + 0.5))),
    mercator_y UInt32 MATERIALIZED greatest(0., least(4294967295., floor(4294967296. * (0.5 - asinh(tan(lat / 180 * pi())) / (2 * pi())) + 0.5))),

    INDEX idx_x (mercator_x) TYPE minmax,
    INDEX idx_y (mercator_y) TYPE minmax,

    time DateTime64(3),
    date Date,
    icao String,
    r String,
    t LowCardinality(String),
    dbFlags Int32,
    noRegData Bool,
    ownOp LowCardinality(String),
    year UInt16,
    desc LowCardinality(String),
    lat Float64,
    lon Float64,
    altitude Int32,
    ground_speed Float32,
    track_degrees Float32,
    flags UInt32,
    vertical_rate Int32,
    aircraft_alert Int64,
    aircraft_alt_geom Int64,
    aircraft_gva Int64,
    aircraft_nac_p Int64,
    aircraft_nac_v Int64,
    aircraft_nic Int64,
    aircraft_nic_baro Int64,
    aircraft_rc Int64,
    aircraft_sda Int64,
    aircraft_sil Int64,
    aircraft_sil_type LowCardinality(String),
    aircraft_spi Int64,
    aircraft_track Float64,
    aircraft_type LowCardinality(String),
    aircraft_version Int64,
    aircraft_category Enum8(
        'A0', 'A1', 'A2', 'A3', 'A4', 'A5', 'A6', 'A7',
        'B0', 'B1', 'B2', 'B3', 'B4', 'B5', 'B6', 'B7',
        'C0', 'C1', 'C2', 'C3', 'C4', 'C5', 'C6', 'C7',
        'D0', 'D1', 'D2', 'D3', 'D4', 'D5', 'D6', 'D7',
        ''),
    aircraft_emergency Enum8('', 'none', 'general', 'downed', 'lifeguard', 'minfuel', 'nordo', 'unlawful', 'reserved'),
    aircraft_flight LowCardinality(String),
    aircraft_squawk String,
    aircraft_baro_rate Int64,
    aircraft_nav_altitude_fms Int64,
    aircraft_nav_altitude_mcp Int64,
    aircraft_nav_modes Array(Enum8('althold', 'approach', 'autopilot', 'lnav', 'tcas', 'vnav')),
    aircraft_nav_qnh Float64,
    aircraft_geom_rate Int64,
    aircraft_ias Int64,
    aircraft_mach Float64,
    aircraft_mag_heading Float64,
    aircraft_oat Int64,
    aircraft_roll Float64,
    aircraft_tas Int64,
    aircraft_tat Int64,
    aircraft_true_heading Float64,
    aircraft_wd Int64,
    aircraft_ws Int64,
    aircraft_track_rate Float64,
    aircraft_nav_heading Float64,
    source LowCardinality(String),
    geometric_altitude Int32,
    geometric_vertical_rate Int32,
    indicated_airspeed Int32,
    roll_angle Float32,

    data_source Enum8('', 'adsb.lol', 'adsbexchange', 'airplanes.live')
) ENGINE = MergeTree ORDER BY (mortonEncode(mercator_x, mercator_y), time);

-- The 1/10 and 1/100 sampled tables the upstream uses for progressive loading.
-- For a one-city one-day slice they are optional (the Map panel can read the full
-- local table), but keeping them mirrors upstream and lets the panel exercise the
-- sampling ladder (ADR-0096 §SD10) unchanged.
CREATE TABLE IF NOT EXISTS planes_mercator_sample10  AS planes_mercator;
CREATE TABLE IF NOT EXISTS planes_mercator_sample100 AS planes_mercator;

CREATE MATERIALIZED VIEW IF NOT EXISTS view_sample10  TO planes_mercator_sample10  AS SELECT * FROM planes_mercator WHERE rand() % 10  = 0;
CREATE MATERIALIZED VIEW IF NOT EXISTS view_sample100 TO planes_mercator_sample100 AS SELECT * FROM planes_mercator WHERE rand() % 100 = 0;
