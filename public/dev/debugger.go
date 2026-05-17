package dev

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

// getTracerPidLinux Credits: https://stackoverflow.com/questions/47879070/how-can-i-see-if-the-goland-debugger-is-running-in-the-program
func getTracerPidLinux() (tpid int, err error) {
	var file *os.File
	file, err = os.Open("/proc/self/status")
	if err != nil {
		return -1, eh.Errorf("can't open process status file: %w", err)
	}
	defer file.Close()

	for {
		var num int
		num, err = fmt.Fscanf(file, "TracerPid: %d\n", &tpid)
		if err == io.EOF {
			break
		}
		if num != 0 {
			return tpid, nil
		}
	}

	return -1, eh.Errorf("unknown format of process status file")
}

// WaitForDebugger gates a Linux-only "block until ptrace tracer attached"
// loop in the CLI Action. The env var is registered on every platform so
// the registry is uniform; the cli.Flag is only attached on Linux.
var WaitForDebugger = env.NewBool(env.Spec{
	Name:        "BOXER_WAIT_FOR_DEBUGGER",
	Description: "execution of program waits until an attached debugger is detected (linux only)",
	Category:    env.CategoryDev,
	CliFlagName: "waitForDebugger",
})

var DebuggerFlags = []cli.Flag{}

func init() {
	switch runtime.GOOS {
	case "linux":
		DebuggerFlags = []cli.Flag{
			WaitForDebugger.AsCliFlag(env.WithBoolAction(func(context *cli.Context, b bool) error {
				for {
					log.Info().Msg("waiting for debugger to attach")
					tpid, err := getTracerPidLinux()
					if err != nil {
						err = eh.Errorf("unable to get tracer pid (linux only): %w", err)
						return err
					}
					if tpid > 0 {
						log.Info().Int("tpid", tpid).Msg("detected debugger, continuing")
						break
					}
					time.Sleep(time.Second)
				}
				return nil
			})),
		}
	}
}
