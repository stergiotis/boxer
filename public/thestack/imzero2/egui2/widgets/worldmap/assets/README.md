# Natural Earth 110m admin-0 countries (vendored)

`ne_110m_admin_0_countries.geojson.gz` is the **verbatim** upstream file,
gzipped (`gzip -9 -n`, no timestamp) — no properties stripped, no geometry
re-encoded — so it stays checksummable against upstream (ADR-0114 §SD1).

- Source: <https://github.com/nvkelso/natural-earth-vector>,
  path `geojson/ne_110m_admin_0_countries.geojson`
- Upstream commit: `9380cca83db5f9aef52d5e762765100745f84b27` (2022-05-13)
- sha256 (uncompressed): `6866c877d39cba9c357620878839b336d569f8c662d3cfab4cb1dbe2d39c977f`
- License: Natural Earth is in the **public domain**
  (<https://www.naturalearthdata.com/about/terms-of-use/>)

To verify: `gunzip -c ne_110m_admin_0_countries.geojson.gz | sha256sum`.

The widget consumes only `ADMIN`, `NAME`, `ISO_A2_EH`, `ISO_A3_EH` and the
geometry; everything else rides along for auditability. Known upstream
quirks (handled in `countries.go`): plain `ISO_A2`/`ISO_A3` are `-99` for
France, Norway and Kosovo (the `_EH` variants fix France/Norway fully and
Kosovo's alpha-2 = `XK`); Northern Cyprus and Somaliland carry no ISO codes
at all and resolve by name only.
