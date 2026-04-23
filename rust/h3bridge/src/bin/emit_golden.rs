//! Emits authoritative golden NDJSON vectors for every H3 bulk operation.
//!
//! These vectors are consumed by the Go test suite under
//! `public/science/geo/h3/testdata/`. The Rust side is the source of truth:
//! the Go bridge must agree with whatever this binary emits. If the Go
//! bridge disagrees, the Go bridge is wrong.
//!
//! Output paths are resolved relative to `CARGO_MANIFEST_DIR` so the binary
//! can be invoked from anywhere.

use std::fs::{self, File};
use std::io::{BufWriter, Write};
use std::path::{Path, PathBuf};

use h3o::{
    geom::{ContainmentMode, TilerBuilder},
    CellIndex, LatLng, Resolution,
};

fn testdata_dir() -> PathBuf {
    let manifest = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    manifest
        .parent()
        .and_then(Path::parent)
        .expect("unexpected CARGO_MANIFEST_DIR layout")
        .join("public/science/geo/h3/testdata")
}

fn open(name: &str) -> BufWriter<File> {
    let dir = testdata_dir();
    fs::create_dir_all(&dir).expect("create testdata dir");
    let p = dir.join(name);
    BufWriter::new(File::create(&p).unwrap_or_else(|e| panic!("create {}: {e}", p.display())))
}

// Well-known reference points. Kept deliberately small so the NDJSON files
// stay reviewable as text. Extend here (not in Go) when new edge cases
// surface.
fn reference_points() -> Vec<(&'static str, f64, f64)> {
    vec![
        ("null_island", 0.0, 0.0),
        ("san_francisco", 37.7749, -122.4194),
        ("paris", 48.8566, 2.3522),
        ("sydney", -33.8688, 151.2093),
        ("north_pole", 89.9, 0.0),
        ("south_pole", -89.9, 0.0),
        ("antimeridian_east", 0.0, 179.99),
        ("antimeridian_west", 0.0, -179.99),
    ]
}

fn emit_latlng_to_cell() {
    let mut w = open("golden_latlng_to_cell.ndjson");
    for (name, lat, lng) in reference_points() {
        for r in 0u8..=15 {
            let res = Resolution::try_from(r).expect("resolution");
            let ll = LatLng::new(lat, lng).expect("valid latlng");
            let cell = ll.to_cell(res);
            writeln!(
                w,
                r#"{{"name":"{name}","lat":{lat},"lng":{lng},"res":{r},"cell":{}}}"#,
                u64::from(cell)
            )
            .unwrap();
        }
    }
}

fn emit_cell_to_latlng() {
    let mut w = open("golden_cell_to_latlng.ndjson");
    for (name, lat, lng) in reference_points() {
        for r in 0u8..=15 {
            let res = Resolution::try_from(r).expect("resolution");
            let ll = LatLng::new(lat, lng).expect("valid latlng");
            let cell = ll.to_cell(res);
            let center = LatLng::from(cell);
            writeln!(
                w,
                r#"{{"name":"{name}","cell":{},"res":{r},"center_lat":{},"center_lng":{}}}"#,
                u64::from(cell),
                center.lat(),
                center.lng(),
            )
            .unwrap();
        }
    }
}

fn emit_parents() {
    let mut w = open("golden_parents.ndjson");
    for (name, lat, lng) in reference_points() {
        let ll = LatLng::new(lat, lng).expect("valid latlng");
        let leaf = ll.to_cell(Resolution::Ten);
        for r in 0u8..=10 {
            let res = Resolution::try_from(r).expect("resolution");
            let parent = leaf.parent(res).expect("parent");
            writeln!(
                w,
                r#"{{"name":"{name}","cell":{},"res":{r},"parent":{}}}"#,
                u64::from(leaf),
                u64::from(parent),
            )
            .unwrap();
        }
    }
}

fn emit_children() {
    let mut w = open("golden_children.ndjson");
    for (name, lat, lng) in reference_points() {
        let ll = LatLng::new(lat, lng).expect("valid latlng");
        // A resolution-3 cell with its resolution-4 children keeps the
        // vector small and human-reviewable.
        let parent = ll.to_cell(Resolution::Three);
        let children: Vec<u64> = parent
            .children(Resolution::Four)
            .map(u64::from)
            .collect();
        let children_str = children
            .iter()
            .map(|c| c.to_string())
            .collect::<Vec<_>>()
            .join(",");
        writeln!(
            w,
            r#"{{"name":"{name}","cell":{},"child_res":4,"children":[{children_str}]}}"#,
            u64::from(parent),
        )
        .unwrap();
    }
}

