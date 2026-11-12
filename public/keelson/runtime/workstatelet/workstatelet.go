package workstatelet

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	// MaxConfigBytes bounds one window's decoded launch config. It is the
	// window host's cap on a launch config at the open boundary (ADR-0135
	// §SD6), restated here so a document that could never open is refused
	// when it is read; a test in windowhost holds the two equal.
	MaxConfigBytes = 64 << 10
	// MaxWindows bounds how many windows one document opens, so a stored
	// document cannot ask for more than a person would open by hand.
	MaxWindows = 32
	// WindowFenceLang and WindowFenceRole open a window's fence:
	// "```yaml window".
	WindowFenceLang = "yaml"
	WindowFenceRole = "window"
	// configLineWidth is where the writer wraps a config's base64.
	configLineWidth = 76
)

// SlugPattern is the shape of a document's slug: the file's base name in a
// book, the key of a stored document, and the tail of the minted app id.
var SlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// namePattern is the shape of an app id, a subject alias or a launch kind as
// a window names it. Each is written as a plain YAML scalar, so the charset
// leaves out everything YAML would read as syntax.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// layoutPattern is the shape of a layout ident. Which idents exist is the
// window host's to say (windowhost.ParseLayout).
var layoutPattern = regexp.MustCompile(`^[a-z][a-z-]*$`)

// Desktop is the layout a document asks for (ADR-0369 §SD1). The zero value
// asks for nothing: the desktop keeps its mode.
type Desktop struct {
	// Layout is a layout ident of the window host; empty leaves the
	// desktop's mode as it is.
	Layout string
	// MainRatio is the main column's share under the main-and-stack layout;
	// 0 leaves the host's value.
	MainRatio float32
	// MainCount is how many windows are main under that layout; 0 leaves the
	// host's value.
	MainCount int32
}

// Window is one window a document opens.
type Window struct {
	// App is the app's id or subject alias; empty when the host is to
	// resolve Kind to an app.
	App string
	// Kind is the launch kind of Config, or the kind to resolve to an app.
	Kind string
	// Config is the launch config's facts-CBOR bytes; nil opens the window
	// as a click on the app would.
	Config []byte
	// Floating takes the window out of a tiled layout.
	Floating bool
}

// Doc is one parsed workstatelet document.
type Doc struct {
	// Slug is the document's durable name.
	Slug    string
	Title   string
	Summary string
	Icon    string
	// Topics and Keywords file the launcher entry (app.Manifest).
	Topics   []app.TopicT
	Keywords []string
	Desktop  Desktop
	// Windows are in document order; the first is the main one.
	Windows []Window
	// Note is the commentary Compose writes under the frontmatter. Parse
	// leaves it empty: a parsed document keeps its text in Source.
	Note string
	// Source is the document as parsed, or as Compose wrote it.
	Source []byte
}

type desktopYaml struct {
	Layout    string  `yaml:"layout"`
	MainRatio float32 `yaml:"mainRatio,omitempty"`
	MainCount int32   `yaml:"mainCount,omitempty"`
}

// frontYaml is the frontmatter. The first five keys are the repository's
// document stanza (DOCUMENTATION_STANDARD §4), which a committed document
// carries as an applet document does; they are read so a strict decode
// accepts them and are otherwise not this package's. A capture writes none.
type frontYaml struct {
	Type         string `yaml:"type,omitempty"`
	Audience     string `yaml:"audience,omitempty"`
	Status       string `yaml:"status,omitempty"`
	ReviewedBy   string `yaml:"reviewed-by,omitempty"`
	ReviewedDate string `yaml:"reviewed-date,omitempty"`

	Title    string       `yaml:"title"`
	Summary  string       `yaml:"summary"`
	Icon     string       `yaml:"icon,omitempty"`
	Topics   []string     `yaml:"topics,flow,omitempty"`
	Keywords []string     `yaml:"keywords,flow,omitempty"`
	Desktop  *desktopYaml `yaml:"desktop,omitempty"`
}

type windowYaml struct {
	App      string `yaml:"app"`
	Kind     string `yaml:"kind"`
	Config   string `yaml:"config"`
	Floating bool   `yaml:"floating"`
}

// fence is one fenced block: the first two words of its info string and its
// text.
type fence struct {
	lang, role string
	text       string
	line       int
}

