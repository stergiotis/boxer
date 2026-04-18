//go:build llm_generated_gemini3pro

package stubber

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stergiotis/boxer/public/ea"
	"github.com/stretchr/testify/require"
)

func TestTreeProcessor_ProcessTree(t *testing.T) {
	// Set up a virtual filesystem with package hierarchy
	srcFS := fstest.MapFS{
		"main.go": &fstest.MapFile{
			Data: []byte(`package main
				import "example.com/foo"
				func PublicMain() { foo.PublicFoo() }
				func privateMain() {}`),
		},
		"pkg/a/a.go": &fstest.MapFile{
			Data: []byte(`package a
				const PublicA = 1
				const privateA = 2`),
		},
		"pkg/a/a_test.go": &fstest.MapFile{
			Data: []byte(`package a
				func TestA() {}`),
		},
		"pkg/b/b.go": &fstest.MapFile{
			Data: []byte(`package b
				type PublicB struct { Field int }`),
		},
		// Deeply nested
		"pkg/b/c/c.go": &fstest.MapFile{
			Data: []byte(`package c
				func PublicC() {}`),
		},
	}

	tp := &TreeProcessor{Filter: NewGoFilter("")}

	t.Run("Recursive ./...", func(t *testing.T) {
		output := make(map[string]string)
		writer := func(path string) (w io.WriteCloser, err error) {
			b := bytes.NewBuffer(make([]byte, 0, 1024))
			w = ea.NewAnonymousCloseWriter(func() error {
				output[path] = b.String()
				return nil
			}, func(p []byte) (n int, err error) {
				return b.Write(p)
			})
			return
		}

		err := tp.ProcessTree(context.Background(), srcFS, "./...", nil, nil, writer, nil, nil)
		require.NoError(t, err)

		// Verification
		require.Contains(t, output, "main.go")
		require.Contains(t, output, "pkg/a/a.go")
		require.Contains(t, output, "pkg/b/b.go")
		require.Contains(t, output, "pkg/b/c/c.go")
		require.NotContains(t, output, "pkg/a/a_test.go", "Should ignore test files")

		// Content check
		require.NotContains(t, output["main.go"], "privateMain")
		require.NotContains(t, output["pkg/a/a.go"], "privateA")
	})

	t.Run("Single package directory", func(t *testing.T) {
		output := make(map[string]string)
		writer := func(path string) (w io.WriteCloser, err error) {
			b := bytes.NewBuffer(make([]byte, 0, 1024))
			w = ea.NewAnonymousCloseWriter(func() error {
				output[path] = b.String()
				return nil
			}, func(p []byte) (n int, err error) {
				return b.Write(p)
			})
			return
		}

		err := tp.ProcessTree(context.Background(), srcFS, "pkg/b", nil, nil, writer, nil, nil)
		require.NoError(t, err)

		require.Contains(t, output, "pkg/b/b.go")
		require.NotContains(t, output, "pkg/b/c/c.go", "Should not be recursive")
		require.NotContains(t, output, "pkg/a/a.go")
	})

	t.Run("Specific directory recursive", func(t *testing.T) {
		output := make(map[string]string)
		writer := func(path string) (w io.WriteCloser, err error) {
			b := bytes.NewBuffer(make([]byte, 0, 1024))
			w = ea.NewAnonymousCloseWriter(func() error {
				output[path] = b.String()
				return nil
			}, func(p []byte) (n int, err error) {
				return b.Write(p)
			})
			return
		}

		err := tp.ProcessTree(context.Background(), srcFS, "pkg/b/...", nil, nil, writer, nil, nil)
		require.NoError(t, err)

		require.Contains(t, output, "pkg/b/b.go")
		require.Contains(t, output, "pkg/b/c/c.go")
		require.NotContains(t, output, "pkg/a/a.go")
	})

	t.Run("shouldProcessDir skips subtree", func(t *testing.T) {
		output := make(map[string]string)
		writer := func(path string) (w io.WriteCloser, err error) {
			b := bytes.NewBuffer(make([]byte, 0, 1024))
			w = ea.NewAnonymousCloseWriter(func() error {
				output[path] = b.String()
				return nil
			}, func(p []byte) (n int, err error) {
				return b.Write(p)
			})
			return
		}

		shouldProcessDir := func(fpath string) bool {
			return !strings.HasPrefix(fpath, "pkg/b")
		}

		err := tp.ProcessTree(context.Background(), srcFS, "./...", nil, nil, writer, shouldProcessDir, nil)
		require.NoError(t, err)

		require.Contains(t, output, "main.go")
		require.Contains(t, output, "pkg/a/a.go")
		require.NotContains(t, output, "pkg/b/b.go", "shouldProcessDir should skip pkg/b")
		require.NotContains(t, output, "pkg/b/c/c.go", "shouldProcessDir should skip nested dirs under pkg/b")
	})

	t.Run("Imports Formatted", func(t *testing.T) {
		output := make(map[string]string)
		writer := func(path string) (w io.WriteCloser, err error) {
			b := bytes.NewBuffer(make([]byte, 0, 1024))
			w = ea.NewAnonymousCloseWriter(func() error {
				output[path] = b.String()
				return nil
			}, func(p []byte) (n int, err error) {
				return b.Write(p)
			})
			return
		}
		err := tp.ProcessTree(context.Background(), srcFS, "main.go", nil, nil, writer, nil, nil)
		require.NoError(t, err)

		// goimports should clean up the spacing
		require.True(t, strings.Contains(output["main.go"], `import _ "example.com/foo"`))
	})
}
