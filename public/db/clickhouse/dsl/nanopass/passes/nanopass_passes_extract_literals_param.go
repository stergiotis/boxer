//go:build llm_generated_opus46

package passes

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// cborEncMode is a pinned canonical CBOR encoding mode for deterministic output.
var cborEncMode cbor.EncMode

func init() {
	var initErr error
	cborEncMode, initErr = cbor.CanonicalEncOptions().EncMode()
	if initErr != nil {
		panic(fmt.Sprintf("passes: failed to create CBOR encoder: %v", initErr))
	}
}

// ParamMetadata holds the structured metadata encoded into parameter names.
// It is serialized as CBOR and hex-encoded into the parameter name suffix.
type ParamMetadata struct {
	// ArgIndex is the argument position within the function/operator context.
	ArgIndex uint32 `cbor:"a"`

	// ContentHash is the xxhash3 of the literal text (0 if sequential naming).
	ContentHash uint64 `cbor:"h,omitempty"`

	// HashCollisionCounter is the collision suffix (0 if no collision, 2+ if collision).
	HashCollisionCounter uint8 `cbor:"c,omitempty"`

	// CastTypeCanonical is the canonical type string from an explicit cast (e.g. "u64", "u64h", "u8-s").
	// Empty if no cast was present.
	CastTypeCanonical string `cbor:"t,omitempty"`

	// SequentialIndex is the sequential counter (only used when IsSequential is true).
	SequentialIndex uint32 `cbor:"s,omitempty"`

	// IsSequential indicates that sequential naming was used instead of content hashing.
	IsSequential bool `cbor:"q,omitempty"`
}

// EncodeParamMetadata serializes ParamMetadata to a hex string suitable for use in parameter names.
func EncodeParamMetadata(meta *ParamMetadata) (encoded string, err error) {
	data, marshalErr := cborEncMode.Marshal(meta)
	if marshalErr != nil {
		err = eh.Errorf("EncodeParamMetadata: %w", marshalErr)
		return
	}
	encoded = hex.EncodeToString(data)
	return
}

// DecodeParamMetadata deserializes a hex string back into ParamMetadata.
func DecodeParamMetadata(encoded string) (meta ParamMetadata, err error) {
	data, decodeErr := hex.DecodeString(encoded)
	if decodeErr != nil {
		err = eh.Errorf("DecodeParamMetadata: invalid hex: %w", decodeErr)
		return
	}
	unmarshalErr := cbor.Unmarshal(data, &meta)
	if unmarshalErr != nil {
		err = eh.Errorf("DecodeParamMetadata: invalid cbor: %w", unmarshalErr)
		return
	}
	return
}

// BuildParamName constructs a parameter name from the context and metadata.
// Format: <prefix>_<context>_<hex(cbor(metadata))>
func BuildParamName(prefix string, contextName string, meta *ParamMetadata) (name string, err error) {
	encoded, encodeErr := EncodeParamMetadata(meta)
	if encodeErr != nil {
		err = eh.Errorf("BuildParamName: %w", encodeErr)
		return
	}
	name = fmt.Sprintf("%s_%s_%s", prefix, sanitizeName(contextName), encoded)
	return
}

// ParseParamName extracts the context name and metadata from a parameter name.
// Format: <prefix>_<context>_<hex(cbor(metadata))>
func ParseParamName(name string, prefix string) (contextName string, meta ParamMetadata, err error) {
	if !strings.HasPrefix(name, prefix+"_") {
		err = eh.Errorf("ParseParamName: name %q does not start with prefix %q", name, prefix)
		return
	}

	remainder := name[len(prefix)+1:]

	// The last underscore-separated part is the hex-encoded metadata.
	// Everything before it (joined by _) is the context name.
	lastUnderscore := strings.LastIndex(remainder, "_")
	if lastUnderscore < 0 {
		err = eh.Errorf("ParseParamName: name %q has no metadata suffix", name)
		return
	}

	contextName = remainder[:lastUnderscore]
	hexPart := remainder[lastUnderscore+1:]

	meta, err = DecodeParamMetadata(hexPart)
	if err != nil {
		err = eh.Errorf("ParseParamName: %w", err)
	}
	return
}