// splitDoc separates the frontmatter and the fences. A fence opens with three
// backticks at the start of a line and closes with a bare three-backtick
// line, the rule the applet book and the scene document have.
func splitDoc(src []byte) (front []byte, fences []fence, err error) {
	lines := strings.Split(string(src), "\n")
	i := 0
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		err = eh.Errorf("document has no frontmatter")
		return
	}
	end := -1
	for j := 1; j < len(lines); j++ {
		if strings.TrimRight(lines[j], "\r") == "---" {
			end = j
			break
		}
	}
	if end < 0 {
		err = eh.Errorf("frontmatter is not closed")
		return
	}
	front = []byte(strings.Join(lines[1:end], "\n"))
	i = end + 1
	for i < len(lines) {
		line := strings.TrimRight(lines[i], "\r")
		if !strings.HasPrefix(line, "```") {
			i++
			continue
		}
		words := strings.Fields(strings.TrimPrefix(line, "```"))
		f := fence{line: i + 1}
		if len(words) > 0 {
			f.lang = words[0]
		}
		if len(words) > 1 {
			f.role = words[1]
		}
		i++
		start := i
		closed := false
		for i < len(lines) {
			if strings.TrimRight(lines[i], "\r") == "```" {
				closed = true
				break
			}
			i++
		}
		if !closed {
			err = eb.Build().Int("line", f.line).Errorf("fence is not closed")
			return
		}
		f.text = strings.Join(lines[start:i], "\n")
		fences = append(fences, f)
		i++
	}
	return
}

func strictYaml(src []byte, into any) (err error) {
	dec := yaml.NewDecoder(bytes.NewReader(src))
	dec.KnownFields(true)
	err = dec.Decode(into)
	return
}

// Parse reads src as the workstatelet document named slug. defaultTopics file
// a document that names none. A document is refused as a whole: one window
// that does not read leaves no document.
func Parse(slug string, src []byte, defaultTopics []app.TopicT) (doc Doc, err error) {
	if !SlugPattern.MatchString(slug) {
		err = eb.Build().Str("slug", slug).Str("pattern", SlugPattern.String()).Errorf("slug does not match the pattern")
		return
	}
	front, fences, err := splitDoc(src)
	if err != nil {
		err = eb.Build().Str("slug", slug).Errorf("split document: %w", err)
		return
	}
	var fy frontYaml
	if err = strictYaml(front, &fy); err != nil {
		err = eb.Build().Str("slug", slug).Errorf("frontmatter: %w", err)
		return
	}
	doc = Doc{
		Slug:     slug,
		Title:    strings.TrimSpace(fy.Title),
		Summary:  strings.TrimSpace(fy.Summary),
		Icon:     fy.Icon,
		Keywords: fy.Keywords,
		Source:   src,
	}
	for _, t := range fy.Topics {
		doc.Topics = append(doc.Topics, app.TopicT(t))
	}
	if len(doc.Topics) == 0 {
		doc.Topics = append(doc.Topics, defaultTopics...)
	}
	if fy.Desktop != nil {
		doc.Desktop = Desktop{Layout: fy.Desktop.Layout, MainRatio: fy.Desktop.MainRatio, MainCount: fy.Desktop.MainCount}
	}
	for _, f := range fences {
		if f.lang != WindowFenceLang || f.role != WindowFenceRole {
			continue
		}
		var wy windowYaml
		if err = strictYaml([]byte(f.text), &wy); err != nil {
			err = eb.Build().Str("slug", slug).Int("line", f.line).Errorf("window fence: %w", err)
			return
		}
		w := Window{App: wy.App, Kind: wy.Kind, Floating: wy.Floating}
		if wy.Config != "" {
			w.Config, err = decodeConfig(wy.Config)
			if err != nil {
				err = eb.Build().Str("slug", slug).Int("line", f.line).Errorf("window config: %w", err)
				return
			}
		}
		doc.Windows = append(doc.Windows, w)
	}
	if err = doc.Validate(); err != nil {
		err = eb.Build().Str("slug", slug).Errorf("invalid document: %w", err)
	}
	return
}

// decodeConfig reads a config's base64, ignoring the whitespace a wrapped
// block scalar carries.
func decodeConfig(s string) (cfg []byte, err error) {
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, s)
	cfg, err = base64.StdEncoding.DecodeString(compact)
	if err != nil {
		err = eh.Errorf("config is not standard base64: %w", err)
		return
	}
	if len(cfg) == 0 {
		err = eh.Errorf("config decodes to no bytes")
	}
	return
}