fn emit_grid_disk() {
    let mut w = open("golden_grid_disk.ndjson");
    for (name, lat, lng) in reference_points() {
        let ll = LatLng::new(lat, lng).expect("valid latlng");
        let cell = ll.to_cell(Resolution::Five);
        for k in [0u32, 1, 2, 3] {
            let neighbours: Vec<u64> = cell
                .grid_disk::<Vec<CellIndex>>(k)
                .into_iter()
                .map(u64::from)
                .collect();
            let ns = neighbours
                .iter()
                .map(|c| c.to_string())
                .collect::<Vec<_>>()
                .join(",");
            writeln!(
                w,
                r#"{{"name":"{name}","cell":{},"k":{k},"neighbours":[{ns}]}}"#,
                u64::from(cell),
            )
            .unwrap();
        }
    }
}

fn emit_strings() {
    let mut w = open("golden_strings.ndjson");
    for (name, lat, lng) in reference_points() {
        for r in [0u8, 5, 10, 15] {
            let res = Resolution::try_from(r).expect("resolution");
            let ll = LatLng::new(lat, lng).expect("valid latlng");
            let cell = ll.to_cell(res);
            let s = format!("{cell}");
            writeln!(
                w,
                r#"{{"name":"{name}","cell":{},"res":{r},"string":"{s}"}}"#,
                u64::from(cell),
            )
            .unwrap();
        }
    }
}

fn emit_boundaries() {
    let mut w = open("golden_boundaries.ndjson");
    for (name, lat, lng) in reference_points() {
        for r in [0u8, 5, 10] {
            let res = Resolution::try_from(r).expect("resolution");
            let ll = LatLng::new(lat, lng).expect("valid latlng");
            let cell = ll.to_cell(res);
            let boundary = cell.boundary();
            let lats_str = boundary
                .iter()
                .map(|v| v.lat().to_string())
                .collect::<Vec<_>>()
                .join(",");
            let lngs_str = boundary
                .iter()
                .map(|v| v.lng().to_string())
                .collect::<Vec<_>>()
                .join(",");
            writeln!(
                w,
                r#"{{"name":"{name}","cell":{},"res":{r},"vertex_count":{},"lats":[{lats_str}],"lngs":[{lngs_str}]}}"#,
                u64::from(cell),
                boundary.len(),
            )
            .unwrap();
        }
    }
}

fn emit_validate() {
    let mut w = open("golden_validate.ndjson");
    // Known-valid cells.
    for (name, lat, lng) in reference_points() {
        for r in [0u8, 5, 10, 15] {
            let res = Resolution::try_from(r).expect("resolution");
            let ll = LatLng::new(lat, lng).expect("valid latlng");
            let cell = ll.to_cell(res);
            writeln!(
                w,
                r#"{{"name":"{name}","cell":{},"res":{r},"valid":true}}"#,
                u64::from(cell),
            )
            .unwrap();
        }
    }
    // Known-invalid cells: zero, all-ones, and a random non-H3 bit pattern.
    for (label, bad) in [
        ("zero", 0u64),
        ("all_ones", u64::MAX),
        ("non_h3_pattern", 0xdead_beef_cafe_babe_u64),
    ] {
        writeln!(
            w,
            r#"{{"name":"{label}","cell":{bad},"res":0,"valid":false}}"#,
        )
        .unwrap();
    }
}

// Reference polygons, each given as (name, rings) where the first ring is
// the exterior and subsequent rings are holes. Coords are (lng, lat).
fn reference_polygons() -> Vec<(&'static str, Vec<Vec<(f64, f64)>>)> {
    vec![
        (
            "unit_square",
            vec![vec![
                (0.0, 0.0),
                (1.0, 0.0),
                (1.0, 1.0),
                (0.0, 1.0),
                (0.0, 0.0),
            ]],
        ),
        (
            "square_with_hole",
            vec![
                vec![
                    (-2.0, -2.0),
                    (2.0, -2.0),
                    (2.0, 2.0),
                    (-2.0, 2.0),
                    (-2.0, -2.0),
                ],
                vec![
                    (-1.0, -1.0),
                    (1.0, -1.0),
                    (1.0, 1.0),
                    (-1.0, 1.0),
                    (-1.0, -1.0),
                ],
            ],
        ),
        (
            "triangle_sf",
            vec![vec![
                (-122.5, 37.7),
                (-122.3, 37.7),
                (-122.4, 37.8),
                (-122.5, 37.7),
            ]],
        ),
    ]
}

