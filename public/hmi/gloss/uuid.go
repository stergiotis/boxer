package gloss

import (
	"encoding/hex"
	"strings"
)

// MediaTypeUUID shows a UUID the way it is written: `8-4-4-4-12` hex digits.
// An Arrow result carries one as 16 packed bytes, which the grids hex-encode
// as one 32-digit run. Unlike an address, a UUID column does say what it
// holds: ClickHouse tags it with the canonical `arrow.uuid` extension, and the
// host spells that into the spec line's `arrow:` token as
// `extension<arrow.uuid>` — the spelling arrow-go gives the registered type —
// which is what the affinity keys on.
const MediaTypeUUID = "gloss/uuid"

func uuidGloss() GlossI {
	return &simpleGloss{
		mediaType:  MediaTypeUUID,
		doc:        "a UUID written out — 16 packed bytes or UUID text",
		affinities: []string{`\barrow:extension<arrow\.uuid>`},
		accepts:    []ValueKindE{ValueKindText, ValueKindBytes},
		inline:     uuidFace,
	}
}

// uuidFace reads text by shape, and only a bytes cell by width: sixteen
// characters of text are sixteen characters, not a packed UUID. Text is tried
// first on a bytes cell too, for the reason ipAddrFace gives — a lane that
// already wrote the value out may hand it over as bytes. A value that is
// neither keeps its plain rendering in the error tone.
func uuidFace(cell CellI) Inline {
	bytesKind := cell.Kind() == ValueKindBytes
	raw := cell.Text()
	if bytesKind {
		raw, _ = cell.Raw()
	}
	if raw == "" {
		return Inline{}
	}
	if s := uuidText(raw); s != "" {
		return Inline{Text: s}
	}
	if bytesKind && len(raw) == 16 {
		return Inline{Text: uuidPacked(raw)}
	}
	return Inline{Text: cell.Text(), Tone: ToneError}
}

// uuidText canonicalises a UUID already written out — lower case, hyphenated;
// empty when raw is not one.
func uuidText(raw string) string {
	if len(raw) != 36 || raw[8] != '-' || raw[13] != '-' || raw[18] != '-' || raw[23] != '-' {
		return ""
	}
	digits := raw[:8] + raw[9:13] + raw[14:18] + raw[19:23] + raw[24:]
	if _, err := hex.DecodeString(digits); err != nil {
		return ""
	}
	return strings.ToLower(raw)
}

// uuidPacked writes 16 bytes out in RFC 9562 order — the order ClickHouse
// sends `arrow.uuid` in, so the text matches what the server prints.
func uuidPacked(raw string) string {
	h := hex.EncodeToString([]byte(raw))
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
