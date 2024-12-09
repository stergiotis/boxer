package compiletime

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	cli "github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func makeFileReadOnly(path string) error {
	s, err := os.Stat(path)
	if err != nil {
		return eb.Build().Str("path", path).Errorf("unable to stat file: %w", err)
	}
	err = os.Chmod(path, s.Mode()&^(os.FileMode(syscall.S_IWUSR)|os.FileMode(syscall.S_IWGRP)|os.FileMode(syscall.S_IWOTH)))
	if err != nil {
		return eb.Build().Str("path", path).Errorf("unable to chmod file: %w", err)
	}
	return nil
}

func emitToFile(path string, emitter Emitter) (err error) {
	_ = os.MkdirAll(filepath.Dir(path), 0o000)

	var w io.WriteCloser
	err = os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		err = eb.Build().Str("path", path).Errorf("unable to remove output file: %w", err)
		return
	}
	w, err = os.Create(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to create file: %w", err)
		return
	}
	_, err = emitter.Emit(w)
	_ = w.Close()
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to populate file: %w", err)
		return
	}
	err = makeFileReadOnly(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to make file read-only: %w", err)
		return
	}

	return
}

func generateBackendCode(idlDriver IDLDriver, cfg *Config, namer *Namer) (err error) {
	be := NewCodeTransformerBackendPresenterCpp(namer)
	err = idlDriver.DriveBackend(be)
	if err != nil {
		err = eh.Errorf("unable to generate code: %w", err)
		return
	}
	err = emitToFile(cfg.CppOutputFile, be)
	if err != nil {
		err = eh.Errorf("unable to generate cpp file: %w", err)
		return
	}

	return
}

func generateFrontendCode(idlDriver IDLDriver, cfg *Config, namer *Namer) (err error) {
	fe := NewCodeTransformerFrontendGo(namer, cfg.GoCodeProlog)
	err = idlDriver.DriveFrontend(fe)
	if err != nil {
		err = eh.Errorf("unable to generate code: %w", err)
		return
	}
	err = emitToFile(cfg.GoOutputFile, fe)
	if err != nil {
		err = eh.Errorf("unable to generate go file: %w", err)
		return
	}

	return
}

func mainE(config *Config, namerCfg *NamerConfig) (err error) {
	namer := NewNamer(namerCfg)
	_ = os.Remove(config.GoOutputFile)
	_ = os.Remove(config.CppOutputFile)
	var idlDriver *IDLDriverGoFile
	idlDriver, err = NewIDLDriverGoFile(config.IdlBuildTag, config.IdlPackagePattern, runtime.FuncProcId(config.FuncProcIdOffset))
	if err != nil {
		err = eh.Errorf("unable to process IDL file: %w", err)
		return
	}
	err = generateBackendCode(idlDriver, config, namer)
	if err != nil {
		err = eh.Errorf("unable to generate backend code: %w", err)
		return
	}
	err = generateFrontendCode(idlDriver, config, namer)
	if err != nil {
		err = eh.Errorf("unable to generate frontend code: %w", err)
		return
	}
	return
}

func NewCommand(cfg *Config, namerCfg *NamerConfig) *cli.Command {
	if cfg == nil {
		cfg = &Config{
			IdlBuildTag:         "fffi_idl_code",
			GoCodeProlog:        "",
			IdlPackagePattern:   "",
			GoOutputFile:        "",
			CppOutputFile:       "",
			FuncProcIdOffset:    0,
			validated:           false,
			nValidationMessages: 0,
		}
	}
	if namerCfg == nil {
		namerCfg = &NamerConfig{
			RuneCppType: "rune_t",
		}
	}
	return &cli.Command{
		Name:  "generateFffiCode",
		Flags: append(cfg.ToCliFlags(config.IdentityNameTransf, config.IdentityNameTransf), namerCfg.ToCliFlags(config.IdentityNameTransf, config.IdentityNameTransf)...),
		Action: func(context *cli.Context) error {
			nMessages := cfg.FromContext(config.IdentityNameTransf, context)
			if nMessages > 0 {
				return eb.Build().Int("nMessages", nMessages).Errorf("unable to compose config")
			}
			nMessages = namerCfg.FromContext(config.IdentityNameTransf, context)
			if nMessages > 0 {
				return eb.Build().Int("nMessages", nMessages).Errorf("unable to compose namer config")
			}
			return mainE(cfg, namerCfg)
		},
	}
}
