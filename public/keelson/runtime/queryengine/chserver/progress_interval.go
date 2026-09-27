package chserver

// progressIntervalMs is the server-side tick spacing requested for
// progress headers. 250 ms reads as live without header spam on
// minute-long queries (~240 lines/min). Requested on every platform; only
// the native transport (progress.go) reads the headers as they stream.
const progressIntervalMs = 250
