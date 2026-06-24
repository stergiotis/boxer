#!/bin/bash
# Open an interactive pprof web UI on a 15s allocation profile sampled
# from a running process that exposes net/http/pprof on :6060.
#   UI:     http://localhost:7654
#   target: http://localhost:6060/debug/pprof/allocs
go tool pprof -http localhost:7654 http://localhost:6060/debug/pprof/allocs?seconds=15
