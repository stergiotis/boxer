package tally

import (
	"context"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/fs/lading/ladingview"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// previewKindE is how a file is shown in the Preview pane.
type previewKindE uint8

const (
	previewKindNone previewKindE = iota
	previewKindText
	previewKindMarkdown
	previewKindJSON
	previewKindSQL
	previewKindGo
	previewKindImage
	previewKindHex
	previewKindTooLarge
	previewKindError
)

const (
	// previewMaxBytes bounds a text preview; above it the file shows a hex
	// head and its size. It is below the store's default inline threshold on
	// purpose: a preview is a look, not a load.
	previewMaxBytes = 1 << 20
	// previewHexBytes is how much of a binary file the hex dump shows.
	previewHexBytes = 4096
	// previewSniffBytes is how much of a file decides whether it is text.
	previewSniffBytes = 8192
	previewImageMaxW  = 900
	previewImageMaxH  = 600
	// previewImageMaxBytes bounds the encoded image read for a preview.
	previewImageMaxBytes = 32 << 20
)

// previewContent is what the preview lane produces for one file.
type previewContent struct {
	kind   previewKindE
	path   string
	size   int64
	note   string
	job    typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	doc    *markdown.Doc
	pixels []uint32
	imgW   uint32
	imgH   uint32
}

// loadPreview reads one file through the view and classifies it. Off the
// render thread: every adapter call is a query. The read goes through
// ladingview so this package handles bytes, never file handles.
func loadPreview(ctx context.Context, fsys fs.FS, p string) (out previewContent, err error) {
	out.path = p
	kind := classifyByName(p)
	budget := int64(previewMaxBytes)
	if kind == previewKindImage {
		// An encoded image is read whole up to previewImageMaxBytes; the
		// decoder then bounds the pixels it will allocate for it.
		budget = previewImageMaxBytes
	}
	head, err := ladingview.ReadHead(fsys, p, budget, previewHexBytes)
	if err != nil {
		return
	}
	out.size = head.Size
	if head.IsDir {
		out.kind = previewKindNone
		out.note = "a directory"
		return
	}
	if ctx.Err() != nil {
		err = ctx.Err()
		return
	}
	if head.Truncated {
		out.kind = previewKindTooLarge
		out.note = fmt.Sprintf("%s is over the %s preview limit; first %d bytes:", humanSize(out.size), humanSize(previewMaxBytes), len(head.Data))
		out.job = c.CodeViewJob(hexDump(head.Data)).Keep()
		return
	}
	data := head.Data
	if kind == previewKindNone {
		if looksText(data) {
			kind = previewKindText
		} else {
			kind = previewKindHex
		}
	}
	out.kind = kind
	switch kind {
	case previewKindMarkdown:
		out.doc = markdown.Parse(data)
	case previewKindJSON:
		out.job = codeview.BuildJson(utfsafe.EnsureUTF8(string(data)))
	case previewKindSQL:
		out.job = codeview.BuildSql(utfsafe.EnsureUTF8(string(data)))
	case previewKindGo:
		out.job = codeview.BuildGo(utfsafe.EnsureUTF8(string(data)))
	case previewKindImage:
		pixels, w, h, derr := imagedecode.DecodeRGBA8(data, imagedecode.DefaultMaxPixels)
		if derr != nil {
			out.kind = previewKindError
			out.note = derr.Error()
			return
		}
		out.pixels, out.imgW, out.imgH = pixels, w, h
	case previewKindHex:
		n := min(len(data), previewHexBytes)
		out.note = fmt.Sprintf("binary, %s; first %d bytes:", humanSize(out.size), n)
		out.job = c.CodeViewJob(hexDump(data[:n])).Keep()
	default:
		out.job = c.CodeViewJob(utfsafe.EnsureUTF8(string(data))).Keep()
	}
	return
}

// classifyByName picks a preview kind from the extension; previewKindNone
// means "decide from the bytes".
func classifyByName(p string) previewKindE {
	switch strings.ToLower(path.Ext(p)) {
	case ".md", ".markdown":
		return previewKindMarkdown
	case ".json":
		return previewKindJSON
	case ".sql":
		return previewKindSQL
	case ".go":
		return previewKindGo
	case ".png", ".jpg", ".jpeg", ".gif":
		return previewKindImage
	}
	return previewKindNone
}

// looksText is the store's own text rule (ADR-0198 M2, TextSniff): the first
// 8 KiB decode as UTF-8 and carry no NUL.
func looksText(data []byte) bool {
	n := min(len(data), previewSniffBytes)
	head := data[:n]
	for _, b := range head {
		if b == 0 {
			return false
		}
	}
	if utf8.Valid(head) {
		return true
	}
	// A cut in the middle of a multi-byte sequence at the sniff boundary is
	// not a verdict; check everything but the last rune's worth.
	if n == previewSniffBytes && n > 4 {
		return utf8.Valid(head[:n-4])
	}
	return false
}

// hexDump is the classic 16-bytes-per-line dump: offset, hex, printable ASCII.
func hexDump(data []byte) string {
	var sb strings.Builder
	sb.Grow(len(data) * 4)
	for off := 0; off < len(data); off += 16 {
		end := min(off+16, len(data))
		line := data[off:end]
		fmt.Fprintf(&sb, "%08x  ", off)
		hx := hex.EncodeToString(line)
		for i := 0; i < 32; i += 2 {
			if i < len(hx) {
				sb.WriteString(hx[i : i+2])
			} else {
				sb.WriteString("  ")
			}
			sb.WriteByte(' ')
			if i == 14 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(" |")
		for _, b := range line {
			if b >= 0x20 && b < 0x7f {
				sb.WriteByte(b)
			} else {
				sb.WriteByte('.')
			}
		}
		sb.WriteString("|\n")
	}
	return sb.String()
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit && exp < 5; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
