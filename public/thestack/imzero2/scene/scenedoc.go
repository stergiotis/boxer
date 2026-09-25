package scene

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
	"gopkg.in/yaml.v3"
)

// DocSuffix is what marks a markdown file as a scene, so a directory can hold
// scenes beside ordinary documents.
const DocSuffix = ".scene.md"

// Spec is a scene's launch: what the frontmatter's `scene:` key holds
// (ADR-0248 §SD3). The document's other frontmatter keys are the repository's
// own (type, audience, status) and are not this package's to read.
type Spec struct {
	// Launch is the app alias handed to `imzero2 demo --launch`.
	Launch string `yaml:"launch"`
	// Size is the viewport, "WxH" in logical points. Empty means DefaultSize.
	Size string `yaml:"size"`
	// FPS is the headless host's frame rate; 0 means DefaultFPS.
	FPS int `yaml:"fps"`
	// Env is added to the host's environment: seed variables, focus knobs.
	Env map[string]string `yaml:"env"`
	// SQLEnv names the variable the document's first `sql` fence is passed
	// in. Empty means DefaultSQLEnv.
	SQLEnv string `yaml:"sqlEnv"`
	// Needs lists what the scene requires of the client build; "raster" is
	// the one that exists, implied by any capture.
	Needs []string `yaml:"needs"`
	// Requires lists preconditions by name (§SD6). One that does not hold
	// skips the scene.
	Requires []string `yaml:"requires"`
	// Services lists helpers the runner starts and reaps by name (§SD6).
	Services []string `yaml:"services"`
	// SettleMs is held after the carrier answers and before the first step,
	// for an app that seeds itself asynchronously. 0 means none.
	SettleMs int `yaml:"settleMs"`
	// StepSettleMs is the pause after a step that sets no settleMs of its own.
	// 0 means the runner's default. A scene tuned against one value keeps it
	// here, where a change to the runner's default cannot move it.
	StepSettleMs int `yaml:"stepSettleMs"`
}

const (
	DefaultSize   = "1400x1000"
	DefaultFPS    = 30
	DefaultSQLEnv = "BOXER_PLAY_SQL"
)

// Doc is one parsed scene document.
type Doc struct {
	// Name is the file's base name without DocSuffix; captures and logs are
	// named after it.
	Name string
	Path string
	Spec Spec
	// Prose is the document body with its fences removed — the gallery entry.
	Prose string
	// SQL is the first role-less `sql` fence, or empty.
	SQL string
	// Trace is the `jsonl trace` fence as written, for the gallery.
	Trace string
	Steps []carrierclient.Step
}

// Dimensions parses Size.
func (inst Spec) Dimensions() (w, h int, err error) {
	size := inst.Size
	if size == "" {
		size = DefaultSize
	}
	ws, hs, ok := strings.Cut(strings.ToLower(size), "x")
	if ok {
		w, _ = strconv.Atoi(ws)
		h, _ = strconv.Atoi(hs)
	}
	if w <= 0 || h <= 0 {
		return 0, 0, eb.Build().Str("size", size).Errorf("scene size must be WxH")
	}
	return w, h, nil
}

type fence struct {
	lang string
	role string
	text string
}

// splitDoc separates frontmatter, fences and prose. Fences open with three
// backticks at the start of a line and close with a bare three-backtick line,
// which is the applet book's rule too.
func splitDoc(src []byte) (front []byte, fences []fence, prose string) {
	lines := strings.Split(string(src), "\n")
	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for j := 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "---" {
				front = []byte(strings.Join(lines[1:j], "\n"))
				i = j + 1
				break
			}
		}
	}
	var cur *fence
	var body, text []string
	for ; i < len(lines); i++ {
		line := lines[i]
		switch {
		case cur == nil && strings.HasPrefix(line, "```"):
			info := strings.Fields(strings.TrimPrefix(line, "```"))
			cur = &fence{}
			if len(info) > 0 {
				cur.lang = info[0]
			}
			if len(info) > 1 {
				cur.role = info[1]
			}
			body = body[:0]
		case cur != nil && strings.TrimSpace(line) == "```":
			cur.text = strings.Join(body, "\n")
			fences = append(fences, *cur)
			cur = nil
		case cur != nil:
			body = append(body, line)
		case strings.HasPrefix(line, "> **Status:"):
			// The repository's draft banner: required of the file, and noise
			// in a gallery entry.
		default:
			text = append(text, line)
		}
	}
	return front, fences, strings.TrimSpace(strings.Join(text, "\n"))
}

