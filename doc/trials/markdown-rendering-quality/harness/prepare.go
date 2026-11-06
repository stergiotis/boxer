//go:build integration

// Package mdquality is the harness of the markdown-rendering-quality trial
// (../README.md). It prepares the arms' inputs, reads their captures back
// into one line model, computes the §4 metrics and builds the blind voting
// page. It is driven by ../measure.sh through `go test`, the way the
// map-tile-addressing harness is; without MDQ_WORK_DIR the tests skip.
package mdquality

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	idsweb "github.com/stergiotis/boxer/public/keelson/designsystem/web"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/yuin/goldmark/parser"
)

//go:embed geometry.js
var geometryJS string

//go:embed vendor/github-markdown-light.css
var githubCSS string

// corpusEntry is one document of the corpus (README §2): where it lives in
// the tree and the slug every file derived from it carries.
type corpusEntry struct {
	Slug string
	Path string
}

// corpus is the README §2 corpus, in the order the voting page shows it.
var corpus = []corpusEntry{
	{Slug: "play-overview", Path: "apps/play/help/overview.md"},
	{Slug: "mdedit-blocks", Path: "apps/mdedit/help/blocks.md"},
	{Slug: "keelson-positioning", Path: "doc/explanation/positioning/keelson.md"},
	{Slug: "why-boxer", Path: "doc/explanation/why-boxer.md"},
	{Slug: "adr-0334", Path: "doc/adr/0334-mdedit-blocks-over-the-source.md"},
	{Slug: "stress", Path: "doc/trials/markdown-rendering-quality/harness/corpus/stress.md"},
}

// The common viewport (README §3): logical points for the native host, CSS
// pixels for the browsers, both at a device scale of one.
const (
	viewportW = 1000
	viewportH = 1400
)

// styleE names one HTML arm's stylesheet.
type styleE string

const (
	styleIDS      styleE = "ids"
	styleObsidian styleE = "obsidian"
	styleGitHub   styleE = "github"
)

var styles = []styleE{styleIDS, styleObsidian, styleGitHub}

// Font files pinned into the IDS arm, copied beside the pages so that a
// file:// page may load them under either browser's file-origin policy.
// They are the faces the scene runner hands the headless host
// (scene/launch.go: Noto Sans as the main font, DejaVu Sans Mono as the
// mono font, both resolved from the system), not the IDS token faces the
// desktop build embeds — the comparison pins what the native arm draws
// with. Absolute paths are looked up on the system; relative ones in the
// checkout.
var pinnedFonts = []string{
	"/usr/share/fonts/truetype/noto/NotoSans-Regular.ttf",
	"/usr/share/fonts/truetype/noto/NotoSans-Bold.ttf",
	"/usr/share/fonts/truetype/noto/NotoSans-Italic.ttf",
	"/usr/share/fonts/truetype/noto/NotoSans-BoldItalic.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
	"/usr/share/fonts/truetype/dejavu/DejaVuSansMono-Bold.ttf",
}

// prepare writes, under work: html/<slug>-<style>.html for every corpus
// document and style, scenes/<slug>.scene.md for the native arm, and
// fonts/ with the pinned faces.
func prepare(root, work string) (err error) {
	for _, d := range []string{"html", "scenes", "fonts", "captures", "typst-src"} {
		if err = os.MkdirAll(filepath.Join(work, d), 0o755); err != nil {
			return eb.Build().Str("dir", d).Errorf("unable to create the work directory: %w", err)
		}
	}
	for _, f := range pinnedFonts {
		var b []byte
		src := f
		if !filepath.IsAbs(src) {
			src = filepath.Join(root, f)
		}
		if b, err = os.ReadFile(src); err != nil {
			return eb.Build().Str("font", f).Errorf("unable to read a pinned font: %w", err)
		}
		if err = os.WriteFile(filepath.Join(work, "fonts", filepath.Base(f)), b, 0o644); err != nil {
			return eb.Build().Str("font", f).Errorf("unable to copy a pinned font: %w", err)
		}
	}
	for _, e := range corpus {
		var src []byte
		if src, err = os.ReadFile(filepath.Join(root, e.Path)); err != nil {
			return eb.Build().Str("path", e.Path).Errorf("unable to read a corpus document: %w", err)
		}
		var body []byte
		if body, err = toHTML(src); err != nil {
			return eb.Build().Str("path", e.Path).Errorf("unable to render a corpus document: %w", err)
		}
		for _, s := range styles {
			p := filepath.Join(work, "html", e.Slug+"-"+string(s)+".html")
			if err = os.WriteFile(p, []byte(page(s, e.Slug, body)), 0o644); err != nil {
				return eb.Build().Str("path", p).Errorf("unable to write an HTML arm: %w", err)
			}
		}
		// The typst arms' input: the document's body without its
		// frontmatter, as the HTML arms and mdedit's reading mode take it,
		// under the slug the capture names use (measure.sh, the typst step).
		p := filepath.Join(work, "typst-src", e.Slug+".md")
		if err = os.WriteFile(p, stripFrontmatter(src), 0o644); err != nil {
			return eb.Build().Str("path", p).Errorf("unable to write a typst source: %w", err)
		}
		p = filepath.Join(work, "scenes", e.Slug+".scene.md")
		var sc string
		if sc, err = sceneDoc(e.Slug, src); err != nil {
			return err
		}
		if err = os.WriteFile(p, []byte(sc), 0o644); err != nil {
			return eb.Build().Str("path", p).Errorf("unable to write a scene: %w", err)
		}
	}
	return
}

