//go:build !unix

package hostboot

import "github.com/rs/zerolog"

// disableCoreDumps has nothing to do where there is no rlimit: a wasm
// module has no core file to spill into (ADR-0077 SD8).
func disableCoreDumps(log zerolog.Logger) {
	log.Debug().Msg("hostboot: no core-dump limit on this platform")
}
