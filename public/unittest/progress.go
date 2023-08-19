package unittest

import (
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog/log"
	progressbar "github.com/schollz/progressbar/v3"
)

var progressFifo = filepath.Join(os.TempDir(), "gotestprogress")

var progressWriter io.Writer

func NewProgressBar(max int64, options ...progressbar.Option) *progressbar.ProgressBar {
	initialize()
	options = append(options,
		progressbar.OptionSetWriter(progressWriter),
		progressbar.OptionEnableColorCodes(true),
	)
	return progressbar.NewOptions64(max,
		options...)
}

var inited = false

func initialize() {
	if inited {
		return
	}
	_ = os.Remove(progressFifo)
	err := syscall.Mkfifo(progressFifo, 0o700)
	if err != nil {
		log.Error().Err(err).Str("progressFifo", progressFifo).Msg("unable to create progress fifo")
	}
	file, err := os.OpenFile(progressFifo, os.O_WRONLY, 0o600)
	if err != nil {
		log.Error().Err(err).Str("progressFifo", progressFifo).Msg("unable to open progress fifo for writing")
	}
	log.Debug().Str("progressFifo", progressFifo).Msg("writing progress bar info to fifo")
	progressWriter = file
	inited = true
}