// toHTML renders the document body the way the obsidian package's consumers
// do: every wired feature on, frontmatter parsed and left out of the body,
// which is what mdedit's reading mode shows too.
func toHTML(src []byte) (body []byte, err error) {
	md := obsidian.New(obsidian.Options{Features: obsidian.FeatureAll})
	pc := obsidian.NewParserContext()
	var buf bytes.Buffer
	if err = md.Convert(src, &buf, parser.WithContext(pc)); err != nil {
		return nil, eh.Errorf("unable to convert markdown to HTML: %w", err)
	}
	body = buf.Bytes()
	return
}

// page wraps a rendered body in a complete document for one style. The
// IDS arm pins the faces the native arm draws with (README §3, a decision
// of the design dialogue); the Obsidian arm gets the reading column the stylesheet leaves
// to its host, at Obsidian's default readable line width; the GitHub arm
// gets the container its README prescribes.
func page(s styleE, title string, body []byte) (html string) {
	var css, open, closeTag string
	switch s {
	case styleIDS:
		css = idsweb.Stylesheet() + `
@font-face { font-family: "Pinned Sans"; src: url("../fonts/NotoSans-Regular.ttf") format("truetype"); font-weight: normal; font-style: normal; }
@font-face { font-family: "Pinned Sans"; src: url("../fonts/NotoSans-Bold.ttf") format("truetype"); font-weight: bold; font-style: normal; }
@font-face { font-family: "Pinned Sans"; src: url("../fonts/NotoSans-Italic.ttf") format("truetype"); font-weight: normal; font-style: italic; }
@font-face { font-family: "Pinned Sans"; src: url("../fonts/NotoSans-BoldItalic.ttf") format("truetype"); font-weight: bold; font-style: italic; }
@font-face { font-family: "Pinned Mono"; src: url("../fonts/DejaVuSansMono.ttf") format("truetype"); font-weight: normal; }
@font-face { font-family: "Pinned Mono"; src: url("../fonts/DejaVuSansMono-Bold.ttf") format("truetype"); font-weight: bold; }
:root { --ids-font-sans: "Pinned Sans", sans-serif; --ids-font-mono: "Pinned Mono", monospace; }
`
	case styleObsidian:
		css = obsidian.DefaultStylesheet() + `
body { max-width: 700px; margin: 0 auto; padding: 32px; }
`
	case styleGitHub:
		css = githubCSS + `
.markdown-body { box-sizing: border-box; min-width: 200px; max-width: 980px; margin: 0 auto; padding: 45px; }
@media (max-width: 767px) { .markdown-body { padding: 15px; } }
`
		open, closeTag = `<article class="markdown-body">`, `</article>`
	}
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>" + title + " — " + string(s) + "</title>\n<style>\n")
	b.WriteString(css)
	b.WriteString("\n</style>\n</head>\n<body>\n")
	b.WriteString(open)
	b.Write(body)
	b.WriteString(closeTag)
	b.WriteString("\n<script>\n")
	b.WriteString(geometryJS)
	b.WriteString("\n</script>\n</body>\n</html>\n")
	html = b.String()
	return
}

// sceneDoc is the native arm's scene (ADR-0248): mdedit opened on the
// document through its seed variable on a tiled desktop, so the one window
// fills the viewport; the caret sent to the top, which the preview follows;
// the reading mode; and a capture with the SVG and tree sidecars the
// metrics read.
func sceneDoc(slug string, src []byte) (doc string, err error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err = enc.Encode(string(src)); err != nil {
		return "", eh.Errorf("unable to encode the document for the scene's environment: %w", err)
	}
	quoted := strings.TrimSpace(buf.String())
	doc = fmt.Sprintf(`---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: %dx%d
  env:
    IMZERO2_DESKTOP_LAYOUT: tile
    BOXER_MDEDIT_DOC: %s
---

# markdown-rendering-quality — native arm — %s

Generated by the trial harness; the protocol is doc/trials/markdown-rendering-quality/README.md.

`+"```jsonl trace"+`
{"do":"note","text":"native arm: mdedit's reading mode over the corpus document %s"}
{"do":"wait","role":"multiline_text_input","settleMs":800,"comment":"the window is up in Split, tiled to the whole desktop; the source pane holds the seeded document"}
{"do":"focus","role":"multiline_text_input","settleMs":300,"comment":"focus the source pane"}
{"do":"key","text":"Home","modifiers":2,"settleMs":600,"comment":"Ctrl+Home: the caret to the top, which the preview's scroll follows at heading granularity"}
{"do":"click","contains":"Read","role":"button","settleMs":1000,"comment":"the reading mode: the preview alone, full width"}
%s{"do":"capture","text":"%s-native","sidecars":["svg","tree"],"comment":"the first viewport of the document"}
`+"```"+`
`, viewportW, viewportH, quoted, slug, slug, scrollTopStep(src), slug)
	return
}

// scrollTopStep is the step that brings the reading mode's scroll to the
// document's start: scroll its first heading into view. The preview
// otherwise keeps whatever offset the source caret last asked for. Empty
// when the document has no ATX heading to anchor on.
func scrollTopStep(src []byte) (step string) {
	for _, l := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(l, "# ") {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			_ = enc.Encode(strings.TrimSpace(strings.TrimPrefix(l, "# ")))
			step = `{"do":"scroll_into_view","value":` + strings.TrimSpace(buf.String()) + `,"role":"label","nth":0,"settleMs":800,"comment":"the first heading at the top of the viewport"}` + "\n"
			return
		}
	}
	return
}

// stripFrontmatter drops a leading YAML block, which pandoc would otherwise
// render as a title block; the HTML arms parse it out the same way.
func stripFrontmatter(src []byte) (body []byte) {
	body = src
	if !bytes.HasPrefix(src, []byte("---\n")) {
		return
	}
	rest := src[4:]
	i := bytes.Index(rest, []byte("\n---\n"))
	if i < 0 {
		return
	}
	body = rest[i+5:]
	return
}
