package application

import (
	cli "github.com/urfave/cli/v2"
	"slices"

	"github.com/stergiotis/boxer/public/config"
	"github.com/stoewer/go-strcase"
)

type ImZeroSkiaClientConfig struct {
	AppTitle                string `json:"appTitle"`
	Fullscreen              string `json:"fullscreen"`
	InitialMainWindowWidth  string `json:"initialMainWindowWidth"`
	InitialMainWindowHeight string `json:"initialMainWindowHeight"`
	AllowMainWindowResize   string `json:"allowMainWindowResize"`
	ExportBasePath          string `json:"exportBasePath"`
	Vsync                   string `json:"vsync"`
	BackgroundColorRGBA     string `json:"backgroundColorRGBA"`
	BackdropFilter          string `json:"backdropFilter"`
	SkiaBackendType         string `json:"backendType"`
	SketchFilter            string `json:"sketchFilter"`
	VectorCmd               string `json:"vectorCmd"`
	ImguiNavKeyboard        string `json:"imguiNavKeyboard"`
	ImguiNavGamepad         string `json:"imguiNavGamepad"`
	ImguiDocking            string `json:"imguiDocking"`
	FontDyFudge             string `json:"fontDyFudge"`
	FontManager             string `json:"fontManager"`
	FontManagerArg          string `json:"fontManagerArg"`
	TtfFilePath             string `json:"ttfFilePath"`

	nValidationMessages int
	validated           bool
}

func (inst *ImZeroSkiaClientConfig) PassthroughArgs(args []string) (argsOut []string) {
	argsOut = args
	add := func(name string, val string) {
		if val != "" {
			argsOut = append(argsOut, "-"+name, val)
		}
	}
	add("appTitle", inst.AppTitle)
	add("fullscreen", inst.Fullscreen)
	add("initialMainWindowWidth", inst.InitialMainWindowWidth)
	add("initialMainWindowHeight", inst.InitialMainWindowHeight)
	add("allowMainWindowResize", inst.AllowMainWindowResize)
	add("exportBasePath", inst.ExportBasePath)
	add("vsync", inst.Vsync)
	add("backgroundColorRGBA", inst.BackgroundColorRGBA)
	add("backdropFilter", inst.BackdropFilter)
	add("skiaBackendType", inst.SkiaBackendType)
	add("sketchFilter", inst.SketchFilter)
	add("vectorCmd", inst.VectorCmd)
	add("imguiNavKeyboard", inst.ImguiNavKeyboard)
	add("imguiNavGamepad", inst.ImguiNavGamepad)
	add("imguiDocking", inst.ImguiDocking)
	add("fontDyFudge", inst.FontDyFudge)
	add("fontManager", inst.FontManager)
	add("fontManagerArg", inst.FontManagerArg)
	add("ttfFilePath", inst.TtfFilePath)
	return
}

func (inst *ImZeroSkiaClientConfig) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:  nameTransf("appTitle"),
			Value: "",
		},
		&cli.StringFlag{
			Name: nameTransf("fullscreen"),
		},
		&cli.StringFlag{
			Name: nameTransf("initialMainWindowWidth"),
		},
		&cli.StringFlag{
			Name: nameTransf("initialMainWindowHeight"),
		},
		&cli.StringFlag{
			Name: nameTransf("allowMainWindowResize"),
		},
		&cli.StringFlag{
			Name: nameTransf("exportBasePath"),
		},
		&cli.StringFlag{
			Name: nameTransf("vsync"),
		},
		&cli.StringFlag{
			Name: nameTransf("backgroundColorRGBA"),
		},
		&cli.StringFlag{
			Name: nameTransf("backdropFilter"),
		},
		&cli.StringFlag{
			Name: nameTransf("sketchFilter"),
		},
		&cli.StringFlag{
			Name: nameTransf("skiaBackendType"),
		},
		&cli.StringFlag{
			Name: nameTransf("vectorCmd"),
		},
		&cli.StringFlag{
			Name: nameTransf("imguiNavKeyboard"),
		},
		&cli.StringFlag{
			Name: nameTransf("imguiNavGamepad"),
		},
		&cli.StringFlag{
			Name: nameTransf("imguiDocking"),
		},
		&cli.StringFlag{
			Name: nameTransf("fontDyFudge"),
		},
		&cli.StringFlag{
			Name: nameTransf("fontManager"),
		},
		&cli.StringFlag{
			Name: nameTransf("fontManagerArg"),
		},
		&cli.StringFlag{
			Name: nameTransf("ttfFilePath"),
		},
	}
}

