package application

import (
	"slices"
	"strings"

	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/config"
)

type ImZeroClientConfig struct {
	AppTitle                string `json:"appTitle"`
	Fullscreen              string `json:"fullscreen"`
	InitialMainWindowWidth  string `json:"initialMainWindowWidth"`
	InitialMainWindowHeight string `json:"initialMainWindowHeight"`
	AllowMainWindowResize   string `json:"allowMainWindowResize"`
	ExportBasePath          string `json:"exportBasePath"`
	Vsync                   string `json:"vsync"`
	BackgroundColorRGBA     string `json:"backgroundColorRGBA"`
	BackdropFilter          string `json:"backdropFilter"`

	nValidationMessages int
	validated           bool
}

func (inst *ImZeroClientConfig) PassthroughArgs(args []string) (argsOut []string) {
	argsOut = args
	add := func(name string, val string) {
		if val != "" {
			argsOut = append(argsOut, "-"+name, val)
		}
	}
	// general
	add("appTitle", inst.AppTitle)
	//add("fullscreen", inst.Fullscreen)
	add("initialMainWindowWidth", inst.InitialMainWindowWidth)
	add("initialMainWindowHeight", inst.InitialMainWindowHeight)
	//add("allowMainWindowResize", inst.AllowMainWindowResize)
	//add("exportBasePath", inst.ExportBasePath)
	// graphics
	add("vsync", inst.Vsync)
	//add("backgroundColorRGBA", inst.BackgroundColorRGBA)
	//add("backdropFilter", inst.BackdropFilter)
	return
}

func (inst *ImZeroClientConfig) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("appTitle"),
			Value:    inst.AppTitle,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("fullscreen"),
			Value:    inst.Fullscreen,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("initialMainWindowWidth"),
			Value:    inst.InitialMainWindowWidth,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("initialMainWindowHeight"),
			Value:    inst.InitialMainWindowHeight,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("allowMainWindowResize"),
			Value:    inst.AllowMainWindowResize,
		},
		&cli.StringFlag{
			Category: "general",
			Name:     nameTransf("exportBasePath"),
			Value:    inst.ExportBasePath,
		},
		&cli.StringFlag{
			Category: "graphics",
			Name:     nameTransf("vsync"),
			Value:    inst.Vsync,
		},
		&cli.StringFlag{
			Category: "graphics",
			Name:     nameTransf("backgroundColorRGBA"),
			Value:    inst.BackgroundColorRGBA,
		},
		&cli.StringFlag{
			Category: "graphics",
			Name:     nameTransf("backdropFilter"),
			Value:    inst.BackdropFilter,
		},
	}
}

func (inst *ImZeroClientConfig) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.AppTitle = ctx.String(nameTransf("appTitle"))
	inst.Fullscreen = ctx.String(nameTransf("fullscreen"))
	inst.InitialMainWindowWidth = ctx.String(nameTransf("initialMainWindowWidth"))
	inst.InitialMainWindowHeight = ctx.String(nameTransf("initialMainWindowHeight"))
	inst.AllowMainWindowResize = ctx.String(nameTransf("allowMainWindowResize"))
	inst.ExportBasePath = ctx.String(nameTransf("exportBasePath"))
	inst.Vsync = ctx.String(nameTransf("vsync"))
	inst.BackgroundColorRGBA = ctx.String(nameTransf("backgroundColorRGBA"))
	inst.BackdropFilter = ctx.String(nameTransf("backdropFilter"))
	return inst.Validate(true)
}

func (inst *ImZeroClientConfig) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	inst.nValidationMessages = nMessages
	inst.validated = true
	return
}

var _ config.ConfigerI = (*ImZeroClientConfig)(nil)

type FontTweakConfig struct {
	Scale         float32
	YOffsetFactor float32
	YOffset       float32
}

type Config struct {
	ImZeroSkiaClientConfig *ImZeroClientConfig

	MainFontTTF       string
	MonoFontTTF       string
	PhosphorFontTTF   string
	FallbackFontTTF   string
	MainFontTweak     FontTweakConfig
	MonoFontTweak     FontTweakConfig
	PhosphorFontTweak FontTweakConfig
	FallbackFontTweak FontTweakConfig
	ClientBinary      string
	ImZeroCmdOutFile  string
	ImZeroCmdInFile   string

	nValidationMessages  int
	MainFontSizeInPixels float32
	validated            bool
}

