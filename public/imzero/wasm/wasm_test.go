package wasm

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/logging"
	"github.com/tetratelabs/wazero"
	"os"
	"testing"
)

func init() {
	logging.SetupZeroLog()
}
func runMainLoop(cfg wazero.RuntimeConfig, b *testing.B) (err error) {
	var imzero *ImZero
	var w []byte
	file := "../../../src3/imgui.wasm"
	w, err = os.ReadFile(file)
	if err != nil {
		err = eb.Build().Str("file", file).Errorf("unable to read wasm file: %w", err)
		return
	}
	imzero, err = NewImZero(w, cfg, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		return eh.Errorf("unable to crate imzero instance: %w", err)
	}
	defer imzero.Close()

	// InstantiateModule runs the "_start" function, WASI's "main".
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		err = imzero.Instantiate()
		if err != nil {
			return eh.Errorf("unable to instantiate imzero: %w", err)
		}
	}
	b.StopTimer()
	return
}
func BenchmarkMainLoopCompiler(b *testing.B) {
	cfg := wazero.NewRuntimeConfigCompiler()
	err := runMainLoop(cfg, b)
	if err != nil {
		log.Fatal().Err(err).Msg("unexpected error")
	}
	return
}
func BenchmarkMainLoopInterpreter(b *testing.B) {
	cfg := wazero.NewRuntimeConfigInterpreter()
	err := runMainLoop(cfg, b)
	if err != nil {
		log.Fatal().Err(err).Msg("unexpected error")
	}
	return
}