// Validate checks what a document must hold to be minted and opened: the
// descriptor a launcher entry needs, at least one window, and windows the
// host could act on. It does not check that a layout ident, an app or a kind
// exists, nor that a config is its kind; those are the host's, at the open.
func (inst Doc) Validate() (err error) {
	if !SlugPattern.MatchString(inst.Slug) {
		err = eb.Build().Str("slug", inst.Slug).Errorf("slug does not match the pattern")
		return
	}
	if inst.Title == "" {
		err = eh.Errorf("document has no title")
		return
	}
	if inst.Summary == "" {
		err = eh.Errorf("document has no summary")
		return
	}
	if len(inst.Topics) == 0 {
		err = eh.Errorf("document names no topics")
		return
	}
	for _, t := range inst.Topics {
		if !t.IsRegistered() {
			err = eb.Build().Str("topic", string(t)).Errorf("topic is not registered")
			return
		}
	}
	d := inst.Desktop
	if d.Layout == "" && (d.MainRatio != 0 || d.MainCount != 0) {
		err = eh.Errorf("desktop parameters without a layout")
		return
	}
	if d.Layout != "" && !layoutPattern.MatchString(d.Layout) {
		err = eb.Build().Str("layout", d.Layout).Errorf("layout is not an ident")
		return
	}
	if d.MainRatio < 0 || d.MainRatio >= 1 || d.MainRatio != d.MainRatio {
		err = eb.Build().Float32("mainRatio", d.MainRatio).Errorf("mainRatio must be a share between 0 and 1")
		return
	}
	if d.MainCount < 0 {
		err = eb.Build().Int32("mainCount", d.MainCount).Errorf("mainCount must not be negative")
		return
	}
	if len(inst.Windows) == 0 {
		err = eh.Errorf("document opens no window")
		return
	}
	if len(inst.Windows) > MaxWindows {
		err = eb.Build().Int("windows", len(inst.Windows)).Int("max", MaxWindows).Errorf("document opens too many windows")
		return
	}
	for i, w := range inst.Windows {
		if err = w.validate(); err != nil {
			err = eb.Build().Int("window", i+1).Errorf("invalid window: %w", err)
			return
		}
	}
	return
}

func (inst Window) validate() (err error) {
	if inst.App == "" && inst.Kind == "" {
		err = eh.Errorf("window names neither an app nor a kind")
		return
	}
	if inst.App != "" && !namePattern.MatchString(inst.App) {
		err = eb.Build().Str("app", inst.App).Errorf("app is not an id or an alias")
		return
	}
	if inst.Kind != "" && !namePattern.MatchString(inst.Kind) {
		err = eb.Build().Str("kind", inst.Kind).Errorf("kind is not a kind name")
		return
	}
	if len(inst.Config) > 0 && inst.Kind == "" {
		err = eh.Errorf("config without a kind")
		return
	}
	if len(inst.Config) > MaxConfigBytes {
		err = eb.Build().Int("len", len(inst.Config)).Int("max", MaxConfigBytes).Errorf("config exceeds the size cap")
	}
	return
}

// Compose writes doc as a document Parse reads back to the same windows and
// descriptor, and returns doc with Source set to it. A capture and a
// committed document therefore have one shape.
func Compose(doc Doc) (out Doc, err error) {
	if err = doc.Validate(); err != nil {
		err = eh.Errorf("compose: %w", err)
		return
	}
	fy := frontYaml{Title: doc.Title, Summary: doc.Summary, Icon: doc.Icon, Keywords: doc.Keywords}
	for _, t := range doc.Topics {
		fy.Topics = append(fy.Topics, string(t))
	}
	if doc.Desktop != (Desktop{}) {
		fy.Desktop = &desktopYaml{Layout: doc.Desktop.Layout, MainRatio: doc.Desktop.MainRatio, MainCount: doc.Desktop.MainCount}
	}
	var front bytes.Buffer
	enc := yaml.NewEncoder(&front)
	enc.SetIndent(2)
	if err = enc.Encode(fy); err != nil {
		err = eh.Errorf("compose frontmatter: %w", err)
		return
	}
	if err = enc.Close(); err != nil {
		err = eh.Errorf("compose frontmatter: %w", err)
		return
	}
	var b strings.Builder
	b.WriteString("---\n")
	b.Write(front.Bytes())
	b.WriteString("---\n")
	if note := strings.TrimSpace(doc.Note); note != "" {
		b.WriteString("\n")
		b.WriteString(note)
		b.WriteString("\n")
	}
	for i, w := range doc.Windows {
		name := w.App
		if name == "" {
			name = w.Kind
		}
		b.WriteString("\n## ")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(name)
		b.WriteString("\n\n```")
		b.WriteString(WindowFenceLang)
		b.WriteString(" ")
		b.WriteString(WindowFenceRole)
		b.WriteString("\n")
		if w.App != "" {
			b.WriteString("app: ")
			b.WriteString(w.App)
			b.WriteString("\n")
		}
		if w.Kind != "" {
			b.WriteString("kind: ")
			b.WriteString(w.Kind)
			b.WriteString("\n")
		}
		if w.Floating {
			b.WriteString("floating: true\n")
		}
		if len(w.Config) > 0 {
			b.WriteString("config: |\n")
			enc64 := base64.StdEncoding.EncodeToString(w.Config)
			for len(enc64) > 0 {
				n := min(len(enc64), configLineWidth)
				b.WriteString("  ")
				b.WriteString(enc64[:n])
				b.WriteString("\n")
				enc64 = enc64[n:]
			}
		}
		b.WriteString("```\n")
	}
	out = doc
	out.Source = []byte(b.String())
	return
}

// SlugOf derives a slug from a title: lower-case letters and digits kept,
// every other run of characters one dash. ok is false when nothing is left.
func SlugOf(title string) (slug string, ok bool) {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	slug = b.String()
	ok = SlugPattern.MatchString(slug)
	return
}
