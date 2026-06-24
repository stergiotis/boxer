#!/bin/bash
# Open an interactive pprof web UI on a 15s CPU profile sampled from a
# running process that exposes net/http/pprof on :6060.
#   UI:     http://localhost:7654
#   target: http://localhost:6060/debug/pprof/profile
go tool pprof -http localhost:7654 http://localhost:6060/debug/pprof/profile?seconds=15
