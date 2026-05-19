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
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

var flightRecorder *trace.FlightRecorder
var flightRecorderOutputMtx sync.Mutex

// Environment variable declarations for the tracing subsystem.
var (
	// FlightRecorder is the BOXER_FLIGHT_RECORDER env-var spec.
	FlightRecorder = env.NewBool(env.Spec{
		Name:        "BOXER_FLIGHT_RECORDER",
		Description: "enable the Go runtime flight recorder",
		Category:    env.CategoryObservability,
		CliFlagName: "flightRecorder",
	})

	// FlightRecorderOutputFile is the BOXER_FLIGHT_RECORDER_OUTPUT_FILE env-var spec.
	FlightRecorderOutputFile = env.NewPath(env.Spec{
		Name:        "BOXER_FLIGHT_RECORDER_OUTPUT_FILE",
		Default:     "flightRecorder.trace",
		Description: "destination path for the flight recorder trace dump",
		Category:    env.CategoryObservability,
		CliFlagName: "flightRecorderOutputFile",
	})
)

var TracingFlags = []cli.Flag{
	FlightRecorder.AsCliFlag(env.WithBoolAction(func(context *cli.Context, b bool) error {
		if !b {
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
	})),
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
					sig := <-sigChan
					writeFlightRecorderTrace(FlightRecorderOutputFile.Get())
					// see https://stackoverflow.com/questions/61487783/how-can-you-avoid-races-in-overriding-gos-default-signal-handlers
					// for a discussion
					switch sig {
					case syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE:
						log.Info().Str("signal", sig.String()).Msg("caught signal, exiting with exit code 1")
						os.Exit(1)
					}
				}
			}()
			return nil
		},
	},
	FlightRecorderOutputFile.AsCliFlag(),
}

func writeFlightRecorderTrace(d string) {
	if flightRecorderOutputMtx.TryLock() {
		defer flightRecorderOutputMtx.Unlock()
	} else {
		// already writing, do nothing
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
	writeFlightRecorderTrace(FlightRecorderOutputFile.Get())
}
