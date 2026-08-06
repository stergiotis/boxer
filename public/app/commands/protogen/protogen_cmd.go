// Package protogen exposes the protobuf Go generator as a boxer subcommand,
// folding the former standalone carrierclient/internal/protogen main into
// public/app per the entry-point standard.
//
// It compiles a .proto with the pure-Go protocompile and hands the resulting
// descriptor to protoc-gen-go over the standard plugin protocol, so no system
// protoc is needed. This mirrors what the Rust side does at build time (protox
// + prost-build, also protoc-free), so both ends of a wire contract are
// generated from the one .proto and cannot drift from it.
//
// The Go package path is supplied as a generator flag rather than as a
// `go_package` option in the .proto: a contract shared with the Rust
// implementation should not have to carry one consumer's output path.
package protogen

import (
	"bytes"
	"context"
	"os"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/protoutil"
	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Defaults describe the only contract generated this way today (the imzero2
// remote-access wire, ADR-0024/ADR-0154) and are relative to the repository
// root; the `go:generate` directive that drives this passes its own paths.
const (
	defaultProtoRoot  = "proto"
	defaultProtoFile  = "boxer/imzero2/v1/input.proto"
	defaultGoPackage  = "github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	defaultOutputFile = "public/thestack/imzero2/carrierclient/input_pb.out.go"
)

func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "protogen",
		Usage: "generate Go protobuf types from a .proto without a system protoc",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "protoRoot",
				Value: defaultProtoRoot,
				Usage: "import root the proto file is resolved against",
			},
			&cli.StringFlag{
				Name:  "protoFile",
				Value: defaultProtoFile,
				Usage: "proto file to generate, relative to protoRoot",
			},
			&cli.StringFlag{
				Name:  "goPackage",
				Value: defaultGoPackage,
				Usage: "import path the generated file declares",
			},
			&cli.StringFlag{
				Name:  "out",
				Value: defaultOutputFile,
				Usage: "file the generated Go source is written to",
			},
		},
		Action: action,
	}
	return
}

func action(c *cli.Context) (err error) {
	err = Generate(c.Context, Opts{
		ProtoRoot:  c.String("protoRoot"),
		ProtoFile:  c.String("protoFile"),
		GoPackage:  c.String("goPackage"),
		OutputFile: c.String("out"),
	})
	return
}

// Opts is one generation: one proto file in, one Go file out.
type Opts struct {
	ProtoRoot  string
	ProtoFile  string
	GoPackage  string
	OutputFile string
}

// Generate compiles Opts.ProtoFile and writes the protoc-gen-go output to
// Opts.OutputFile. Paths are resolved against the process's working directory.
func Generate(ctx context.Context, o Opts) (err error) {
	compiler := protocompile.Compiler{
		Resolver:       &protocompile.SourceResolver{ImportPaths: []string{o.ProtoRoot}},
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := compiler.Compile(ctx, o.ProtoFile)
	if err != nil {
		err = eb.Build().Str("protoRoot", o.ProtoRoot).Str("protoFile", o.ProtoFile).
			Errorf("unable to compile proto file: %w", err)
		return
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{o.ProtoFile},
		Parameter:      proto.String("paths=source_relative,M" + o.ProtoFile + "=" + o.GoPackage),
	}
	for _, f := range files {
		req.ProtoFile = append(req.ProtoFile, protoutil.ProtoFromFileDescriptor(f))
	}
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		err = eb.Build().Errorf("unable to marshal code generator request: %w", err)
		return
	}

	// protoc-gen-go is a module dependency rather than a host binary, so it is
	// reached through the Go toolchain entry of the extbin registry.
	cmd, err := extbin.Go.Command(ctx, extbin.Opts{},
		"run", "google.golang.org/protobuf/cmd/protoc-gen-go")
	if err != nil {
		err = eb.Build().Errorf("unable to resolve the Go toolchain: %w", err)
		return
	}
	cmd.Stdin = bytes.NewReader(reqBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		err = eb.Build().Str("stderr", stderr.String()).Errorf("protoc-gen-go failed: %w", err)
		return
	}

	resp := &pluginpb.CodeGeneratorResponse{}
	if err = proto.Unmarshal(stdout.Bytes(), resp); err != nil {
		err = eb.Build().Errorf("unable to unmarshal code generator response: %w", err)
		return
	}
	if respErr := resp.GetError(); respErr != "" {
		err = eb.Build().Str("reported", respErr).Errorf("protoc-gen-go reported an error")
		return
	}
	// One input file, one output. Its suggested name mirrors the proto's
	// directory layout; the caller has one flat home for it, so take the
	// content and name it here.
	for _, f := range resp.GetFile() {
		if err = os.WriteFile(o.OutputFile, []byte(f.GetContent()), 0o644); err != nil {
			err = eb.Build().Str("out", o.OutputFile).Errorf("unable to write generated file: %w", err)
			return
		}
		log.Info().
			Str("out", o.OutputFile).
			Str("suggestedName", f.GetName()).
			Int("bytes", len(f.GetContent())).
			Msg("protogen: wrote generated Go source")
	}
	return
}
