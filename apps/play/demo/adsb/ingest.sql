-- Pull one day of a lat/lon bbox from ClickHouse's public adsb.exposed instance
-- straight into the local planes_mercator table — no file download, no parsing.
-- remoteSecure() reads the public read-only `website` user (no password); the
-- MATERIALIZED mercator_x/mercator_y recompute locally on INSERT, and the local
-- sample10/sample100 materialized views populate as a side effect.
--
-- Parameters (passed by demo.sh via clickhouse-client --param_*):
--   min_lat/max_lat/min_lon/max_lon : Float64  viewport bbox (WGS84)
--   day                             : Date     the single UTC day to load
--   hour                            : UInt8    the UTC hour (0-23); demo.sh runs
--                                              one INSERT per hour so each
--                                              transfer is small and retryable
--
-- The bbox is expressed in lat/lon and converted to the mercator UInt32 range
-- with the setup.sql formulas, so the WHERE prunes the remote's morton-indexed
-- mercator_x/mercator_y columns (cheap) rather than scanning lat/lon.
--
-- NOTE: SELECT * excludes the remote's MATERIALIZED mercator_x/y, so the column
-- list lines up with the local non-materialized columns positionally. If the
-- upstream schema ever drifts, name the columns explicitly instead.

INSERT INTO planes_mercator
WITH
    {min_lat:Float64} AS min_lat, {max_lat:Float64} AS max_lat,
    {min_lon:Float64} AS min_lon, {max_lon:Float64} AS max_lon,
    toUInt32(0xFFFFFFFF * ((min_lon + 180) / 360))                                 AS min_x,
    toUInt32(0xFFFFFFFF * ((max_lon + 180) / 360))                                 AS max_x,
    toUInt32(0xFFFFFFFF * (1/2 - log(tan((max_lat + 90) / 360 * pi())) / 2 / pi())) AS min_y, -- north edge -> smaller y
    toUInt32(0xFFFFFFFF * (1/2 - log(tan((min_lat + 90) / 360 * pi())) / 2 / pi())) AS max_y  -- south edge -> larger y
SELECT *
FROM remoteSecure('kvzqttvc2n.eu-west-1.aws.clickhouse-staging.com:9440', default.planes_mercator, 'website', '')
WHERE mercator_x BETWEEN min_x AND max_x
  AND mercator_y BETWEEN min_y AND max_y
  AND date = {day:Date}
  AND toHour(time) = {hour:UInt8};
