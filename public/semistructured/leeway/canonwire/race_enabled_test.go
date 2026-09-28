//go:build race

package canonwire

// raceEnabled scales the heaviest randomized workloads down to their -short
// size under the race detector, which slows these single-threaded parse and
// codegen loops by an order of magnitude and would otherwise run the test
// binary past go test's default 10-minute timeout.
const raceEnabled = true
