package card

import (
	"bufio"
	"fmt"
	"os"

	"encoding/json/jsontext"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/urfave/cli/v2"
)

func NewCliCommandCard() *cli.Command {
	return &cli.Command{
		Name:  "card",
		Usage: "card visualization commands",
		Subcommands: []*cli.Command{
			newCliCommandCardInspect(),
		},
	}
}
func newCliCommandCardInspect() *cli.Command {
	validCardFormats := []string{"html", "unicode", "json", "typst"}
	return &cli.Command{
		Name:  "inspect",
		Usage: "converts an IPC arrow file into cards",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "inputPath",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "cardFormat",
				Value: "html",
				Usage: fmt.Sprintf("valid values: %q", validCardFormats),
			},
		},
		Action: func(context *cli.Context) (err error) {
			allocator := memory.NewGoAllocator()
			opts := make([]ipc.Option, 0, 8)
			opts = append(opts, ipc.WithAllocator(allocator),
				//ipc.WithDictionaryDeltas(true),
				//ipc.WithEnsureNativeEndian(true),
				//ipc.WithZstd(),
				//ipc.WithSchema(schema),
				ipc.WithDelayReadSchema(true),
			)
			var reader *ipc.FileReader
			var f *os.File
			{
				p := context.String("inputPath")
				f, err = os.OpenFile(p, os.O_RDONLY, os.ModePerm)
				if err != nil {
					err = eb.Build().Str("inputPath", p).Errorf("unable to open file for reading: %w", err)
					return
				}
				reader, err = ipc.NewFileReader(f, opts...)
				if err != nil {
					return eh.Errorf("unable to create file reader: %w", err)
				}
			}
			defer reader.Close()

			var recordBatch arrow.RecordBatch
			{
				recordBatch, err = reader.Read()
				if err != nil {
					err = eh.Errorf("unable to read record batch: %w", err)
					return
				}
			}

			var cardDriver *streamreadaccess.Driver
			cardDriver, _, _, err = InferDriverFromRecordBatch(recordBatch, nil)
			if err != nil {
				err = eh.Errorf("unable to infer driver from record batch: %w", err)
				return
			}

			out := bufio.NewWriter(os.Stdout)
			defer out.Flush()
			var sink streamreadaccess.SinkI
			{
				colors := ColorPaletteMagma
				format := context.String("cardFormat")
				switch format {
				case "html":
					sink = NewHtmlCardEmitter(out, colors)
				case "unicode":
					sink = NewUnicodeCardEmitter(out, 210)
				case "json":
					enc := jsontext.NewEncoder(out,
						jsontext.WithIndent("  "),
						jsontext.Multiline(true))
					sink = NewJsonCardEmitter(enc)
				case "typst":
					sink = NewTypstCardEmitter(out, colors)
				default:
					err = eb.Build().Str("cardFormat", format).Strs("validCardFormats", validCardFormats).Errorf("unhandled card format")
					return
				}
			}
			err = cardDriver.DriveRecordBatch(sink, recordBatch)
			if err != nil {
				err = eh.Errorf("unable to driver record: %w", err)
				return
			}
			return
		},
	}
}
