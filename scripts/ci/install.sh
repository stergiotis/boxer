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
go get -tool github.com/mfridman/tparse@latest
go get -tool go.uber.org/nilaway/cmd/nilaway@latest
go get -tool github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest
go get -tool golang.org/x/vuln/cmd/govulncheck@latest
go mod download
