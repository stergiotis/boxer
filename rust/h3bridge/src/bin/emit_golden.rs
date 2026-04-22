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

use h3o::{CellIndex, LatLng, Resolution};

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

fn main() {
    emit_latlng_to_cell();
    emit_cell_to_latlng();
    emit_parents();
    emit_children();
    emit_grid_disk();
    emit_strings();
    emit_validate();
    eprintln!(
        "wrote golden vectors to {}",
        testdata_dir().display()
    );
}
