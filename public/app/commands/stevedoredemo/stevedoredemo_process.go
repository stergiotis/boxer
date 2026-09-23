package stevedoredemo

import (
	"bytes"
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/host"
	"github.com/stergiotis/boxer/public/streaming/stevedore/wire"
)

func newProcessCommand() *cli.Command {
	return &cli.Command{
		Name:  "process",
		Usage: "serve requests on stdin as the framework's external process: one item per line of the body",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "codec", Value: wire.CodecLengthPrefixedUint32BE.String(), Usage: "frame codec: lines, length_prefixed_uint32_be, netstring"},
			&cli.StringFlag{Name: "reply", Value: host.ReplyStdout.String(), Usage: "reply shape: stdout (payload on stdout, failure line on stderr) or three-frame"},
			&cli.BoolFlag{Name: "bare-body", Usage: "a request frame is the body alone, with no header archive around it"},
			&cli.IntFlag{Name: "max-frame", Value: 16 * 1024 * 1024, Usage: "bound on a request frame and a reply, bytes; set with the framework's buffer bound"},
			&cli.IntFlag{Name: "max-body", Usage: "bound on a request body, bytes; zero takes --max-frame"},
			&cli.DurationFlag{Name: "deadline", Value: 20 * time.Second, Usage: "bound on one request's handling; set below the framework's timeout"},
			&cli.StringFlag{Name: "log-level", Value: "info", Usage: "least zerolog level kept from the handler"},
			&cli.StringFlag{Name: "log-file", Usage: "where log lines go under --reply=stdout, where stderr is not available; empty discards"},
		},
		Action: runProcess,
	}
}

func runProcess(c *cli.Context) (err error) {
	codec, err := wire.ParseCodec(c.String("codec"))
	if err != nil {
		return
	}
	reply, err := host.ParseReplyMode(c.String("reply"))
	if err != nil {
		return
	}
	level, err := zerolog.ParseLevel(c.String("log-level"))
	if err != nil {
		return eh.Errorf("log level: %w", err)
	}
	var logOut io.Writer
	if path := c.String("log-file"); path != "" {
		f, oerr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if oerr != nil {
			return eh.Errorf("open log file: %w", oerr)
		}
		defer func() { _ = f.Close() }()
		logOut = f
	}
	cfg := host.Config{
		Codec:     codec,
		Reply:     reply,
		BareBody:  c.Bool("bare-body"),
		MaxFrame:  c.Int("max-frame"),
		MaxBody:   c.Int("max-body"),
		Deadline:  c.Duration("deadline"),
		LogLevel:  level,
		LogOutput: logOut,
	}
	return host.RunStdio(c.Context, cfg, stevedore.HandlerFunc(splitLines))
}

// splitLines is the example handler: one item per line, the line's number
// and byte offset on it, and nothing else. A body starting with "refuse:"
// is refused permanently, so a pipeline's error routing can be tried.
func splitLines(ctx context.Context, req stevedore.Request, emit func(stevedore.Emit) error) error {
	if bytes.HasPrefix(req.Body, []byte("refuse:")) {
		return stevedore.Permanentf("the body asked to be refused")
	}
	zerolog.Ctx(ctx).Debug().Int("bytes", len(req.Body)).Str("hint", req.Hint).Msg("splitting")
	offset := uint64(0)
	line := uint64(0)
	rest := req.Body
	for len(rest) > 0 {
		n := bytes.IndexByte(rest, '\n')
		var l []byte
		if n < 0 {
			l, rest = rest, nil
		} else {
			l, rest = rest[:n], rest[n+1:]
		}
		line++
		err := emit(stevedore.Emit{Payload: l, Line: line, Offset: offset})
		if err != nil {
			return err
		}
		offset += uint64(len(l) + 1)
	}
	return nil
}
