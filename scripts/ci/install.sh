#!/bin/bash
# Provision the Go toolchain side of the dev environment. The Rust toolchain
# used by rust/h3bridge is an optional prereq, not covered here:
#
#   - Contributors who do not touch rust/h3bridge do not need cargo; the
#     committed public/science/geo/h3/internal/h3o_wasm/h3.wasm is
#     embedded at build time.
#   - Contributors who rebuild the wasm artifact need cargo plus the
#     wasm32-unknown-unknown stdlib. Under rustup:
#         rustup target add wasm32-unknown-unknown
#     Under Fedora's system cargo:
#         sudo dnf install rust-std-static-wasm32-unknown-unknown
#     scripts/dev/build_h3_wasm.sh probes both layouts and fails with
#     pointed instructions on miss.
#   - CI enforces byte parity between a fresh rebuild and the committed
#     artifact via scripts/ci/h3_wasm_parity.sh, which is invoked by
#     scripts/ci/lint.sh and gracefully skips when the Rust toolchain is
#     absent.
set -ev
here=$(dirname "$(readlink -f "$BASH_SOURCE")")
cd "$here/../.."
# Tool versions are pinned (not @latest) so `go mod tidy --diff` stays
# deterministic: an unpinned @latest silently upgrades go.mod/go.sum on each
# install run, drifting from the committed versions and failing the tidy gate.
# Bump these intentionally (and re-run `go mod tidy`) to adopt a newer release;
# keep them in sync with the versions in go.mod.
go get -tool github.com/mfridman/tparse@v0.18.0
go get -tool go.uber.org/nilaway/cmd/nilaway@v0.0.0-20260528182042-490362de4fb6
go get -tool github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.10.0
go get -tool golang.org/x/vuln/cmd/govulncheck@v1.3.0
go mod download
