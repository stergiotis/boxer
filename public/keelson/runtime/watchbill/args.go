package watchbill

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Args on a job are a vocabulary kind with a generated codec, or they are
// not representable (ADR-0223 §SD1, ADR-0135 §SD2). These two helpers keep
// that by construction: T must be a DTO the keelson codec generator
// produced, and its kind name is read from the codec rather than typed by
// the caller, so a job can never claim a kind its bytes are not.

// WithArgs sets req.Args and req.ArgsKind from v. It is River's JobArgs
// with the kind taken from the codec.
func WithArgs[T any](req Request, v T) (out Request, err error) {
	out = req
	kind, err := kindOf[T]()
	if err != nil {
		return
	}
	if out.Args, err = buscodec.Encode(v); err != nil {
		return out, eb.Build().Str("kind", kind).Errorf("encode args: %w", err)
	}
	out.ArgsKind = kind
	return
}

// ArgsOf decodes job.Args as T, refusing a job whose ArgsKind is not T's
// kind rather than decoding bytes that claim something else.
func ArgsOf[T any](job watchbillstore.Job) (v T, err error) {
	kind, err := kindOf[T]()
	if err != nil {
		return
	}
	if job.ArgsKind != kind {
		return v, eb.Build().Str("id", job.ID).Str("argsKind", job.ArgsKind).Str("want", kind).Errorf("watchbill: args claim another kind")
	}
	if v, err = buscodec.Decode[T](job.Args); err != nil {
		err = eb.Build().Str("id", job.ID).Str("kind", kind).Errorf("decode args: %w", err)
	}
	return
}

// kindOf reads T's vocabulary kind from its registered codec's content
// type, `application/x-runtime-facts+rb;kind=<name>`.
func kindOf[T any]() (kind string, err error) {
	codec := buscodec.Lookup[T]()
	if codec == nil {
		return "", eh.Errorf("watchbill: no codec is registered for the args type")
	}
	const marker = ";kind="
	ct := codec.ContentType()
	i := strings.Index(ct, marker)
	if i < 0 {
		return "", eb.Build().Str("contentType", ct).Errorf("watchbill: the args codec names no kind; args must be a generated facts DTO")
	}
	kind = ct[i+len(marker):]
	if j := strings.IndexByte(kind, ';'); j >= 0 {
		kind = kind[:j]
	}
	return
}
