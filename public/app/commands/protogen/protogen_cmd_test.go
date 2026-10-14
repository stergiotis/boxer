package protogen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A contract that imports a well-known type and another contract compiles,
// and the generated source imports both Go packages.
func TestGenerateResolvesImports(t *testing.T) {
	root := t.TempDir()
	write := func(name string, content string) {
		p := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	write("a/v1/base.proto", `syntax = "proto3";
package a.v1;
message Base { string id = 1; }
`)
	write("b/v1/event.proto", `syntax = "proto3";
package b.v1;
import "google/protobuf/timestamp.proto";
import "a/v1/base.proto";
message Event {
  google.protobuf.Timestamp at = 1;
  a.v1.Base base = 2;
}
`)
	out := filepath.Join(t.TempDir(), "event_pb.out.go")
	require.NoError(t, Generate(t.Context(), Opts{
		ProtoRoot:  root,
		ProtoFile:  "b/v1/event.proto",
		GoPackage:  "example.com/b/v1",
		OutputFile: out,
		Imports:    map[string]string{"a/v1/base.proto": "example.com/a/v1"},
	}))
	src, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Contains(t, string(src), `"google.golang.org/protobuf/types/known/timestamppb"`)
	require.Contains(t, string(src), `"example.com/a/v1"`)
	require.Contains(t, string(src), "package v1")
}
