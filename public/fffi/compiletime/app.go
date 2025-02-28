package compiletime

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/rs/zerolog/log"
	cbor2 "github.com/stergiotis/boxer/public/semistructured/cbor"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/fffi/runtime"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

type generatorApp struct {
}

func newGeneratorApp() *generatorApp {
	return &generatorApp{}
}

func (inst *generatorApp) makeFileReadOnly(path string) error {
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

func (inst *generatorApp) emitToFile(path string, emitter Emitter, preamble []byte) (err error) {
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
	_, err = emitter.Emit(w, preamble)
	_ = w.Close()
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to populate file: %w", err)
		return
	}
	err = inst.makeFileReadOnly(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to make file read-only: %w", err)
		return
	}

	return
}
func (inst *generatorApp) handleInterfaceExport(idlDriver IDLDriver, cfg *Config, namer *Namer) (compat *CompatibilityRecord, err error) {
	exporter := NewBackendInterfaceExporter(cbor2.NewEncoder(nil, nil), namer)
	err = idlDriver.DriveBackend(exporter, cfg.NoThrow)
	if err != nil {
		err = eh.Errorf("unable to export interface description: %w", err)
		return
	}
	if cfg.InterfaceOutputFile != "" {
		err = inst.emitToFile(cfg.InterfaceOutputFile, exporter, nil)
		if err != nil {
			err = eh.Errorf("unable to generate interface description file: %w", err)
			return
		}
	}
	compat = exporter.GetCompatibilityRecord()
	log.Info().Interface("compatibilityRecord", compat).Msg("derived compatibility record from idl description")
	return
}

func (inst *generatorApp) generateBackendCode(idlDriver IDLDriver, cfg *Config, namer *Namer) (err error) {
	var compat *CompatibilityRecord
	compat, err = inst.handleInterfaceExport(idlDriver, cfg, namer)
	if err != nil {
		err = eh.Errorf("unable to generate interface exports: %w", err)
		return
	}
	be := NewCodeTransformerBackendPresenterCpp(namer)
	err = idlDriver.DriveBackend(be, cfg.NoThrow)
	if err != nil {
		err = eh.Errorf("unable to generate code: %w", err)
		return
	}
	var b64 string
	var diag string
	b64, diag, err = compat.ToBase64()
	if err != nil {
		err = eh.Errorf("unable to generate compatibility record: %w", err)
		return
	}
	preamble := []byte(fmt.Sprintf("/* %s */\n#define FFFI_COMPATIBILITY_RECORD \"%s\";\n", diag, b64))
	err = inst.emitToFile(cfg.CppOutputFile, be, preamble)
	if err != nil {
		err = eh.Errorf("unable to generate cpp file: %w", err)
		return
	}

	return
}

func (inst *generatorApp) generateFrontendCode(idlDriver IDLDriver, cfg *Config, namer *Namer) (err error) {
	var compat *CompatibilityRecord
	compat, err = inst.handleInterfaceExport(idlDriver, cfg, namer)
	if err != nil {
		err = eh.Errorf("unable to generate interface exports: %w", err)
		return
	}
	fe := NewCodeTransformerFrontendGo(namer, cfg.GoCodeProlog)
	err = idlDriver.DriveFrontend(fe, cfg.NoThrow)
	if err != nil {
		err = eh.Errorf("unable to generate code: %w", err)
		return
	}
	var b64, diag string
	b64, diag, err = compat.ToBase64()
	if err != nil {
		err = eh.Errorf("unable to generate compatibility record: %w", err)
		return
	}
	preamble := []byte(fmt.Sprintf("/* ffiCompatibilityRecord diag=%s */\nconst fffiCompatibilityRecord = \"%s\";\n", diag, b64))
	err = inst.emitToFile(cfg.GoOutputFile, fe, preamble)
	if err != nil {
		err = eh.Errorf("unable to generate go file: %w", err)
		return
	}

	return
}

func mainE(cfg *Config, namerCfg *NamerConfig) (err error) {
	namer := NewNamer(namerCfg)
	if cfg.GoOutputFile != "" {
		_ = os.Remove(cfg.GoOutputFile)
	}
	if cfg.CppOutputFile != "" {
		_ = os.Remove(cfg.CppOutputFile)
	}
	if cfg.InterfaceOutputFile != "" {
		_ = os.Remove(cfg.InterfaceOutputFile)
	}
	var idlDriver *IDLDriverGoFile
	idlDriver, err = NewIDLDriverGoFile(cfg.IdlBuildTag, cfg.IdlPackagePattern, runtime.FuncProcId(cfg.FuncProcIdOffset))
	if err != nil {
		err = eh.Errorf("unable to process IDL file: %w", err)
		return
	}
	app := newGeneratorApp()
	err = app.generateBackendCode(idlDriver, cfg, namer)
	if err != nil {
		err = eh.Errorf("unable to generate backend code: %w", err)
		return
	}
	err = app.generateFrontendCode(idlDriver, cfg, namer)
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
