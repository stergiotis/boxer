// Package runtimecodegen is the CLI wiring for the runtime/factsschema
// code generator. Mirrors the pattern established by designsystem and
// envgen: a thin urfave/cli v2 surface that bridges to the existing
// implementation at
// [github.com/stergiotis/boxer/public/keelson/runtime/factsschema/codegen]
// without changing the library.
//
// The same command tree is exposed two ways:
//
//   - `./pebble.sh runtimecodegen <subcmd>` — aggregated app
//   - `./cmd/runtimecodegen <subcmd>` — standalone CI binary
//
// Subcommands (each accepts `--out` to override the default path):
//
//	runtimecodegen dml         — Arrow record builders (chstore ingest)
//	runtimecodegen dml-cbor    — same DML against arrowrowcbor shim
//	runtimecodegen readaccess  — alias: ra — typed read helpers
//	runtimecodegen ddl         — CH CREATE TABLE wrapper
//	runtimecodegen all         — regenerate everything (default)
package runtimecodegen

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema/codegen"
)

// NewCliCommand returns the top-level `runtimecodegen` command for the
// pebble.sh-aggregated app. Use [Subcommands] when wiring the standalone
// binary so the user types the subcommands directly.
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:        "runtimecodegen",
		Usage:       "regenerate runtime/factsschema artefacts from the leeway schema",
		Description: "Default action (no subcommand) regenerates every artefact (dml/, dml_cbor/, ra/, ddl/).",
		Action: func(ctx *cli.Context) error {
			return runAll()
		},
		Subcommands: Subcommands(),
	}
	return
}

// Subcommands returns the full subcommand set, suitable for the
// standalone binary's top-level App.Commands list.
func Subcommands() (cmds []*cli.Command) {
	cmds = []*cli.Command{
		newDMLCommand(),
		newDMLCBORCommand(),
		newReadAccessCommand(),
		newDDLCommand(),
		newAllCommand(),
	}
	return
}

func newDMLCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "dml",
		Usage: "regenerate " + codegen.DefaultDMLOutputPath,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Value: codegen.DefaultDMLOutputPath,
				Usage: "output path for the generated DML file",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			err = codegen.GenerateDML(ctx.String("out"))
			if err != nil {
				err = eh.Errorf("runtimecodegen dml: %w", err)
			}
			return
		},
	}
	return
}

func newDMLCBORCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "dml-cbor",
		Usage: "regenerate " + codegen.DefaultDMLCBOROutputPath,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Value: codegen.DefaultDMLCBOROutputPath,
				Usage: "output path for the generated DML (sparse CBOR backend) file",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			err = codegen.GenerateDMLCBOR(ctx.String("out"))
			if err != nil {
				err = eh.Errorf("runtimecodegen dml-cbor: %w", err)
			}
			return
		},
	}
	return
}

func newReadAccessCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:    "readaccess",
		Aliases: []string{"ra"},
		Usage:   "regenerate " + codegen.DefaultReadAccessOutputPath,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Value: codegen.DefaultReadAccessOutputPath,
				Usage: "output path for the generated readaccess file",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			err = codegen.GenerateReadAccess(ctx.String("out"))
			if err != nil {
				err = eh.Errorf("runtimecodegen readaccess: %w", err)
			}
			return
		},
	}
	return
}

func newDDLCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "ddl",
		Usage: "regenerate " + codegen.DefaultDDLOutputPath,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "out",
				Value: codegen.DefaultDDLOutputPath,
				Usage: "output path for the generated DDL file",
			},
		},
		Action: func(ctx *cli.Context) (err error) {
			err = codegen.GenerateDDL(ctx.String("out"))
			if err != nil {
				err = eh.Errorf("runtimecodegen ddl: %w", err)
			}
			return
		},
	}
	return
}

func newAllCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "all",
		Usage: "regenerate every runtime/factsschema artefact",
		Action: func(ctx *cli.Context) error {
			return runAll()
		},
	}
	return
}

func runAll() (err error) {
	err = codegen.GenerateDML(codegen.DefaultDMLOutputPath)
	if err != nil {
		err = eh.Errorf("runtimecodegen all: dml: %w", err)
		return
	}
	err = codegen.GenerateDMLCBOR(codegen.DefaultDMLCBOROutputPath)
	if err != nil {
		err = eh.Errorf("runtimecodegen all: dml-cbor: %w", err)
		return
	}
	err = codegen.GenerateReadAccess(codegen.DefaultReadAccessOutputPath)
	if err != nil {
		err = eh.Errorf("runtimecodegen all: readaccess: %w", err)
		return
	}
	err = codegen.GenerateDDL(codegen.DefaultDDLOutputPath)
	if err != nil {
		err = eh.Errorf("runtimecodegen all: ddl: %w", err)
		return
	}
	return
}
