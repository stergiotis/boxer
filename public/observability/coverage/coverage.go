package coverage

import (
	"bytes"
	"os"
	"os/signal"
	"runtime/coverage"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

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
		err = os.MkdirAll(countersDir, os.ModeDir)
		if err != nil {
			err = eb.Build().Str("countersDir", countersDir).Errorf("unable to create output directory for cover counter information")
			return
		}
	}
	if metaDir != "" {
		err = os.MkdirAll(metaDir, os.ModeDir)
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
				e := coverage.WriteCountersDir(countersDir)
				if e == nil {
					log.Info().Str("countersDir", countersDir).Stringer("signal", s).Msg("successfully wrote cover counter information to directory")
				} else {
					log.Error().Str("countersDir", countersDir).Stringer("signal", s).Msg("unable to write cover counter information to directory")
				}
			}
			if metaDir != "" {
				e := coverage.WriteMetaDir(metaDir)
				if e == nil {
					log.Info().Str("metaDir", metaDir).Stringer("signal", s).Msg("successfully wrote cover meta information to directory")
				} else {
					log.Error().Str("metaDir", metaDir).Stringer("signal", s).Msg("unable to write cover meta information to directory")
				}
			}
		}
	}()
	return
}
