#!/bin/bash
# Capture a 15s execution trace from a running process that exposes
# net/http/pprof on :6060, then open it in the Go trace viewer —
# e.g. the desktop host started as `HMI_PPROF_ADDR=localhost:6060 rust/imzero2/hmi.sh`.
set -e
curl -o trace.out http://localhost:6060/debug/pprof/trace?seconds=15
go tool trace trace.out
