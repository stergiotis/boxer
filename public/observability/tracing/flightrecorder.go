package tracing

import (
	"os"
	"os/signal"
	"runtime/trace"
	"slices"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

var flightRecorder *trace.FlightRecorder
var flightRecorderOutputFile string
var flightRecorderOutputMtx sync.Mutex

var TracingFlags = []cli.Flag{
	&cli.BoolFlag{
		Name:     "flightRecorder",
		Category: "tracing",
		EnvVars:  []string{"BOXER_FLIGHT_RECORDER"},
		Action: func(context *cli.Context, b bool) error {
			if !context.Bool("flightRecorder") {
				return nil
			}
			cfg := trace.FlightRecorderConfig{
				MinAge:   time.Second * 10,
				MaxBytes: 128 * 1024 * 1024,
			}
			flightRecorder = trace.NewFlightRecorder(cfg)
			err := flightRecorder.Start()
			if err != nil {
				return eh.Errorf("unable to start flight recorder")
			}
			log.Info().Msg("started golang flight recorder")
			return nil
		},
	},
	&cli.StringSliceFlag{
		Name: "flightRecorderFlushOnSignal",
		Action: func(context *cli.Context, signalNames []string) error {
			if len(signalNames) == 0 {
				return nil
			}
			sigChan := make(chan os.Signal, 1)
			signals := make([]os.Signal, 0, len(signalNames))
			signalLuName := []string{"SIGUSR1", "SIGUSR2", "SIGCHLD", "SIGTERM", "SIGINT", "SIGPIPE"}
			signalLuSig := []os.Signal{syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGCHLD, syscall.SIGTERM, syscall.SIGINT, syscall.SIGPIPE}
			for _, s := range signalNames {
				i := slices.Index(signalLuName, s)
				if i < 0 {
					return eb.Build().Strs("possible", signalLuName).Str("given", s).Errorf("given signal name is invalid")
				}
				signals = append(signals, signalLuSig[i])
			}
			signal.Notify(sigChan, signals...)

			go func() {
				for {
					_ = <-sigChan
					writeFlightRecorderTrace(flightRecorderOutputFile)
				}
			}()
			return nil
		},
	},
	&cli.PathFlag{
		EnvVars:  []string{"BOXER_FLIGHT_RECORDER_OUTPUT_FILE"},
		Name:     "flightRecorderOutputFile",
		Value:    "flightRecorder.trace",
		Category: "tracing",
		Action: func(context *cli.Context, path cli.Path) error {
			flightRecorderOutputFile = path
			return nil
		},
	},
}

func writeFlightRecorderTrace(d string) {
	if flightRecorderOutputMtx.TryLock() {
		defer flightRecorderOutputMtx.Unlock()
	} else {
		return
	}
	if flightRecorder == nil || !flightRecorder.Enabled() {
		return
	}
	var err error
	if d == "" {
		_, err = flightRecorder.WriteTo(os.Stdout)
	} else {
		var f *os.File
		f, err = os.Create(d)
		if f != nil {
			defer f.Close()
		}
		if err != nil {
			err = eb.Build().Str("path", d).Errorf("unable to open file for writing flight recorder output trace data: %w", err)
		} else {
			_, err = flightRecorder.WriteTo(f)
		}
	}
	if err == nil {
		log.Info().Str("file", d).Msg("wrote flight tracker output trace data to file")
	} else {
		log.Warn().Err(err).Msg("unable to write flight tracker output trace data, skipping")
		err = nil
	}
}
func TracingHandleExit(context *cli.Context) {
	if flightRecorder == nil {
		return
	}
	writeFlightRecorderTrace(flightRecorderOutputFile)
	flightRecorder.Stop()
}
