package gloss

import "github.com/dustin/go-humanize"

// MediaTypeBytes shows a byte count in binary units (KiB, MiB, …), which is
// what the count is; a negative value is not a size and shows as-is in the
// error tone.
const MediaTypeBytes = "gloss/bytes"

func bytesGloss() GlossI {
	return &simpleGloss{
		mediaType: MediaTypeBytes,
		doc:       "a byte count in binary units (KiB, MiB, …)",
		accepts:   numericOnly,
		inline: func(cell CellI) Inline {
			v, ok := cell.Float64()
			if !ok {
				return Inline{Text: cell.Text()}
			}
			if v < 0 {
				return Inline{Text: cell.Text(), Tone: ToneError}
			}
			return Inline{Text: humanize.IBytes(uint64(v))}
		},
	}
}