fn emit_polyfill() {
    let mut w = open("golden_polyfill.ndjson");
    let modes: [(u8, ContainmentMode); 4] = [
        (0, ContainmentMode::ContainsCentroid),
        (1, ContainmentMode::ContainsBoundary),
        (2, ContainmentMode::IntersectsBoundary),
        (3, ContainmentMode::Covers),
    ];
    for (name, rings) in reference_polygons() {
        // Flatten rings into parallel lat/lng slices + ring offsets.
        let mut lats: Vec<f64> = Vec::new();
        let mut lngs: Vec<f64> = Vec::new();
        let mut ring_offsets: Vec<i32> = Vec::with_capacity(rings.len() + 1);
        ring_offsets.push(0);
        for ring in &rings {
            for &(lng, lat) in ring {
                lats.push(lat);
                lngs.push(lng);
            }
            ring_offsets.push(lats.len() as i32);
        }

        for res_u8 in [3u8, 5, 7] {
            let r = Resolution::try_from(res_u8).expect("resolution");
            for (mode_u8, mode) in modes {
                let exterior = geo_types::LineString::new(
                    rings[0]
                        .iter()
                        .map(|&(lng, lat)| geo_types::Coord { x: lng, y: lat })
                        .collect(),
                );
                let holes: Vec<geo_types::LineString<f64>> = rings[1..]
                    .iter()
                    .map(|ring| {
                        geo_types::LineString::new(
                            ring.iter()
                                .map(|&(lng, lat)| geo_types::Coord {
                                    x: lng,
                                    y: lat,
                                })
                                .collect(),
                        )
                    })
                    .collect();
                let polygon = geo_types::Polygon::new(exterior, holes);
                let mut tiler =
                    TilerBuilder::new(r).containment_mode(mode).build();
                tiler.add(polygon).expect("valid polygon");
                let cells: Vec<u64> = tiler
                    .into_coverage()
                    .map(u64::from)
                    .collect();
                let cells_str = cells
                    .iter()
                    .map(|c| c.to_string())
                    .collect::<Vec<_>>()
                    .join(",");
                let lats_str = lats
                    .iter()
                    .map(|v| v.to_string())
                    .collect::<Vec<_>>()
                    .join(",");
                let lngs_str = lngs
                    .iter()
                    .map(|v| v.to_string())
                    .collect::<Vec<_>>()
                    .join(",");
                let ring_str = ring_offsets
                    .iter()
                    .map(|v| v.to_string())
                    .collect::<Vec<_>>()
                    .join(",");
                writeln!(
                    w,
                    r#"{{"name":"{name}","res":{res_u8},"mode":{mode_u8},"verts_lat":[{lats_str}],"verts_lng":[{lngs_str}],"ring_offsets":[{ring_str}],"cells":[{cells_str}]}}"#,
                )
                .unwrap();
            }
        }
    }
}

fn emit_compact_uncompact() {
    let mut w_compact = open("golden_compact.ndjson");
    let mut w_uncompact = open("golden_uncompact.ndjson");

    // Take a base res-3 cell, collect all its res-5 children — a
    // perfectly compactable set.
    for (name, lat, lng) in reference_points().iter().take(3) {
        let ll = LatLng::new(*lat, *lng).expect("valid latlng");
        let base = ll.to_cell(Resolution::Three);
        let children: Vec<u64> = base
            .children(Resolution::Five)
            .map(u64::from)
            .collect();
        // Compacted form: sort + compact in-place.
        let mut cells_for_compact: Vec<CellIndex> = children
            .iter()
            .map(|&c| CellIndex::try_from(c).unwrap())
            .collect();
        CellIndex::compact(&mut cells_for_compact).expect("compactable");
        let compacted: Vec<u64> =
            cells_for_compact.iter().map(|c| u64::from(*c)).collect();

        let cells_str = children
            .iter()
            .map(|c| c.to_string())
            .collect::<Vec<_>>()
            .join(",");
        let compact_str = compacted
            .iter()
            .map(|c| c.to_string())
            .collect::<Vec<_>>()
            .join(",");
        writeln!(
            w_compact,
            r#"{{"name":"{name}","cells":[{cells_str}],"compacted":[{compact_str}]}}"#,
        )
        .unwrap();

        // Uncompact: expand the compacted form back to res 5.
        let expanded: Vec<u64> = CellIndex::uncompact(
            compacted
                .iter()
                .map(|&c| CellIndex::try_from(c).unwrap()),
            Resolution::Five,
        )
        .map(u64::from)
        .collect();
        let expanded_str = expanded
            .iter()
            .map(|c| c.to_string())
            .collect::<Vec<_>>()
            .join(",");
        writeln!(
            w_uncompact,
            r#"{{"name":"{name}","cells":[{compact_str}],"res":5,"expanded":[{expanded_str}]}}"#,
        )
        .unwrap();
    }
}

fn main() {
    emit_latlng_to_cell();
    emit_cell_to_latlng();
    emit_parents();
    emit_children();
    emit_grid_disk();
    emit_strings();
    emit_validate();
    emit_boundaries();
    emit_polyfill();
    emit_compact_uncompact();
    eprintln!(
        "wrote golden vectors to {}",
        testdata_dir().display()
    );
}