func (inst *ImZeroSkiaClientConfig) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.AppTitle = ctx.String(nameTransf("appTitle"))
	inst.Fullscreen = ctx.String(nameTransf("fullscreen"))
	inst.InitialMainWindowWidth = ctx.String(nameTransf("initialMainWindowWidth"))
	inst.InitialMainWindowHeight = ctx.String(nameTransf("initialMainWindowHeight"))
	inst.AllowMainWindowResize = ctx.String(nameTransf("allowMainWindowResize"))
	inst.ExportBasePath = ctx.String(nameTransf("exportBasePath"))
	inst.Vsync = ctx.String(nameTransf("vsync"))
	inst.BackgroundColorRGBA = ctx.String(nameTransf("backgroundColorRGBA"))
	inst.BackdropFilter = ctx.String(nameTransf("backdropFilter"))
	inst.SketchFilter = ctx.String(nameTransf("sketchFilter"))
	inst.SkiaBackendType = ctx.String(nameTransf("skiaBackendType"))
	inst.VectorCmd = ctx.String(nameTransf("vectorCmd"))
	inst.ImguiNavKeyboard = ctx.String(nameTransf("imguiNavKeyboard"))
	inst.ImguiNavGamepad = ctx.String(nameTransf("imguiNavGamepad"))
	inst.ImguiDocking = ctx.String(nameTransf("imguiDocking"))
	inst.FontDyFudge = ctx.String(nameTransf("fontDyFudge"))
	inst.FontManager = ctx.String(nameTransf("fontManager"))
	inst.FontManagerArg = ctx.String(nameTransf("fontManagerArg"))
	inst.TtfFilePath = ctx.String(nameTransf("ttfFilePath"))
	return inst.Validate(true)
}

func (inst *ImZeroSkiaClientConfig) Validate(force bool) (nMessages int) {
	if inst.validated && !force {
		return inst.nValidationMessages
	}
	inst.nValidationMessages = nMessages
	inst.validated = true
	return
}

var _ config.Configer = (*ImZeroSkiaClientConfig)(nil)

type Config struct {
	ImZeroSkiaClientConfig *ImZeroSkiaClientConfig

	MainFontTTF   string
	ImGuiBinary   string
	MaxRelaunches int

	nValidationMessages  int
	MainFontSizeInPixels float32
	UseWasm              bool
	validated            bool
}

func (inst *Config) ToCliFlags(nameTransf config.NameTransformFunc, envVarNameTransf config.NameTransformFunc) []cli.Flag {
	return slices.Concat([]cli.Flag{
		&cli.StringFlag{
			Name:  nameTransf("mainFontTTF"),
			Value: inst.MainFontTTF,
		},
		&cli.Float64Flag{
			Name:  nameTransf("mainFontSizeInPixels"),
			Value: float64(inst.MainFontSizeInPixels),
		},
		&cli.StringFlag{
			Name:     nameTransf("imGuiBinary"),
			Value:    inst.ImGuiBinary,
			Required: false,
		},
		&cli.BoolFlag{
			Name:     nameTransf("useWasm"),
			Value:    inst.UseWasm,
			Required: false,
		},
		&cli.IntFlag{
			Name:     nameTransf("maxRelaunches"),
			Value:    inst.MaxRelaunches,
			Required: false,
		},
		&cli.StringFlag{
			Name:  nameTransf("clientType"),
			Value: "skia",
		},
	}, inst.ImZeroSkiaClientConfig.ToCliFlags(clientPrefixNameTransf, clientPrefixNameTransf))
}
func clientPrefixNameTransf(name string) (newName string) {
	return "client" + strcase.UpperCamelCase(name)
}

func (inst *Config) FromContext(nameTransf config.NameTransformFunc, ctx *cli.Context) (nMessages int) {
	inst.MainFontTTF = ctx.String(nameTransf("mainFontTTF"))
	inst.MainFontSizeInPixels = float32(ctx.Float64(nameTransf("mainFontSizeInPixels")))
	inst.ImGuiBinary = ctx.String(nameTransf("imGuiBinary"))
	inst.UseWasm = ctx.Bool(nameTransf("useWasm"))
	inst.MaxRelaunches = ctx.Int(nameTransf("maxRelaunches"))
	switch ctx.String("clientType") {
	case "skia":
		inst.ImZeroSkiaClientConfig = &ImZeroSkiaClientConfig{}
		inst.ImZeroSkiaClientConfig.FromContext(clientPrefixNameTransf, ctx)
	}
	return inst.Validate(true)
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

var _ config.Configer = (*Config)(nil)