func (inst *Config) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return slices.Concat([]cli.Flag{
		&cli.StringFlag{
			Name:     nameTransf("mainFontTTF"),
			Value:    inst.MainFontTTF,
			Category: "fonts",
		},
		&cli.StringFlag{
			Name:     nameTransf("monoFontTTF"),
			Value:    inst.MonoFontTTF,
			Category: "fonts",
		},
		&cli.StringFlag{
			Name:     nameTransf("phosphorFontTTF"),
			Value:    inst.PhosphorFontTTF,
			Category: "fonts",
		},
		&cli.StringFlag{
			Name:     nameTransf("fallbackFontTTF"),
			Value:    inst.FallbackFontTTF,
			Category: "fonts",
		},
		&cli.Float64Flag{
			Name:     nameTransf("mainFontSizeInPixels"),
			Value:    float64(inst.MainFontSizeInPixels),
			Category: "fonts",
		},
		&cli.Float64Flag{Name: nameTransf("mainFontScale"), Value: 1.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("mainFontYOffsetFactor"), Value: 0.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("mainFontYOffset"), Value: 0.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("monoFontScale"), Value: 1.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("monoFontYOffsetFactor"), Value: 0.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("monoFontYOffset"), Value: 0.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("phosphorFontScale"), Value: 1.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("phosphorFontYOffsetFactor"), Value: 0.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("phosphorFontYOffset"), Value: 0.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("fallbackFontScale"), Value: 1.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("fallbackFontYOffsetFactor"), Value: 0.0, Category: "font tweaks"},
		&cli.Float64Flag{Name: nameTransf("fallbackFontYOffset"), Value: 0.0, Category: "font tweaks"},
		&cli.StringFlag{
			Name:     nameTransf("clientBinary"),
			Value:    inst.ClientBinary,
			Required: false,
		},
		&cli.StringFlag{
			Name:     nameTransf("imZeroCmdOutFile"),
			Value:    inst.ImZeroCmdOutFile,
			Required: false,
		},
		&cli.StringFlag{
			Name:     nameTransf("imZeroCmdInFile"),
			Value:    inst.ImZeroCmdInFile,
			Required: false,
		},
		&cli.StringFlag{
			Name:  nameTransf("clientType"),
			Value: "egui",
		},
	}, inst.ImZeroSkiaClientConfig.ToCliFlags(clientPrefixNameTransf, clientPrefixNameTransf))
}
func clientPrefixNameTransf(name string) (newName string) {
	return "client" + strings.ToUpper(string(name[0])) + name[1:]
}

func (inst *Config) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.MainFontTTF = ctx.String(nameTransf("mainFontTTF"))
	inst.MonoFontTTF = ctx.String(nameTransf("monoFontTTF"))
	inst.PhosphorFontTTF = ctx.String(nameTransf("phosphorFontTTF"))
	inst.FallbackFontTTF = ctx.String(nameTransf("fallbackFontTTF"))
	inst.MainFontSizeInPixels = float32(ctx.Float64(nameTransf("mainFontSizeInPixels")))
	inst.MainFontTweak = FontTweakConfig{
		Scale:         float32(ctx.Float64(nameTransf("mainFontScale"))),
		YOffsetFactor: float32(ctx.Float64(nameTransf("mainFontYOffsetFactor"))),
		YOffset:       float32(ctx.Float64(nameTransf("mainFontYOffset"))),
	}
	inst.MonoFontTweak = FontTweakConfig{
		Scale:         float32(ctx.Float64(nameTransf("monoFontScale"))),
		YOffsetFactor: float32(ctx.Float64(nameTransf("monoFontYOffsetFactor"))),
		YOffset:       float32(ctx.Float64(nameTransf("monoFontYOffset"))),
	}
	inst.PhosphorFontTweak = FontTweakConfig{
		Scale:         float32(ctx.Float64(nameTransf("phosphorFontScale"))),
		YOffsetFactor: float32(ctx.Float64(nameTransf("phosphorFontYOffsetFactor"))),
		YOffset:       float32(ctx.Float64(nameTransf("phosphorFontYOffset"))),
	}
	inst.FallbackFontTweak = FontTweakConfig{
		Scale:         float32(ctx.Float64(nameTransf("fallbackFontScale"))),
		YOffsetFactor: float32(ctx.Float64(nameTransf("fallbackFontYOffsetFactor"))),
		YOffset:       float32(ctx.Float64(nameTransf("fallbackFontYOffset"))),
	}
	inst.ClientBinary = ctx.String(nameTransf("clientBinary"))
	inst.ImZeroCmdInFile = ctx.String(nameTransf("imZeroCmdInFile"))
	inst.ImZeroCmdOutFile = ctx.String(nameTransf("imZeroCmdOutFile"))
	if inst.ImZeroSkiaClientConfig != nil {
		nMessages = inst.ImZeroSkiaClientConfig.FromContext(func(name string) (newName string) {
			return clientPrefixNameTransf(nameTransf(name))
		}, ctx)
	}
	nMessages += inst.Validate(true)
	return
}

func (inst *Config) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	if inst.ImZeroSkiaClientConfig != nil {
		nMessages += inst.ImZeroSkiaClientConfig.Validate(force)
	}
	inst.nValidationMessages = nMessages
	inst.validated = true
	return
}

var _ config.ConfigerI = (*Config)(nil)
