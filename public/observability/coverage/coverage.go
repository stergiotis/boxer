package coverage

import (
	"bytes"
	"io"
	"os"
	"os/signal"
	"runtime/coverage"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ProbeRuntimeSupport reports whether the running binary can snapshot
// coverage counters at runtime. WriteCounters — the capability the signal
// trap and the ADR-0169 sampler rely on — requires a binary built with
// -cover -covermode=atomic (set and count modes refuse runtime snapshots).
// Probing with WriteCounters itself is side-effect-free; ClearCounters, the
// obvious alternative, resets the counters accumulated so far.
//
// In test binaries the probe always errors: meta-data finalization is
// deferred to an exit hook there, so the success path is reachable only in
// a real binary.
func ProbeRuntimeSupport() (err error) {
	return coverage.WriteCounters(io.Discard)
}

type Collector struct {
	buf *bytes.Buffer
}

func NewCollector() *Collector {
	return &Collector{
		buf: nil,
	}
}
func (inst *Collector) SetupSignalTrap(countersDir string, metaDir string, sig os.Signal) (err error) {
	if countersDir != "" {
		err = os.MkdirAll(countersDir, 0o755)
		if err != nil {
			err = eb.Build().Str("countersDir", countersDir).Errorf("unable to create output directory for cover counter information")
			return
		}
	}
	if metaDir != "" {
		err = os.MkdirAll(metaDir, 0o755)
		if err != nil {
			err = eb.Build().Str("metaDir", metaDir).Errorf("unable to create output directory for cover meta information")
			return
		}
	}
	if metaDir == "" && countersDir == "" {
		return
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig)
	go func() {
		for {
			s := <-ch
			if countersDir != "" {
				t0 := time.Now()
				e := coverage.WriteCountersDir(countersDir)
				if e == nil {
					log.Info().Str("countersDir", countersDir).Stringer("signal", s).Dur("took", time.Since(t0)).Msg("successfully wrote cover counter information to directory")
				} else {
					log.Error().Err(e).Str("countersDir", countersDir).Stringer("signal", s).Msg("unable to write cover counter information to directory")
				}
			}
			if metaDir != "" {
				t0 := time.Now()
				e := coverage.WriteMetaDir(metaDir)
				if e == nil {
					log.Info().Str("metaDir", metaDir).Stringer("signal", s).Dur("took", time.Since(t0)).Msg("successfully wrote cover meta information to directory")
				} else {
					log.Error().Err(e).Str("metaDir", metaDir).Stringer("signal", s).Msg("unable to write cover meta information to directory")
				}
			}
		}
	}()
	return
}
