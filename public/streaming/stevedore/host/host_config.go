package host

import (
	"io"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/streaming/stevedore"
	"github.com/stergiotis/boxer/public/streaming/stevedore/wire"
)

// ReplyModeE says how a reply and its failure reach the framework.
type ReplyModeE uint8

const (
	// ReplyStdout writes the payload frame to stdout on success and one
	// status line to stderr on failure, the upstream subprocess processor's
	// contract. Nothing else is written to stderr.
	ReplyStdout ReplyModeE = iota
	// ReplyThreeFrame writes three frames to stdout per request: the status
	// (empty on success), the payload (empty on failure), and the log lines
	// as newline-separated JSON.
	ReplyThreeFrame
)

// AllReplyModes lists every reply mode, in declaration order.
var AllReplyModes = []ReplyModeE{ReplyStdout, ReplyThreeFrame}

// String spells the mode the way the configuration flag names it.
func (inst ReplyModeE) String() string {
	switch inst {
	case ReplyStdout:
		return "stdout"
	case ReplyThreeFrame:
		return "three-frame"
	default:
		return "unknown"
	}
}

// ParseReplyMode reads a mode name as the configuration flag spells it.
func ParseReplyMode(name string) (mode ReplyModeE, err error) {
	for _, m := range AllReplyModes {
		if m.String() == name {
			return m, nil
		}
	}
	err = eb.Build().Str("name", name).Errorf("unknown reply mode — one of stdout, three-frame")
	return
}

// Config bounds one host. The zero value reads and writes lines, replies on
// stdout, bounds frames at the wire default, retries with the default policy
// and applies no deadline.
type Config struct {
	Codec wire.CodecE
	Reply ReplyModeE
	// BareBody says a request frame is the body alone, with no header and no
	// archive around it — for a pipeline that has nothing to say about a
	// body and composes nothing. The origin is then empty and the reference
	// hashes the body.
	BareBody bool
	// MaxFrame bounds a request frame and a reply; zero takes the wire
	// default. Set it together with the framework's own buffer bound: a reply
	// past what the framework reads is a killed process, not a status.
	MaxFrame int
	// MaxBody bounds a request's body; zero takes MaxFrame. A larger body is
	// refused with a permanent status before the handler runs.
	MaxBody int
	// Deadline bounds one request's handling, attempts included; zero means
	// none. Set it below the framework's per-message timeout so a slow
	// handler yields a transient status rather than a restart.
	Deadline time.Duration
	// Retry is the in-place policy for a transient handler error; a zero
	// Attempts takes the default policy.
	Retry stevedore.RetryPolicy
	// LogLevel is the least level a log line the handler writes through
	// zerolog.Ctx is kept at; zero is debug, so set it. Under ReplyThreeFrame
	// an error-level line is what a framework may read as the message's
	// failure, so the host writes one only when it fails the request.
	LogLevel zerolog.Level
	// LogOutput receives log lines under ReplyStdout, where stderr is not
	// available for them; nil discards them.
	LogOutput io.Writer
}

func (inst Config) maxFrame() int {
	if inst.MaxFrame <= 0 {
		return wire.DefaultMaxFrame
	}
	return inst.MaxFrame
}

func (inst Config) maxBody() int {
	if inst.MaxBody <= 0 {
		return inst.maxFrame()
	}
	return inst.MaxBody
}

func (inst Config) retry() stevedore.RetryPolicy {
	if inst.Retry.Attempts == 0 {
		return stevedore.DefaultRetryPolicy()
	}
	return inst.Retry
}
