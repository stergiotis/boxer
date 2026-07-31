// Command protogen generates input.pb.go from the remote-access wire contract
// (proto/boxer/imzero2/v1/input.proto) without a system protoc: the schema is
// compiled by the pure-Go protocompile and the resulting descriptor is handed to
// protoc-gen-go over the standard plugin protocol.
//
// This mirrors what the Rust side already does at build time (protox +
// prost-build, also protoc-free), so both ends of the wire are generated from
// the one contract and cannot drift from it — the failure that motivated the
// Rust generation in the first place, per rust/imzero2/build.rs.
//
// The Go package path is supplied as a generator parameter rather than as a
// `go_package` option in the .proto: the contract is shared with the Rust
// implementation, and one consumer's output path is not something the other
// should have to carry.
//
// Run from the package directory:
//
//	go generate ./public/thestack/imzero2/carrierclient/
//
// The `go:generate` directive lives in that package, so the process runs with
// the package directory as its working directory; the paths below are relative
// to it.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/protoutil"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

const (
	// protoFile is relative to protoRoot, and is also the name the generator
	// parameter below binds to a Go import path.
	protoFile = "boxer/imzero2/v1/input.proto"
	protoRoot = "../../../../proto"
	goPackage = "github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	outFile   = "input.pb.go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "protogen: %v\n", err)
		os.Exit(1)
	}
}

func run() (err error) {
	compiler := protocompile.Compiler{
		Resolver:       &protocompile.SourceResolver{ImportPaths: []string{protoRoot}},
		SourceInfoMode: protocompile.SourceInfoStandard,
	}
	files, err := compiler.Compile(context.Background(), protoFile)
	if err != nil {
		return fmt.Errorf("unable to compile %s: %w", protoFile, err)
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{protoFile},
		Parameter:      proto.String("paths=source_relative,M" + protoFile + "=" + goPackage),
	}
	for _, f := range files {
		req.ProtoFile = append(req.ProtoFile, protoutil.ProtoFromFileDescriptor(f))
	}
	reqBytes, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("unable to marshal code generator request: %w", err)
	}

	cmd := exec.Command("go", "run", "google.golang.org/protobuf/cmd/protoc-gen-go")
	cmd.Stdin = bytes.NewReader(reqBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("protoc-gen-go failed: %w\n%s", err, stderr.String())
	}

	resp := &pluginpb.CodeGeneratorResponse{}
	if err = proto.Unmarshal(stdout.Bytes(), resp); err != nil {
		return fmt.Errorf("unable to unmarshal code generator response: %w", err)
	}
	if respErr := resp.GetError(); respErr != "" {
		return fmt.Errorf("protoc-gen-go reported: %s", respErr)
	}
	// One input file, one output. Its suggested name mirrors the proto's
	// directory layout; the package has one flat home for it, so take the
	// content and name it here.
	for _, f := range resp.GetFile() {
		if err = os.WriteFile(outFile, []byte(f.GetContent()), 0o644); err != nil {
			return fmt.Errorf("unable to write %s: %w", outFile, err)
		}
		fmt.Printf("protogen: wrote %s from %s (%d bytes)\n",
			outFile, f.GetName(), len(f.GetContent()))
	}
	return nil
}
