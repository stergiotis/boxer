package driver

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/golangci/gofmt"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/code/synthesis/golang"
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/observability/vcs"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime/docgen"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime/goserver"
	"github.com/stergiotis/boxer/public/thestack/fffi2/compiletime/rustclient"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/definition"
)

var CodeGeneratorName = "TheStack (" + vcs.ModuleInfo() + ")"

func writeFileFormated(path string, code string) (err error) {
	var formatted []byte
	formatted, err = gofmt.Source(path, unsafeperf.UnsafeStringToBytes(code), gofmt.Options{
		NeedSimplify: false,
		RewriteRules: nil,
	})
	if err != nil {
		log.Warn().Err(err).Str("path", path).Msg("unable to format go output, ignoring")
		err = nil
		formatted = unsafeperf.UnsafeStringToBytes(code)
	}
	err = os.WriteFile(path, formatted, os.ModePerm)
	if err != nil {
		err = eh.Errorf("unable to write go source code: %w", err)
		return
	}
	return
}

func GenerateGoFiles(packageName string, basePath string) (err error) {
	tracker := compiletime.NewStateAndErrTracker[goserver.GeneratorStateE](goserver.GenerateStateInitial, "")
	buf1 := bytes.NewBuffer(nil)
	buf2 := bytes.NewBuffer(nil)
	buf3 := bytes.NewBuffer(nil)
	buf4 := bytes.NewBuffer(nil)
	buf5 := bytes.NewBuffer(nil)
	p := "package " + packageName + "\n\n"
	for _, w := range []*bytes.Buffer{buf1, buf2, buf3, buf4, buf5} {
		_, err = golang.AddCodeGenComment(w, CodeGeneratorName)
		if err != nil {
			return
		}
		_, err = w.WriteString(p)
		if err != nil {
			return
		}
	}
	wh := goserver.WriterHolder{
		FactoryWriter: buf1,
		MethodWriter:  buf2,
		EnumWriter:    buf3,
		TypeWriter:    buf4,
		FetcherWriter: buf5,
	}
	err = goserver.GenerateCode(wh, definition.Definitions(), tracker)
	if err != nil {
		return
	}

	for path, c := range functional.MakeIter2FromAlternatedValue(
		"factories.out.go", buf1.String(),
		"methods.out.go", buf2.String(),
		"enums.out.go", buf3.String(),
		"types.out.go", buf4.String(),
		"fetchers.out.go", buf5.String(),
	) {
		err = writeFileFormated(filepath.Join(basePath, path), c)
		if err != nil {
			return
		}
	}
	return
}
func overwriteCodeAtMarker(candidateFiles []string, marker string, content string) (foundFile string, err error) {
	const whitespace = "\n \t\r"
	prolog := "/*--------------------- " + marker + " -----------------------*/"
	epilog := "/*===================== " + marker + " =======================*/"
	var a, b string
	for _, c := range candidateFiles {
		var rs []byte
		rs, err = os.ReadFile(c)
		if err != nil {
			err = eb.Build().Str("path", c).Errorf("unable to read rust file: %w", err)
			return
		}
		var found bool
		a, b, found = strings.Cut(unsafeperf.UnsafeBytesToString(rs), marker)
		if found {
			foundFile = c
			break
		}
	}

	if foundFile != "" {
		var f *os.File
		f, err = os.OpenFile(foundFile, os.O_WRONLY|os.O_TRUNC, os.ModePerm)
		if err != nil {
			err = eb.Build().Str("path", foundFile).Errorf("unable to open rust file for writing: %w", err)
			return
		}
		defer f.Close()
		var after string
		lastidx := strings.LastIndex(b, epilog)
		if lastidx >= 0 {
			after = b[lastidx+len(epilog):]
		} else {
			after = b
		}
		for _, s := range []string{
			strings.TrimRight(a, whitespace),
			"\n",
			marker,
			"\n",
			prolog,
			"\n",
			content,
			"\n",
			epilog,
			"\n",
			strings.TrimLeft(after, whitespace)} {
			_, err = f.WriteString(s)
			if err != nil {
				err = eb.Build().Str("path", foundFile).Errorf("unable to write to rust file: %w", err)
				return
			}
		}
	}
	return
}

func GenerateRustFiles(basePath string) (err error) {
	tracker := compiletime.NewStateAndErrTracker[rustclient.GeneratorStateE](rustclient.GenerateStateInitial, "")
	factoryBuf := bytes.NewBuffer(nil)
	methodBuf := bytes.NewBuffer(nil)
	dispatchBuf := bytes.NewBuffer(nil)
	enumBuf := bytes.NewBuffer(nil)
	typeBuf := bytes.NewBuffer(nil)
	for _, w := range []*bytes.Buffer{factoryBuf, methodBuf, dispatchBuf, enumBuf, typeBuf} {
		_, err = golang.AddCodeGenComment(w, CodeGeneratorName)
		if err != nil {
			return
		}
	}
	wh := rustclient.WriterHolder{
		MethodWriter:   methodBuf,
		FactoryWriter:  factoryBuf,
		DispatchWriter: dispatchBuf,
		EnumWriter:     enumBuf,
		TypeWriter:     typeBuf,
	}

	err = rustclient.GenerateCode(wh, definition.Definitions(), tracker)
	if err != nil {
		return
	}
	err = os.WriteFile(filepath.Join(basePath, "enums_out.rs"), enumBuf.Bytes(), os.ModePerm)
	if err != nil {
		return
	}

	var candidates []string
	candidates, err = filepath.Glob(filepath.Join(basePath, "*.rs"))
	if err != nil {
		err = eb.Build().Str("basePath", basePath).Errorf("unable to glob rust files: %w", err)
		return
	}
	var rustfmtPath string
	rustfmtPath, err = exec.LookPath("rustfmt")
	if err != nil {
		log.Warn().Msg("rustfmt not found in path, templated files will not be reformated")
		err = nil
	}
	for marker, content := range functional.MakeIter2FromAlternatedValue("//IMZERO2_INCLUDE_FFFI_DISPATCH_OUT", dispatchBuf.String()) {
		var p string
		p, err = overwriteCodeAtMarker(candidates, marker, content)
		if err != nil {
			err = eh.Errorf("unable to insert code at marker: %w", err)
			return
		}
		log.Info().Int("lines", strings.Count(content, "\n")).Str("marker", marker).Str("path", p).Msg("inserted code at marker")
		if rustfmtPath != "" {
			buf := bytes.NewBuffer(nil)
			c := exec.Command(rustfmtPath, p)
			c.Stderr = buf
			err = c.Run()
			if err != nil {
				log.Warn().Err(err).Str("rustfmtPath", rustfmtPath).Str("file", p).Str("stderr", buf.String()).Msg("unable to format rust file using rustfmt, ignoring")
				err = nil
			}
		}
	}

	return
}

func GenerateDocFile(outputPath string) (err error) {
	tracker := compiletime.NewStateAndErrTracker[docgen.GeneratorStateE](docgen.GenerateStateInitial, "")
	buf := bytes.NewBuffer(nil)
	err = docgen.GenerateDoc(buf, definition.Definitions(), tracker)
	if err != nil {
		return
	}
	err = os.WriteFile(outputPath, buf.Bytes(), os.ModePerm)
	if err != nil {
		err = eh.Errorf("unable to write doc file: %w", err)
		return
	}
	return
}
