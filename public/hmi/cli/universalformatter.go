package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-json-experiment/json/v1"
	md "github.com/nao1215/markdown"
	"github.com/stergiotis/boxer/public/config"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/unsafeperf"
	"github.com/urfave/cli/v2"
	"github.com/yassinebenaid/godump"
)

type UniversalCliFormatter struct {
	flags        []cli.Flag
	dumper       *godump.Dumper
	cborEncMode  cbor.EncMode
	cborDiagMode cbor.DiagMode
	markdown     bool
	nametransf   config.NameTransformFunc
}

func NewUniversalCliFormatter(nametransf config.NameTransformFunc) (inst *UniversalCliFormatter, err error) {
	flags := []cli.Flag{
		&cli.StringFlag{
			Name:  nametransf("format"),
			Value: "godump",
			Usage: "Output format. Possible values: 'godump','json','json-indent','cbor','diag','go-stringer','go-quote'.",
		},
		&cli.BoolFlag{
			Name: nametransf("markdown"),
		},
	}
	var cborEncMode cbor.EncMode
	cborEncMode, err = cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		err = eh.Errorf("unable to create cbor encode mode: %w", err)
		return
	}
	var cborDiagMode cbor.DiagMode
	cborDiagMode, err = cbor.DiagOptions{
		ByteStringEncoding:      0,
		ByteStringHexWhitespace: false,
		ByteStringText:          false,
		ByteStringEmbeddedCBOR:  false,
		CBORSequence:            false,
		FloatPrecisionIndicator: false,
		MaxNestedLevels:         65535,
		MaxArrayElements:        2147483647,
		MaxMapPairs:             2147483647,
	}.DiagMode()
	if err != nil {
		err = eh.Errorf("unable to create cbor diagnose mode: %w", err)
		return
	}
	inst = &UniversalCliFormatter{
		flags: flags,
		dumper: &godump.Dumper{
			Indentation:             "  ",
			ShowPrimitiveNamedTypes: false,
			HidePrivateFields:       false,
			Theme:                   godump.DefaultTheme,
		},
		cborEncMode:  cborEncMode,
		cborDiagMode: cborDiagMode,
		nametransf:   nametransf,
	}
	return
}

func (inst *UniversalCliFormatter) ToCliFlags() []cli.Flag {
	return inst.flags
}
func (inst *UniversalCliFormatter) FormatValue(context *cli.Context, v any) (err error) {
	f := context.String(inst.nametransf("format"))
	markdown := context.Bool(inst.nametransf("markdown"))
	var out io.Writer
	var buf *bytes.Buffer
	out = os.Stdout
	if markdown {
		buf = bytes.NewBuffer(make([]byte, 0, 4096))
		out = buf
	}
	syntax := md.SyntaxHighlightNone
	switch f {
	case "go-stringer":
		s, ok := v.(fmt.Stringer)
		if ok {
			_, err = out.Write(unsafeperf.UnsafeStringToByte(s.String()))
			if err == nil {
				_, err = out.Write([]byte("\n"))
			}
		} else {
			err = eb.Build().Type("v", v).Errorf("type is not compatible with fmt.Stringer")
		}
	case "go-quote":
		syntax = md.SyntaxHighlightGo
		_, err = fmt.Fprintf(out, "%q\n", v)
	case "json":
		syntax = md.SyntaxHighlightJSON
		w := json.NewEncoder(out)
		w.SetIndent("", "")
		w.SetEscapeHTML(false)
		err = w.Encode(v)
		break
	case "json-indent":
		syntax = md.SyntaxHighlightJSON
		w := json.NewEncoder(out)
		w.SetIndent("", "  ")
		w.SetEscapeHTML(false)
		err = w.Encode(v)
		break
	case "cbor":
		err = inst.cborEncMode.NewEncoder(out).Encode(v)
		break
	case "diag":
		{
			b := bytes.NewBuffer(make([]byte, 0, 4096))
			err = inst.cborEncMode.NewEncoder(b).Encode(v)
			if err == nil {
				var diag string
				diag, err = inst.cborDiagMode.Diagnose(b.Bytes())
				if err == nil {
					_, err = out.Write(unsafeperf.UnsafeStringToByte(diag))
					if err == nil {
						_, err = out.Write([]byte("\n"))
					}
				}
			}
		}
		break
	case "godump":
		err = inst.dumper.Fprint(out, v)
	default:
		err = eb.Build().Str("format", f).Errorf("unhandled format")
		return
	}
	if markdown {
		err = md.NewMarkdown(os.Stdout).CodeBlocks(syntax, buf.String()).Build()
		if err != nil {
			err = eh.Errorf("unable to create markdown output: %w", err)
			return
		}
	}

	return
}
