package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/wire"
)

// ErrBodyTooLarge is wrapped, permanent, when a request's body exceeds
// Config.MaxBody.
var ErrBodyTooLarge = eh.Errorf("request body exceeds the host's bound")

// ErrReplyTooLarge is wrapped, permanent, when the items of one request do
// not fit one reply frame.
var ErrReplyTooLarge = eh.Errorf("reply exceeds the host's frame bound — raise it with the framework's, or emit fewer items per request")

// panicStackLimit bounds the stack a recovered panic carries into its error.
const panicStackLimit = 4096

// RunStdio is Run over the process's own stdin, stdout and stderr.
func RunStdio(ctx context.Context, cfg Config, handler stevedore.HandlerI) error {
	return Run(ctx, cfg, handler, os.Stdin, os.Stdout, os.Stderr)
}

// Run serves requests from stdin until it ends cleanly, the pipe breaks, or
// ctx is cancelled between requests. Every request produces exactly one reply
// in the configured mode; a failure is a status, never a missing reply, so
// the framework's count of replies matches its count of requests.
func Run(ctx context.Context, cfg Config, handler stevedore.HandlerI, stdin io.Reader, stdout io.Writer, stderr io.Writer) (err error) {
	if handler == nil {
		return eh.Errorf("no handler")
	}
	if cfg.Codec == wire.CodecLines {
		return eh.Errorf("the lines codec cannot carry a reply, which is a binary archive — frame with length_prefixed_uint32_be or netstring")
	}
	inst := &runner{
		cfg:     cfg,
		handler: handler,
		in:      wire.NewFrameReader(stdin, cfg.Codec, cfg.maxFrame()),
		out:     wire.NewFrameWriter(stdout, cfg.Codec),
		stderr:  stderr,
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var frame []byte
		frame, err = inst.in.Read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			if errors.Is(err, wire.ErrFrameTooLarge) {
				// The frame was skipped; the framework still expects a reply.
				werr := inst.reply(nil, nil, stevedore.Permanent(err))
				if werr != nil {
					return werr
				}
				continue
			}
			return eh.Errorf("read request: %w", err)
		}
		payload, logs, herr := inst.handle(ctx, frame)
		err = inst.reply(payload, logs, herr)
		if err != nil {
			return err
		}
	}
}

type runner struct {
	cfg     Config
	handler stevedore.HandlerI
	in      *wire.FrameReader
	out     *wire.FrameWriter
	stderr  io.Writer
	logBuf  bytes.Buffer
	emits   []stevedore.Emit
	parts   [][]byte
}

