package hostboot

import (
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

// disableCoreDumps sets RLIMIT_CORE to zero so a crash cannot spill
// process memory — sealed plaintext, keys and AES round-key schedules
// included — to disk via a core dump (ADR-0240 §SD1, from ADR-0134 §SD8).
// It closes one of the two RAM→disk bridges the ephemerality guarantee
// must not leak through; swap is the other, a box-level concern. Plain Go
// panics do not dump core; the exposure this closes is an FFI or `unsafe`
// fault. Best-effort: a failure is logged, not fatal.
func disableCoreDumps(log zerolog.Logger) {
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0}); err != nil {
		log.Warn().Err(err).Msg("hostboot: could not disable core dumps (RLIMIT_CORE=0)")
		return
	}
	log.Info().Msg("hostboot: core dumps disabled (RLIMIT_CORE=0)")
}