// ParseDoc parses one scene document.
func ParseDoc(path string, src []byte) (doc *Doc, err error) {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, DocSuffix) {
		return nil, eb.Build().Str("path", path).Errorf("a scene document is named *" + DocSuffix)
	}
	doc = &Doc{Name: strings.TrimSuffix(base, DocSuffix), Path: path}
	front, fences, prose := splitDoc(src)
	doc.Prose = prose
	if front == nil {
		return nil, eb.Build().Str("path", path).Errorf("scene document has no frontmatter")
	}
	var fm struct {
		Scene yaml.Node `yaml:"scene"`
	}
	if err = yaml.Unmarshal(front, &fm); err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to parse the frontmatter: %w", err)
	}
	if fm.Scene.Kind == 0 {
		return nil, eb.Build().Str("path", path).Errorf("frontmatter has no `scene:` key")
	}
	// Strict inside `scene:` only: a misspelt launch key should not silently
	// launch the default, and the keys outside it are not this parser's.
	var raw bytes.Buffer
	enc := yaml.NewEncoder(&raw)
	if err = enc.Encode(&fm.Scene); err == nil {
		err = enc.Close()
	}
	if err != nil {
		return nil, eh.Errorf("unable to re-encode the scene spec: %w", err)
	}
	dec := yaml.NewDecoder(&raw)
	dec.KnownFields(true)
	if err = dec.Decode(&doc.Spec); err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to parse the `scene:` spec: %w", err)
	}
	if doc.Spec.Launch == "" {
		return nil, eb.Build().Str("path", path).Errorf("scene spec needs `launch`")
	}
	if _, _, err = doc.Spec.Dimensions(); err != nil {
		return nil, err
	}
	for _, f := range fences {
		switch {
		case f.lang == "sql" && f.role == "" && doc.SQL == "":
			doc.SQL = strings.TrimSpace(f.text)
		case f.lang == "jsonl" && f.role == "trace":
			if doc.Trace != "" {
				return nil, eb.Build().Str("path", path).Errorf("more than one `jsonl trace` fence")
			}
			doc.Trace = strings.TrimSpace(f.text)
		}
	}
	if doc.Trace == "" {
		// A scene with no trace is a picture of the launch state.
		doc.Steps = []carrierclient.Step{{Do: "capture", Text: doc.Name}}
		return doc, nil
	}
	if doc.Steps, err = carrierclient.ParseTrace(strings.NewReader(doc.Trace)); err != nil {
		return nil, eb.Build().Str("path", path).Errorf("unable to parse the trace fence: %w", err)
	}
	return doc, nil
}

// Captures lists the file names (without extension) the scene's trace writes.
func (inst *Doc) Captures() (names []string) {
	for _, st := range inst.Steps {
		if st.Do == "capture" && st.Text != "" {
			names = append(names, st.Text)
		}
	}
	return names
}

// CaptureFiles lists every file the scene's trace writes, relative to the
// output directory: each capture's PNG and then the sidecars its step asks
// for (ADR-0257 (proposed) §SD5), in trace order.
func (inst *Doc) CaptureFiles() (files []CaptureFile) {
	for _, st := range inst.Steps {
		if st.Do != "capture" || st.Text == "" {
			continue
		}
		f := CaptureFile{PNG: carrierclient.SidecarFile(st.Text, "")}
		for _, sc := range st.Sidecars {
			f.Sidecars = append(f.Sidecars, carrierclient.SidecarFile(st.Text, sc))
		}
		files = append(files, f)
	}
	return files
}

// CaptureFile is one capture's PNG and the sidecar files written beside it.
type CaptureFile struct {
	PNG      string
	Sidecars []string
}

// Title is the document's first heading, or its name when it has none.
func (inst *Doc) Title() string {
	for _, line := range strings.Split(inst.Prose, "\n") {
		if t, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(t)
		}
	}
	return inst.Name
}