// handle turns one frame into one reply payload, or an error the reply
// reports. Whatever the handler logged rides back as logs.
func (inst *runner) handle(ctx context.Context, frame []byte) (payload []byte, logs []byte, err error) {
	inst.logBuf.Reset()
	var logOut io.Writer = &inst.logBuf
	if inst.cfg.Reply == ReplyStdout {
		logOut = inst.cfg.LogOutput
		if logOut == nil {
			logOut = io.Discard
		}
	}
	logger := zerolog.New(logOut).Level(inst.cfg.LogLevel)

	var req stevedore.Request
	if inst.cfg.BareBody {
		req.Body = frame
	} else {
		req, err = stevedore.DecodeRequest(frame)
		if err != nil {
			return nil, inst.finishLogs(logger, err), err
		}
	}
	if len(req.Body) > inst.cfg.maxBody() {
		err = stevedore.Permanent(eb.Build().Int("len", len(req.Body)).Int("max", inst.cfg.maxBody()).Errorf("request: %w", ErrBodyTooLarge))
		return nil, inst.finishLogs(logger, err), err
	}
	ref := stevedore.ReferenceOf(req)
	logger = logger.With().Str("origin", req.Origin).Uint64("ref", ref.Value()).Logger()
	hctx := logger.WithContext(ctx)
	var cancel context.CancelFunc
	if inst.cfg.Deadline > 0 {
		hctx, cancel = context.WithTimeout(hctx, inst.cfg.Deadline)
		defer cancel()
	}

	err = stevedore.Retry(hctx, inst.cfg.retry(), func(actx context.Context) error {
		inst.emits = inst.emits[:0]
		return inst.attempt(actx, req)
	})
	if err != nil {
		return nil, inst.finishLogs(logger, err), err
	}

	inst.parts = inst.parts[:0]
	size := wire.SerializedPartsLen(nil)
	for i, e := range inst.emits {
		var b []byte
		b, err = stevedore.EncodeItem(stevedore.Item{
			Ref: ref, Origin: req.Origin, Ordinal: uint64(i),
			Line: e.Line, Offset: e.Offset,
			Split: req.Split, Part: req.Part, Parts: req.Parts, Last: req.Last,
			PayloadKind: e.PayloadKind, Payload: e.Payload,
		})
		if err != nil {
			err = stevedore.Permanent(err)
			return nil, inst.finishLogs(logger, err), err
		}
		size += 4 + len(b)
		if size > inst.cfg.maxFrame() {
			err = stevedore.Permanent(eb.Build().Int("items", len(inst.emits)).Int("max", inst.cfg.maxFrame()).Errorf("reply: %w", ErrReplyTooLarge))
			return nil, inst.finishLogs(logger, err), err
		}
		inst.parts = append(inst.parts, b)
	}
	payload = wire.SerializeParts(inst.parts)
	logger.Debug().Int("items", len(inst.parts)).Msg("handled")
	return payload, inst.finishLogs(logger, nil), nil
}

// attempt runs the handler once, with its panic turned into a permanent
// error: one bad body must not cost the process and the requests behind it.
func (inst *runner) attempt(ctx context.Context, req stevedore.Request) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		stack := debug.Stack()
		if len(stack) > panicStackLimit {
			stack = stack[:panicStackLimit]
		}
		err = stevedore.Permanent(eb.Build().Str("panic", fmt.Sprint(r)).Str("stack", string(stack)).Errorf("handler panicked"))
	}()
	err = inst.handler.Handle(ctx, req, func(e stevedore.Emit) error {
		inst.emits = append(inst.emits, e)
		return nil
	})
	if err == nil && ctx.Err() != nil {
		// A handler that returned nil after its context ended did not finish
		// its work; say so rather than reply with a partial fan-out.
		err = eh.Errorf("handler context ended: %w", ctx.Err())
	}
	return
}

// finishLogs writes the failure line, when there is one, and returns what
// the request logged. Under ReplyStdout the lines already went to LogOutput
// and nothing rides the reply.
func (inst *runner) finishLogs(logger zerolog.Logger, err error) []byte {
	if err != nil {
		logger.Error().Str("class", stevedore.ClassOf(err).String()).Err(err).Msg("request failed")
	}
	if inst.cfg.Reply == ReplyStdout {
		return nil
	}
	return bytes.TrimRight(inst.logBuf.Bytes(), "\n")
}

// reply writes one request's outcome in the configured mode.
func (inst *runner) reply(payload []byte, logs []byte, herr error) (err error) {
	status := stevedore.StatusText(herr)
	switch inst.cfg.Reply {
	case ReplyStdout:
		if herr != nil {
			_, err = io.WriteString(inst.stderr, status+"\n")
			if err != nil {
				return eh.Errorf("write status: %w", err)
			}
			return
		}
		err = inst.out.Write(payload)
		if err != nil {
			return eh.Errorf("write reply: %w", err)
		}
	case ReplyThreeFrame:
		err = inst.out.Write([]byte(status))
		if err == nil {
			err = inst.out.Write(payload)
		}
		if err == nil {
			err = inst.out.Write(logs)
		}
		if err != nil {
			return eh.Errorf("write reply frames: %w", err)
		}
	default:
		return eb.Build().Uint8("mode", uint8(inst.cfg.Reply)).Errorf("unknown reply mode")
	}
	return
}
