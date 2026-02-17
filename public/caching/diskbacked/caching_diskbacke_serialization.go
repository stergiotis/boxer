//go:build llm_generated_gemini3pro
package diskbacked

import "github.com/fxamacker/cbor/v2"

// EncMode for Keys: Must be Canonical (RFC 7049) to ensure deterministic hashing
// of maps/structs used as keys.
var keyEncMode, _ = cbor.CanonicalEncOptions().EncMode()
