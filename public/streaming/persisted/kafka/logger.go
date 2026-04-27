//go:build llm_generated_opus47

// Copyright 2024 Redpanda Data, Inc.
// Copyright 2026 Panos Stergiotis
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Modifications applied during port (see ./NOTICE):
//   - Replaced *service.Logger with *zerolog.Logger.
//   - Preserved upstream's deliberate level inversion: kgo Info →
//     zerolog Debug, kgo Debug → zerolog Trace. franz-go's Info level
//     contains per-fetch / per-produce records that would drown out
//     application-level Debug logs at default verbosity.
//   - Added structured key/value attachment via zerolog's Interface
//     fields (upstream chained service.Logger.With(keyvals...) which
//     was logger-API-specific).
//   - nil logger is silently a no-op.

package kafka

import (
	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
)

// KGoLogger adapts a *zerolog.Logger to the franz-go kgo.Logger
// interface so franz-go's internal events are routed through the
// application's logger.
type KGoLogger struct {
	logger *zerolog.Logger
}

// NewKGoLogger constructs a KGoLogger wrapping the given zerolog logger.
// A nil logger produces a logger that silently discards all output;
// callers that want a no-op logger may pass nil instead of constructing
// a discard-logger explicitly.
func NewKGoLogger(logger *zerolog.Logger) (out *KGoLogger) {
	out = &KGoLogger{logger: logger}
	return
}

// Level returns the maximum log level emitted by this logger; franz-go
// uses it to short-circuit verbose log construction at the call site.
func (inst *KGoLogger) Level() (level kgo.LogLevel) {
	level = kgo.LogLevelDebug
	return
}

// Log emits a single log line at the given franz-go log level. The
// keyvals are alternating string key / arbitrary value pairs; an odd
// trailing value is dropped, and non-string keys are skipped.
func (inst *KGoLogger) Log(level kgo.LogLevel, msg string, keyvals ...any) {
	if inst.logger == nil {
		return
	}
	var event *zerolog.Event
	switch level {
	case kgo.LogLevelError:
		event = inst.logger.Error()
	case kgo.LogLevelWarn:
		event = inst.logger.Warn()
	case kgo.LogLevelInfo:
		event = inst.logger.Debug()
	case kgo.LogLevelDebug:
		event = inst.logger.Trace()
	default:
		return
	}
	for i := 0; i+1 < len(keyvals); i += 2 {
		key, ok := keyvals[i].(string)
		if !ok {
			continue
		}
		event = event.Interface(key, keyvals[i+1])
	}
	event.Msg(msg)
}

var _ kgo.Logger = (*KGoLogger)(nil)
