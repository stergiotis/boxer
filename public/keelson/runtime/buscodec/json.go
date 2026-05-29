//go:build llm_generated_opus47

package buscodec

import (
	"encoding/json"
)

// jsonCodec is the debug/replay fallback. Not the default — opt in via
// SetDefault(NewJSON()) at init time when a human reader needs to inspect
// captured payloads.
type jsonCodec struct{}

// NewJSON constructs the JSON fallback codec.
func NewJSON() (c CodecI) {
	c = &jsonCodec{}
	return
}

var _ CodecI = (*jsonCodec)(nil)

func (inst *jsonCodec) Name() (n string) {
	n = "json"
	return
}

func (inst *jsonCodec) ContentType() (ct string) {
	ct = "application/json"
	return
}

func (inst *jsonCodec) Encode(v any) (b []byte, err error) {
	b, err = json.Marshal(v)
	return
}

func (inst *jsonCodec) Decode(b []byte, v any) (err error) {
	err = json.Unmarshal(b, v)
	return
}
