package scene

import (
	"maps"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/carrierclient"
)

// Matrix runs one scene's trace at several viewport sizes and, launch by
// launch, in each theme (ADR-0363 M1). Each combination is a launch of its
// own, because the theme is fixed for the life of the process (ADR-0258
// §SD1). A scene without a matrix runs once, as it always has.
type Matrix struct {
	// Sizes are viewports, "WxH" as `size` takes them. A scene with sizes
	// leaves `size` empty.
	Sizes []string `yaml:"sizes"`
	// Themes are ThemeDark and ThemeLight. A scene with themes leaves
	// IMZERO2_THEME out of its env.
	Themes []ThemeE `yaml:"themes"`
}

// ThemeE is a matrix theme.
type ThemeE string

const (
	// ThemeDark is the IDS palette, the host's default.
	ThemeDark ThemeE = "dark"
	// ThemeLight is ADR-0258's light theme, which IMZERO2_THEME calls fresh.
	ThemeLight ThemeE = "light"
)

var AllThemes = []ThemeE{ThemeDark, ThemeLight}

// envValue is the IMZERO2_THEME value that selects the theme. Dark is set
// explicitly, so an IMZERO2_THEME in the runner's own environment does not
// leak into the dark launch.
func (inst ThemeE) envValue() string {
	if inst == ThemeLight {
		return "fresh"
	}
	return "dark"
}

const themeEnv = "IMZERO2_THEME"

// IsEmpty reports whether the matrix asks for nothing.
func (inst Matrix) IsEmpty() bool {
	return len(inst.Sizes) == 0 && len(inst.Themes) == 0
}

// validateLayout checks the spec's matrix and layout exceptions.
func (inst Spec) validateLayout() (err error) {
	m := inst.Matrix
	if len(m.Sizes) > 0 && inst.Size != "" {
		return eb.Build().Str("size", inst.Size).Errorf("a scene with matrix.sizes leaves `size` empty; list it in matrix.sizes")
	}
	for _, s := range m.Sizes {
		if _, _, err = (Spec{Size: s}).Dimensions(); err != nil {
			return err
		}
	}
	if len(m.Themes) > 0 {
		if _, set := inst.Env[themeEnv]; set {
			return eb.Build().Errorf("a scene with matrix.themes leaves " + themeEnv + " out of its env")
		}
	}
	for _, t := range m.Themes {
		if !slices.Contains(AllThemes, t) {
			return eb.Build().Str("theme", string(t)).Str("known", "dark, light").Errorf("unknown matrix theme")
		}
	}
	if hasDuplicates(m.Sizes) || hasDuplicates(m.Themes) {
		return eb.Build().Errorf("the matrix lists a size or a theme twice")
	}
	for _, e := range inst.LayoutExceptions {
		if err = e.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func hasDuplicates[T comparable](vs []T) bool {
	seen := make(map[T]struct{}, len(vs))
	for _, v := range vs {
		if _, dup := seen[v]; dup {
			return true
		}
		seen[v] = struct{}{}
	}
	return false
}

// Variants expands a scene with a matrix into one document per size and
// theme, sizes outermost. Each is named <name>-<WxH>-<theme> (an axis the
// matrix leaves out is left out of the name too), and so are its captures and
// the captures its layout exceptions name, so the variants' files sit side by
// side in one output directory. A scene without a matrix is returned as is.
func (inst *Doc) Variants() (docs []*Doc) {
	m := inst.Spec.Matrix
	if m.IsEmpty() {
		return []*Doc{inst}
	}
	sizes := m.Sizes
	if len(sizes) == 0 {
		sizes = []string{inst.Spec.Size}
	}
	themes := m.Themes
	if len(themes) == 0 {
		themes = []ThemeE{""}
	}
	docs = make([]*Doc, 0, len(sizes)*len(themes))
	for _, size := range sizes {
		for _, theme := range themes {
			var suffix string
			if len(m.Sizes) > 0 {
				suffix += "-" + strings.ToLower(size)
			}
			if theme != "" {
				suffix += "-" + string(theme)
			}
			docs = append(docs, inst.variant(size, theme, suffix))
		}
	}
	return docs
}

func (inst *Doc) variant(size string, theme ThemeE, suffix string) *Doc {
	d := *inst
	d.Name = inst.Name + suffix
	d.Spec.Size = size
	d.Spec.Matrix = Matrix{}
	d.Spec.Env = maps.Clone(inst.Spec.Env)
	if theme != "" {
		if d.Spec.Env == nil {
			d.Spec.Env = make(map[string]string, 1)
		}
		d.Spec.Env[themeEnv] = theme.envValue()
	}
	d.Steps = slices.Clone(inst.Steps)
	for i, st := range d.Steps {
		if st.Do == "capture" && st.Text != "" {
			d.Steps[i].Text = CaptureStem(st.Text) + suffix
		}
	}
	d.Spec.LayoutExceptions = slices.Clone(inst.Spec.LayoutExceptions)
	for i, e := range d.Spec.LayoutExceptions {
		if e.Capture != "" {
			d.Spec.LayoutExceptions[i].Capture = CaptureStem(e.Capture) + suffix
		}
	}
	return &d
}

// CaptureStem is the name a capture's files share: the step's text reduced
// the way the host reduces it, without the ".png".
func CaptureStem(text string) string {
	return strings.TrimSuffix(carrierclient.SidecarFile(text, ""), ".png")
}
