package fieldview

import (
	"fmt"
	"time"
)

// formatField renders a leaf Field's active-slot value as plain
// text. Container kinds are not formatted here — the Renderer
// dispatches them through the CollapsingHeader path before this is
// called. Empty / unknown kinds fall back to Str so a malformed
// field still shows something the operator can act on.
//
// bytesMax bounds the hex-dump of Bytes values so a 1 MiB blob
// doesn't blow the panel; values past the limit get truncated with
// an "(N bytes)" suffix so the operator knows how much was elided.
// A bytesMax of 0 disables truncation entirely (full hex dump).
func formatField(f Field, bytesMax int) (s string) {
	switch f.Kind {
	case KindString:
		s = f.Str
	case KindInt:
		s = fmt.Sprintf("%d", f.Int)
	case KindUint:
		s = fmt.Sprintf("%d", f.Uint)
	case KindFloat:
		s = fmt.Sprintf("%g", f.Float)
	case KindBool:
		s = fmt.Sprintf("%t", f.Bool)
	case KindBytes:
		if bytesMax > 0 && len(f.Bytes) > bytesMax {
			s = fmt.Sprintf("%x… (%d bytes)", f.Bytes[:bytesMax], len(f.Bytes))
			return
		}
		s = fmt.Sprintf("%x", f.Bytes)
	case KindTime:
		s = f.Time.UTC().Format(time.RFC3339Nano)
	case KindObject, KindArray:
		// Containers shouldn't reach here, but if they do (caller
		// invoked formatField directly on a container) print a brief
		// summary so the test caller doesn't see "".
		s = fmt.Sprintf("%s(%d)", kindName(f.Kind), len(f.Children))
	default:
		s = f.Str
	}
	return
}

// kindName is a short label for the KindE — surfaced next to each
// field name so the operator knows which typed slot the value lives
// in. Future kinds added here need matching short labels; an unknown
// kind renders as "?" rather than crashing.
func kindName(k KindE) (s string) {
	switch k {
	case KindString:
		s = "str"
	case KindInt:
		s = "int"
	case KindUint:
		s = "uint"
	case KindFloat:
		s = "float"
	case KindBool:
		s = "bool"
	case KindBytes:
		s = "bytes"
	case KindTime:
		s = "time"
	case KindObject:
		s = "obj"
	case KindArray:
		s = "arr"
	default:
		s = "?"
	}
	return
}
